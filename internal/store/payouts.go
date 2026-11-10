package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/aalvaropc/commitpool/internal/domain"
)

const (
	KindPayout      = "payout"
	KindPayoutCheck = "payout_check"
)

var ErrPayoutConflict = errors.New("store: the payout already holds another PayPal batch")

type ErrPayoutNotReady struct{ Reason string }

func (e *ErrPayoutNotReady) Error() string { return "store: the payout is not ready: " + e.Reason }

type PayoutRow struct {
	ID, WorkshopID, SenderBatchID, Receiver string
	AmountMinor, GrossMinor, FeeMinor       int64
	Currency                                string
	Seats                                   int
	Early                                   bool
	Status                                  domain.PayoutStatus
	PayoutBatchID, BatchStatus              *string
	PayoutItemID, ItemStatus, TransactionID *string
	Issue, DebugID                          *string
	CreatedAt                               time.Time
	SentAt, CheckedAt, CompletedAt          *time.Time
}

const payoutCols = `id, workshop_id, sender_batch_id, receiver, amount_minor, gross_minor, fee_minor, currency, seats, early, status,
	payout_batch_id, batch_status, payout_item_id, item_status, transaction_id, issue, debug_id,
	created_at, sent_at, checked_at, completed_at`

func SenderBatchID(workshopID string) string { return "cp-" + workshopID }

func (s *Store) BeginPayout(ctx context.Context, workshopID string, now time.Time, early bool) (PayoutRow, bool, error) {
	var (
		out     PayoutRow
		created bool
	)
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		w, err := lockWorkshopRow(ctx, tx, workshopID)
		if err != nil {
			return err
		}
		existing, err := oneOrNil[PayoutRow](ctx, tx, "payout by workshop",
			`select `+payoutCols+` from payouts where workshop_id = $1`, workshopID)
		if err != nil {
			return err
		}
		if existing != nil {
			out = *existing
			return nil
		}
		p, err := latestProposal(ctx, tx, workshopID)
		if err != nil {
			return err
		}
		if p == nil {
			return &ErrPayoutNotReady{Reason: domain.PayoutNotConfirmed}
		}
		f, fee, err := payoutFacts(ctx, tx, w, *p)
		if err != nil {
			return err
		}
		if reason := domain.PayoutBlocker(f, now, early); reason != "" {
			return &ErrPayoutNotReady{Reason: reason}
		}
		row, err := one[PayoutRow](ctx, tx,
			`insert into payouts (workshop_id, sender_batch_id, receiver, amount_minor, gross_minor, fee_minor, currency, seats, early, created_at, updated_at)
			 values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)
			 returning `+payoutCols,
			workshopID, SenderBatchID(workshopID), w.PayoutEmail, f.Net, f.Net+fee, fee, p.Currency, f.Paid, early, now)
		if err != nil {
			return wrap("insert payout", err)
		}
		tag, err := tx.Exec(ctx,
			`update workshops set payout_locked_at = $2, updated_at = now() where id = $1 and payout_locked_at is null`, workshopID, now)
		if err != nil {
			return wrap("lock workshop for its payout", err)
		}
		if tag.RowsAffected() != 1 {
			return errors.New("store: workshop is locked for a payout it does not have")
		}
		out, created = row, true
		return nil
	})
	if err != nil {
		return PayoutRow{}, false, err
	}
	return out, created, nil
}

func payoutFacts(ctx context.Context, tx pgx.Tx, w WorkshopRow, p ProposalRow) (domain.PayoutFacts, int64, error) {
	f := domain.PayoutFacts{
		Status: w.Status, Resolution: w.Resolution, PayoutEmail: w.PayoutEmail,
		ClosesAt: p.ClosesAt, StartsAt: p.StartsAt,
	}
	var fee int64
	err := tx.QueryRow(ctx,
		`with latest as (
		   select distinct on (a.enrollment_id) a.enrollment_id, a.capture_fee_minor, a.capture_net_minor
		   from payment_attempts a join enrollments e on e.id = a.enrollment_id
		   where e.workshop_id = $1
		   order by a.enrollment_id, a.attempt_no desc)
		 select
		   count(*) filter (where e.status = any($2)),
		   count(*) filter (where e.needs_attention_at is not null),
		   count(*) filter (where e.status = $3),
		   count(*) filter (where e.status = $3 and (l.capture_net_minor is null or l.capture_fee_minor is null)),
		   coalesce(sum(l.capture_net_minor) filter (where e.status = $3), 0),
		   coalesce(sum(l.capture_fee_minor) filter (where e.status = $3), 0)
		 from enrollments e left join latest l on l.enrollment_id = e.id
		 where e.workshop_id = $1`,
		w.ID, texts(domain.PayoutHoldStatuses), string(domain.EnrollmentPaid)).
		Scan(&f.InFlight, &f.Flagged, &f.Paid, &f.MissingNet, &f.Net, &fee)
	if err != nil {
		return domain.PayoutFacts{}, 0, wrap("payout facts", err)
	}
	return f, fee, nil
}

func (s *Store) PayoutByWorkshop(ctx context.Context, workshopID string) (*PayoutRow, error) {
	return oneOrNil[PayoutRow](ctx, s.Pool, "payout by workshop",
		`select `+payoutCols+` from payouts where workshop_id = $1`, workshopID)
}

func (s *Store) PayoutByID(ctx context.Context, id string) (*PayoutRow, error) {
	return oneOrNil[PayoutRow](ctx, s.Pool, "payout by id",
		`select `+payoutCols+` from payouts where id = $1`, id)
}

func (s *Store) PayoutByPayPalRef(ctx context.Context, batchID, senderBatchID, itemID string) (*PayoutRow, error) {
	if batchID == "" && senderBatchID == "" && itemID == "" {
		return nil, nil
	}
	return oneOrNil[PayoutRow](ctx, s.Pool, "payout by PayPal reference",
		`select `+payoutCols+` from payouts
		 where ($1 <> '' and payout_batch_id = $1) or ($2 <> '' and sender_batch_id = $2) or ($3 <> '' and payout_item_id = $3)
		 order by (payout_batch_id = $1) desc nulls last
		 limit 1`, batchID, senderBatchID, itemID)
}

func (s *Store) MarkPayoutSent(ctx context.Context, id, batchID, batchStatus string, now time.Time) (PayoutRow, error) {
	row, err := oneOrNil[PayoutRow](ctx, s.Pool, "mark payout sent",
		`update payouts set
		   status = case when status in ('created', 'unknown') then 'sent' else status end,
		   issue = case when status in ('created', 'unknown') then null else issue end,
		   debug_id = case when status in ('created', 'unknown') then null else debug_id end,
		   payout_batch_id = $2,
		   batch_status = coalesce(nullif($3, ''), batch_status),
		   sent_at = coalesce(sent_at, $4),
		   updated_at = now()
		 where id = $1 and (payout_batch_id is null or payout_batch_id = $2)
		 returning `+payoutCols, id, batchID, batchStatus, now)
	if err != nil || row != nil {
		return derefRow(row), err
	}
	existing, err := s.PayoutByID(ctx, id)
	if err != nil {
		return PayoutRow{}, err
	}
	if existing == nil {
		return PayoutRow{}, ErrNotFound
	}
	return PayoutRow{}, ErrPayoutConflict
}

func derefRow(r *PayoutRow) PayoutRow {
	if r == nil {
		return PayoutRow{}
	}
	return *r
}

func (s *Store) MarkPayoutUnsent(ctx context.Context, id string, status domain.PayoutStatus, issue, debugID string, now time.Time) (bool, error) {
	if status != domain.PayoutFailed && status != domain.PayoutUnknown {
		return false, errors.New("store: an unsent payout is only failed or unknown")
	}
	tag, err := s.Pool.Exec(ctx,
		`update payouts set status = $2, issue = nullif($3, ''), debug_id = nullif($4, ''),
		   completed_at = case when $2 = 'failed' then $5 else completed_at end, updated_at = now()
		 where id = $1 and status = 'created' and payout_batch_id is null`,
		id, string(status), issue, debugID, now)
	if err != nil {
		return false, lookupErr("mark payout unsent", err)
	}
	return tag.RowsAffected() == 1, nil
}

type PayoutCheck struct {
	Status                   domain.PayoutStatus
	BatchStatus              string
	ItemID, ItemStatus       string
	TransactionID, ItemIssue string
}

func (s *Store) RecordPayoutCheck(ctx context.Context, id string, c PayoutCheck, now time.Time) (before, after PayoutRow, err error) {
	err = pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		row, err := one[PayoutRow](ctx, tx, `select `+payoutCols+` from payouts where id = $1 for update`, id)
		if err != nil {
			return lookupErr("lock payout", err)
		}
		next := domain.NextPayoutStatus(row.Status, c.Status)
		updated, err := one[PayoutRow](ctx, tx,
			`update payouts set
			   status = $2,
			   batch_status = coalesce(nullif($3, ''), batch_status),
			   payout_item_id = coalesce(payout_item_id, nullif($4, '')),
			   item_status = coalesce(nullif($5, ''), item_status),
			   transaction_id = coalesce(nullif($6, ''), transaction_id),
			   issue = case when $2 = 'succeeded' then null else coalesce(nullif($7, ''), issue) end,
			   checked_at = $8,
			   completed_at = case when $2 in ('succeeded', 'failed') then coalesce(completed_at, $8) else null end,
			   updated_at = now()
			 where id = $1
			 returning `+payoutCols,
			id, string(next), c.BatchStatus, c.ItemID, c.ItemStatus, c.TransactionID, c.ItemIssue, now)
		if err != nil {
			return wrap("record payout check", err)
		}
		before, after = row, updated
		return nil
	})
	return before, after, err
}

func (s *Store) DuePayouts(ctx context.Context, now time.Time) ([]string, error) {
	rows, err := s.Pool.Query(ctx,
		`select w.id::text from workshops w
		 join lateral (select starts_at from proposals where workshop_id = w.id order by version desc limit 1) p on true
		 left join payouts po on po.workshop_id = w.id
		 where w.status = $1 and w.payout_email is not null and p.starts_at <= $2
		   and (po.id is null or po.status = $3)
		 order by p.starts_at, w.id`,
		string(domain.WorkshopConfirmed), now, string(domain.PayoutCreated))
	if err != nil {
		return nil, wrap("due payouts", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, wrap("due payouts", err)
	}
	return ids, nil
}

func (s *Store) PayoutsToCheck(ctx context.Context) ([]PayoutRow, error) {
	return many[PayoutRow](ctx, s.Pool, "payouts to check",
		`select `+payoutCols+` from payouts
		 where status in ('sent', 'unclaimed', 'unknown') and payout_batch_id is not null
		 order by created_at, id`)
}

func (s *Store) PaidAttemptsMissingFees(ctx context.Context, workshopID string) ([]AttemptRow, error) {
	return many[AttemptRow](ctx, s.Pool, "paid attempts missing fees",
		`select `+attemptCols+` from (
		   select distinct on (a.enrollment_id) a.* from payment_attempts a join enrollments e on e.id = a.enrollment_id
		   where e.workshop_id = $1 and e.status = $2
		   order by a.enrollment_id, a.attempt_no desc) latest
		 where capture_id is not null and (capture_fee_minor is null or capture_net_minor is null)
		 order by id`, workshopID, string(domain.EnrollmentPaid))
}
