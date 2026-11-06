package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
)

const casRetries = 3

var errWorkshopMoving = errors.New("worker: the workshop kept changing while a decision about it was being recorded")

type view struct {
	w    domain.Workshop
	p    domain.Proposal
	es   []domain.Enrollment
	rows []store.EnrollmentRow
	att  []*store.AttemptRow
	jobs []map[string]store.Job
}

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
	latest, err := r.st.LatestAttemptsByWorkshop(ctx, workshopID)
	if err != nil {
		return view{}, err
	}
	ids := make([]string, 0, len(latest))
	for _, a := range latest {
		ids = append(ids, a.ID)
	}
	jobs, err := r.st.JobsByAttempts(ctx, ids)
	if err != nil {
		return view{}, err
	}
	byAttempt := make(map[string]map[string]store.Job, len(latest))
	for _, j := range jobs {
		if byAttempt[*j.AttemptID] == nil {
			byAttempt[*j.AttemptID] = map[string]store.Job{}
		}
		byAttempt[*j.AttemptID][j.IdempotencyKey] = j
	}
	for i, row := range rows {
		v.jobs[i] = map[string]store.Job{}
		if v.att[i] = latest[row.ID]; v.att[i] != nil && byAttempt[v.att[i].ID] != nil {
			v.jobs[i] = byAttempt[v.att[i].ID]
		}
		v.es[i] = row.Domain()
		if j, ok := v.job(i, store.KindCapture); ok {
			due := j.RunAt
			v.es[i].CaptureStartedAt = &due
		}
	}
	return v, nil
}

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
		err := r.afterPaid(ctx, en)
		if w, werr := r.st.WorkshopByID(ctx, en.WorkshopID); werr == nil && w.Status == domain.WorkshopConfirmed {
			r.notifyEnrollment(ctx, en.ID, NotifyConfirmed)
		}
		return err
	case domain.EnrollmentCaptureFailed:
		r.notifyEnrollment(ctx, en.ID, NotifyPaymentFailed)
		return errors.Join(r.StartCaptureRoundIfReady(ctx, en.WorkshopID), r.SettleIfResolved(ctx, en.WorkshopID))
	case domain.EnrollmentRefunded:
		r.notifyEnrollment(ctx, en.ID, NotifyRefundCompleted)
		return r.SettleIfResolved(ctx, en.WorkshopID)
	case domain.EnrollmentNeedsAttention, domain.EnrollmentRefundFailed:
		return errors.Join(r.flagIfConfirmed(ctx, en.WorkshopID), r.SettleIfResolved(ctx, en.WorkshopID))
	default:
		return r.SettleIfResolved(ctx, en.WorkshopID)
	}
}

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
		if status == domain.WorkshopConfirmed {
			for _, en := range v.rows {
				if en.Status == domain.EnrollmentPaid {
					r.notifyEnrollment(ctx, en.ID, NotifyConfirmed)
				}
			}
		}
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
