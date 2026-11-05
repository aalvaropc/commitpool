// Package storetest opens the test database for tests in other packages.
package storetest

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/store"
)

const testDBLockKey int64 = 7412530002

// New returns a migrated Store on TEST_DATABASE_URL with every table
// emptied. It holds a database-wide advisory lock until the test ends so test
// packages running in parallel (go test ./...) never truncate each other's data.
//
// Without TEST_DATABASE_URL the test fails, unless COMMITPOOL_SKIP_DB_TESTS=1.
func New(t testing.TB) *store.Store {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		if os.Getenv("COMMITPOOL_SKIP_DB_TESTS") == "1" {
			t.Skip("COMMITPOOL_SKIP_DB_TESTS=1: skipping database test")
		}
		t.Fatal("TEST_DATABASE_URL is not set: run `make db-up && make test`, or set COMMITPOOL_SKIP_DB_TESTS=1 to skip database tests")
	}
	if err := checkTestDatabaseName(dsn); err != nil {
		t.Fatalf("TEST_DATABASE_URL: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	s, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}

	lock, err := s.Pool.Acquire(ctx)
	if err != nil {
		s.Close()
		t.Fatalf("acquire test connection: %v", err)
	}
	t.Cleanup(func() {
		lock.Release()
		s.Close()
	})
	if _, err := lock.Exec(ctx, `select pg_advisory_lock($1)`, testDBLockKey); err != nil {
		t.Fatalf("lock test database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = lock.Exec(context.Background(), `select pg_advisory_unlock($1)`, testDBLockKey)
	})

	if err := store.Migrate(ctx, s.Pool); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	rows, err := s.Pool.Query(ctx, `select quote_ident(tablename) from pg_tables where schemaname = 'public' and tablename <> 'schema_migrations'`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		tables = append(tables, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("list tables: %v", err)
	}
	if len(tables) > 0 {
		if _, err := s.Pool.Exec(ctx, `truncate `+strings.Join(tables, ", ")+` restart identity cascade`); err != nil {
			t.Fatalf("truncate test tables: %v", err)
		}
	}
	return s
}

// checkTestDatabaseName refuses a database whose name does not end in _test:
// New truncates every table of the database it is pointed at.
func checkTestDatabaseName(databaseURL string) error {
	u, err := url.Parse(databaseURL)
	if err != nil {
		return fmt.Errorf("not a URL: %w", err)
	}
	name := strings.TrimPrefix(path.Base(u.Path), "/")
	if name == "" || name == "." || !strings.HasSuffix(name, "_test") {
		return fmt.Errorf("database %q is not a test database: its name must end in _test, because every table in it is truncated", name)
	}
	return nil
}
