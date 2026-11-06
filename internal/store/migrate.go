package store

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aalvaropc/commitpool/migrations"
)

const migrateLockKey int64 = 7412530001

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	err := inLockedTx(ctx, pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `create table if not exists schema_migrations (
			version text primary key,
			applied_at timestamptz not null default now())`)
		return err
	})
	if err != nil {
		return fmt.Errorf("store: create schema_migrations: %w", err)
	}

	files, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		return fmt.Errorf("store: list migrations: %w", err)
	}
	sort.Strings(files)

	for _, file := range files {
		version := strings.TrimSuffix(file, ".sql")
		body, err := fs.ReadFile(migrations.FS, file)
		if err != nil {
			return fmt.Errorf("store: read migration %s: %w", version, err)
		}
		if err := applyMigration(ctx, pool, version, string(body)); err != nil {
			return fmt.Errorf("store: apply migration %s: %w", version, err)
		}
	}
	return nil
}

func applyMigration(ctx context.Context, pool *pgxpool.Pool, version, body string) error {
	return inLockedTx(ctx, pool, func(tx pgx.Tx) error {
		var applied bool
		err := tx.QueryRow(ctx, `select exists (select 1 from schema_migrations where version = $1)`, version).Scan(&applied)
		if err != nil || applied {
			return err
		}
		if _, err := tx.Exec(ctx, body, pgx.QueryExecModeSimpleProtocol); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `insert into schema_migrations (version) values ($1)`, version)
		return err
	})
}

func inLockedTx(ctx context.Context, pool *pgxpool.Pool, fn func(tx pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, migrateLockKey); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
