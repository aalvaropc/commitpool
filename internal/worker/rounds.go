package worker

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
)

// workshopView loads the workshop, its active proposal and its enrollments as
// the domain rules take them, with the enrollment rows in the same order. An
// enrollment's CaptureStartedAt is set, to when the job was due, once a
// capture:{attempt} job exists for its latest attempt, whatever that job's
// status; an enrollment without an attempt carries only its status.
func (r *Runner) workshopView(ctx context.Context, workshopID string) (domain.Workshop, domain.Proposal, []domain.Enrollment, []store.EnrollmentRow, error) {
	wr, err := r.st.WorkshopByID(ctx, workshopID)
	if err != nil {
		return domain.Workshop{}, domain.Proposal{}, nil, nil, err
	}
	pr, err := r.st.ActiveProposal(ctx, workshopID)
	if err != nil {
		return domain.Workshop{}, domain.Proposal{}, nil, nil, err
	}
	if pr == nil {
		return domain.Workshop{}, domain.Proposal{}, nil, nil, fmt.Errorf("proposal of workshop %s: %w", workshopID, store.ErrNotFound)
	}
	rows, err := r.st.EnrollmentsByWorkshop(ctx, workshopID)
	if err != nil {
		return domain.Workshop{}, domain.Proposal{}, nil, nil, err
	}
	es := make([]domain.Enrollment, len(rows))
	for i, row := range rows {
		es[i] = domain.Enrollment{ID: row.ID, ParticipantID: row.ParticipantID, Status: row.Status, SeatExpiresAt: row.SeatExpiresAt}
		if es[i].CaptureStartedAt, err = r.captureStartedAt(ctx, row.ID); err != nil {
			return domain.Workshop{}, domain.Proposal{}, nil, nil, err
		}
	}
	w := domain.Workshop{
		ID: wr.ID, Title: wr.Title, Timezone: wr.Timezone,
		Minimum: wr.Minimum, Capacity: wr.Capacity,
		Price:         domain.Money{Minor: wr.PriceMinor, Currency: wr.Currency},
		Status:        wr.Status,
		Resolution:    wr.Resolution,
		PaymentWindow: time.Duration(wr.PaymentWindowMin) * time.Minute,
	}
	p := domain.Proposal{
		ID: pr.ID, Version: pr.Version, DateID: pr.ChosenDateID,
		OpensAt: pr.OpensAt, ClosesAt: pr.ClosesAt, StartsAt: pr.StartsAt,
		Price:   domain.Money{Minor: pr.PriceMinor, Currency: pr.Currency},
		Minimum: pr.Minimum, Capacity: pr.Capacity,
	}
	return w, p, es, rows, nil
}

func (r *Runner) captureStartedAt(ctx context.Context, enrollmentID string) (*time.Time, error) {
	a, err := r.st.LatestAttempt(ctx, enrollmentID)
	if err != nil || a == nil {
		return nil, err
	}
	j, err := r.st.JobByKey(ctx, kindCapture+":"+a.ID)
	if err != nil || j == nil {
		return nil, err
	}
	due := j.RunAt
	return &due, nil
}

// StartCaptureRoundIfReady enqueues, due now, the captures the workshop is
// ready for. In a FUNDING workshop that is once the authorized and the paid
// enrollments together reach the minimum before payments close
// (domain.ShouldStartCaptureRound): one capture:{attempt} for every AUTHORIZED
// enrollment. In a CONFIRMED workshop, until payments close, every AUTHORIZED
// enrollment is captured; the others already have their capture job, so in
// practice that is the one just authorized. Nothing is enqueued for a
// cancelled workshop or once payments have closed. Captures are keyed by
// their attempt, so calling it again enqueues nothing twice.
func (r *Runner) StartCaptureRoundIfReady(ctx context.Context, workshopID string) error {
	w, p, es, rows, err := r.workshopView(ctx, workshopID)
	if err != nil {
		return err
	}
	now := r.clock.Now()
	round := domain.ShouldStartCaptureRound(w, p, domain.Counts(es), now)
	if !round && (w.Status != domain.WorkshopConfirmed || !now.Before(p.ClosesAt)) {
		return nil
	}
	enqueued := 0
	for _, en := range rows {
		if en.Status != domain.EnrollmentAuthorized {
			continue
		}
		created, err := r.enqueueLatest(ctx, kindCapture, en, "")
		if err != nil {
			return err
		}
		if created {
			enqueued++
		}
	}
	if enqueued > 0 {
		r.log.Info("captures enqueued", "workshop_id", workshopID, "workshop_status", w.Status, "count", enqueued)
	}
	return nil
}

// enqueueLatest enqueues, or revives, the job of this kind for the
// enrollment's latest attempt, the one the enrollment follows, due now under
// the key {kind}:{attempt}{suffix}.
func (r *Runner) enqueueLatest(ctx context.Context, kind string, en store.EnrollmentRow, suffix string) (bool, error) {
	a, err := r.st.LatestAttempt(ctx, en.ID)
	if err != nil {
		return false, err
	}
	if a == nil {
		return false, fmt.Errorf("enrollment %s has no payment attempt to %s", en.ID, kind)
	}
	return r.EnqueueOrRevive(ctx, kind, kind+":"+a.ID+suffix, &en.WorkshopID, &a.ID, nil, r.clock.Now())
}

// afterMove runs what an enrollment's move sets off: a new authorization may
// start a capture round, a completed payment may confirm the workshop or have
// to be refunded, and a finished enrollment may settle the workshop. It only
// runs for a move that happened, so applying the same PayPal answer again
// sets off nothing.
func (r *Runner) afterMove(ctx context.Context, enrollmentID string, to domain.EnrollmentStatus) error {
	if to != domain.EnrollmentAuthorized && to != domain.EnrollmentPaid && !to.Terminal() {
		return nil
	}
	en, err := r.st.EnrollmentByID(ctx, enrollmentID)
	if err != nil {
		return err
	}
	switch to {
	case domain.EnrollmentAuthorized:
		return r.StartCaptureRoundIfReady(ctx, en.WorkshopID)
	case domain.EnrollmentPaid:
		return r.afterPaid(ctx, en)
	default:
		return r.SettleIfResolved(ctx, en.WorkshopID)
	}
}

// afterPaid acts on a payment that has just completed. A FUNDING workshop is
// confirmed once enough payments have completed (domain.AfterCaptureResult).
// A payment completed after the workshop was cancelled is refunded
// (domain.LateCaptureAfterCancel), and one completed after it was confirmed
// keeps its seat until the workshop starts and is refunded after that
// (domain.LateCaptureAfterConfirm).
func (r *Runner) afterPaid(ctx context.Context, en store.EnrollmentRow) error {
	w, p, es, _, err := r.workshopView(ctx, en.WorkshopID)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(es, func(e domain.Enrollment) bool { return e.ID == en.ID })
	if i < 0 {
		return fmt.Errorf("enrollment %s of workshop %s: %w", en.ID, en.WorkshopID, store.ErrNotFound)
	}
	log := r.log.With("workshop_id", w.ID, "enrollment_id", en.ID)
	action := domain.ActionNone
	switch w.Status {
	case domain.WorkshopFunding:
		t := domain.Counts(es)
		status, resolution := domain.AfterCaptureResult(w, p, t)
		if status == w.Status && resolution == w.Resolution {
			return nil
		}
		log.Info("workshop confirmed: enough payments completed", "paid", t.Paid, "minimum", p.Minimum)
		return r.st.SetWorkshopStatus(ctx, w.ID, status, resolution)
	case domain.WorkshopCancelled:
		action = domain.LateCaptureAfterCancel(es[i])
	case domain.WorkshopConfirmed:
		action = domain.LateCaptureAfterConfirm(es[i], p.StartsAt, r.clock.Now())
	}
	switch action {
	case domain.ActionRefund:
		log.Warn("payment completed too late to keep; refunding it", "workshop_status", w.Status)
		_, err := r.enqueueLatest(ctx, kindRefund, en, "")
		return err
	case domain.ActionKeepSeat:
		log.Info("payment completed after the workshop was confirmed; it keeps its seat")
	}
	return nil
}
