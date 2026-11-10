package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/aalvaropc/commitpool/internal/domain"
)

var (
	ErrCloseNotPending  = errors.New("store: the close job is not pending, so the deadline cannot move")
	ErrExtensionChanged = errors.New("store: the deadline or the extension on offer changed")
)

func (p ProposalRow) Domain() domain.Proposal {
	return domain.Proposal{
		ID: p.ID, Version: p.Version, DateID: p.ChosenDateID,
		OpensAt: p.OpensAt, ClosesAt: p.ClosesAt, StartsAt: p.StartsAt,
		Price:   domain.Money{Minor: p.PriceMinor, Currency: p.Currency},
		Minimum: p.Minimum, Capacity: p.Capacity,
	}
}

func (s *Store) ExtensionCheck(ctx context.Context, status domain.WorkshopStatus, p ProposalRow, now time.Time) (domain.ExtensionCheck, error) {
	rows, err := many[EnrollmentRow](ctx, s.Pool, "enrollments for an extension",
		`select `+enrollmentCols+` from enrollments where workshop_id = $1 order by created_at, id`, p.WorkshopID)
	if err != nil {
		return domain.ExtensionCheck{}, err
	}
	return extensionCheck(ctx, s.Pool, status, p, rows, now)
}

func lockSeatsForExtension(ctx context.Context, tx pgx.Tx, workshopID string) ([]EnrollmentRow, error) {
	return many[EnrollmentRow](ctx, tx, "lock seats for an extension",
		`select `+enrollmentCols+` from enrollments where workshop_id = $1 and status = any($2::text[]) order by id for update`,
		workshopID, append(occupyingStatuses(), string(domain.EnrollmentReleased)))
}

func extensionCheck(ctx context.Context, q querier, status domain.WorkshopStatus, p ProposalRow, rows []EnrollmentRow, now time.Time) (domain.ExtensionCheck, error) {
	charging, err := scalars[string](ctx, q, "captures for an extension",
		`select distinct a.enrollment_id::text from jobs j
		 join payment_attempts a on a.id = j.attempt_id
		 join enrollments e on e.id = a.enrollment_id
		 where e.workshop_id = $1 and j.kind = $2`, p.WorkshopID, KindCapture)
	if err != nil {
		return domain.ExtensionCheck{}, err
	}
	started := make(map[string]bool, len(charging))
	for _, id := range charging {
		started[id] = true
	}
	es := make([]domain.Enrollment, len(rows))
	for i, r := range rows {
		es[i] = r.Domain()
		if started[r.ID] {
			at := now
			es[i].CaptureStartedAt = &at
		}
	}
	holds, err := scalars[time.Time](ctx, q, "holds for an extension",
		`select a.created_at from payment_attempts a join enrollments e on e.id = a.enrollment_id
		 where e.workshop_id = $1
		   and ((a.authorization_id is not null and coalesce(a.authorization_status, '') not in ('VOIDED', 'DENIED', 'EXPIRED', 'CAPTURED'))
		        or e.status = any($2::text[]))
		 order by a.created_at`, p.WorkshopID, occupyingStatuses())
	if err != nil {
		return domain.ExtensionCheck{}, err
	}
	return domain.ExtensionCheck{Status: status, Proposal: p.Domain(), Extended: p.ExtendedAt != nil, Enrollments: es, Holds: holds, Now: now}, nil
}

func occupyingStatuses() []string {
	return []string{
		string(domain.EnrollmentSeatReserved), string(domain.EnrollmentAuthorized), string(domain.EnrollmentCapturing),
		string(domain.EnrollmentCapturePending), string(domain.EnrollmentPaid),
	}
}

func scalars[T any](ctx context.Context, q querier, op, sql string, args ...any) ([]T, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, wrap(op, err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[T])
	if err != nil {
		return nil, wrap(op, err)
	}
	return out, nil
}

func (s *Store) ExtendPaymentWindow(ctx context.Context, proposalID string, from, to, now time.Time) (ProposalRow, error) {
	var extended ProposalRow
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var workshopID string
		if err := tx.QueryRow(ctx, `select workshop_id from proposals where id = $1`, proposalID).Scan(&workshopID); err != nil {
			return lookupErr("find proposal", err)
		}
		status, err := lockWorkshop(ctx, tx, workshopID)
		if err != nil {
			return err
		}
		p, err := one[ProposalRow](ctx, tx, `select `+proposalCols+` from proposals where id = $1 for update`, proposalID)
		if err != nil {
			return lookupErr("lock proposal", err)
		}
		seats, err := lockSeatsForExtension(ctx, tx, workshopID)
		if err != nil {
			return err
		}
		check, err := extensionCheck(ctx, tx, status, p, seats, now)
		if err != nil {
			return err
		}
		option, err := domain.CanExtend(check)
		if err != nil {
			return err
		}
		if !p.ClosesAt.Equal(from) || !option.Equal(to) {
			return fmt.Errorf("%w: closes at %s with %s on offer, asked to move %s to %s", ErrExtensionChanged,
				p.ClosesAt.UTC().Format(time.RFC3339), option.UTC().Format(time.RFC3339), from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339))
		}
		row, err := one[ProposalRow](ctx, tx,
			`update proposals set closes_at = $2, extended_from = $3, extended_at = $4
			 where id = $1 and extended_at is null
			 returning `+proposalCols, proposalID, to, p.ClosesAt, now)
		if err != nil {
			return wrap("extend payment window", err)
		}
		tag, err := tx.Exec(ctx,
			`update jobs set run_at = $2, updated_at = now() where idempotency_key = $1 and status = $3`,
			JobKey(KindClose, proposalID), to, JobPending)
		if err != nil {
			return wrap("move the close", err)
		}
		if tag.RowsAffected() != 1 {
			return ErrCloseNotPending
		}
		extended = row
		return nil
	})
	if err != nil {
		return ProposalRow{}, err
	}
	return extended, nil
}

func (s *Store) SkipToLastHours(ctx context.Context, proposalID string, closesAt time.Time) (ProposalRow, error) {
	var shortened ProposalRow
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var workshopID string
		if err := tx.QueryRow(ctx, `select workshop_id from proposals where id = $1`, proposalID).Scan(&workshopID); err != nil {
			return lookupErr("find proposal", err)
		}
		if _, err := lockWorkshop(ctx, tx, workshopID); err != nil {
			return err
		}
		row, err := one[ProposalRow](ctx, tx,
			`update proposals set closes_at = least(closes_at, $2) where id = $1 and extended_at is null returning `+proposalCols,
			proposalID, closesAt)
		if isMissing(err) {
			return domain.ErrAlreadyExtended
		}
		if err != nil {
			return wrap("skip to the last hours", err)
		}
		if _, err := tx.Exec(ctx,
			`update jobs set run_at = $2, updated_at = now() where idempotency_key = $1 and status = $3 and run_at > $2`,
			JobKey(KindClose, proposalID), row.ClosesAt, JobPending); err != nil {
			return wrap("bring the close forward", err)
		}
		shortened = row
		return nil
	})
	if err != nil {
		return ProposalRow{}, err
	}
	return shortened, nil
}
