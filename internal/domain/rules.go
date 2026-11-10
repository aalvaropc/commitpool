package domain

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrCapacityFull          = errors.New("capacity full")
	ErrWorkshopNotCollecting = errors.New("workshop is not collecting availability")
	ErrWindowMismatch        = errors.New("payment window does not match the workshop setting")
	ErrWindowTooLong         = errors.New("payment window is longer than PayPal's authorization honour period")
	ErrStartsAtMismatch      = errors.New("proposal start does not match the chosen date")
	ErrStartsTooSoon         = errors.New("workshop must start at least 24h after payments close")
	ErrMinimumAboveCapacity  = errors.New("minimum exceeds capacity")
	ErrNotEnoughCompatible   = errors.New("not enough compatible participants")
	ErrAlreadyCancelled      = errors.New("workshop is already cancelled")
	ErrWorkshopStarted       = errors.New("workshop has already started")
	ErrPayoutLocked          = fmt.Errorf("%w: the organizer's payout has started", ErrWorkshopStarted)
	ErrNothingToWithdraw     = errors.New("no seat to withdraw")
	ErrCaptureInProgress     = errors.New("the payment is being charged")
	ErrPaidNoSelfRefund      = errors.New("a paid seat is refunded only if the workshop is cancelled")
	ErrHoldUnderReview       = errors.New("PayPal is reviewing the hold")
	ErrNeedsOperator         = errors.New("the enrollment needs an operator")
	ErrNotRefundFailed       = errors.New("the enrollment's refund has not failed")
	ErrStillUnresolved       = errors.New("something still needs an operator or is still in flight")
)

const minLeadAfterClose = 24 * time.Hour

const MaxPaymentWindow = 72 * time.Hour

func Counts(es []Enrollment) Tally {
	var t Tally
	for _, e := range es {
		switch e.Status {
		case EnrollmentSeatReserved:
			t.Reserved++
		case EnrollmentAuthorized:
			t.Authorized++
		case EnrollmentCapturing:
			t.Capturing++
		case EnrollmentCapturePending:
			t.Pending++
		case EnrollmentPaid:
			t.Paid++
		case EnrollmentCaptureFailed:
			t.Failed++
		}
		if e.Status.Occupies() {
			t.Occupying++
		}
	}
	return t
}

func ReserveSeat(t Tally, capacity int) error {
	if t.Occupying >= capacity {
		return ErrCapacityFull
	}
	return nil
}

func ValidateProposal(w Workshop, p Proposal, date CandidateDate, compatible int) error {
	if w.Status != WorkshopCollecting {
		return fmt.Errorf("%w: status is %s", ErrWorkshopNotCollecting, w.Status)
	}
	if w.PaymentWindow > MaxPaymentWindow {
		return fmt.Errorf("%w: %s, at most %s", ErrWindowTooLong, w.PaymentWindow, MaxPaymentWindow)
	}
	if want := p.OpensAt.Add(w.PaymentWindow); !p.ClosesAt.Equal(want) {
		return fmt.Errorf("%w: closes at %s, want %s", ErrWindowMismatch, p.ClosesAt.Format(time.RFC3339), want.Format(time.RFC3339))
	}
	if !p.StartsAt.Equal(date.StartsAt) {
		return fmt.Errorf("%w: proposal starts at %s, date starts at %s", ErrStartsAtMismatch, p.StartsAt.Format(time.RFC3339), date.StartsAt.Format(time.RFC3339))
	}
	if earliest := p.ClosesAt.Add(minLeadAfterClose); p.StartsAt.Before(earliest) {
		return fmt.Errorf("%w: starts at %s, earliest allowed %s", ErrStartsTooSoon, p.StartsAt.Format(time.RFC3339), earliest.Format(time.RFC3339))
	}
	if p.Minimum > p.Capacity {
		return fmt.Errorf("%w: minimum %d, capacity %d", ErrMinimumAboveCapacity, p.Minimum, p.Capacity)
	}
	if compatible < p.Minimum {
		return fmt.Errorf("%w: %d compatible, minimum %d", ErrNotEnoughCompatible, compatible, p.Minimum)
	}
	return nil
}

func EffectiveStart(startsAt time.Time, payoutLockedAt *time.Time) time.Time {
	if payoutLockedAt != nil && payoutLockedAt.Before(startsAt) {
		return *payoutLockedAt
	}
	return startsAt
}

func CanCancel(status WorkshopStatus, startsAt, payoutLockedAt *time.Time, now time.Time) error {
	if status == WorkshopCancelled {
		return ErrAlreadyCancelled
	}
	if payoutLockedAt != nil {
		return fmt.Errorf("%w at %s", ErrPayoutLocked, payoutLockedAt.Format(time.RFC3339))
	}
	if startsAt != nil && !now.Before(*startsAt) {
		return fmt.Errorf("%w: it started at %s", ErrWorkshopStarted, startsAt.Format(time.RFC3339))
	}
	return nil
}

func PaymentsOpen(w Workshop, p Proposal, now time.Time) bool {
	return (w.Status == WorkshopFunding || w.Status == WorkshopConfirmed) && now.Before(p.ClosesAt)
}

func ShouldStartCaptureRound(w Workshop, p Proposal, t Tally, now time.Time) bool {
	return w.Status == WorkshopFunding &&
		now.Before(p.ClosesAt) &&
		t.Paid < p.Minimum &&
		t.Paid+t.Authorized >= p.Minimum
}

func AfterCaptureResult(w Workshop, p Proposal, t Tally) (WorkshopStatus, Resolution) {
	if w.Status == WorkshopFunding && t.Paid >= p.Minimum {
		return WorkshopConfirmed, ResolutionOpen
	}
	return w.Status, w.Resolution
}

type Close struct {
	NewStatus                  WorkshopStatus
	Resolution                 Resolution
	Voids, Refunds, Reconciles []string
}

func CloseDecision(w Workshop, p Proposal, es []Enrollment) Close {
	c := Close{NewStatus: w.Status, Resolution: w.Resolution, Voids: []string{}, Refunds: []string{}, Reconciles: []string{}}
	switch {
	case w.Status == WorkshopConfirmed,
		w.Status == WorkshopFunding && Counts(es).Paid >= p.Minimum:
		c.NewStatus = WorkshopConfirmed
		for _, e := range es {
			switch {
			case e.Status == EnrollmentAuthorized && e.CaptureStartedAt == nil:
				c.Voids = append(c.Voids, e.ID)
			case inFlight(e):
				c.Reconciles = append(c.Reconciles, e.ID)
			}
		}
	case w.Status == WorkshopFunding:
		c.NewStatus = WorkshopCancelled
		c.Resolution = ResolutionResolving
		for _, e := range es {
			switch {
			case e.Status == EnrollmentAuthorized:
				c.Voids = append(c.Voids, e.ID)
			case e.Status == EnrollmentPaid:
				c.Refunds = append(c.Refunds, e.ID)
			case inFlight(e):
				c.Reconciles = append(c.Reconciles, e.ID)
			}
		}
	}
	return c
}

func inFlight(e Enrollment) bool {
	return e.Status == EnrollmentCapturing || e.Status == EnrollmentCapturePending
}

type Action string

const (
	ActionNone     Action = "none"
	ActionRefund   Action = "refund"
	ActionKeepSeat Action = "keep_seat"
)

func LateCaptureAfterCancel(e Enrollment) Action {
	if e.Status == EnrollmentPaid {
		return ActionRefund
	}
	return ActionNone
}

func LateCaptureAfterConfirm(e Enrollment, startsAt time.Time, payoutLockedAt *time.Time, now time.Time) Action {
	if e.Status != EnrollmentPaid {
		return ActionNone
	}
	if now.Before(EffectiveStart(startsAt, payoutLockedAt)) {
		return ActionKeepSeat
	}
	return ActionRefund
}

func CaptureDeadline(closesAt, authExpiresAt time.Time) time.Time {
	if authExpiresAt.IsZero() || closesAt.Before(authExpiresAt) {
		return closesAt
	}
	return authExpiresAt
}
