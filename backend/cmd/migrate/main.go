// Command migrate runs reviewed embedded migrations without starting the gateway.
// Connection settings are supplied through lib/pq's PG* environment variables.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/migrations"
	_ "github.com/lib/pq"
)

func main() {
	apply := flag.Bool("apply", false, "Apply the exact reviewed migration list")
	expected := flag.String("expect", "", "Comma-separated filenames expected to be pending")
	flag.Parse()
	if err := run(*apply, *expected); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(apply bool, expected string) error {
	for _, key := range []string{"PGHOST", "PGDATABASE", "PGUSER"} {
		if os.Getenv(key) == "" {
			return fmt.Errorf("%s must be set explicitly", key)
		}
	}
	db, err := sql.Open("postgres", "")
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	files, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		return err
	}
	var pending []string
	for _, name := range files {
		var exists bool
		if err := db.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE filename=$1)", name).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			pending = append(pending, name)
		}
	}
	fmt.Printf("Pending migrations: %s\n", strings.Join(pending, ","))
	if !apply {
		return nil
	}
	want := strings.Split(expected, ",")
	slices.Sort(want)
	slices.Sort(pending)
	if expected == "" || !slices.Equal(pending, want) {
		return fmt.Errorf("pending migrations differ from --expect; nothing applied")
	}
	// Use the application's checksum validation, advisory lock, transactional
	// handling and concurrent-index recovery; never manufacture ledger entries.
	if err := repository.ApplyMigrations(ctx, db); err != nil {
		return err
	}
	fmt.Println("Reviewed migrations applied")
	return nil
}
