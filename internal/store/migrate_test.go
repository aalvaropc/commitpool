package store_test

import (
	"context"
	"io/fs"
	"testing"

	"github.com/aalvaropc/commitpool/internal/store"
	"github.com/aalvaropc/commitpool/migrations"
)

func TestMigrate_AppliesOnceAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)

	files, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("embedded migrations: files=%v err=%v", files, err)
	}

	if err := store.Migrate(ctx, s.Pool); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}

	var applied int
	if err := s.Pool.QueryRow(ctx, `select count(*) from schema_migrations`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != len(files) {
		t.Errorf("schema_migrations has %d rows, want %d (one per migration file)", applied, len(files))
	}

	var initApplied bool
	if err := s.Pool.QueryRow(ctx, `select exists (select 1 from schema_migrations where version = '0001_init')`).Scan(&initApplied); err != nil {
		t.Fatal(err)
	}
	if !initApplied {
		t.Error("version 0001_init is not recorded in schema_migrations")
	}

	for _, table := range []string{
		"workshops", "candidate_dates", "proposals", "participants", "availabilities",
		"enrollments", "payment_attempts", "jobs", "provider_events",
	} {
		var exists bool
		err := s.Pool.QueryRow(ctx,
			`select exists (select 1 from information_schema.tables where table_schema = 'public' and table_name = $1)`,
			table).Scan(&exists)
		if err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Errorf("table %s does not exist after migration", table)
		}
	}

	var indexed bool
	if err := s.Pool.QueryRow(ctx,
		`select exists (select 1 from pg_indexes where schemaname = 'public' and indexname = 'one_active_enrollment_per_participant')`,
	).Scan(&indexed); err != nil {
		t.Fatal(err)
	}
	if !indexed {
		t.Error("partial unique index one_active_enrollment_per_participant is missing")
	}
}
