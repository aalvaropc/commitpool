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
	ErrConflict        = errors.New("store: the change conflicts with the current state")

	ErrAttemptNotAllowed = errors.New("store: a new payment attempt needs a SEAT_RESERVED enrollment")
	ErrPaidOut           = errors.New("store: the payment was paid out to the organizer")
)

const pgInvalidTextRepresentation = "22P02"

func wrap(op string, err error) error {
	return fmt.Errorf("store: %s: %w", op, err)
}

func isMissing(err error) bool {
	if errors.Is(err, pgx.ErrNoRows) {
		return true
	}
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgInvalidTextRepresentation
}

func lookupErr(op string, err error) error {
	if isMissing(err) {
		return ErrNotFound
	}
	return wrap(op, err)
}

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func one[T any](ctx context.Context, q querier, sql string, args ...any) (T, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		var zero T
		return zero, err
	}
	return pgx.CollectOneRow(rows, pgx.RowToStructByPos[T])
}

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
