package service

import (
	"context"
	"database/sql"
	"os"
	"slices"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// SQL mocks cannot detect PostgreSQL's inconsistent parameter-type inference
// when one value is assigned to worker_host and compared with a hub slug.
// Temporary tables and rollback keep this test isolated from application data.
func TestOpenClawCreateInstancePostgresParameterTypes(t *testing.T) {
	dsn := os.Getenv("OPENCLAW_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("OPENCLAW_TEST_DATABASE_URL is required for the PostgreSQL regression")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `
		CREATE TEMP TABLE openclaw_hubs (id bigint PRIMARY KEY, slug varchar(64)) ON COMMIT DROP;
		CREATE TEMP TABLE openclaw_instances (
			id bigint GENERATED ALWAYS AS IDENTITY, user_id bigint,
			tenant_slug varchar(100), name varchar(100), status varchar(32),
			worker_host varchar(100), hub_id bigint, port integer, last_error text,
			created_at timestamptz DEFAULT NOW(), updated_at timestamptz DEFAULT NOW(),
			deletion_requested_at timestamptz, deletion_scheduled_at timestamptz
		) ON COMMIT DROP;
		INSERT INTO openclaw_hubs (id, slug) VALUES (7, 'default');`)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := scanOpenClawInstance(tx.QueryRowContext(ctx, createOpenClawInstanceSQL,
		int64(42), "u42-0123456789ab", "Regression workspace", "default"))
	if err != nil {
		t.Fatal(err)
	}
	if instance.Status != "provisioning" || instance.WorkerHost != "default" {
		t.Fatalf("unexpected workspace state: %#v", instance)
	}
	var hubID int64
	if err = tx.QueryRowContext(ctx, "SELECT hub_id FROM pg_temp.openclaw_instances WHERE id=$1", instance.ID).Scan(&hubID); err != nil {
		t.Fatal(err)
	}
	if hubID != 7 {
		t.Fatalf("hub ID = %d, want 7", hubID)
	}
}

// The fleet's usage column once summed openclaw_connections.usage_count, a
// counter nothing increments, so it read "0 calls" beside real spend. Request
// counts, spend, tokens and recency must all come from the billing ledger.
func TestOpenClawListAdminInstancesUsageComesFromBillingLedger(t *testing.T) {
	t.Setenv("OPENCLAW_MODEL_GATEWAY_BASE_URL", "")
	t.Setenv("OPENCLAW_MODEL_MAPPING_JSON", "")
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
		CREATE TEMP TABLE users (id bigint PRIMARY KEY, email text);
		CREATE TEMP TABLE groups (
			id bigint PRIMARY KEY, name text, rate_multiplier numeric,
			model_allowlist jsonb NOT NULL DEFAULT '{}'::jsonb, default_mapped_model varchar(100) NOT NULL DEFAULT ''
		);
		CREATE TEMP TABLE api_keys (
			id bigint PRIMARY KEY, name text DEFAULT 'key', status text, quota numeric DEFAULT 0,
			quota_used numeric DEFAULT 0, group_id bigint, last_used_at timestamptz, deleted_at timestamptz
		);
		CREATE TEMP TABLE openclaw_instances (
			id bigint PRIMARY KEY, user_id bigint, tenant_slug text, name text, status text,
			worker_host text, port integer, last_error text,
			created_at timestamptz DEFAULT NOW(), updated_at timestamptz DEFAULT NOW(),
			deletion_requested_at timestamptz, deletion_scheduled_at timestamptz,
			last_latency_ms integer, last_seen_at timestamptz
		);
		CREATE TEMP TABLE openclaw_api_keys (instance_id bigint PRIMARY KEY, api_key_id bigint);
		CREATE TEMP TABLE openclaw_connections (
			id bigint PRIMARY KEY, instance_id bigint, kind text, status text, label text DEFAULT '',
			created_at timestamptz DEFAULT NOW(), updated_at timestamptz DEFAULT NOW()
		);
		CREATE TEMP TABLE openclaw_connection_catalog (kind text PRIMARY KEY, title text, sort_order int);
		INSERT INTO openclaw_connection_catalog VALUES ('wechat', 'WeChat', 10), ('telegram', 'Telegram', 20), ('gws', 'Google Workspace', 30);
		CREATE TEMP TABLE usage_logs (
			id bigint PRIMARY KEY, api_key_id bigint, actual_cost numeric(20,10),
			input_tokens int, output_tokens int, cache_creation_tokens int, cache_read_tokens int,
			created_at timestamptz
		);
		INSERT INTO users VALUES (1, 'busy@example.test'), (2, 'idle@example.test');
		INSERT INTO groups VALUES (5, 'openai-claw', 0.3, '{"enabled":true,"models":["gpt-6.2-sol","gpt-5.6-sol"]}', 'gpt-6.2-sol');
		INSERT INTO api_keys (id, status, group_id) VALUES (10, 'active', 5), (11, 'active', 5), (99, 'active', 5);
		INSERT INTO openclaw_instances (id, user_id, tenant_slug, name, status, worker_host, created_at) VALUES
			(1, 1, 'u1-busy', 'Busy', 'running', 'default', NOW() - INTERVAL '2 days'),
			(2, 2, 'u2-idle', 'Idle', 'running', 'default', NOW() - INTERVAL '1 day');
		INSERT INTO openclaw_api_keys VALUES (1, 10), (2, 11);
		INSERT INTO openclaw_connections (id, instance_id, kind, status, label) VALUES
			(1, 1, 'telegram', 'connected', 'Support bot'), (2, 1, 'wechat', 'pending', '');
		INSERT INTO usage_logs VALUES
			(1, 10, 20.00, 1000, 200, 0, 5000, NOW() - INTERVAL '30 days'),
			(2, 10, 30.00, 2000, 300, 100, 0, NOW() - INTERVAL '20 days'),
			(3, 10, 1.9634, 400, 50, 0, 1000, NOW() - INTERVAL '1 hour'),
			(4, 99, 500.00, 9, 9, 9, 9, NOW());`)
	if err != nil {
		t.Fatal(err)
	}
	items, err := NewOpenClawService(db, nil).ListAdminInstances(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d instances, want 2", len(items))
	}
	idle, busy := items[0], items[1]
	if busy.Name != "Busy" || idle.Name != "Idle" {
		t.Fatalf("unexpected order: %q, %q", items[0].Name, items[1].Name)
	}
	if busy.RequestCount != 3 || busy.Requests7d != 1 {
		t.Fatalf("busy requests = %d (7d %d), want 3 (7d 1)", busy.RequestCount, busy.Requests7d)
	}
	if diff := busy.UsageCost - 51.9634; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("busy cost = %v, want 51.9634", busy.UsageCost)
	}
	if diff := busy.UsageCost7d - 1.9634; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("busy 7d cost = %v, want 1.9634", busy.UsageCost7d)
	}
	if busy.TokenCount != 10050 {
		t.Fatalf("busy tokens = %d, want 10050", busy.TokenCount)
	}
	if busy.LastRequestAt == nil || time.Since(*busy.LastRequestAt) > 2*time.Hour {
		t.Fatalf("busy last request = %v, want about an hour ago", busy.LastRequestAt)
	}
	if busy.ConnectionCount != 2 || busy.ConnectedCount != 1 {
		t.Fatalf("busy connections = %d/%d, want 1/2", busy.ConnectedCount, busy.ConnectionCount)
	}
	if busy.BillingGroup != "openai-claw" || busy.APIKeyStatus != "active" || busy.BillingRate != 0.3 {
		t.Fatalf("busy billing = %q/%q x%v", busy.BillingGroup, busy.APIKeyStatus, busy.BillingRate)
	}
	// Each service is listed, catalog order first, so the fleet can show which
	// set-ups are finished instead of an unexplained "configured" total.
	if len(busy.Connections) != 2 || busy.Connections[0].Kind != "wechat" || busy.Connections[0].Title != "WeChat" || busy.Connections[0].Status != "pending" ||
		busy.Connections[1].Kind != "telegram" || busy.Connections[1].Status != "connected" || busy.Connections[1].Label != "Support bot" {
		t.Fatalf("busy connections = %#v", busy.Connections)
	}
	if idle.Connections == nil || len(idle.Connections) != 0 || idle.ConnectionCount != 0 {
		t.Fatalf("idle connections = %#v", idle.Connections)
	}
	// Models are the billing group's live allowlist, not a list in code.
	if !slices.Equal(busy.AllowedModels, []string{"gpt-6.2-sol", "gpt-5.6-sol"}) || busy.DefaultModel != "gpt-6.2-sol" || busy.ModelsSource != OpenClawModelsFromGroup {
		t.Fatalf("busy models = %v default %q source %q", busy.AllowedModels, busy.DefaultModel, busy.ModelsSource)
	}
	if idle.RequestCount != 0 || idle.UsageCost != 0 || idle.TokenCount != 0 || idle.LastRequestAt != nil {
		t.Fatalf("idle usage should be empty: %#v", idle)
	}

	// The customer dashboard's latest-activity card reads the same ledger.
	service := NewOpenClawService(db, nil)
	key, err := service.loadInstanceAPIKey(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if key == nil || key.UsageCount != 3 || key.LastRequestAt == nil || !key.LastRequestAt.Equal(*busy.LastRequestAt) {
		t.Fatalf("busy key activity = %#v, want 3 requests last at %v", key, busy.LastRequestAt)
	}
	idleKey, err := service.loadInstanceAPIKey(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if idleKey == nil || idleKey.UsageCount != 0 || idleKey.LastRequestAt != nil {
		t.Fatalf("idle key activity = %#v, want no requests", idleKey)
	}
}
