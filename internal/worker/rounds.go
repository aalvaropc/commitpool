package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
)

// casRetries bounds how often a decision is taken again when the workshop
// changed between reading it and writing the outcome.
const casRetries = 3

var errWorkshopMoving = errors.New("worker: the workshop kept changing while a decision about it was being recorded")

// view is a workshop as the domain rules see it, with what the worker needs
// beside each enrollment: its row, its latest attempt (nil when it has none)
// and that attempt's jobs by key. The slices share one order, the
// enrollments' creation order.
type view struct {
	w    domain.Workshop
	p    domain.Proposal
	es   []domain.Enrollment
	rows []store.EnrollmentRow
	att  []*store.AttemptRow
	jobs []map[string]store.Job
}

// job returns the job of this kind for enrollment i's latest attempt.
func (v view) job(i int, kind string) (store.Job, bool) {
	if v.att[i] == nil {
		return store.Job{}, false
	}
	j, ok := v.jobs[i][store.JobKey(kind, v.att[i].ID)]
	return j, ok
}

func (v view) has(i int, kind string) bool {
	_, ok := v.job(i, kind)
	return ok
}

// workshopView loads the workshop, its active proposal and its enrollments.
// An enrollment's CaptureStartedAt is set, to when the job was due, once a
// capture:{attempt} job exists for its latest attempt, whatever that job's
// status; an enrollment without an attempt carries only its status.
func (r *Runner) workshopView(ctx context.Context, workshopID string) (view, error) {
	wr, err := r.st.WorkshopByID(ctx, workshopID)
	if err != nil {
		return view{}, err
	}
	pr, err := r.st.ActiveProposal(ctx, workshopID)
	if err != nil {
		return view{}, err
	}
	if pr == nil {
		return view{}, fmt.Errorf("proposal of workshop %s: %w", workshopID, store.ErrNotFound)
	}
	rows, err := r.st.EnrollmentsByWorkshop(ctx, workshopID)
	if err != nil {
		return view{}, err
	}
	v := view{
		w: domain.Workshop{
			ID: wr.ID, Title: wr.Title, Timezone: wr.Timezone,
			Minimum: wr.Minimum, Capacity: wr.Capacity,
			Price:         domain.Money{Minor: wr.PriceMinor, Currency: wr.Currency},
			Status:        wr.Status,
			Resolution:    wr.Resolution,
			PaymentWindow: time.Duration(wr.PaymentWindowMin) * time.Minute,
		},
		p: domain.Proposal{
			ID: pr.ID, Version: pr.Version, DateID: pr.ChosenDateID,
			OpensAt: pr.OpensAt, ClosesAt: pr.ClosesAt, StartsAt: pr.StartsAt,
			Price:   domain.Money{Minor: pr.PriceMinor, Currency: pr.Currency},
			Minimum: pr.Minimum, Capacity: pr.Capacity,
		},
		es:   make([]domain.Enrollment, len(rows)),
		rows: rows,
		att:  make([]*store.AttemptRow, len(rows)),
		jobs: make([]map[string]store.Job, len(rows)),
	}
	for i, row := range rows {
		v.jobs[i] = map[string]store.Job{}
		if v.att[i], err = r.st.LatestAttempt(ctx, row.ID); err != nil {
			return view{}, err
		}
		if v.att[i] != nil {
			jobs, err := r.st.JobsByAttempt(ctx, v.att[i].ID)
			if err != nil {
				return view{}, err
			}
			for _, j := range jobs {
				v.jobs[i][j.IdempotencyKey] = j
			}
		}
		v.es[i] = row.Domain()
		if j, ok := v.job(i, store.KindCapture); ok {
			due := j.RunAt
			v.es[i].CaptureStartedAt = &due
		}
	}
	return v, nil
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
	v, err := r.workshopView(ctx, workshopID)
	if err != nil {
		return err
	}
	now := r.clock.Now()
	round := domain.ShouldStartCaptureRound(v.w, v.p, domain.Counts(v.es), now)
	if !round && (v.w.Status != domain.WorkshopConfirmed || !now.Before(v.p.ClosesAt)) {
		return nil
	}
	enqueued := 0
	for _, en := range v.rows {
		if en.Status != domain.EnrollmentAuthorized {
			continue
		}
		created, err := r.enqueueLatest(ctx, store.KindCapture, en, "")
		if err != nil {
			return err
		}
		if created {
			enqueued++
		}
	}
	if enqueued > 0 {
		r.log.Info("captures enqueued", "workshop_id", workshopID, "workshop_status", v.w.Status, "count", enqueued)
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
	return r.EnqueueOrRevive(ctx, kind, store.JobKey(kind, a.ID)+suffix, &en.WorkshopID, &a.ID, nil, r.clock.Now())
}

// afterMove runs what an enrollment's move sets off: a new authorization is
// captured or, once payments are closed, voided; a completed payment may
// confirm the workshop or have to be refunded; a failed capture may leave the
// round short of the minimum; an enrollment that needs an operator flags a
// confirmed workshop; a finished enrollment may settle the workshop.
// It only runs for a move that happened, so applying the same PayPal answer
// again sets off nothing.
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
		return r.afterAuthorized(ctx, en)
	case domain.EnrollmentPaid:
		return r.afterPaid(ctx, en)
	case domain.EnrollmentCaptureFailed:
		return errors.Join(r.StartCaptureRoundIfReady(ctx, en.WorkshopID), r.SettleIfResolved(ctx, en.WorkshopID))
	case domain.EnrollmentNeedsAttention, domain.EnrollmentRefundFailed:
		return errors.Join(r.flagIfConfirmed(ctx, en.WorkshopID), r.SettleIfResolved(ctx, en.WorkshopID))
	default:
		return r.SettleIfResolved(ctx, en.WorkshopID)
	}
}

// afterAuthorized starts the captures a new authorization makes ready. An
// authorization that lands once the workshop is cancelled or payments have
// closed is a hold nobody will capture, so it is voided under the same
// void:{attempt} key close would use.
func (r *Runner) afterAuthorized(ctx context.Context, en store.EnrollmentRow) error {
	open, status, err := r.paymentsOpen(ctx, en, r.clock.Now())
	if err != nil {
		return err
	}
	if open {
		return r.StartCaptureRoundIfReady(ctx, en.WorkshopID)
	}
	r.log.Warn("authorization landed after payments closed; voiding it",
		"workshop_id", en.WorkshopID, "enrollment_id", en.ID, "workshop_status", status)
	_, err = r.enqueueLatest(ctx, store.KindVoid, en, "")
	return err
}

// afterPaid acts on a payment that has just completed. In a FUNDING
// workshop it applies domain.AfterCaptureResult (confirmIfFunded). A payment
// completed after the workshop was cancelled is refunded
// (domain.LateCaptureAfterCancel), and one completed after it was confirmed
// keeps its seat until the workshop starts and is refunded after that
// (domain.LateCaptureAfterConfirm). That refund is decided from the
// workshop, its proposal and this enrollment alone, since nothing marks a
// payment as late once this hook has passed. When the workshop leaves
// FUNDING while this decides, it decides again from the new state.
func (r *Runner) afterPaid(ctx context.Context, en store.EnrollmentRow) error {
	paid := en.Domain()
	for range casRetries {
		w, err := r.st.WorkshopByID(ctx, en.WorkshopID)
		if err != nil {
			return err
		}
		switch w.Status {
		case domain.WorkshopFunding:
			done, err := r.confirmIfFunded(ctx, en.WorkshopID)
			if err != nil || done {
				return err
			}
		case domain.WorkshopCancelled:
			return r.lateCapture(ctx, en, w.Status, domain.LateCaptureAfterCancel(paid))
		case domain.WorkshopConfirmed:
			p, err := r.st.ProposalByID(ctx, en.ProposalID)
			if err != nil {
				return err
			}
			if p == nil {
				return fmt.Errorf("proposal %s: %w", en.ProposalID, store.ErrNotFound)
			}
			return r.lateCapture(ctx, en, w.Status, domain.LateCaptureAfterConfirm(paid, p.StartsAt, r.clock.Now()))
		default:
			return nil
		}
	}
	return errWorkshopMoving
}

// confirmIfFunded applies domain.AfterCaptureResult to a FUNDING workshop:
// it is confirmed, with the resolution fundingResolution gives, by
// compare-and-set from the status and resolution it read, once enough
// payments have completed; short of that the capture round is evaluated
// again, since authorizations that arrived while a capture was in flight may
// now reach the minimum. done is false when the workshop was no longer
// FUNDING or changed meanwhile.
func (r *Runner) confirmIfFunded(ctx context.Context, workshopID string) (done bool, err error) {
	v, err := r.workshopView(ctx, workshopID)
	if err != nil || v.w.Status != domain.WorkshopFunding {
		return false, err
	}
	t := domain.Counts(v.es)
	status, resolution := domain.AfterCaptureResult(v.w, v.p, t)
	if status == v.w.Status && resolution == v.w.Resolution {
		return true, r.StartCaptureRoundIfReady(ctx, workshopID)
	}
	resolution = fundingResolution(v, status, resolution)
	ok, err := r.st.TransitionWorkshop(ctx, workshopID,
		[]domain.WorkshopStatus{domain.WorkshopFunding}, []domain.Resolution{v.w.Resolution}, status, resolution)
	if ok {
		r.log.Info("workshop confirmed: enough payments completed", "workshop_id", workshopID, "paid", t.Paid, "minimum", v.p.Minimum)
	}
	return ok, err
}

func (r *Runner) lateCapture(ctx context.Context, en store.EnrollmentRow, status domain.WorkshopStatus, action domain.Action) error {
	log := r.log.With("workshop_id", en.WorkshopID, "enrollment_id", en.ID, "workshop_status", status)
	switch action {
	case domain.ActionRefund:
		log.Warn("payment completed too late to keep; refunding it")
		_, err := r.enqueueLatest(ctx, store.KindRefund, en, "")
		return err
	case domain.ActionKeepSeat:
		log.Info("payment completed after the workshop was confirmed; it keeps its seat")
	}
	return nil
}
