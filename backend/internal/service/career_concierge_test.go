package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestCareerScheduledProfileRejectsOtherCustomerAndDisabledJob(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		rows := sqlmock.NewRows([]string{"enabled"})
		if disabled {
			rows.AddRow(false)
		}
		mock.ExpectQuery(`SELECT enabled FROM career_concierge_schedules WHERE id=\$1 AND user_id=\$2`).WithArgs(int64(7), int64(42)).WillReturnRows(rows)
		_, err = NewOpenClawService(db, nil).GetCareerInternalProfile(context.Background(), 42, 7)
		if !errors.Is(err, ErrCareerScheduleNotFound) {
			t.Fatalf("expected inaccessible schedule, got %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
		_ = db.Close()
	}
}

func TestCareerPairingNormalizesCodeAndConsumesOnlyUnexpiredHash(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	hash := sha256.Sum256([]byte("ABCDEF123456"))
	mock.ExpectExec(`UPDATE career_concierge_profiles[\s\S]*pairing_hash=NULL,[\s\S]*WHERE pairing_hash=\$2 AND pairing_expires_at > NOW\(\)`).WithArgs("12345", hex.EncodeToString(hash[:])).WillReturnResult(sqlmock.NewResult(0, 1))
	s := NewOpenClawService(db, nil)
	if err := s.PairCareerTelegram(context.Background(), " abcdef123456 ", "12345"); err != nil {
		t.Fatal(err)
	}
	if err := s.PairCareerTelegram(context.Background(), "ABCDEF123456", "-10012345"); !errors.Is(err, ErrCareerPairingInvalid) {
		t.Fatal("group pairing accepted")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCareerDeleteDisablesBeforeGatewayCleanup(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	t.Setenv("OPENCLAW_CONCIERGE_ENABLED", "1")
	t.Setenv("OPENCLAW_BIN", "/does-not-exist/openclaw")
	mock.ExpectQuery(`SELECT COALESCE\(openclaw_job_id,''\) FROM career_concierge_schedules WHERE id=\$1 AND user_id=\$2`).WithArgs(int64(7), int64(42)).WillReturnRows(sqlmock.NewRows([]string{"job_id"}).AddRow("job7"))
	mock.ExpectExec(`UPDATE career_concierge_schedules SET enabled=FALSE,updated_at=NOW\(\) WHERE id=\$1 AND user_id=\$2`).WithArgs(int64(7), int64(42)).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := NewOpenClawService(db, nil).DeleteCareerSchedule(context.Background(), 42, 7); err == nil {
		t.Fatal("expected cleanup error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCareerScheduleReportsDisabledWhenSchedulerIsOff(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	t.Setenv("OPENCLAW_CONCIERGE_ENABLED", "0")
	now := time.Now()
	mock.ExpectQuery(`FROM career_concierge_profiles WHERE user_id = \$1`).WithArgs(int64(42)).WillReturnRows(sqlmock.NewRows([]string{"user_id", "display_name", "roles", "locations", "remote_ok", "salary_min", "notes", "timezone", "telegram_chat_id", "paired", "paired_at", "updated_at"}).AddRow(42, "Customer", "developer", "London", true, 0, "", "Europe/London", "12345", true, now, now))
	mock.ExpectQuery(`INSERT INTO career_concierge_schedules`).WithArgs(int64(42), "0 9 * * 1-5", "Europe/London", true).WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "cron", "timezone", "enabled", "job_id", "last_error", "created_at", "updated_at"}).AddRow(7, 42, "0 9 * * 1-5", "Europe/London", true, "", "", now, now))
	mock.ExpectExec(`UPDATE career_concierge_schedules SET enabled=FALSE,last_error=\$1,updated_at=NOW\(\) WHERE id=\$2`).WithArgs("career concierge scheduling is disabled", int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	schedule, err := NewOpenClawService(db, nil).CreateCareerSchedule(context.Background(), 42, CareerScheduleInput{CronExpression: "0 9 * * 1-5", Timezone: "Europe/London"})
	if err != nil {
		t.Fatal(err)
	}
	if schedule.Enabled || schedule.LastError == "" {
		t.Fatal("unscheduled job was reported as enabled")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
