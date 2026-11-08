package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrApprovalPending      = errors.New("store: the organizer has not approved these terms")
	ErrApprovalExpired      = errors.New("store: the approval expired")
	ErrApprovalUsed         = errors.New("store: the approval was already used")
	ErrApprovalTermsChanged = errors.New("store: the terms changed since the approval was requested")
)

type OpenApprovalRow struct {
	ID, WorkshopID, DateID string
	TermsHash              []byte
	PriceMinor             int64
	Currency               string
	Minimum, Capacity      int
	PaymentWindowMin       int
	CreatedAt, ExpiresAt   time.Time
	ApprovedAt, UsedAt     *time.Time
}

const approvalCols = `id, workshop_id, date_id, terms_hash, price_minor, currency, minimum, capacity, payment_window_min, created_at, expires_at, approved_at, used_at`

func (w WorkshopRow) OpenTermsHash(dateID string) []byte {
	sum := sha256.Sum256([]byte(strings.Join([]string{dateID, strconv.FormatInt(w.PriceMinor, 10), w.Currency,
		strconv.Itoa(w.Minimum), strconv.Itoa(w.Capacity), strconv.Itoa(w.PaymentWindowMin)}, "|")))
	return sum[:]
}

func (a OpenApprovalRow) Check(current []byte, now time.Time) error {
	switch {
	case a.UsedAt != nil:
		return ErrApprovalUsed
	case !bytes.Equal(a.TermsHash, current):
		return ErrApprovalTermsChanged
	case !now.Before(a.ExpiresAt):
		return ErrApprovalExpired
	case a.ApprovedAt == nil:
		return ErrApprovalPending
	}
	return nil
}

func (a OpenApprovalRow) covers(p ProposalRow) bool {
	return a.DateID == p.ChosenDateID && a.PriceMinor == p.PriceMinor && a.Currency == p.Currency &&
		a.Minimum == p.Minimum && a.Capacity == p.Capacity
}

func (s *Store) RequestOpenApproval(ctx context.Context, w WorkshopRow, dateID string, now time.Time, ttl, keep time.Duration) (OpenApprovalRow, bool, error) {
	hash := w.OpenTermsHash(dateID)
	var out OpenApprovalRow
	created := false
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := lockWorkshop(ctx, tx, w.ID); err != nil {
			return err
		}
		open, err := oneOrNil[OpenApprovalRow](ctx, tx, "find open approval",
			`select `+approvalCols+` from open_approvals
			 where workshop_id = $1 and date_id = $2 and terms_hash = $3 and used_at is null and expires_at >= $4
			 order by created_at desc limit 1`, w.ID, dateID, hash, now.Add(keep))
		if err != nil {
			return err
		}
		if open != nil {
			out = *open
			return nil
		}
		row, err := one[OpenApprovalRow](ctx, tx,
			`insert into open_approvals (workshop_id, date_id, terms_hash, price_minor, currency, minimum, capacity, payment_window_min, created_at, expires_at)
			 values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			 returning `+approvalCols,
			w.ID, dateID, hash, w.PriceMinor, w.Currency, w.Minimum, w.Capacity, w.PaymentWindowMin, now, now.Add(ttl))
		if err != nil {
			return wrap("insert open approval", err)
		}
		out, created = row, true
		return nil
	})
	if err != nil {
		return OpenApprovalRow{}, false, err
	}
	return out, created, nil
}

func (s *Store) OpenApproval(ctx context.Context, workshopID, id string) (OpenApprovalRow, error) {
	a, err := one[OpenApprovalRow](ctx, s.Pool,
		`select `+approvalCols+` from open_approvals where id = $1 and workshop_id = $2`, id, workshopID)
	if err != nil {
		return OpenApprovalRow{}, lookupErr("open approval", err)
	}
	return a, nil
}

func lockWorkshopRow(ctx context.Context, tx pgx.Tx, id string) (WorkshopRow, error) {
	w, err := one[WorkshopRow](ctx, tx, `select `+workshopCols+` from workshops where id = $1 for update`, id)
	if err != nil {
		return WorkshopRow{}, lookupErr("lock workshop", err)
	}
	return w, nil
}

func lockApproval(ctx context.Context, tx pgx.Tx, workshopID, id string) (OpenApprovalRow, error) {
	a, err := one[OpenApprovalRow](ctx, tx,
		`select `+approvalCols+` from open_approvals where id = $1 and workshop_id = $2 for update`, id, workshopID)
	if err != nil {
		return OpenApprovalRow{}, lookupErr("lock open approval", err)
	}
	return a, nil
}

func (s *Store) ApproveOpen(ctx context.Context, workshopID, id string, now time.Time, ttl time.Duration) (OpenApprovalRow, error) {
	var out OpenApprovalRow
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		w, err := lockWorkshopRow(ctx, tx, workshopID)
		if err != nil {
			return err
		}
		a, err := lockApproval(ctx, tx, workshopID, id)
		if err != nil {
			return err
		}
		if err := a.Check(w.OpenTermsHash(a.DateID), now); err != nil && !errors.Is(err, ErrApprovalPending) {
			return err
		}
		if a.ApprovedAt != nil {
			out = a
			return nil
		}
		row, err := one[OpenApprovalRow](ctx, tx,
			`update open_approvals set approved_at = $2, expires_at = $3 where id = $1 returning `+approvalCols, id, now, now.Add(ttl))
		if err != nil {
			return wrap("approve open", err)
		}
		out = row
		return nil
	})
	if err != nil {
		return OpenApprovalRow{}, err
	}
	return out, nil
}

func (s *Store) OpenProposalWithApproval(ctx context.Context, p ProposalRow, approvalID string) (ProposalRow, error) {
	var opened ProposalRow
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		w, err := lockWorkshopRow(ctx, tx, p.WorkshopID)
		if err != nil {
			return err
		}
		a, err := lockApproval(ctx, tx, p.WorkshopID, approvalID)
		if err != nil {
			return err
		}
		if err := a.Check(w.OpenTermsHash(a.DateID), p.OpensAt); err != nil {
			return err
		}
		if !a.covers(p) {
			return ErrApprovalTermsChanged
		}
		row, err := openProposal(ctx, tx, p)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `update open_approvals set used_at = $2 where id = $1`, approvalID, p.OpensAt); err != nil {
			return wrap("use open approval", err)
		}
		opened = row
		return nil
	})
	if err != nil {
		return ProposalRow{}, err
	}
	return opened, nil
}
