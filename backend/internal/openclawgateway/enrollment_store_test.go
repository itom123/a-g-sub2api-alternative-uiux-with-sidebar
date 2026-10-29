package openclawgateway

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func enrollmentRows(e Enrollment, b *EnrollmentBinding) *sqlmock.Rows {
	var raw any
	if b != nil {
		encoded, _ := encodeEnrollmentBinding(b)
		raw = encoded
	}
	return sqlmock.NewRows([]string{"id", "owner_id", "instance_id", "tenant_slug", "client", "state", "created_at", "expires_at", "binding"}).AddRow(e.ID, e.OwnerID, e.InstanceID, e.Tenant, e.Client, e.State, e.CreatedAt, e.ExpiresAt, raw)
}

type enrollmentDigestArg struct{}

func TestEnrollmentApprovalCandidatesUseDurableOwnerScopedRecords(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	e, b, _ := enrollmentFixture(t)
	query := `SELECT id FROM openclaw_device_enrollments WHERE owner_id = $1 AND instance_id = $2 AND (binding->>'request_id' = $4 OR (native_archived_receipt IS NULL AND (binding->>'device_id' = $3 OR binding IS NULL))) ORDER BY created_at DESC LIMIT 101`
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(e.OwnerID, e.InstanceID, b.DeviceID, b.RequestID).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(e.ID))
	ids, err := (EnrollmentStore{DB: db}).ApprovalCandidates(context.Background(), e.OwnerID, e.InstanceID, b.DeviceID, b.RequestID)
	if err != nil || len(ids) != 1 || ids[0] != e.ID {
		t.Fatal("durable lookup failed", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func (enrollmentDigestArg) Match(value driver.Value) bool {
	digest, ok := value.([]byte)
	return ok && len(digest) == 32
}

type enrollmentBindingArg struct{ binding EnrollmentBinding }

func (a enrollmentBindingArg) Match(value driver.Value) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	var stored storedEnrollmentBinding
	if json.Unmarshal([]byte(text), &stored) != nil {
		return false
	}
	return sameEnrollmentBinding(a.binding, EnrollmentBinding{RequestID: stored.RequestID, DeviceID: stored.DeviceID, PublicKey: stored.PublicKey, Roles: stored.Roles, Scopes: stored.Scopes})
}

func TestEnrollmentStorePersistsExactBindingWithAtomicAudit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	e, b, now := enrollmentFixture(t)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, owner_id.*FOR UPDATE").WithArgs(e.ID, e.OwnerID, e.InstanceID).WillReturnRows(enrollmentRows(e, nil))
	mock.ExpectExec("UPDATE openclaw_device_enrollments").WithArgs(e.ID, EnrollmentPending, enrollmentBindingArg{b}, now).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO openclaw_device_enrollment_events").WithArgs(e.ID, e.OwnerID, e.InstanceID, EnrollmentPending, now).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	actual, err := (EnrollmentStore{DB: db}).Mutate(context.Background(), e.OwnerID, e.InstanceID, e.ID, EnrollmentMutation{Action: "bind", Binding: b}, now)
	if err != nil || actual.State != EnrollmentPending || actual.Binding.PublicKey != b.PublicKey {
		t.Fatal("exact binding not persisted", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEnrollmentStoreCommitsExpiryDespiteRejectedApproval(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	e, b, now := enrollmentFixture(t)
	if err := e.Bind(b, now); err != nil {
		t.Fatal(err)
	}
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, owner_id.*FOR UPDATE").WithArgs(e.ID, e.OwnerID, e.InstanceID).WillReturnRows(enrollmentRows(e, &b))
	mock.ExpectExec("UPDATE openclaw_device_enrollments").WithArgs(e.ID, EnrollmentExpired, enrollmentBindingArg{b}, e.ExpiresAt).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO openclaw_device_enrollment_events").WithArgs(e.ID, e.OwnerID, e.InstanceID, EnrollmentExpired, e.ExpiresAt).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	actual, err := (EnrollmentStore{DB: db}).Mutate(context.Background(), e.OwnerID, e.InstanceID, e.ID, EnrollmentMutation{Action: "approve", Binding: b}, e.ExpiresAt)
	if !errors.Is(err, ErrEnrollmentTransition) || actual.State != EnrollmentExpired {
		t.Fatal("expired approval accepted or expiry lost", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEnrollmentStoreUsesNativeDeliveryTimeWithoutExtendingAdmission(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	e, b, now := enrollmentFixture(t)
	if err := e.Bind(b, now); err != nil {
		t.Fatal(err)
	}
	if err := e.Approve(e.OwnerID, e.InstanceID, b, now); err != nil {
		t.Fatal(err)
	}
	delivered := e.ExpiresAt.Add(-time.Second)
	observed := e.ExpiresAt.Add(time.Minute)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, owner_id.*FOR UPDATE").WithArgs(e.ID, e.OwnerID, e.InstanceID).WillReturnRows(enrollmentRows(e, &b))
	mock.ExpectExec("UPDATE openclaw_device_enrollments").WithArgs(e.ID, EnrollmentConnected, enrollmentBindingArg{b}, observed).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO openclaw_device_enrollment_events").WithArgs(e.ID, e.OwnerID, e.InstanceID, EnrollmentConnected, observed).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	actual, err := (EnrollmentStore{DB: db}).Mutate(context.Background(), e.OwnerID, e.InstanceID, e.ID, EnrollmentMutation{Action: "connected", Binding: b, DeliveredAt: delivered}, observed)
	if err != nil || actual.State != EnrollmentConnected {
		t.Fatal("verified delivery lost to delayed status polling", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEnrollmentStoreDoesNotPromoteUnapprovedNativeDelivery(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	e, b, now := enrollmentFixture(t)
	if err := e.Bind(b, now); err != nil {
		t.Fatal(err)
	}
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, owner_id.*FOR UPDATE").WithArgs(e.ID, e.OwnerID, e.InstanceID).WillReturnRows(enrollmentRows(e, &b))
	mock.ExpectCommit()
	if _, err := (EnrollmentStore{DB: db}).Mutate(context.Background(), e.OwnerID, e.InstanceID, e.ID, EnrollmentMutation{Action: "connected", Binding: b, DeliveredAt: now}, now.Add(time.Second)); !errors.Is(err, ErrEnrollmentTransition) {
		t.Fatal("status receipt manufactured owner approval", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEnrollmentStoreAuditFailureRollsBackBinding(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	e, b, now := enrollmentFixture(t)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, owner_id.*FOR UPDATE").WithArgs(e.ID, e.OwnerID, e.InstanceID).WillReturnRows(enrollmentRows(e, nil))
	mock.ExpectExec("UPDATE openclaw_device_enrollments").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO openclaw_device_enrollment_events").WillReturnError(errors.New("audit write failed"))
	mock.ExpectRollback()
	if _, err := (EnrollmentStore{DB: db}).Mutate(context.Background(), e.OwnerID, e.InstanceID, e.ID, EnrollmentMutation{Action: "bind", Binding: b}, now); err == nil {
		t.Fatal("binding succeeded without durable audit")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEnrollmentStoreDoesNotExposeAnotherOwnersEnrollment(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	e, _, now := enrollmentFixture(t)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, owner_id.*WHERE id = \\$1 AND owner_id = \\$2 AND instance_id = \\$3 FOR UPDATE").WithArgs(e.ID, int64(43), e.InstanceID).WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	if _, err := (EnrollmentStore{DB: db}).Mutate(context.Background(), 43, e.InstanceID, e.ID, EnrollmentMutation{Action: "status"}, now); !errors.Is(err, ErrEnrollmentTransition) {
		t.Fatal("another owner's session disclosed", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEnrollmentStoreRejectsChangedGrantWithoutMutation(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	e, b, now := enrollmentFixture(t)
	if err := e.Bind(b, now); err != nil {
		t.Fatal(err)
	}
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, owner_id.*FOR UPDATE").WithArgs(e.ID, e.OwnerID, e.InstanceID).WillReturnRows(enrollmentRows(e, &b))
	mock.ExpectCommit()
	changed := b
	changed.Scopes = []string{"operator.admin"}
	if _, err := (EnrollmentStore{DB: db}).Mutate(context.Background(), e.OwnerID, e.InstanceID, e.ID, EnrollmentMutation{Action: "approve", Binding: changed}, now); !errors.Is(err, ErrEnrollmentTransition) {
		t.Fatal("changed permissions approved", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEnrollmentStoreCreateStoresOnlyDigestAndChecksQuotaUnderOwnerLock(t *testing.T) {
	for _, quota := range []struct {
		recent, live int
		allowed      bool
	}{{0, 0, true}, {10, 0, false}, {5, 5, false}} {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		e, _, now := enrollmentFixture(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT id FROM users.*FOR UPDATE").WithArgs(e.OwnerID).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(e.OwnerID))
		mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*), COUNT(*) FILTER")).WithArgs(e.OwnerID, now).WillReturnRows(sqlmock.NewRows([]string{"recent", "live"}).AddRow(quota.recent, quota.live))
		if quota.allowed {
			mock.ExpectExec("INSERT INTO openclaw_device_enrollments").WithArgs(sqlmock.AnyArg(), e.OwnerID, e.InstanceID, e.Tenant, e.Client, enrollmentDigestArg{}, now, now.Add(5*time.Minute)).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec("INSERT INTO openclaw_device_enrollment_events").WithArgs(sqlmock.AnyArg(), e.OwnerID, e.InstanceID, EnrollmentCreated, now).WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectCommit()
		} else {
			mock.ExpectRollback()
		}
		actual, ticket, err := (EnrollmentStore{DB: db}).Create(context.Background(), e.OwnerID, e.InstanceID, e.Tenant, e.Client, now)
		if quota.allowed {
			if err != nil || actual.State != EnrollmentCreated {
				t.Fatal("enrollment not created", err)
			}
			if _, err := EnrollmentTicketDigest(ticket); err != nil {
				t.Fatal("invalid one-time returned ticket")
			}
		} else if !errors.Is(err, ErrEnrollmentRateLimit) || ticket != "" {
			t.Fatal("quota bypassed or ticket returned after failed create", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
		_ = db.Close()
	}
}
