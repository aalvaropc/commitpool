package domain

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

var (
	ErrNotFunding               = errors.New("reservations are not open for a class still short of its minimum")
	ErrTooLateToExtend          = errors.New("too little time is left before reservations close")
	ErrAlreadyExtended          = errors.New("the deadline was already extended once")
	ErrAlreadyFunded            = errors.New("completed payments already reach the minimum")
	ErrChargingStarted          = errors.New("charging has started, so someone could no longer leave")
	ErrConsentPredatesExtension = errors.New("someone reserved on terms that did not allow an extension")
	ErrExtensionTooSmall        = errors.New("the extension would add less than an hour")
)

var ExtensionConsents = []string{"consent-v5"}

const (
	ExtensionStep    = 15 * time.Minute
	MinExtensionGain = time.Hour
	MinLeftToExtend  = 10 * time.Minute
	RescueWhenLeft   = 6 * time.Hour
)

type ExtensionCheck struct {
	Status      WorkshopStatus
	Proposal    Proposal
	Extended    bool
	Enrollments []Enrollment
	Holds       []time.Time
	Now         time.Time
}

func LatestDeadline(p Proposal) time.Time {
	return earlierOf(p.OpensAt.Add(MaxPaymentWindow), p.StartsAt.Add(-minLeadAfterClose)).Truncate(ExtensionStep)
}

func ExtendedDeadline(p Proposal, holds []time.Time) time.Time {
	limit := LatestDeadline(p)
	for _, h := range holds {
		limit = earlierOf(limit, h.Add(MaxPaymentWindow).Truncate(ExtensionStep))
	}
	return limit
}

func LatestPossibleDeadline(c ExtensionCheck) time.Time {
	p := c.Proposal
	if bound := LatestDeadline(p); deadlineMayMove(c) && bound.Sub(p.ClosesAt) >= MinExtensionGain {
		return bound
	}
	return p.ClosesAt
}

func deadlineMayMove(c ExtensionCheck) bool {
	return c.Status == WorkshopFunding && !c.Extended &&
		c.Proposal.ClosesAt.Sub(c.Now) >= MinLeftToExtend &&
		Counts(c.Enrollments).Paid == 0
}

func CanExtend(c ExtensionCheck) (time.Time, error) {
	p := c.Proposal
	if c.Status != WorkshopFunding || !PaymentsOpen(Workshop{Status: c.Status}, p, c.Now) {
		return time.Time{}, fmt.Errorf("%w: status %s", ErrNotFunding, c.Status)
	}
	if left := p.ClosesAt.Sub(c.Now); left < MinLeftToExtend {
		return time.Time{}, fmt.Errorf("%w: %s left", ErrTooLateToExtend, left.Round(time.Second))
	}
	if c.Extended {
		return time.Time{}, ErrAlreadyExtended
	}
	t := Counts(c.Enrollments)
	if t.Paid >= p.Minimum {
		return time.Time{}, fmt.Errorf("%w: %d paid, minimum %d", ErrAlreadyFunded, t.Paid, p.Minimum)
	}
	if t.Paid+t.Capturing+t.Pending > 0 || slices.ContainsFunc(c.Enrollments, captureStarted) {
		return time.Time{}, fmt.Errorf("%w: %d paid, %d being charged", ErrChargingStarted, t.Paid, t.Capturing+t.Pending)
	}
	if t.Authorized >= p.Minimum {
		return time.Time{}, fmt.Errorf("%w: %d holds reach the minimum of %d", ErrChargingStarted, t.Authorized, p.Minimum)
	}
	for _, e := range c.Enrollments {
		if e.Status.Occupies() && !slices.Contains(ExtensionConsents, e.ConsentVersion) {
			return time.Time{}, fmt.Errorf("%w: enrollment %s accepted %q", ErrConsentPredatesExtension, e.ID, e.ConsentVersion)
		}
	}
	to := ExtendedDeadline(p, c.Holds)
	if gain := to.Sub(p.ClosesAt); gain < MinExtensionGain {
		return time.Time{}, fmt.Errorf("%w: %s", ErrExtensionTooSmall, gain)
	}
	return to, nil
}

func captureStarted(e Enrollment) bool {
	return e.CaptureStartedAt != nil && e.Status.Occupies()
}

func earlierOf(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}
