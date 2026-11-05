package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/aalvaropc/commitpool/internal/domain"
)

type EnrollmentRow struct {
	ID, WorkshopID, ParticipantID, ProposalID string
	Status                                    domain.EnrollmentStatus
	ConsentAt                                 time.Time
	ConsentVersion                            string
	SeatReservedAt, SeatExpiresAt             time.Time
	ApprovalCancelledAt, ReleasedAt           *time.Time
	// NeedsAttentionAt is when the enrollment was flagged for an operator,
	// and AttentionReason why; both nil when it is not flagged.
	NeedsAttentionAt *time.Time
	AttentionReason  *string
}

// Domain is the enrollment as the domain rules see it.
func (e EnrollmentRow) Domain() domain.Enrollment {
	return domain.Enrollment{ID: e.ID, ParticipantID: e.ParticipantID, Status: e.Status, SeatExpiresAt: e.SeatExpiresAt, NeedsAttention: e.NeedsAttentionAt != nil}
}

type ReserveParams struct {
	WorkshopID, ParticipantID, ProposalID, ConsentVersion string
	Now                                                   time.Time
	SeatTTL                                               time.Duration
}

const enrollmentCols = `id, workshop_id, participant_id, proposal_id, status, consent_at, consent_version, seat_reserved_at, seat_expires_at, approval_cancelled_at, released_at, needs_attention_at, attention_reason`

// ReserveSeatAndCreateEnrollment reserves a seat for the participant under the
// workshop's row lock, so concurrent requests can never exceed the capacity of
// the active proposal.
//
// A participant who already has an active enrollment gets it back with
// created=false and nothing is inserted. Otherwise payments must be open
// (domain.PaymentsOpen: FUNDING, or CONFIRMED and still selling seats, with
// p.Now before the proposal closes; else ErrProposalNotOpen) and a seat must
// be free (domain.ErrCapacityFull).
func (s *Store) ReserveSeatAndCreateEnrollment(ctx context.Context, p ReserveParams) (EnrollmentRow, bool, error) {
	var (
		enrollment EnrollmentRow
		created    bool
	)
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		status, err := lockWorkshop(ctx, tx, p.WorkshopID)
		if err != nil {
			return err
		}

		existing, err := activeEnrollment(ctx, tx, p.ParticipantID)
		if err != nil {
			return err
		}
		if existing != nil {
			enrollment = *existing
			return nil
		}

		proposal, err := latestProposal(ctx, tx, p.WorkshopID)
		if err != nil {
			return err
		}
		if proposal == nil || !strings.EqualFold(proposal.ID, p.ProposalID) ||
			!domain.PaymentsOpen(domain.Workshop{Status: status}, domain.Proposal{ClosesAt: proposal.ClosesAt}, p.Now) {
			return ErrProposalNotOpen
		}

		// A lapsed seat no release job has freed yet does not count.
		if _, err := tx.Exec(ctx,
			`update enrollments e set status = 'RELEASED', released_at = $2, updated_at = now()
			 where e.workshop_id = $1 and `+releasable("$2"), p.WorkshopID, p.Now); err != nil {
			return wrap("release lapsed seats", err)
		}
		if err := checkCapacity(ctx, tx, p.WorkshopID, proposal.Capacity); err != nil {
			return err
		}

		row, err := one[EnrollmentRow](ctx, tx,
			`insert into enrollments (workshop_id, participant_id, proposal_id, status, consent_at, consent_version, seat_reserved_at, seat_expires_at)
			values ($1, $2, $3, $4, $5, $6, $5, $7)
			returning `+enrollmentCols,
			p.WorkshopID, p.ParticipantID, proposal.ID, string(domain.EnrollmentSeatReserved), p.Now, p.ConsentVersion, p.Now.Add(p.SeatTTL))
		if err != nil {
			return wrap("insert enrollment", err)
		}
		enrollment, created = row, true
		return nil
	})
	if err != nil {
		return EnrollmentRow{}, false, err
	}
	return enrollment, created, nil
}

// ReReserveSeat gives a RELEASED enrollment a fresh seat when payments are
// still open (domain.PaymentsOpen: FUNDING, or CONFIRMED before the proposal
// closes) and a seat is free, under the same workshop lock and the same rule
// as ReserveSeatAndCreateEnrollment. It reports false, without error,
// whenever the seat cannot be given back: no capacity, payments closed, the
// enrollment is not RELEASED, or the participant already holds a newer
// active enrollment.
func (s *Store) ReReserveSeat(ctx context.Context, enrollmentID string, now time.Time, ttl time.Duration) (bool, error) {
	reserved := false
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var workshopID string
		if err := tx.QueryRow(ctx, `select workshop_id from enrollments where id = $1`, enrollmentID).Scan(&workshopID); err != nil {
			return lookupErr("find enrollment", err)
		}
		status, err := lockWorkshop(ctx, tx, workshopID)
		if err != nil {
			return err
		}
		current, err := one[EnrollmentRow](ctx, tx, `select `+enrollmentCols+` from enrollments where id = $1 for update`, enrollmentID)
		if err != nil {
			return lookupErr("lock enrollment", err)
		}
		if current.Status != domain.EnrollmentReleased {
			return nil
		}

		proposal, err := latestProposal(ctx, tx, workshopID)
		if err != nil {
			return err
		}
		if proposal == nil ||
			!domain.PaymentsOpen(domain.Workshop{Status: status}, domain.Proposal{ClosesAt: proposal.ClosesAt}, now) {
			return nil
		}
		newer, err := activeEnrollment(ctx, tx, current.ParticipantID)
		if err != nil {
			return err
		}
		if newer != nil {
			return nil
		}
		if err := checkCapacity(ctx, tx, workshopID, proposal.Capacity); err != nil {
			if errors.Is(err, domain.ErrCapacityFull) {
				return nil
			}
			return err
		}

		if _, err := tx.Exec(ctx,
			`update enrollments
			 set status = $2, seat_reserved_at = $3, seat_expires_at = $4, released_at = null, updated_at = now()
			 where id = $1`,
			enrollmentID, string(domain.EnrollmentSeatReserved), now, now.Add(ttl)); err != nil {
			return wrap("re-reserve seat", err)
		}
		reserved = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return reserved, nil
}

// checkCapacity applies the domain seat rule to the workshop's enrollments. The
// caller must hold the workshop lock.
func checkCapacity(ctx context.Context, tx pgx.Tx, workshopID string, capacity int) error {
	rows, err := tx.Query(ctx,
		`select id, participant_id, status, seat_expires_at from enrollments where workshop_id = $1`, workshopID)
	if err != nil {
		return wrap("load enrollments", err)
	}
	enrollments, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Enrollment, error) {
		var e domain.Enrollment
		err := row.Scan(&e.ID, &e.ParticipantID, &e.Status, &e.SeatExpiresAt)
		return e, err
	})
	if err != nil {
		return wrap("load enrollments", err)
	}
	return domain.ReserveSeat(domain.Counts(enrollments), capacity)
}

// activeEnrollment returns the participant's one non-terminal enrollment, or nil.
func activeEnrollment(ctx context.Context, q querier, participantID string) (*EnrollmentRow, error) {
	all, err := many[EnrollmentRow](ctx, q, "enrollments by participant",
		`select `+enrollmentCols+` from enrollments where participant_id = $1 order by created_at desc, id`, participantID)
	if err != nil {
		return nil, err
	}
	for _, e := range all {
		if !e.Status.Terminal() {
			return &e, nil
		}
	}
	return nil, nil
}

func (s *Store) EnrollmentByID(ctx context.Context, id string) (EnrollmentRow, error) {
	e, err := one[EnrollmentRow](ctx, s.Pool, `select `+enrollmentCols+` from enrollments where id = $1`, id)
	if err != nil {
		return EnrollmentRow{}, lookupErr("enrollment by id", err)
	}
	return e, nil
}

// EnrollmentsByWorkshop returns every enrollment of the workshop in creation order.
func (s *Store) EnrollmentsByWorkshop(ctx context.Context, workshopID string) ([]EnrollmentRow, error) {
	return many[EnrollmentRow](ctx, s.Pool, "enrollments by workshop",
		`select `+enrollmentCols+` from enrollments where workshop_id = $1 order by created_at, id`, workshopID)
}

// ActiveEnrollmentByParticipant returns the participant's non-terminal
// enrollment, or nil when they have none.
func (s *Store) ActiveEnrollmentByParticipant(ctx context.Context, participantID string) (*EnrollmentRow, error) {
	e, err := activeEnrollment(ctx, s.Pool, participantID)
	if isMissing(err) {
		return nil, nil
	}
	return e, err
}

// TransitionEnrollment is a compare-and-set: the enrollment moves to `to` only
// if its current status is one of `from`. It reports whether a row changed.
func (s *Store) TransitionEnrollment(ctx context.Context, id string, from []domain.EnrollmentStatus, to domain.EnrollmentStatus) (bool, error) {
	allowed := make([]string, len(from))
	for i, status := range from {
		allowed[i] = string(status)
	}
	tag, err := s.Pool.Exec(ctx,
		`update enrollments
		 set status = $3::text,
		     released_at = case when $3::text = $4::text then now() else released_at end,
		     updated_at = now()
		 where id = $1 and status = any($2::text[])`,
		id, allowed, string(to), string(domain.EnrollmentReleased))
	if err != nil {
		if isMissing(err) {
			return false, nil
		}
		return false, wrap("transition enrollment", err)
	}
	return tag.RowsAffected() == 1, nil
}

// FlagEnrollment marks the enrollment for an operator without moving it, so
// its seat and its hold stay accounted for. The first reason stays; it
// reports whether this call set the flag.
func (s *Store) FlagEnrollment(ctx context.Context, id, reason string) (bool, error) {
	tag, err := s.Pool.Exec(ctx,
		`update enrollments set needs_attention_at = now(), attention_reason = $2, updated_at = now()
		 where id = $1 and needs_attention_at is null`, id, reason)
	if err != nil {
		if isMissing(err) {
			return false, nil
		}
		return false, wrap("flag enrollment", err)
	}
	return tag.RowsAffected() == 1, nil
}

// ClearEnrollmentFlag removes the operator flag; it reports whether there was one.
func (s *Store) ClearEnrollmentFlag(ctx context.Context, id string) (bool, error) {
	tag, err := s.Pool.Exec(ctx,
		`update enrollments set needs_attention_at = null, attention_reason = null, updated_at = now()
		 where id = $1 and needs_attention_at is not null`, id)
	if err != nil {
		if isMissing(err) {
			return false, nil
		}
		return false, wrap("clear enrollment flag", err)
	}
	return tag.RowsAffected() == 1, nil
}

// releasable is the condition, on enrollment e, of a seat ReleaseExpiredSeats
// releases at the time in parameter now (such as "$2"): SEAT_RESERVED,
// expired before it (domain.SeatExpired), and not kept for an authorization
// PayPal holds PENDING.
func releasable(now string) string {
	return `e.status = 'SEAT_RESERVED' and e.seat_expires_at < ` + now + `
		and coalesce((select a.authorization_status from payment_attempts a
		              where a.enrollment_id = e.id order by a.attempt_no desc limit 1), '') <> 'PENDING'`
}

// WorkshopIDsWithExpiredSeats returns, once each and in order, the
// workshops with a seat ReleaseExpiredSeats would release at now. It is an
// empty list when there are none.
func (s *Store) WorkshopIDsWithExpiredSeats(ctx context.Context, now time.Time) ([]string, error) {
	rows, err := s.Pool.Query(ctx,
		`select e.workshop_id::text from enrollments e where `+releasable("$1")+`
		 group by e.workshop_id order by e.workshop_id`, now)
	if err != nil {
		return nil, wrap("workshops with expired seats", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, wrap("workshops with expired seats", err)
	}
	if ids == nil {
		ids = []string{}
	}
	return ids, nil
}

// ReleaseReservedSeats moves every SEAT_RESERVED enrollment of the workshop
// to RELEASED, expired or not, except a seat kept for an authorization
// PayPal holds PENDING, and returns how many it released: for a workshop
// that will take no payment any more.
func (s *Store) ReleaseReservedSeats(ctx context.Context, workshopID string, now time.Time) (int, error) {
	tag, err := s.Pool.Exec(ctx,
		`update enrollments e set status = 'RELEASED', released_at = $2, updated_at = now()
		 where e.workshop_id = $1 and `+releasable("'infinity'::timestamptz"), workshopID, now)
	if err != nil {
		return 0, wrap("release reserved seats", err)
	}
	return int(tag.RowsAffected()), nil
}

// ReleaseExpiredSeats moves the workshop's SEAT_RESERVED enrollments whose seat
// expired before now to RELEASED and returns how many it released. A seat
// whose latest attempt holds an authorization PayPal reports PENDING (held
// for review) is kept past its expiry: the buyer approved, and the periodic
// reconcile moves it once PayPal decides.
func (s *Store) ReleaseExpiredSeats(ctx context.Context, workshopID string, now time.Time) (int, error) {
	tag, err := s.Pool.Exec(ctx,
		`update enrollments e
		 set status = $3, released_at = $2, updated_at = now()
		 where e.workshop_id = $1 and `+releasable("$2"),
		workshopID, now, string(domain.EnrollmentReleased))
	if err != nil {
		return 0, wrap("release expired seats", err)
	}
	return int(tag.RowsAffected()), nil
}

// MarkApprovalCancelled records that the buyer cancelled the enrollment's
// checkout at PayPal, setting approval_cancelled_at to now (a later cancel
// moves it forward). Only a SEAT_RESERVED enrollment is marked, and nothing
// else changes: the seat stays reserved until it expires on its own, so the
// buyer can still approve the same checkout. It reports whether a row was
// marked; a missing or malformed ID, or an enrollment past checkout, is false
// without error.
func (s *Store) MarkApprovalCancelled(ctx context.Context, enrollmentID string, now time.Time) (bool, error) {
	tag, err := s.Pool.Exec(ctx,
		`update enrollments set approval_cancelled_at = $2, updated_at = now()
		 where id = $1 and status = $3`,
		enrollmentID, now, string(domain.EnrollmentSeatReserved))
	if err != nil {
		if isMissing(err) {
			return false, nil
		}
		return false, wrap("mark approval cancelled", err)
	}
	return tag.RowsAffected() == 1, nil
}
