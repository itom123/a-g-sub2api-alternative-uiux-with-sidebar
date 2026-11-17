package service

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	_ "github.com/lib/pq"
)

// Migration 264 drops openclaw_connections.usage_count and last_used_at,
// which nothing ever wrote. Every connection query must work without them.

type connectFleetStub struct {
	OpenClawFleetClient
	states []string
}

func (f *connectFleetStub) Enabled() bool { return true }
func (f *connectFleetStub) Connect(context.Context, string, string, string) (OpenClawFleetResult, error) {
	if len(f.states) == 0 {
		return OpenClawFleetResult{}, fmt.Errorf("unexpected connect")
	}
	state := f.states[0]
	f.states = f.states[1:]
	return OpenClawFleetResult{State: state}, nil
}

const connectionTestToken = "123456:telegram-test-token-value"

// exerciseOpenClawConnections runs every connection query: the insert and
// update paths of CreateConnection, both CompleteConnection queries and
// ListConnections.
func exerciseOpenClawConnections(t *testing.T, ctx context.Context, service *OpenClawService) {
	t.Helper()
	created, err := service.CreateConnection(ctx, 42, 7, OpenClawConnectionTelegram, "Bot", connectionTestToken)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == 0 || created.Status != "pending" || created.Label != "Bot" {
		t.Fatalf("created = %+v", created)
	}
	retried, err := service.CreateConnection(ctx, 42, 7, OpenClawConnectionTelegram, "Bot 2", connectionTestToken)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if retried.ID != created.ID || retried.Status != "pending" || retried.Label != "Bot 2" {
		t.Fatalf("retried = %+v, want the existing pending row relabelled", retried)
	}
	completed, err := service.CompleteConnection(ctx, 42, 7, created.ID, "session-key")
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if completed.ID != created.ID || completed.Status != "connected" {
		t.Fatalf("completed = %+v", completed)
	}
	listed, err := service.ListConnections(ctx, 42, 7)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID || listed[0].Status != "connected" || listed[0].Label != "Bot 2" {
		t.Fatalf("listed = %+v", listed)
	}
}

func TestOpenClawConnectionQueriesNeverReadDroppedUsageColumns(t *testing.T) {
	rejectDropped := sqlmock.QueryMatcherFunc(func(expected, actual string) error {
		if strings.Contains(actual, "usage_count") || strings.Contains(actual, "last_used_at") {
			return fmt.Errorf("query reads a column dropped by migration 264: %s", actual)
		}
		return sqlmock.QueryMatcherRegexp.Match(expected, actual)
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(rejectDropped))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	instance := statusInstance()
	instance.Status = "running"
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	columns := []string{"id", "instance_id", "kind", "status", "label", "created_at", "updated_at"}
	row := func(status, label string) *sqlmock.Rows {
		return sqlmock.NewRows(columns).AddRow(int64(5), instance.ID, "telegram", status, label, now, now)
	}
	expectInstance := func() {
		mock.ExpectQuery(`FROM openclaw_instances WHERE id = \$1 AND user_id = \$2`).WithArgs(instance.ID, int64(42)).WillReturnRows(statusInstanceRows(instance))
	}
	existing := `SELECT id, instance_id, kind, status, label, created_at, updated_at\s+FROM openclaw_connections WHERE instance_id = \$1 AND kind = \$2`
	returning := `RETURNING id, instance_id, kind, status, label, created_at, updated_at`

	expectInstance()
	mock.ExpectQuery(existing).WithArgs(instance.ID, "telegram").WillReturnRows(sqlmock.NewRows(columns))
	mock.ExpectQuery(`INSERT INTO openclaw_connections \(instance_id, kind, status, label\)[\s\S]+`+returning).
		WithArgs(instance.ID, "telegram", "pending", "Bot").WillReturnRows(row("pending", "Bot"))
	expectInstance()
	mock.ExpectQuery(existing).WithArgs(instance.ID, "telegram").WillReturnRows(row("pending", "Bot"))
	mock.ExpectQuery(`UPDATE openclaw_connections SET status = \$1, label = \$2[\s\S]+`+returning).
		WithArgs("pending", "Bot 2", int64(5)).WillReturnRows(row("pending", "Bot 2"))
	expectInstance()
	mock.ExpectQuery(`SELECT c\.id, c\.instance_id, c\.kind, c\.status, c\.label, c\.created_at, c\.updated_at\s+FROM openclaw_connections c`).
		WithArgs(int64(42), int64(5), instance.ID).WillReturnRows(row("pending", "Bot 2"))
	mock.ExpectQuery(`UPDATE openclaw_connections SET status = \$1, updated_at = NOW\(\)[\s\S]+`+returning).
		WithArgs("connected", int64(5), instance.ID).WillReturnRows(row("connected", "Bot 2"))
	expectInstance()
	mock.ExpectQuery(`SELECT id, instance_id, kind, status, label, created_at, updated_at\s+FROM openclaw_connections WHERE instance_id = \$1 ORDER BY created_at ASC`).
		WithArgs(instance.ID).WillReturnRows(row("connected", "Bot 2"))

	exerciseOpenClawConnections(t, context.Background(), NewOpenClawService(db, &connectFleetStub{states: []string{"pending", "pending", "connected"}}))
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// SQL mocks only see query text. This runs the same flow against PostgreSQL
// with openclaw_connections as it stands after 246, 247 and 264.
func TestOpenClawConnectionQueriesAfterUsageColumnDropPostgres(t *testing.T) {
	dsn := os.Getenv("OPENCLAW_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("OPENCLAW_TEST_DATABASE_URL is required for the PostgreSQL regression")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	// One connection keeps the temporary tables visible to the service query;
	// pg_temp shadows any application tables of the same name.
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, err = db.ExecContext(ctx, `
		CREATE TEMP TABLE openclaw_instances (
			id bigint PRIMARY KEY, user_id bigint NOT NULL, tenant_slug text, name text, status text,
			worker_host text, port integer, last_error text,
			created_at timestamptz DEFAULT NOW(), updated_at timestamptz DEFAULT NOW(),
			deletion_requested_at timestamptz, deletion_scheduled_at timestamptz
		);
		CREATE TEMP TABLE openclaw_connections (
			id BIGSERIAL PRIMARY KEY,
			instance_id BIGINT NOT NULL REFERENCES openclaw_instances(id) ON DELETE CASCADE,
			kind VARCHAR(32) NOT NULL,
			status VARCHAR(32) NOT NULL DEFAULT 'pending',
			label VARCHAR(100) NOT NULL DEFAULT '',
			metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (instance_id, kind, label)
		);
		INSERT INTO openclaw_instances (id, user_id, tenant_slug, name, status, worker_host)
		VALUES (7, 42, 'u42-0123456789ab', 'Workspace', 'running', 'default');`)
	if err != nil {
		t.Fatal(err)
	}
	exerciseOpenClawConnections(t, ctx, NewOpenClawService(db, &connectFleetStub{states: []string{"pending", "pending", "connected"}}))
}
