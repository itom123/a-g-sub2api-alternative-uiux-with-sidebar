//go:build integration

package openclawgateway

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"
)

func TestEnrollmentPostgresRetirementArchivesBeforeAcknowledgement(t *testing.T) {
	db := enrollmentPostgres(t)
	store := EnrollmentStore{DB: db}
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	e, _, err := store.Create(ctx, 42, 7, "u42-123456789abc", "ios", now.Add(-2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	_, b, _ := enrollmentFixture(t)
	b.EnrollmentID = e.ID
	transport := b.Transport()
	r := EnrollmentReceipt{ID: e.ID, Client: e.Client, State: EnrollmentExpired, CreatedAtMS: e.CreatedAt.UnixMilli(), ExpiresAtMS: e.ExpiresAt.UnixMilli(), Binding: &transport, RetirementReady: true}
	if _, err := store.ArchiveNativeReceipt(ctx, 43, 8, r, now); err == nil {
		t.Fatal("cross-owner archive accepted")
	}
	if _, err := store.ArchiveNativeReceipt(ctx, 42, 7, r, e.ExpiresAt); err == nil {
		t.Fatal("immature receipt accepted")
	}
	if _, err := store.ArchiveNativeReceipt(ctx, 42, 7, r, now); err != nil {
		t.Fatal(err)
	}
	// Simulate controller loss after durable archive, before native cleanup.
	batch, err := store.RetirementBatch(ctx, 42, 7, now)
	if err != nil || len(batch) != 1 || batch[0].Archived == nil {
		t.Fatal("archive was not restart-safe", err)
	}
	if batch[0].Archived.Binding.PublicKey != b.PublicKey {
		t.Fatal("late native binding was lost")
	}
	if err := store.ConfirmNativeRetirement(ctx, 42, 7, *batch[0].Archived, now); err != nil {
		t.Fatal(err)
	}
	if err := store.ConfirmNativeRetirement(ctx, 42, 7, *batch[0].Archived, now); err != nil {
		t.Fatal("lost acknowledgement was not idempotent", err)
	}
	if _, err := store.ArchiveNativeReceipt(ctx, 42, 7, r, now); err != nil {
		t.Fatal("archive retry failed", err)
	}
	batch, err = store.RetirementBatch(ctx, 42, 7, now)
	if err != nil || len(batch) != 0 {
		t.Fatal("confirmed retirement remained due", err)
	}
	ids, err := store.ApprovalCandidates(ctx, 42, 7, b.DeviceID, b.RequestID)
	if err != nil || len(ids) != 1 || ids[0] != e.ID {
		t.Fatal("retired exact request lost approval provenance", err)
	}
	ids, err = store.ApprovalCandidates(ctx, 42, 7, b.DeviceID, "another-native-request")
	if err != nil || len(ids) != 0 {
		t.Fatal("retired history blocked unrelated native request", err)
	}
	var events int
	if err := db.QueryRow("SELECT COUNT(*) FROM openclaw_device_enrollment_events WHERE state IN ('native_archived','native_retired')").Scan(&events); err != nil || events != 2 {
		t.Fatal("retirement retries duplicated audit history", err)
	}
}

func TestEnrollmentPostgresRetirementAuditFailuresRetainProvenance(t *testing.T) {
	db := enrollmentPostgres(t)
	store := EnrollmentStore{DB: db}
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	e, _, err := store.Create(ctx, 42, 7, "u42-123456789abc", "ios", now.Add(-2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	r := EnrollmentReceipt{ID: e.ID, Client: e.Client, State: EnrollmentExpired, CreatedAtMS: e.CreatedAt.UnixMilli(), ExpiresAtMS: e.ExpiresAt.UnixMilli(), RetirementReady: true}
	if _, err := db.Exec(`CREATE FUNCTION refuse_retirement_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture audit unavailable'; END $$;
CREATE TRIGGER refuse_audit BEFORE INSERT ON openclaw_device_enrollment_events FOR EACH ROW EXECUTE FUNCTION refuse_retirement_audit();`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ArchiveNativeReceipt(ctx, 42, 7, r, now); err == nil {
		t.Fatal("failed archive audit was ignored")
	}
	var archived, retired bool
	if err := db.QueryRow("SELECT native_archived_receipt IS NOT NULL,native_retired_at IS NOT NULL FROM openclaw_device_enrollments WHERE id=$1", e.ID).Scan(&archived, &retired); err != nil || archived || retired {
		t.Fatal("failed archive committed markers", err)
	}
	if _, err := db.Exec("ALTER TABLE openclaw_device_enrollment_events DISABLE TRIGGER refuse_audit"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ArchiveNativeReceipt(ctx, 42, 7, r, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("ALTER TABLE openclaw_device_enrollment_events ENABLE TRIGGER refuse_audit"); err != nil {
		t.Fatal(err)
	}
	if err := store.ConfirmNativeRetirement(ctx, 42, 7, r, now); err == nil {
		t.Fatal("failed retirement audit was ignored")
	}
	batch, err := store.RetirementBatch(ctx, 42, 7, now)
	if err != nil || len(batch) != 1 || batch[0].Archived == nil {
		t.Fatal("native-cleanup acknowledgement cannot be retried", err)
	}
	// Native may already have removed its record; the durable controller
	// archive still proves absence, so legacy approval can safely bypass it.
	_, b, _ := enrollmentFixture(t)
	ids, err := store.ApprovalCandidates(ctx, 42, 7, b.DeviceID, b.RequestID)
	if err != nil || len(ids) != 0 {
		t.Fatal("archived absent binding still required missing native state", err)
	}
}

func TestEnrollmentPostgresRetirementMoreThanCandidateLimit(t *testing.T) {
	db := enrollmentPostgres(t)
	store := EnrollmentStore{DB: db}
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	created := now.Add(-2 * time.Hour)
	for i := range 120 {
		id := fmt.Sprintf("retired-history-%08d", i)
		digest := sha256.Sum256([]byte(id))
		if _, err := db.Exec(`INSERT INTO openclaw_device_enrollments(id,owner_id,instance_id,tenant_slug,client,state,ticket_digest,created_at,expires_at,updated_at) VALUES($1,42,7,'u42-123456789abc','ios','created',$2,$3,$4,$3)`, id, digest[:], created, created.Add(EnrollmentTTL)); err != nil {
			t.Fatal(err)
		}
	}
	count := 0
	for {
		batch, err := store.RetirementBatch(ctx, 42, 7, now)
		if err != nil {
			t.Fatal(err)
		}
		if len(batch) == 0 {
			break
		}
		if len(batch) > 50 {
			t.Fatal("unbounded batch")
		}
		for _, c := range batch {
			r := EnrollmentReceipt{ID: c.ID, Client: "ios", State: EnrollmentExpired, CreatedAtMS: created.UnixMilli(), ExpiresAtMS: created.Add(EnrollmentTTL).UnixMilli(), RetirementReady: true}
			if _, err := store.ArchiveNativeReceipt(ctx, 42, 7, r, now); err != nil {
				t.Fatal(err)
			}
			if err := store.ConfirmNativeRetirement(ctx, 42, 7, r, now); err != nil {
				t.Fatal(err)
			}
			count++
		}
	}
	_, b, _ := enrollmentFixture(t)
	ids, err := store.ApprovalCandidates(ctx, 42, 7, b.DeviceID, b.RequestID)
	if err != nil || len(ids) != 0 || count != 120 {
		t.Fatal("compacted history exhausted approval lookup", err)
	}
	var rows int
	if err := db.QueryRow("SELECT COUNT(*) FROM openclaw_device_enrollments").Scan(&rows); err != nil || rows != 120 {
		t.Fatal("historical controller provenance was deleted", err)
	}
}
