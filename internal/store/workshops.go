package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/aalvaropc/commitpool/internal/domain"
)

// DefaultPaymentWindowMin is the payment window applied when a workshop is
// created without one.
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
}

type CreateWorkshopParams struct {
	Title, Description string
	DurationMin        int
	Timezone           string
	Minimum, Capacity  int
	PriceMinor         int64
	Currency           string
	PaymentWindowMin   int
	Dates              []struct{ StartsAt, EndsAt time.Time }
}

const (
	workshopCols      = `id, title, description, duration_min, timezone, minimum, capacity, price_minor, currency, payment_window_min, status, resolution, created_at`
	candidateDateCols = `id, workshop_id, starts_at, ends_at`
	proposalCols      = `id, workshop_id, version, chosen_date_id, price_minor, currency, minimum, capacity, opens_at, closes_at, starts_at`
)

// CreateWorkshop stores the workshop and its candidate dates in one
// transaction and returns the owner and share tokens in plaintext, the only
// time they exist outside a URL: only their hashes are stored.
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
			`insert into workshops (owner_token_hash, share_token_hash, title, description, duration_min, timezone,
				minimum, capacity, price_minor, currency, payment_window_min)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			returning `+workshopCols,
			ownerHash, shareHash, p.Title, p.Description, p.DurationMin, p.Timezone,
			p.Minimum, p.Capacity, p.PriceMinor, p.Currency, window)
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

// CandidateDates returns the workshop's candidate dates, earliest first.
func (s *Store) CandidateDates(ctx context.Context, workshopID string) ([]CandidateDateRow, error) {
	return many[CandidateDateRow](ctx, s.Pool, "candidate dates",
		`select `+candidateDateCols+` from candidate_dates where workshop_id = $1 order by starts_at, id`, workshopID)
}

// OpenProposal stores the workshop's first proposal as version 1 and moves the
// workshop from COLLECTING to FUNDING in one transaction. The ID and Version
// of p are ignored. It fails with ErrProposalExists when payments were already
// opened.
func (s *Store) OpenProposal(ctx context.Context, p ProposalRow) (ProposalRow, error) {
	var opened ProposalRow
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		status, err := lockWorkshop(ctx, tx, p.WorkshopID)
		if err != nil {
			return err
		}
		if status != domain.WorkshopCollecting {
			return ErrProposalExists
		}
		row, err := one[ProposalRow](ctx, tx,
			`insert into proposals (workshop_id, version, chosen_date_id, price_minor, currency, minimum, capacity, opens_at, closes_at, starts_at)
			values ($1, 1, $2, $3, $4, $5, $6, $7, $8, $9)
			returning `+proposalCols,
			p.WorkshopID, p.ChosenDateID, p.PriceMinor, p.Currency, p.Minimum, p.Capacity, p.OpensAt, p.ClosesAt, p.StartsAt)
		if err != nil {
			return wrap("insert proposal", err)
		}
		if _, err := tx.Exec(ctx,
			`update workshops set status = $2, updated_at = now() where id = $1`,
			p.WorkshopID, string(domain.WorkshopFunding)); err != nil {
			return wrap("open payments", err)
		}
		opened = row
		return nil
	})
	if err != nil {
		return ProposalRow{}, err
	}
	return opened, nil
}

// ActiveProposal returns the workshop's current proposal, or nil when payments
// have not been opened.
func (s *Store) ActiveProposal(ctx context.Context, workshopID string) (*ProposalRow, error) {
	return latestProposal(ctx, s.Pool, workshopID)
}

func latestProposal(ctx context.Context, q querier, workshopID string) (*ProposalRow, error) {
	return oneOrNil[ProposalRow](ctx, q, "active proposal",
		`select `+proposalCols+` from proposals where workshop_id = $1 order by version desc limit 1`, workshopID)
}

// lockWorkshop takes the workshop's row lock for the rest of the transaction
// and returns its status. Every operation that decides based on how many seats
// are taken goes through this lock.
func lockWorkshop(ctx context.Context, tx pgx.Tx, workshopID string) (domain.WorkshopStatus, error) {
	var status domain.WorkshopStatus
	if err := tx.QueryRow(ctx, `select status from workshops where id = $1 for update`, workshopID).Scan(&status); err != nil {
		return "", lookupErr("lock workshop", err)
	}
	return status, nil
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
