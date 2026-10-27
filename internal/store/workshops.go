package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

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
	// ShareToken is the plaintext share token, kept so the owner can show
	// the share link again; empty for workshops created before it was kept.
	ShareToken string
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
	workshopCols      = `id, title, description, duration_min, timezone, minimum, capacity, price_minor, currency, payment_window_min, status, resolution, created_at, coalesce(share_token, '')`
	candidateDateCols = `id, workshop_id, starts_at, ends_at`
	proposalCols      = `id, workshop_id, version, chosen_date_id, price_minor, currency, minimum, capacity, opens_at, closes_at, starts_at`
)

// CreateWorkshop stores the workshop and its candidate dates in one
// transaction and returns the owner and share tokens in plaintext. The owner
// token exists nowhere else: only its hash is stored. The share token, meant
// to be passed around, is also kept in plaintext so the owner can show the
// share link again.
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
				minimum, capacity, price_minor, currency, payment_window_min)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			returning `+workshopCols,
			ownerHash, shareHash, shareToken, p.Title, p.Description, p.DurationMin, p.Timezone,
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
// workshop from COLLECTING to FUNDING, by the same compare-and-set as
// TransitionWorkshop, in one transaction. The ID and Version of p are
// ignored. It fails with ErrProposalExists when the workshop is no longer
// COLLECTING, because payments were already opened, and with ErrNotFound for
// an unknown workshop.
func (s *Store) OpenProposal(ctx context.Context, p ProposalRow) (ProposalRow, error) {
	var opened ProposalRow
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := lockWorkshop(ctx, tx, p.WorkshopID); err != nil {
			return err
		}
		moved, err := transitionWorkshop(ctx, tx, p.WorkshopID,
			[]domain.WorkshopStatus{domain.WorkshopCollecting}, nil, domain.WorkshopFunding, domain.ResolutionOpen)
		if err != nil {
			return err
		}
		if !moved {
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
		opened = row
		return nil
	})
	if err != nil {
		return ProposalRow{}, err
	}
	return opened, nil
}

// ShortenPaymentWindow moves the proposal's close to closesAt when that is
// earlier, under the workshop's row lock, and returns the proposal as stored.
// It never extends the window. An unknown proposal is ErrNotFound.
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

// ActiveProposal returns the workshop's current proposal, or nil when payments
// have not been opened.
func (s *Store) ActiveProposal(ctx context.Context, workshopID string) (*ProposalRow, error) {
	return latestProposal(ctx, s.Pool, workshopID)
}

// ProposalByID returns the proposal, or nil when there is none.
func (s *Store) ProposalByID(ctx context.Context, id string) (*ProposalRow, error) {
	return oneOrNil[ProposalRow](ctx, s.Pool, "proposal by id",
		`select `+proposalCols+` from proposals where id = $1`, id)
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

// TransitionWorkshop is a compare-and-set on the workshop's status and
// resolution: they become to and resolution only while the status is one of
// fromStatus and the resolution one of fromResolution, a nil list meaning
// any. It reports whether the row changed; a missing workshop is false.
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

// texts turns a list of string-like values into a text array; nil stays nil,
// which is sent as NULL.
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

// SetWorkshopStatus writes the status and resolution unconditionally; the
// worker uses TransitionWorkshop instead.
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
