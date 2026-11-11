package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/aalvaropc/commitpool/internal/domain"
)

const DefaultPaymentWindowMin = 1440

type WorkshopRow struct {
	ID, Title, Description string
	DurationMin            int
	Timezone               string
	Minimum, Capacity      int
	PriceMinor             int64
	Currency               string
	PaymentWindowMin       int
	Status                 domain.WorkshopStatus
	Resolution             domain.Resolution
	CreatedAt              time.Time
	ShareToken             string
	CancelledAt            *time.Time
	CancelReason           *string
	HostName, Location     string
	VenueCostMinor         *int64
	IsDemo                 bool
	OwnerEmail             string
	PayoutEmail            string
	PayoutLockedAt         *time.Time
	ChatImports            int
}

type CandidateDateRow struct {
	ID, WorkshopID   string
	StartsAt, EndsAt time.Time
}

type ProposalRow struct {
	ID, WorkshopID              string
	Version                     int
	ChosenDateID                string
	PriceMinor                  int64
	Currency                    string
	Minimum, Capacity           int
	OpensAt, ClosesAt, StartsAt time.Time
	ExtendedFrom, ExtendedAt    *time.Time
}

type CreateWorkshopParams struct {
	Title, Description string
	DurationMin        int
	Timezone           string
	Minimum, Capacity  int
	PriceMinor         int64
	Currency           string
	PaymentWindowMin   int
	HostName, Location string
	VenueCostMinor     *int64
	IsDemo             bool
	OwnerEmail         string
	PayoutEmail        string
	Dates              []struct{ StartsAt, EndsAt time.Time }
}

const (
	workshopCols      = `id, title, description, duration_min, timezone, minimum, capacity, price_minor, currency, payment_window_min, status, resolution, created_at, coalesce(share_token, ''), cancelled_at, cancel_reason, host_name, location, venue_cost_minor, is_demo, coalesce(owner_email, ''), coalesce(payout_email, ''), payout_locked_at, chat_imports`
	candidateDateCols = `id, workshop_id, starts_at, ends_at`
	proposalCols      = `id, workshop_id, version, chosen_date_id, price_minor, currency, minimum, capacity, opens_at, closes_at, starts_at, extended_from, extended_at`
)

func (s *Store) CreateWorkshop(ctx context.Context, p CreateWorkshopParams) (w WorkshopRow, ownerToken, shareToken string, err error) {
	ownerToken, ownerHash, err := NewToken()
	if err != nil {
		return WorkshopRow{}, "", "", err
	}
	shareToken, shareHash, err := NewToken()
	if err != nil {
		return WorkshopRow{}, "", "", err
	}
	window := p.PaymentWindowMin
	if window == 0 {
		window = DefaultPaymentWindowMin
	}

	err = pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		row, err := one[WorkshopRow](ctx, tx,
			`insert into workshops (owner_token_hash, share_token_hash, share_token, title, description, duration_min, timezone,
				minimum, capacity, price_minor, currency, payment_window_min, host_name, location, venue_cost_minor, is_demo, owner_email, payout_email)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, nullif($17, ''), nullif($18, ''))
			returning `+workshopCols,
			ownerHash, shareHash, shareToken, p.Title, p.Description, p.DurationMin, p.Timezone,
			p.Minimum, p.Capacity, p.PriceMinor, p.Currency, window, p.HostName, p.Location, p.VenueCostMinor, p.IsDemo, p.OwnerEmail, p.PayoutEmail)
		if err != nil {
			return wrap("insert workshop", err)
		}
		for _, d := range p.Dates {
			if _, err := tx.Exec(ctx,
				`insert into candidate_dates (workshop_id, starts_at, ends_at) values ($1, $2, $3)`,
				row.ID, d.StartsAt, d.EndsAt); err != nil {
				return wrap("insert candidate date", err)
			}
		}
		w = row
		return nil
	})
	if err != nil {
		return WorkshopRow{}, "", "", err
	}
	return w, ownerToken, shareToken, nil
}

func (s *Store) WorkshopByOwnerToken(ctx context.Context, token string) (WorkshopRow, error) {
	return s.workshopWhere(ctx, "workshop by owner token", `owner_token_hash = $1`, HashToken(token))
}

func (s *Store) WorkshopByShareToken(ctx context.Context, token string) (WorkshopRow, error) {
	return s.workshopWhere(ctx, "workshop by share token", `share_token_hash = $1`, HashToken(token))
}

func (s *Store) WorkshopByID(ctx context.Context, id string) (WorkshopRow, error) {
	return s.workshopWhere(ctx, "workshop by id", `id = $1`, id)
}

func (s *Store) workshopWhere(ctx context.Context, op, where string, arg any) (WorkshopRow, error) {
	w, err := one[WorkshopRow](ctx, s.Pool, `select `+workshopCols+` from workshops where `+where, arg)
	if err != nil {
		return WorkshopRow{}, lookupErr(op, err)
	}
	return w, nil
}

func (s *Store) CandidateDates(ctx context.Context, workshopID string) ([]CandidateDateRow, error) {
	return many[CandidateDateRow](ctx, s.Pool, "candidate dates",
		`select `+candidateDateCols+` from candidate_dates where workshop_id = $1 order by starts_at, id`, workshopID)
}

func (s *Store) OpenProposal(ctx context.Context, p ProposalRow) (ProposalRow, error) {
	var opened ProposalRow
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := lockWorkshop(ctx, tx, p.WorkshopID); err != nil {
			return err
		}
		row, err := openProposal(ctx, tx, p)
		opened = row
		return err
	})
	if err != nil {
		return ProposalRow{}, err
	}
	return opened, nil
}

func openProposal(ctx context.Context, tx pgx.Tx, p ProposalRow) (ProposalRow, error) {
	moved, err := transitionWorkshop(ctx, tx, p.WorkshopID,
		[]domain.WorkshopStatus{domain.WorkshopCollecting}, nil, domain.WorkshopFunding, domain.ResolutionOpen)
	if err != nil {
		return ProposalRow{}, err
	}
	if !moved {
		return ProposalRow{}, ErrProposalExists
	}
	row, err := one[ProposalRow](ctx, tx,
		`insert into proposals (workshop_id, version, chosen_date_id, price_minor, currency, minimum, capacity, opens_at, closes_at, starts_at)
		values ($1, 1, $2, $3, $4, $5, $6, $7, $8, $9)
		returning `+proposalCols,
		p.WorkshopID, p.ChosenDateID, p.PriceMinor, p.Currency, p.Minimum, p.Capacity, p.OpensAt, p.ClosesAt, p.StartsAt)
	if err != nil {
		return ProposalRow{}, wrap("insert proposal", err)
	}
	if _, err := tx.Exec(ctx,
		`insert into jobs (kind, idempotency_key, workshop_id, run_at) values ($1, $2, $3, $4)
		 on conflict (idempotency_key) do nothing`,
		KindClose, JobKey(KindClose, row.ID), p.WorkshopID, row.ClosesAt); err != nil {
		return ProposalRow{}, wrap("schedule close", err)
	}
	return row, nil
}

func (s *Store) ShortenPaymentWindow(ctx context.Context, proposalID string, closesAt time.Time) (ProposalRow, error) {
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
			`update proposals set closes_at = least(closes_at, $2) where id = $1 returning `+proposalCols,
			proposalID, closesAt)
		if err != nil {
			return wrap("shorten payment window", err)
		}
		shortened = row
		return nil
	})
	if err != nil {
		return ProposalRow{}, err
	}
	return shortened, nil
}

func (s *Store) ActiveProposal(ctx context.Context, workshopID string) (*ProposalRow, error) {
	return latestProposal(ctx, s.Pool, workshopID)
}

func (s *Store) ProposalByID(ctx context.Context, id string) (*ProposalRow, error) {
	return oneOrNil[ProposalRow](ctx, s.Pool, "proposal by id",
		`select `+proposalCols+` from proposals where id = $1`, id)
}

func latestProposal(ctx context.Context, q querier, workshopID string) (*ProposalRow, error) {
	return oneOrNil[ProposalRow](ctx, q, "active proposal",
		`select `+proposalCols+` from proposals where workshop_id = $1 order by version desc limit 1`, workshopID)
}

func lockWorkshop(ctx context.Context, tx pgx.Tx, workshopID string) (domain.WorkshopStatus, error) {
	var status domain.WorkshopStatus
	if err := tx.QueryRow(ctx, `select status from workshops where id = $1 for update`, workshopID).Scan(&status); err != nil {
		return "", lookupErr("lock workshop", err)
	}
	return status, nil
}

func (s *Store) TransitionWorkshop(ctx context.Context, id string, fromStatus []domain.WorkshopStatus, fromResolution []domain.Resolution, to domain.WorkshopStatus, resolution domain.Resolution) (bool, error) {
	return transitionWorkshop(ctx, s.Pool, id, fromStatus, fromResolution, to, resolution)
}

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func transitionWorkshop(ctx context.Context, q execer, id string, fromStatus []domain.WorkshopStatus, fromResolution []domain.Resolution, to domain.WorkshopStatus, resolution domain.Resolution) (bool, error) {
	tag, err := q.Exec(ctx,
		`update workshops set status = $4, resolution = $5, updated_at = now()
		 where id = $1
		   and ($2::text[] is null or status = any($2::text[]))
		   and ($3::text[] is null or resolution = any($3::text[]))`,
		id, texts(fromStatus), texts(fromResolution), string(to), string(resolution))
	if err != nil {
		if isMissing(err) {
			return false, nil
		}
		return false, wrap("transition workshop", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Store) CancelWorkshop(ctx context.Context, id string, from domain.WorkshopStatus, fromResolution, resolution domain.Resolution, reason string, now time.Time) (bool, error) {
	tag, err := s.Pool.Exec(ctx,
		`update workshops set status = $5, resolution = $4, cancelled_at = $6, cancel_reason = nullif($7, ''), updated_at = now()
		 where id = $1 and status = $2 and resolution = $3 and payout_locked_at is null`,
		id, string(from), string(fromResolution), string(resolution), string(domain.WorkshopCancelled), now, reason)
	if err != nil {
		if isMissing(err) {
			return false, nil
		}
		return false, wrap("cancel workshop", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Store) WorkshopIDsFundingOrUnsettled(ctx context.Context) ([]string, error) {
	rows, err := s.Pool.Query(ctx,
		`select id::text from workshops
		 where status = $1 or resolution = $2 or (status = $3 and resolution <> $4)
		 order by id`,
		string(domain.WorkshopFunding), string(domain.ResolutionResolving),
		string(domain.WorkshopCancelled), string(domain.ResolutionSettled))
	if err != nil {
		return nil, wrap("workshops funding or unsettled", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, wrap("workshops funding or unsettled", err)
	}
	if ids == nil {
		ids = []string{}
	}
	return ids, nil
}

type OverdueClose struct {
	WorkshopID, ProposalID string
}

func (s *Store) OverdueCloses(ctx context.Context, before time.Time) ([]OverdueClose, error) {
	return many[OverdueClose](ctx, s.Pool, "overdue closes",
		`select w.id::text, p.id::text from workshops w join proposals p on p.workshop_id = w.id
		 where w.status = $1 and p.closes_at < $2
		 order by p.closes_at, w.id`, string(domain.WorkshopFunding), before)
}

func texts[T ~string](vs []T) []string {
	if vs == nil {
		return nil
	}
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = string(v)
	}
	return out
}

func (s *Store) SetWorkshopStatus(ctx context.Context, id string, status domain.WorkshopStatus, resolution domain.Resolution) error {
	tag, err := s.Pool.Exec(ctx,
		`update workshops set status = $2, resolution = $3, updated_at = now() where id = $1`,
		id, string(status), string(resolution))
	if err != nil {
		return lookupErr("set workshop status", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
