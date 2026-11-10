package worker

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/notice"
	"github.com/aalvaropc/commitpool/internal/present"
	"github.com/aalvaropc/commitpool/internal/store"
)

const NotifyDeadlineExtended = string(notice.DeadlineExtended)

const earlyCloseTolerance = time.Minute

func (r *Runner) ExtendDeadline(ctx context.Context, proposalID string, from, to time.Time) (store.ProposalRow, error) {
	p, err := r.st.ExtendPaymentWindow(ctx, proposalID, from, to, r.clock.Now())
	if err != nil {
		return store.ProposalRow{}, err
	}
	r.log.Info("deadline extended", "workshop_id", p.WorkshopID, "proposal_id", p.ID,
		"from", from.UTC().Format(time.RFC3339), "to", p.ClosesAt.UTC().Format(time.RFC3339))
	if err := r.queueDeadlineExtended(ctx, p); err != nil {
		r.log.Error("notify: could not queue the deadline notices", "workshop_id", p.WorkshopID, "error", err)
	}
	return p, nil
}

func (r *Runner) queueDeadlineExtended(ctx context.Context, p store.ProposalRow) error {
	if p.ExtendedFrom == nil {
		return nil
	}
	w, err := r.st.WorkshopByID(ctx, p.WorkshopID)
	if err != nil {
		return err
	}
	d, err := r.noticeData(ctx, w)
	if err != nil {
		return err
	}
	loc, err := time.LoadLocation(w.Timezone)
	if err != nil {
		loc = time.UTC
	}
	d.Earlier = present.Moment(*p.ExtendedFrom, loc)
	enrollments, err := r.st.EnrollmentsByWorkshop(ctx, w.ID)
	if err != nil {
		return err
	}
	var errs []error
	for _, en := range enrollments {
		if !en.Status.Occupies() {
			continue
		}
		name, email, err := r.st.ParticipantEmail(ctx, en.ParticipantID)
		if err != nil || email == "" {
			errs = append(errs, err)
			continue
		}
		pd := d
		pd.Name = name
		errs = append(errs, r.queueNotice(ctx, notice.DeadlineExtended, pd, store.Notification{
			WorkshopID: w.ID, ParticipantID: en.ParticipantID, To: email, Key: "notify:" + NotifyDeadlineExtended + ":" + en.ID,
		}))
	}
	return errors.Join(errs...)
}

func (r *Runner) deadlineNoticeStale(ctx context.Context, n *store.NotificationRow) (bool, error) {
	w, err := r.st.WorkshopByID(ctx, n.WorkshopID)
	if err != nil || w.Status != domain.WorkshopFunding {
		return err == nil, err
	}
	en, err := r.st.EnrollmentByID(ctx, strings.TrimPrefix(n.Key, "notify:"+NotifyDeadlineExtended+":"))
	if err != nil {
		return false, err
	}
	return !en.Status.Occupies(), nil
}

func (r *Runner) closeTooEarly(ctx context.Context, job *store.Job) error {
	proposalID, ok := strings.CutPrefix(job.IdempotencyKey, store.JobKey(store.KindClose, ""))
	if !ok {
		return nil
	}
	p, err := r.st.ProposalByID(ctx, proposalID)
	if err != nil || p == nil {
		return err
	}
	now := r.clock.Now()
	if !now.Before(p.ClosesAt.Add(-earlyCloseTolerance)) {
		return nil
	}
	r.jobLog(job).Warn("close ran before its deadline; refused without closing, the periodic work runs it once it is due",
		"workshop_id", p.WorkshopID, "closes_at", p.ClosesAt.UTC().Format(time.RFC3339))
	return &refusal{reason: "skipped: payments close at " + p.ClosesAt.UTC().Format(time.RFC3339)}
}
