package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/mail"
	"github.com/aalvaropc/commitpool/internal/store"
)

const (
	NotifyJoined          = "joined"
	NotifyConfirmed       = "confirmed"
	NotifyCancelled       = "cancelled"
	NotifyPaymentFailed   = "payment_failed"
	NotifyRefundCompleted = "refund_completed"
)

func (r *Runner) notify(ctx context.Context, job *store.Job) (*int, error) {
	n, err := r.st.NotificationByKey(ctx, job.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if n == nil || n.Status != "pending" {
		return nil, nil
	}
	id, err := r.mailer.Send(ctx, mail.Message{To: n.To, Subject: n.Subject, Text: n.Body})
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
	return nil, r.st.MarkNotificationSent(ctx, n.Key, id, r.clock.Now(), n.Kind == NotifyJoined)
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
	return r.queue(ctx, store.Notification{
		WorkshopID: workshopID, ParticipantID: participantID, Kind: NotifyJoined, To: email,
		Key:     "notify:" + NotifyJoined + ":" + participantID,
		Subject: "Your seat link for " + w.Title,
		Body: fmt.Sprintf("Hi %s,\n\nYou joined %s on CommitPool. This private link is your seat: keep it, it is where you say when you can come and, once payments open, commit.\n\n%s\n\nNothing is charged until enough people commit.\n",
			name, w.Title, link),
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
	p, err := r.st.ActiveProposal(ctx, en.WorkshopID)
	if err != nil {
		return err
	}
	if p == nil {
		return nil
	}
	price := domain.Money{Minor: p.PriceMinor, Currency: p.Currency}.String()
	when := describeStart(p.StartsAt, w.Timezone)
	var subject, body string
	switch kind {
	case NotifyConfirmed:
		subject = w.Title + " is confirmed"
		body = fmt.Sprintf("Hi %s,\n\nEnough people paid: %s on %s is happening. Your payment of %s is complete.\n\nOpen the private link you received when you joined to see your seat.\n", name, w.Title, when, price)
	case NotifyPaymentFailed:
		subject = "Your payment for " + w.Title + " did not go through"
		body = fmt.Sprintf("Hi %s,\n\nPayPal could not charge your hold of %s for %s on %s, so nothing was charged and your hold is released. While payments are open you can try again from your private link.\n", name, price, w.Title, when)
	case NotifyRefundCompleted:
		subject = "Your refund for " + w.Title + " is complete"
		body = fmt.Sprintf("Hi %s,\n\nPayPal completed your refund of %s for %s.\n", name, price, w.Title)
	default:
		return fmt.Errorf("notify: unknown kind %q", kind)
	}
	return r.queue(ctx, store.Notification{
		WorkshopID: en.WorkshopID, ParticipantID: en.ParticipantID, Kind: kind, To: email,
		Key: "notify:" + kind + ":" + enrollmentID, Subject: subject, Body: body,
	})
}

func (r *Runner) notifyCancelled(ctx context.Context, workshopID string) {
	w, err := r.st.WorkshopByID(ctx, workshopID)
	if err == nil {
		var contacts []store.Contact
		if contacts, err = r.st.ContactsOfWorkshop(ctx, workshopID); err == nil {
			why := ""
			if w.CancelReason != nil {
				why = " The organizer said: " + *w.CancelReason
			}
			for _, c := range contacts {
				err = errors.Join(err, r.queue(ctx, store.Notification{
					WorkshopID: workshopID, ParticipantID: c.ParticipantID, Kind: NotifyCancelled, To: c.Email,
					Key:     "notify:" + NotifyCancelled + ":" + c.ParticipantID,
					Subject: w.Title + " is cancelled",
					Body: fmt.Sprintf("Hi %s,\n\n%s is cancelled.%s Any hold of yours is released and any payment refunded; refunds can take a few days and you can follow yours from your private link.\n",
						c.Name, w.Title, why),
				}))
			}
		}
	}
	if err != nil {
		r.log.Error("notify: could not queue the cancellation notices", "workshop_id", workshopID, "error", err)
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

func describeStart(t time.Time, tz string) string {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	return t.In(loc).Format("Mon 2 Jan 2006, 15:04 MST")
}

func (r *Runner) RunPending(ctx context.Context) error {
	for {
		ran, err := r.RunOnce(ctx)
		if err != nil || !ran {
			return err
		}
	}
}
