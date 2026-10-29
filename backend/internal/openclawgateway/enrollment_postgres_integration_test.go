//go:build integration

package openclawgateway

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lib/pq"
)

// Run only against a disposable database named ag_enrollment_test. The runner
// mounts the real additive migration; each test gets its own private schema.
func enrollmentPostgres(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("OPENCLAW_ENROLLMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("disposable PostgreSQL DSN not supplied")
	}
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		var err error
		dsn, err = pq.ParseURL(dsn)
		if err != nil {
			t.Fatal("invalid disposable PostgreSQL DSN")
		}
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	var name string
	if err := admin.QueryRow("SELECT current_database()").Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "ag_enrollment_test" {
		t.Fatal("refusing non-disposable database")
	}
	schema := fmt.Sprintf("enrollment_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE SCHEMA " + pq.QuoteIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP SCHEMA " + pq.QuoteIdentifier(schema) + " CASCADE"); err != nil {
			t.Error(err)
		}
	})
	db, err := sql.Open("postgres", dsn+" search_path="+schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(20)
	_, err = db.Exec(`CREATE TABLE users (id BIGINT PRIMARY KEY, deleted_at TIMESTAMPTZ, status TEXT NOT NULL);
CREATE TABLE openclaw_instances (id BIGINT PRIMARY KEY, user_id BIGINT NOT NULL REFERENCES users(id), tenant_slug TEXT NOT NULL, status TEXT NOT NULL, deletion_scheduled_at TIMESTAMPTZ);
INSERT INTO users VALUES (42, NULL, 'active'), (43, NULL, 'active');
INSERT INTO openclaw_instances VALUES (7, 42, 'u42-123456789abc', 'ready', NULL), (8, 43, 'u43-123456789abc', 'ready', NULL);`)
	if err != nil {
		t.Fatal(err)
	}
	initial := os.Getenv("OPENCLAW_ENROLLMENT_TEST_MIGRATION")
	retirement := os.Getenv("OPENCLAW_ENROLLMENT_RETIREMENT_MIGRATION")
	if retirement == "" {
		retirement = filepath.Join(filepath.Dir(initial), "261_openclaw_enrollment_retirement.sql")
	}
	for range 2 {
		for _, file := range []string{initial, retirement} {
			migration, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(string(migration)); err != nil {
				t.Fatal(err)
			}
		}
	}
	return db
}

func TestEnrollmentPostgresConcurrentCreationQuota(t *testing.T) {
	db := enrollmentPostgres(t)
	store := EnrollmentStore{DB: db}
	now := time.Now().UTC().Truncate(time.Millisecond)
	start := make(chan struct{})
	errorsCh := make(chan error, 16)
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _, err := store.Create(context.Background(), 42, 7, "u42-123456789abc", "ios", now)
			errorsCh <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errorsCh)
	created, limited := 0, 0
	for err := range errorsCh {
		switch {
		case err == nil:
			created++
		case errors.Is(err, ErrEnrollmentRateLimit):
			limited++
		default:
			t.Fatal(err)
		}
	}
	if created != 5 || limited != 11 {
		t.Fatalf("quota race: created=%d limited=%d", created, limited)
	}
	var rows, events int
	if err := db.QueryRow("SELECT COUNT(*) FROM openclaw_device_enrollments").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM openclaw_device_enrollment_events").Scan(&events); err != nil {
		t.Fatal(err)
	}
	if rows != 5 || events != 5 {
		t.Fatal("creation/audit are not atomic")
	}
	if _, _, err := store.Create(context.Background(), 43, 8, "u43-123456789abc", "ios", now); err != nil {
		t.Fatal("unrelated owner blocked", err)
	}
}

func TestEnrollmentPostgresConcurrentDecisionsAndDelayedReceipt(t *testing.T) {
	db := enrollmentPostgres(t)
	store := EnrollmentStore{DB: db}
	_, binding, now := enrollmentFixture(t)
	e, _, err := store.Create(context.Background(), 42, 7, "u42-123456789abc", "ios", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Mutate(context.Background(), 43, 8, e.ID, EnrollmentMutation{Action: "bind", Binding: binding}, now); err == nil {
		t.Fatal("cross-tenant mutation accepted")
	}
	if _, err := store.Mutate(context.Background(), 42, 7, e.ID, EnrollmentMutation{Action: "bind", Binding: binding}, now); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, action := range []string{"approve", "cancel"} {
		go func(action string) {
			<-start
			_, err := store.Mutate(context.Background(), 42, 7, e.ID, EnrollmentMutation{Action: action, Binding: binding}, now.Add(time.Second))
			results <- err
		}(action)
	}
	close(start)
	successes := 0
	for range 2 {
		if err := <-results; err == nil {
			successes++
		} else if !errors.Is(err, ErrEnrollmentTransition) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatal("competing owner decisions were both committed")
	}
	current, err := store.Mutate(context.Background(), 42, 7, e.ID, EnrollmentMutation{Action: "inspect"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if current.State == EnrollmentCancelled {
		if _, err := store.Mutate(context.Background(), 42, 7, e.ID, EnrollmentMutation{Action: "connected", Binding: binding, DeliveredAt: now.Add(2 * time.Second)}, now.Add(10*time.Minute)); err == nil {
			t.Fatal("cancelled enrollment connected")
		}
	} else if current.State == EnrollmentApproved {
		connected, err := store.Mutate(context.Background(), 42, 7, e.ID, EnrollmentMutation{Action: "connected", Binding: binding, DeliveredAt: now.Add(2 * time.Second)}, now.Add(10*time.Minute))
		if err != nil || connected.State != EnrollmentConnected {
			t.Fatal("delayed native receipt lost", err)
		}
	} else {
		t.Fatal("unexpected decision state")
	}
	var rows, events int
	if err := db.QueryRow("SELECT COUNT(*) FROM openclaw_device_enrollments").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM openclaw_device_enrollment_events").Scan(&events); err != nil {
		t.Fatal(err)
	}
	expected := 3
	if current.State == EnrollmentApproved {
		expected++
	}
	if rows != 1 || events != expected {
		t.Fatal("decision/audit mismatch")
	}
}

func TestEnrollmentPostgresDelayedReceiptAndAuditRollback(t *testing.T) {
	db := enrollmentPostgres(t)
	store := EnrollmentStore{DB: db}
	_, binding, now := enrollmentFixture(t)
	e, _, err := store.Create(context.Background(), 42, 7, "u42-123456789abc", "ios", now)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"bind", "approve"} {
		if _, err := store.Mutate(context.Background(), 42, 7, e.ID, EnrollmentMutation{Action: action, Binding: binding}, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`CREATE FUNCTION refuse_enrollment_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture audit unavailable'; END $$;
CREATE TRIGGER refuse_audit BEFORE INSERT ON openclaw_device_enrollment_events FOR EACH ROW EXECUTE FUNCTION refuse_enrollment_audit();`); err != nil {
		t.Fatal(err)
	}
	mutation := EnrollmentMutation{Action: "connected", Binding: binding, DeliveredAt: now.Add(time.Second)}
	if _, err := store.Mutate(context.Background(), 42, 7, e.ID, mutation, now.Add(10*time.Minute)); err == nil {
		t.Fatal("audit failure was ignored")
	}
	current, err := store.Mutate(context.Background(), 42, 7, e.ID, EnrollmentMutation{Action: "inspect"}, now)
	if err != nil || current.State != EnrollmentApproved {
		t.Fatal("failed audit committed connection", err)
	}
	if _, err := db.Exec("DROP TRIGGER refuse_audit ON openclaw_device_enrollment_events"); err != nil {
		t.Fatal(err)
	}
	connected, err := store.Mutate(context.Background(), 42, 7, e.ID, mutation, now.Add(10*time.Minute))
	if err != nil || connected.State != EnrollmentConnected {
		t.Fatal("valid delayed receipt rejected", err)
	}
	var events int
	if err := db.QueryRow("SELECT COUNT(*) FROM openclaw_device_enrollment_events").Scan(&events); err != nil || events != 4 {
		t.Fatal("rolled-back audit left inconsistent history", err)
	}
}
