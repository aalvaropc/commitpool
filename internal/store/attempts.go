package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/paypal"
)

type AttemptRow struct {
	ID, EnrollmentID                                           string
	AttemptNo                                                  int
	OrderID, AuthorizationID, CaptureID, RefundID              *string
	AmountMinor                                                int64
	Currency                                                   string
	OrderStatus, AuthorizationStatus                           *string
	AuthExpiresAt                                              *time.Time
	CaptureStatus, CaptureReason, CaptureIssue, CaptureDebugID *string
	RefundStatus, RefundReason, MockCode                       *string
}

const attemptCols = `id, enrollment_id, attempt_no, order_id, authorization_id, capture_id, refund_id, amount_minor, currency,
	order_status, authorization_status, auth_expires_at, capture_status, capture_reason, capture_issue, capture_debug_id,
	refund_status, refund_reason, mock_code`

// CreateAttempt adds the enrollment's next payment attempt, numbered from 1.
// The enrollment row is locked while numbering, so concurrent callers get
// distinct consecutive numbers.
//
// A new attempt needs a SEAT_RESERVED enrollment whose latest attempt holds
// no authorization; anything else returns ErrAttemptNotAllowed. An attempt
// that holds an authorization therefore stays the enrollment's latest for
// good, so the hold, its capture, its void and its refund always belong to
// the attempt the enrollment follows.
func (s *Store) CreateAttempt(ctx context.Context, enrollmentID string, amountMinor int64, currency string) (AttemptRow, error) {
	a, _, err := s.openAttempt(ctx, enrollmentID, amountMinor, currency, false)
	return a, err
}

// CheckoutAttempt returns the attempt a buyer's checkout should use: the
// enrollment's latest attempt while its checkout is open (checkoutOpen), so a
// second tab or a repeated click gets the same order, or else a new one
// (created true), when there is none yet, its order expired at PayPal or
// PayPal refused to create it. It refuses with ErrAttemptNotAllowed exactly
// when CreateAttempt would, and runs under the same enrollment lock, so
// concurrent checkouts share one attempt.
func (s *Store) CheckoutAttempt(ctx context.Context, enrollmentID string, amountMinor int64, currency string) (AttemptRow, bool, error) {
	return s.openAttempt(ctx, enrollmentID, amountMinor, currency, true)
}

func (s *Store) openAttempt(ctx context.Context, enrollmentID string, amountMinor int64, currency string, reuse bool) (AttemptRow, bool, error) {
	var (
		attempt AttemptRow
		created bool
	)
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var status domain.EnrollmentStatus
		if err := tx.QueryRow(ctx, `select status from enrollments where id = $1 for update`, enrollmentID).Scan(&status); err != nil {
			return lookupErr("lock enrollment", err)
		}
		if status != domain.EnrollmentSeatReserved {
			return ErrAttemptNotAllowed
		}
		// The latest attempt is read under its row lock, so an authorization
		// UpdateAttemptFunc is recording on it right now is seen once written.
		latest, err := one[AttemptRow](ctx, tx,
			`select `+attemptCols+` from payment_attempts
			 where enrollment_id = $1 order by attempt_no desc limit 1 for update`,
			enrollmentID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return wrap("lock latest attempt", err)
		case latest.AuthorizationID != nil:
			return ErrAttemptNotAllowed
		case reuse:
			open, err := checkoutOpen(ctx, tx, latest)
			if err != nil {
				return err
			}
			if open {
				attempt = latest
				return nil
			}
		}
		row, err := one[AttemptRow](ctx, tx,
			`insert into payment_attempts (enrollment_id, attempt_no, amount_minor, currency)
			values ($1, (select coalesce(max(attempt_no), 0) + 1 from payment_attempts where enrollment_id = $1), $2, $3)
			returning `+attemptCols,
			enrollmentID, amountMinor, currency)
		if err != nil {
			return wrap("insert attempt", err)
		}
		attempt, created = row, true
		return nil
	})
	if err != nil {
		return AttemptRow{}, false, err
	}
	return attempt, created, nil
}

// checkoutOpen reports whether the attempt's checkout can still be used: its
// order has not expired and PayPal has not refused to create it. A
// create_order job that failed while the attempt has no order is such a
// refusal; one that is pending, running or unknown may still produce an
// order, so the attempt is kept.
func checkoutOpen(ctx context.Context, tx pgx.Tx, a AttemptRow) (bool, error) {
	if a.OrderStatus != nil && *a.OrderStatus == paypal.StatusExpired {
		return false, nil
	}
	if a.OrderID != nil {
		return true, nil
	}
	var status string
	err := tx.QueryRow(ctx, `select status from jobs where idempotency_key = $1`, JobKey(KindCreateOrder, a.ID)).Scan(&status)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return true, nil
	case err != nil:
		return false, wrap("read create_order job", err)
	}
	return status != jobFailed, nil
}

// LatestAttempt returns the enrollment's highest-numbered attempt, or nil.
func (s *Store) LatestAttempt(ctx context.Context, enrollmentID string) (*AttemptRow, error) {
	return oneOrNil[AttemptRow](ctx, s.Pool, "latest attempt",
		`select `+attemptCols+` from payment_attempts where enrollment_id = $1 order by attempt_no desc limit 1`, enrollmentID)
}

// AttemptByID returns the attempt, or nil when there is none.
func (s *Store) AttemptByID(ctx context.Context, id string) (*AttemptRow, error) {
	return oneOrNil[AttemptRow](ctx, s.Pool, "attempt by id",
		`select `+attemptCols+` from payment_attempts where id = $1`, id)
}

// AttemptByOrderID returns the attempt that owns the PayPal order, or nil.
func (s *Store) AttemptByOrderID(ctx context.Context, orderID string) (*AttemptRow, error) {
	return oneOrNil[AttemptRow](ctx, s.Pool, "attempt by order id",
		`select `+attemptCols+` from payment_attempts where order_id = $1`, orderID)
}

// AttemptByResourceID returns the attempt that owns the PayPal order,
// authorization, capture or refund with this ID, or nil when none does.
func (s *Store) AttemptByResourceID(ctx context.Context, id string) (*AttemptRow, error) {
	if id == "" {
		return nil, nil
	}
	return oneOrNil[AttemptRow](ctx, s.Pool, "attempt by resource id",
		`select `+attemptCols+` from payment_attempts
		 where order_id = $1 or authorization_id = $1 or capture_id = $1 or refund_id = $1
		 limit 1`, id)
}

const (
	attemptSetTail = `order_status = $6, authorization_status = $7,
			capture_status = $9, capture_reason = $10, capture_issue = $11, capture_debug_id = $12,
			refund_status = $13, refund_reason = $14, mock_code = $15,
			updated_at = now()
		 where id = $1`

	// updateAttemptSQL never clears the identity columns: a nil value keeps
	// what is stored.
	updateAttemptSQL = `update payment_attempts set
			order_id = coalesce($2, order_id), authorization_id = coalesce($3, authorization_id),
			capture_id = coalesce($4, capture_id), refund_id = coalesce($5, refund_id),
			auth_expires_at = coalesce($8, auth_expires_at),
			` + attemptSetTail

	// saveAttemptSQL writes every provider-side column exactly as given.
	saveAttemptSQL = `update payment_attempts set
			order_id = $2, authorization_id = $3, capture_id = $4, refund_id = $5,
			auth_expires_at = $8,
			` + attemptSetTail
)

func attemptArgs(id string, a AttemptRow) []any {
	return []any{
		id, a.OrderID, a.AuthorizationID, a.CaptureID, a.RefundID,
		a.OrderStatus, a.AuthorizationStatus, a.AuthExpiresAt,
		a.CaptureStatus, a.CaptureReason, a.CaptureIssue, a.CaptureDebugID,
		a.RefundStatus, a.RefundReason, a.MockCode,
	}
}

// UpdateAttempt writes the provider-side columns of a onto the stored attempt.
// It is safe for a row that may be stale: the identity columns (order,
// authorization, capture and refund IDs, and auth_expires_at) are only ever
// set, a nil value leaves the stored one in place. The status, reason, issue,
// debug ID and mock columns are overwritten with what a holds, so a nil there
// clears the column. The enrollment, attempt number, amount and currency are
// fixed when the attempt is created.
//
// A caller that reads the attempt and writes it back should use
// UpdateAttemptFunc, which also stops a concurrent writer from overwriting
// the status columns with stale values.
func (s *Store) UpdateAttempt(ctx context.Context, a AttemptRow) error {
	tag, err := s.Pool.Exec(ctx, updateAttemptSQL, attemptArgs(a.ID, a)...)
	if err != nil {
		return lookupErr("update attempt", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateAttemptFunc runs a read-modify-write of one attempt in a transaction
// that holds the row lock: it loads the attempt, calls fn on it, writes every
// provider-side column back exactly as fn left it (a nil clears the column)
// and returns the saved row. Concurrent calls for the same attempt run one
// after the other, each seeing the previous one's result, so no update is
// lost. fn must not call the store, because it runs inside the transaction.
// fn cannot change the enrollment, attempt number, amount or currency.
//
// It returns ErrNotFound when the attempt does not exist, without calling fn.
// An error from fn rolls the transaction back and is returned unchanged.
func (s *Store) UpdateAttemptFunc(ctx context.Context, id string, fn func(a *AttemptRow) error) (AttemptRow, error) {
	var saved AttemptRow
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		a, err := one[AttemptRow](ctx, tx, `select `+attemptCols+` from payment_attempts where id = $1 for update`, id)
		if err != nil {
			return lookupErr("lock attempt", err)
		}
		if err := fn(&a); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, saveAttemptSQL, attemptArgs(id, a)...); err != nil {
			return wrap("save attempt", err)
		}
		saved, err = one[AttemptRow](ctx, tx, `select `+attemptCols+` from payment_attempts where id = $1`, id)
		if err != nil {
			return wrap("reload attempt", err)
		}
		return nil
	})
	if err != nil {
		return AttemptRow{}, err
	}
	return saved, nil
}
