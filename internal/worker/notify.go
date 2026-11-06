package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/mail"
	"github.com/aalvaropc/commitpool/internal/notice"
	"github.com/aalvaropc/commitpool/internal/present"
	"github.com/aalvaropc/commitpool/internal/store"
)

const (
	NotifyJoined          = string(notice.Joined)
	NotifyReserved        = string(notice.Reserved)
	NotifyConfirmed       = string(notice.Confirmed)
	NotifyCancelled       = string(notice.Cancelled)
	NotifyPaymentFailed   = string(notice.PaymentFailed)
	NotifyRefundCompleted = string(notice.RefundCompleted)
	NotifyOwnerCreated    = string(notice.OwnerCreated)
	NotifyOwnerConfirmed  = string(notice.OwnerConfirmed)
	NotifyOwnerCancelled  = string(notice.OwnerCancelled)
)

var carriesPrivateLink = map[string]bool{NotifyJoined: true, NotifyOwnerCreated: true}

func (r *Runner) notify(ctx context.Context, job *store.Job) (*int, error) {
	n, err := r.st.NotificationByKey(ctx, job.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if n == nil || n.Status != "pending" {
		return nil, nil
	}
	attachments, err := r.attachments(ctx, n)
	if err != nil {
		return nil, err
	}
	id, err := r.mailer.Send(ctx, mail.Message{To: n.To, Subject: n.Subject, Text: n.Body, HTML: n.HTML, Attachments: attachments})
	if errors.Is(err, mail.ErrRejected) {
		r.log.Warn("notification rejected by the mail provider; not sent again", "kind", n.Kind, "to", mail.Mask(n.To), "error", err)
		if err := r.st.MarkNotificationFailed(ctx, n.Key); err != nil {
			return nil, err
		}
		return nil, &refusal{reason: err.Error(), issue: "MAIL_REJECTED"}
	}
	if err != nil {
		return nil, err
	}
	return nil, r.st.MarkNotificationSent(ctx, n.Key, id, r.clock.Now(), carriesPrivateLink[n.Kind])
}

func (r *Runner) attachments(context.Context, *store.NotificationRow) ([]mail.Attachment, error) {
	return nil, nil
}

func (r *Runner) noticeData(ctx context.Context, w store.WorkshopRow) (notice.Data, error) {
	loc, err := time.LoadLocation(w.Timezone)
	if err != nil {
		loc = time.UTC
	}
	d := notice.Data{
		Title:  w.Title,
		Host:   w.HostName,
		Place:  w.Location,
		Price:  present.Price(domain.Money{Minor: w.PriceMinor, Currency: w.Currency}),
		Needed: w.Minimum,
	}
	if w.ShareToken != "" {
		d.InviteLink = r.cfg.BaseURL + "/w/" + w.ShareToken
		d.InviteMessage = notice.InviteMessage(w.Title, w.Location, d.Price, d.Needed, d.InviteLink)
	}
	p, err := r.st.ActiveProposal(ctx, w.ID)
	if err != nil || p == nil {
		return d, err
	}
	d.Price = present.Price(domain.Money{Minor: p.PriceMinor, Currency: p.Currency})
	d.Needed = p.Minimum
	d.Deadline = present.Moment(p.ClosesAt, loc)
	dates, err := r.st.CandidateDates(ctx, w.ID)
	if err != nil {
		return d, err
	}
	for _, c := range dates {
		if c.ID == p.ChosenDateID {
			d.When = present.DateLabel(c.StartsAt, c.EndsAt, loc)
		}
	}
	if d.When == "" {
		d.When = present.Moment(p.StartsAt, loc)
	}
	return d, nil
}

func (r *Runner) queueNotice(ctx context.Context, kind notice.Kind, d notice.Data, n store.Notification) error {
	e, err := notice.Render(kind, d)
	if err != nil {
		return err
	}
	n.Kind, n.Subject, n.Body, n.HTML = string(kind), e.Subject, e.Text, e.HTML
	return r.queue(ctx, n)
}

func (r *Runner) NotifyJoined(ctx context.Context, workshopID, participantID, link string) error {
	name, email, err := r.st.ParticipantEmail(ctx, participantID)
	if err != nil || email == "" {
		return err
	}
	w, err := r.st.WorkshopByID(ctx, workshopID)
	if err != nil {
		return err
	}
	d, err := r.noticeData(ctx, w)
	if err != nil {
		return err
	}
	d.Name, d.Link = name, link
	return r.queueNotice(ctx, notice.Joined, d, store.Notification{
		WorkshopID: workshopID, ParticipantID: participantID, To: email, Key: "notify:" + NotifyJoined + ":" + participantID,
	})
}

func (r *Runner) NotifyOwnerCreated(ctx context.Context, workshopID, ownerLink string) error {
	w, err := r.st.WorkshopByID(ctx, workshopID)
	if err != nil || w.OwnerEmail == "" {
		return err
	}
	d, err := r.noticeData(ctx, w)
	if err != nil {
		return err
	}
	d.Name, d.Link = w.HostName, ownerLink
	return r.queueNotice(ctx, notice.OwnerCreated, d, store.Notification{
		WorkshopID: workshopID, To: w.OwnerEmail, Key: "notify:" + NotifyOwnerCreated + ":" + workshopID,
	})
}

func (r *Runner) Notify(ctx context.Context, enrollmentID, kind string) error {
	en, err := r.st.EnrollmentByID(ctx, enrollmentID)
	if err != nil {
		return err
	}
	name, email, err := r.st.ParticipantEmail(ctx, en.ParticipantID)
	if err != nil || email == "" {
		return err
	}
	w, err := r.st.WorkshopByID(ctx, en.WorkshopID)
	if err != nil {
		return err
	}
	switch kind {
	case NotifyReserved, NotifyConfirmed, NotifyPaymentFailed, NotifyRefundCompleted:
	default:
		return fmt.Errorf("notify: unknown kind %q", kind)
	}
	d, err := r.noticeData(ctx, w)
	if err != nil || d.Deadline == "" {
		return err
	}
	d.Name = name
	return r.queueNotice(ctx, notice.Kind(kind), d, store.Notification{
		WorkshopID: en.WorkshopID, ParticipantID: en.ParticipantID, To: email, Key: "notify:" + kind + ":" + enrollmentID,
	})
}

func (r *Runner) notifyCancelled(ctx context.Context, workshopID string) {
	err := r.queueCancelled(ctx, workshopID)
	if err != nil {
		r.log.Error("notify: could not queue the cancellation notices", "workshop_id", workshopID, "error", err)
	}
}

func (r *Runner) queueCancelled(ctx context.Context, workshopID string) error {
	w, err := r.st.WorkshopByID(ctx, workshopID)
	if err != nil {
		return err
	}
	d, err := r.noticeData(ctx, w)
	if err != nil {
		return err
	}
	if w.CancelReason != nil {
		d.Reason = *w.CancelReason
	}
	contacts, err := r.st.ContactsOfWorkshop(ctx, workshopID)
	if err != nil {
		return err
	}
	for _, c := range contacts {
		pd := d
		pd.Name = c.Name
		err = errors.Join(err, r.queueNotice(ctx, notice.Cancelled, pd, store.Notification{
			WorkshopID: workshopID, ParticipantID: c.ParticipantID, To: c.Email, Key: "notify:" + NotifyCancelled + ":" + c.ParticipantID,
		}))
	}
	if w.OwnerEmail != "" {
		od := d
		od.Name = w.HostName
		err = errors.Join(err, r.queueNotice(ctx, notice.OwnerCancelled, od, store.Notification{
			WorkshopID: workshopID, To: w.OwnerEmail, Key: "notify:" + NotifyOwnerCancelled + ":" + workshopID,
		}))
	}
	return err
}

func (r *Runner) notifyOwnerConfirmed(ctx context.Context, workshopID string, paid int, collected domain.Money) {
	w, err := r.st.WorkshopByID(ctx, workshopID)
	if err == nil && w.OwnerEmail != "" {
		var d notice.Data
		if d, err = r.noticeData(ctx, w); err == nil {
			d.Name, d.Paid, d.Collected = w.HostName, paid, present.Price(collected)
			err = r.queueNotice(ctx, notice.OwnerConfirmed, d, store.Notification{
				WorkshopID: workshopID, To: w.OwnerEmail, Key: "notify:" + NotifyOwnerConfirmed + ":" + workshopID,
			})
		}
	}
	if err != nil {
		r.log.Error("notify: could not queue the organizer's confirmation", "workshop_id", workshopID, "error", err)
	}
}

func (r *Runner) notifyEnrollment(ctx context.Context, enrollmentID, kind string) {
	if err := r.Notify(ctx, enrollmentID, kind); err != nil {
		r.log.Error("notify: could not queue a notification", "enrollment_id", enrollmentID, "kind", kind, "error", err)
	}
}

func (r *Runner) queue(ctx context.Context, n store.Notification) error {
	_, err := r.st.EnqueueNotification(ctx, n, r.clock.Now())
	return err
}

func (r *Runner) RunPending(ctx context.Context) error {
	for {
		ran, err := r.RunOnce(ctx)
		if err != nil || !ran {
			return err
		}
	}
}
