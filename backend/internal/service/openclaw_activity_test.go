package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	_ "github.com/lib/pq"
)

// The tenant dashboard once summed openclaw_connections.usage_count and
// last_used_at, columns nothing writes because the control plane never sees
// channel messages. Workspace activity now comes from the billing ledger.
func TestOpenClawWorkspaceKeyReportsLastBilledRequest(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	touched := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	billed := time.Date(2026, 11, 17, 8, 59, 30, 0, time.UTC)
	columns := []string{"id", "name", "status", "quota", "quota_used", "group_id", "last_used_at", "count", "cost", "last_request"}
	query := `COUNT\(u\.id\), COALESCE\(SUM\(u\.actual_cost\), 0\), MAX\(u\.created_at\)\s+FROM openclaw_api_keys link\s+JOIN api_keys k ON k\.id = link\.api_key_id AND k\.deleted_at IS NULL\s+LEFT JOIN usage_logs u ON u\.api_key_id = k\.id\s+WHERE link\.instance_id = \$1`
	mock.ExpectQuery(query).WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows(columns).AddRow(int64(99), "OpenClaw · Workspace", "active", 0.0, 0.0, int64(5), touched, int64(3), 1.25, billed))
	mock.ExpectQuery(query).WithArgs(int64(8)).
		WillReturnRows(sqlmock.NewRows(columns).AddRow(int64(100), "OpenClaw · Idle", "active", 0.0, 0.0, int64(5), nil, int64(0), 0.0, nil))
	service := NewOpenClawService(db, nil)

	key, err := service.loadInstanceAPIKey(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if key.LastRequestAt == nil || !key.LastRequestAt.Equal(billed) {
		t.Fatalf("last request = %v, want the newest usage_logs row %v", key.LastRequestAt, billed)
	}
	if key.LastUsedAt == nil || !key.LastUsedAt.Equal(touched) || key.UsageCount != 3 {
		t.Fatalf("key fields changed: %+v", key)
	}
	idle, err := service.loadInstanceAPIKey(context.Background(), 8)
	if err != nil {
		t.Fatal(err)
	}
	if idle.LastRequestAt != nil || idle.UsageCount != 0 {
		t.Fatalf("idle key should have no billed request: %+v", idle)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	// The customer view reads api_key.last_request_at; the never-written
	// connection aggregates must not reappear in the dashboard payload.
	payload, err := json.Marshal(OpenClawDashboard{APIKey: key})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, stale := range []string{"usage_count", "last_used_at"} {
		if _, ok := decoded[stale]; ok {
			t.Fatalf("dashboard still reports connection %s: %s", stale, payload)
		}
	}
	var apiKey map[string]json.RawMessage
	if err := json.Unmarshal(decoded["api_key"], &apiKey); err != nil {
		t.Fatal(err)
	}
	if string(apiKey["last_request_at"]) != `"2026-11-17T08:59:30Z"` {
		t.Fatalf("api_key.last_request_at = %s", apiKey["last_request_at"])
	}
}

// SQL mocks cannot prove that MAX(created_at) is limited to the workspace's
// own key, so this regression runs the query against PostgreSQL.
func TestOpenClawWorkspaceKeyLastRequestPostgres(t *testing.T) {
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
		CREATE TEMP TABLE api_keys (
			id bigint PRIMARY KEY, name text, status text, quota numeric(20,8) DEFAULT 0,
			quota_used numeric(20,8) DEFAULT 0, group_id bigint, last_used_at timestamptz, deleted_at timestamptz
		);
		CREATE TEMP TABLE openclaw_api_keys (instance_id bigint PRIMARY KEY, api_key_id bigint);
		CREATE TEMP TABLE usage_logs (id bigint PRIMARY KEY, api_key_id bigint, actual_cost numeric(20,10), created_at timestamptz);
		INSERT INTO api_keys (id, name, status, group_id, last_used_at) VALUES
			(10, 'OpenClaw · Busy', 'active', 5, NOW() - INTERVAL '1 minute'),
			(11, 'OpenClaw · Idle', 'active', 5, NULL),
			(99, 'Unrelated', 'active', 5, NOW());
		INSERT INTO openclaw_api_keys VALUES (1, 10), (2, 11);
		INSERT INTO usage_logs VALUES
			(1, 10, 2.00, NOW() - INTERVAL '3 days'),
			(2, 10, 0.50, NOW() - INTERVAL '2 hours'),
			(3, 99, 9.00, NOW());`)
	if err != nil {
		t.Fatal(err)
	}
	service := NewOpenClawService(db, nil)
	busy, err := service.loadInstanceAPIKey(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if busy == nil || busy.UsageCount != 2 || busy.LastRequestAt == nil {
		t.Fatalf("busy key = %+v", busy)
	}
	if age := time.Since(*busy.LastRequestAt); age < 90*time.Minute || age > 3*time.Hour {
		t.Fatalf("busy last request is %v old, want its own newest log (about 2h), not another key's", age)
	}
	idle, err := service.loadInstanceAPIKey(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if idle == nil || idle.UsageCount != 0 || idle.LastRequestAt != nil {
		t.Fatalf("idle key = %+v", idle)
	}
}
