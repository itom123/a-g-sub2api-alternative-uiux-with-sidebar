package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/openclawgateway"
)

type retirementBridgeFunc func(context.Context, string, openclawgateway.EnrollmentCommand) (*openclawgateway.EnrollmentBridgeResult, error)

func (f retirementBridgeFunc) GatewayEnrollment(ctx context.Context, tenant string, c openclawgateway.EnrollmentCommand) (*openclawgateway.EnrollmentBridgeResult, error) {
	return f(ctx, tenant, c)
}

func TestOpenClawEnrollmentRetirementRequiresCommittedAuditAndResumesLostAcknowledgement(t *testing.T) {
	for _, scenario := range []string{"success", "archive_audit_failure", "native_ack_lost", "resume_committed_archive"} {
		t.Run(scenario, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			created := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Millisecond)
			r := openclawgateway.EnrollmentReceipt{ID: "retirement-fixture", Client: "macos", State: openclawgateway.EnrollmentExpired, CreatedAtMS: created.UnixMilli(), ExpiresAtMS: created.Add(5 * time.Minute).UnixMilli(), RetirementReady: true}
			raw, _ := json.Marshal(r)
			var archived any
			if scenario == "resume_committed_archive" {
				archived = raw
			}
			mock.ExpectQuery("SELECT id, native_archived_receipt FROM openclaw_device_enrollments").WithArgs(int64(42), int64(7), sqlmock.AnyArg()).WillReturnRows(sqlmock.NewRows([]string{"id", "native_archived_receipt"}).AddRow(r.ID, archived))
			if archived == nil {
				mock.ExpectBegin()
				mock.ExpectQuery("SELECT id, owner_id.*native_archived_receipt.*FOR UPDATE").WithArgs(r.ID, int64(42), int64(7)).WillReturnRows(sqlmock.NewRows([]string{"id", "owner_id", "instance_id", "tenant_slug", "client", "state", "created_at", "expires_at", "binding", "native_archived_receipt"}).AddRow(r.ID, int64(42), int64(7), "u42-123456789abc", r.Client, "created", created, created.Add(5*time.Minute), nil, nil))
				mock.ExpectExec("UPDATE openclaw_device_enrollments SET state").WithArgs(r.ID, r.State, nil, string(raw), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
				audit := mock.ExpectExec("INSERT INTO openclaw_device_enrollment_events").WithArgs(r.ID, int64(42), int64(7), "native_archived", sqlmock.AnyArg())
				if scenario == "archive_audit_failure" {
					audit.WillReturnError(errors.New("fixture audit failure"))
					mock.ExpectRollback()
				} else {
					audit.WillReturnResult(sqlmock.NewResult(1, 1))
					mock.ExpectCommit()
				}
			}
			var actions []string
			bridge := retirementBridgeFunc(func(_ context.Context, tenant string, c openclawgateway.EnrollmentCommand) (*openclawgateway.EnrollmentBridgeResult, error) {
				if tenant != "u42-123456789abc" || c.ID != r.ID {
					t.Fatal("retirement changed tenant or request")
				}
				actions = append(actions, c.Action)
				switch c.Action {
				case "seal":
					return &openclawgateway.EnrollmentBridgeResult{Protocol: "ag-enrollment-v1", Enrollment: &r}, nil
				case "retire":
					if err := mock.ExpectationsWereMet(); err != nil {
						t.Fatal("native retirement preceded durable archive/audit commit", err)
					}
					if c.ArchivedReceipt == nil || !c.ArchivedReceipt.Retirable() {
						t.Fatal("retirement lacked committed proof")
					}
					if scenario == "native_ack_lost" {
						return nil, errors.New("fixture lost acknowledgement")
					}
					mock.ExpectBegin()
					mock.ExpectExec("UPDATE openclaw_device_enrollments SET native_retired_at").WithArgs(r.ID, int64(42), int64(7), sqlmock.AnyArg(), string(raw)).WillReturnResult(sqlmock.NewResult(0, 1))
					mock.ExpectExec("INSERT INTO openclaw_device_enrollment_events").WithArgs(r.ID, int64(42), int64(7), "native_retired", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
					mock.ExpectCommit()
					return &openclawgateway.EnrollmentBridgeResult{Protocol: "ag-enrollment-v1", RetiredID: r.ID}, nil
				}
				t.Fatal("unexpected retention operation")
				return nil, nil
			})
			svc := NewOpenClawService(db, nil)
			err = svc.retireGatewayEnrollmentHistory(context.Background(), 42, 7, "u42-123456789abc", bridge)
			failed := scenario == "archive_audit_failure" || scenario == "native_ack_lost"
			if (err != nil) != failed {
				t.Fatal("unexpected retirement result", err)
			}
			if scenario == "archive_audit_failure" && (len(actions) != 1 || actions[0] != "seal") {
				t.Fatal("native history changed after failed archive")
			}
			if scenario == "resume_committed_archive" && (len(actions) != 1 || actions[0] != "retire") {
				t.Fatal("resume consulted already compacted native record")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
