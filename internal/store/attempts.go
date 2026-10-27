package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
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
func (s *Store) CreateAttempt(ctx context.Context, enrollmentID string, amountMinor int64, currency string) (AttemptRow, error) {
	var attempt AttemptRow
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var locked string
		if err := tx.QueryRow(ctx, `select id from enrollments where id = $1 for update`, enrollmentID).Scan(&locked); err != nil {
			return lookupErr("lock enrollment", err)
		}
		row, err := one[AttemptRow](ctx, tx,
			`insert into payment_attempts (enrollment_id, attempt_no, amount_minor, currency)
			values ($1, (select coalesce(max(attempt_no), 0) + 1 from payment_attempts where enrollment_id = $1), $2, $3)
			returning `+attemptCols,
			enrollmentID, amountMinor, currency)
		if err != nil {
			return wrap("insert attempt", err)
		}
		attempt = row
		return nil
	})
	if err != nil {
		return AttemptRow{}, err
	}
	return attempt, nil
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

// UpdateAttempt writes every provider-side column of the attempt from a, so a
// nil field clears its column. The enrollment, attempt number, amount and
// currency are fixed when the attempt is created.
func (s *Store) UpdateAttempt(ctx context.Context, a AttemptRow) error {
	tag, err := s.Pool.Exec(ctx,
		`update payment_attempts set
			order_id = $2, authorization_id = $3, capture_id = $4, refund_id = $5,
			order_status = $6, authorization_status = $7, auth_expires_at = $8,
			capture_status = $9, capture_reason = $10, capture_issue = $11, capture_debug_id = $12,
			refund_status = $13, refund_reason = $14, mock_code = $15,
			updated_at = now()
		 where id = $1`,
		a.ID, a.OrderID, a.AuthorizationID, a.CaptureID, a.RefundID,
		a.OrderStatus, a.AuthorizationStatus, a.AuthExpiresAt,
		a.CaptureStatus, a.CaptureReason, a.CaptureIssue, a.CaptureDebugID,
		a.RefundStatus, a.RefundReason, a.MockCode)
	if err != nil {
		return lookupErr("update attempt", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
