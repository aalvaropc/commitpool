package store

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/aalvaropc/commitpool/internal/domain"
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
	FailedRefundIDs                                            []string
}

const attemptCols = `id, enrollment_id, attempt_no, order_id, authorization_id, capture_id, refund_id, amount_minor, currency,
	order_status, authorization_status, auth_expires_at, capture_status, capture_reason, capture_issue, capture_debug_id,
	refund_status, refund_reason, mock_code, failed_refund_ids`

func (s *Store) CreateAttempt(ctx context.Context, enrollmentID string, amountMinor int64, currency string) (AttemptRow, error) {
	a, _, err := s.openAttempt(ctx, enrollmentID, amountMinor, currency, false)
	return a, err
}

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

const (
	OrderExpired         = "EXPIRED"
	AuthorizationPending = "PENDING"
)

func checkoutOpen(ctx context.Context, tx pgx.Tx, a AttemptRow) (bool, error) {
	if a.OrderStatus != nil && *a.OrderStatus == OrderExpired {
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
	return status != JobFailed, nil
}

func (s *Store) LatestAttempt(ctx context.Context, enrollmentID string) (*AttemptRow, error) {
	return oneOrNil[AttemptRow](ctx, s.Pool, "latest attempt",
		`select `+attemptCols+` from payment_attempts where enrollment_id = $1 order by attempt_no desc limit 1`, enrollmentID)
}

func (s *Store) LatestAttemptsByWorkshop(ctx context.Context, workshopID string) (map[string]*AttemptRow, error) {
	rows, err := many[AttemptRow](ctx, s.Pool, "latest attempts by workshop",
		`select distinct on (enrollment_id) `+attemptCols+` from payment_attempts
		 where enrollment_id in (select id from enrollments where workshop_id = $1)
		 order by enrollment_id, attempt_no desc`, workshopID)
	if err != nil {
		return nil, err
	}
	latest := make(map[string]*AttemptRow, len(rows))
	for i := range rows {
		latest[rows[i].EnrollmentID] = &rows[i]
	}
	return latest, nil
}

type AttemptRef struct {
	AttemptID, WorkshopID string
}

func (s *Store) LatestAttemptsInStatus(ctx context.Context, statuses ...domain.EnrollmentStatus) ([]AttemptRef, error) {
	return many[AttemptRef](ctx, s.Pool, "latest attempts in status",
		`select id, workshop_id from (
		     select distinct on (a.enrollment_id) a.id::text as id, e.workshop_id::text as workshop_id
		     from payment_attempts a join enrollments e on e.id = a.enrollment_id
		     where e.status = any($1)
		     order by a.enrollment_id, a.attempt_no desc) latest
		 order by id`, texts(statuses))
}

func (s *Store) LatestAttemptsWithPendingAuthorization(ctx context.Context) ([]AttemptRef, error) {
	return many[AttemptRef](ctx, s.Pool, "latest attempts with a pending authorization",
		`select id, workshop_id from (
		     select distinct on (a.enrollment_id) a.id::text as id, e.workshop_id::text as workshop_id, a.authorization_status
		     from payment_attempts a join enrollments e on e.id = a.enrollment_id
		     where e.status = $1
		     order by a.enrollment_id, a.attempt_no desc) latest
		 where authorization_status = $2
		 order by id`, string(domain.EnrollmentSeatReserved), AuthorizationPending)
}

func (s *Store) AttemptByID(ctx context.Context, id string) (*AttemptRow, error) {
	return oneOrNil[AttemptRow](ctx, s.Pool, "attempt by id",
		`select `+attemptCols+` from payment_attempts where id = $1`, id)
}

func (s *Store) AttemptByOrderID(ctx context.Context, orderID string) (*AttemptRow, error) {
	return oneOrNil[AttemptRow](ctx, s.Pool, "attempt by order id",
		`select `+attemptCols+` from payment_attempts where order_id = $1`, orderID)
}

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

	updateAttemptSQL = `update payment_attempts set
			order_id = coalesce($2, order_id), authorization_id = coalesce($3, authorization_id),
			capture_id = coalesce($4, capture_id), refund_id = coalesce($5, refund_id),
			auth_expires_at = coalesce($8, auth_expires_at),
			` + attemptSetTail

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

func (s *Store) SetCaptureMock(ctx context.Context, attemptID, code string) (bool, error) {
	var stored bool
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var status domain.EnrollmentStatus
		err := tx.QueryRow(ctx,
			`select e.status from enrollments e join payment_attempts a on a.enrollment_id = e.id
			 where a.id = $1 for update of e`, attemptID).Scan(&status)
		if err != nil {
			return lookupErr("lock enrollment", err)
		}
		if status != domain.EnrollmentSeatReserved && status != domain.EnrollmentAuthorized {
			return nil
		}
		tag, err := tx.Exec(ctx,
			`update payment_attempts set mock_code = $2, updated_at = now()
			 where id = $1 and capture_id is null and capture_status is null and capture_issue is null`,
			attemptID, code)
		if err != nil {
			return wrap("set capture mock", err)
		}
		stored = tag.RowsAffected() == 1
		return nil
	})
	if err != nil {
		return false, err
	}
	return stored, nil
}

func (s *Store) MarkOrderExpired(ctx context.Context, attemptID string) (marked bool, err error) {
	tag, err := s.Pool.Exec(ctx,
		`update payment_attempts set order_status = $2, updated_at = now()
		 where id = $1 and authorization_id is null`,
		attemptID, OrderExpired)
	if err != nil {
		return false, lookupErr("mark order expired", err)
	}
	if tag.RowsAffected() == 1 {
		return true, nil
	}
	a, err := s.AttemptByID(ctx, attemptID)
	if err != nil {
		return false, err
	}
	if a == nil {
		return false, ErrNotFound
	}
	return false, nil
}

func (s *Store) RetryRefund(ctx context.Context, enrollmentID string, now time.Time) (string, error) {
	var key string
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var status, workshopID string
		if err := tx.QueryRow(ctx, `select status, workshop_id from enrollments where id = $1 for update`, enrollmentID).Scan(&status, &workshopID); err != nil {
			return lookupErr("lock enrollment", err)
		}
		if status != string(domain.EnrollmentRefundFailed) {
			return domain.ErrNotRefundFailed
		}
		var attemptID string
		if err := tx.QueryRow(ctx, `select id from payment_attempts where enrollment_id = $1 order by attempt_no desc limit 1 for update`,
			enrollmentID).Scan(&attemptID); err != nil {
			return lookupErr("lock attempt", err)
		}
		var retries int
		if err := tx.QueryRow(ctx, `select count(*) from jobs where attempt_id = $1 and kind = $2 and idempotency_key like $3`,
			attemptID, KindRefund, JobKey(KindRefund, attemptID)+":retry:%").Scan(&retries); err != nil {
			return wrap("count refund retries", err)
		}
		key = JobKey(KindRefund, attemptID) + ":retry:" + strconv.Itoa(retries+1)
		if _, err := tx.Exec(ctx,
			`update payment_attempts set
				failed_refund_ids = case when refund_id is null then failed_refund_ids else array_append(failed_refund_ids, refund_id) end,
				refund_id = null, refund_status = null, refund_reason = null, mock_code = null, updated_at = now()
			 where id = $1`, attemptID); err != nil {
			return wrap("reset refund", err)
		}
		if _, err := tx.Exec(ctx,
			`update enrollments set status = $2, needs_attention_at = null, attention_reason = null, updated_at = now() where id = $1`,
			enrollmentID, string(domain.EnrollmentPaid)); err != nil {
			return wrap("enrollment back to paid", err)
		}
		if _, err := tx.Exec(ctx,
			`insert into jobs (kind, idempotency_key, workshop_id, attempt_id, payload, run_at) values ($1, $2, $3, $4, '{}', $5)`,
			KindRefund, key, workshopID, attemptID, now); err != nil {
			return wrap("enqueue refund retry", err)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return key, nil
}
