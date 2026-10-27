package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrNotFound        = errors.New("store: not found")
	ErrJobNotActive    = errors.New("store: job is not in a state that accepts this transition")
	ErrProposalNotOpen = errors.New("store: payments are not open for this proposal")
	ErrProposalExists  = errors.New("store: workshop already has a proposal")
	ErrConsentMismatch = errors.New("store: consent version mismatch")

	// ErrAttemptNotAllowed refuses a new payment attempt for an enrollment
	// that is past SEAT_RESERVED: its current attempt is already authorized,
	// being charged or finished.
	ErrAttemptNotAllowed = errors.New("store: a new payment attempt needs a SEAT_RESERVED enrollment")
)

// pgInvalidTextRepresentation is the SQLSTATE Postgres raises for a malformed UUID.
const pgInvalidTextRepresentation = "22P02"

func wrap(op string, err error) error {
	return fmt.Errorf("store: %s: %w", op, err)
}

// isMissing reports a lookup that matched nothing: no row, or an identifier
// that is not a well-formed UUID and so can never match one.
func isMissing(err error) bool {
	if errors.Is(err, pgx.ErrNoRows) {
		return true
	}
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgInvalidTextRepresentation
}

// lookupErr turns a missing row into ErrNotFound and wraps any other failure.
func lookupErr(op string, err error) error {
	if isMissing(err) {
		return ErrNotFound
	}
	return wrap(op, err)
}

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// one returns the first row of the query, or pgx.ErrNoRows.
func one[T any](ctx context.Context, q querier, sql string, args ...any) (T, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		var zero T
		return zero, err
	}
	return pgx.CollectOneRow(rows, pgx.RowToStructByPos[T])
}

// oneOrNil returns the first row of the query, or nil when nothing matches.
func oneOrNil[T any](ctx context.Context, q querier, op, sql string, args ...any) (*T, error) {
	v, err := one[T](ctx, q, sql, args...)
	if isMissing(err) {
		return nil, nil
	}
	if err != nil {
		return nil, wrap(op, err)
	}
	return &v, nil
}

// many returns every row of the query, as an empty non-nil slice when there are none.
func many[T any](ctx context.Context, q querier, op, sql string, args ...any) ([]T, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, wrap(op, err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[T])
	if err != nil {
		return nil, wrap(op, err)
	}
	if out == nil {
		out = []T{}
	}
	return out, nil
}
