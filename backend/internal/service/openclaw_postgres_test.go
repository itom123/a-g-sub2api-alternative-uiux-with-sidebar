package service

import (
	"context"
	"database/sql"
	"os"
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
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
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
