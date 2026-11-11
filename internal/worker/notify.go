package worker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/ics"
	"github.com/aalvaropc/commitpool/internal/invite"
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
	NotifyOwnerPaidOut    = string(notice.OwnerPaidOut)
)

const linkAttempts = 5

func (r *Runner) notify(ctx context.Context, job *store.Job) (*int, error) {
	n, err := r.st.NotificationByKey(ctx, job.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if n == nil || n.Status != "pending" {
		return nil, nil
	}
	if stale, err := r.stale(ctx, n); err != nil || stale {
		if err == nil {
			r.log.Info("notification no longer true; not sent", "kind", n.Kind, "to", mail.Mask(n.To))
			err = r.st.MarkNotificationSkipped(ctx, n.Key)
		}
		return nil, err
	}
	attachments, err := r.attachments(ctx, n)
	if err != nil {
		return nil, err
	}
	msg := mail.Message{To: n.To, Subject: n.Subject, Text: n.Body, HTML: n.HTML, Attachments: attachments}
	id, err := r.mailer.Send(ctx, msg)
	if errors.Is(err, mail.ErrRejected) && len(msg.Attachments) > 0 {
		r.log.Warn("message with an attachment rejected; sending it without", "kind", n.Kind, "to", mail.Mask(n.To), "error", err)
		msg.Attachments = nil
		id, err = r.mailer.Send(ctx, msg)
	}
	link := notice.CarriesPrivateLink(notice.Kind(n.Kind))
	if err != nil && !errors.Is(err, mail.ErrRejected) && link && job.Attempts >= linkAttempts {
		r.log.Warn("message carrying a private link still failing; dropping it so the link is not kept", "kind", n.Kind, "to", mail.Mask(n.To), "error", err)
		err = fmt.Errorf("%w: %v", mail.ErrRejected, err)
	}
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
	return nil, r.st.MarkNotificationSent(ctx, n.Key, id, r.clock.Now(), link)
}

func (r *Runner) stale(ctx context.Context, n *store.NotificationRow) (bool, error) {
	switch n.Kind {
	case NotifyReserved:
		en, err := r.st.EnrollmentByID(ctx, strings.TrimPrefix(n.Key, "notify:"+NotifyReserved+":"))
		if err != nil {
			return false, err
		}
		return en.Status != domain.EnrollmentAuthorized, nil
	case NotifyConfirmed, NotifyOwnerConfirmed:
		w, err := r.st.WorkshopByID(ctx, n.WorkshopID)
		if err != nil {
			return false, err
		}
		return w.Status == domain.WorkshopCancelled, nil
	case NotifyDeadlineExtended:
		return r.deadlineNoticeStale(ctx, n)
	}
	return false, nil
}

func (r *Runner) attachments(ctx context.Context, n *store.NotificationRow) ([]mail.Attachment, error) {
	if n.Kind != NotifyConfirmed {
		return nil, nil
	}
	w, err := r.st.WorkshopByID(ctx, n.WorkshopID)
	if err != nil {
		return nil, err
	}
	p, err := r.st.ActiveProposal(ctx, w.ID)
	if err != nil || p == nil {
		return nil, err
	}
	dates, err := r.st.CandidateDates(ctx, w.ID)
	if err != nil {
		return nil, err
	}
	loc, err := time.LoadLocation(w.Timezone)
	if err != nil {
		loc = time.UTC
	}
	enrollments, err := r.st.EnrollmentsByWorkshop(ctx, w.ID)
	if err != nil {
		return nil, err
	}
	paid := 0
	for _, en := range enrollments {
		if en.Status == domain.EnrollmentPaid {
			paid++
		}
	}
	price := present.Price(domain.Money{Minor: p.PriceMinor, Currency: p.Currency})
	terms := invite.Terms(w.Status, price, p.Minimum, paid, loc)
	event, ok := invite.Event(w, p, dates, invite.Describe(w, terms), "", r.clock.Now())
	if !ok {
		return nil, nil
	}
	return []mail.Attachment{{Name: invite.FileName(w.Title), ContentType: "text/calendar; charset=utf-8; method=PUBLISH", Data: ics.Calendar(event)}}, nil
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
	when, err := r.proposalWhen(ctx, w, *p)
	if err != nil {
		return d, err
	}
	d.When = when
	return d, nil
}

func (r *Runner) proposalWhen(ctx context.Context, w store.WorkshopRow, p store.ProposalRow) (string, error) {
	loc, err := time.LoadLocation(w.Timezone)
	if err != nil {
		loc = time.UTC
	}
	dates, err := r.st.CandidateDates(ctx, w.ID)
	if err != nil {
		return "", err
	}
	for _, c := range dates {
		if c.ID == p.ChosenDateID {
			return present.DateLabel(c.StartsAt, c.EndsAt, loc), nil
		}
	}
	return present.Moment(p.StartsAt, loc), nil
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
	d.Opened = d.Deadline != ""
	d.Closed = w.CancelledAt == nil && d.Opened
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

func (r *Runner) onConfirmed(ctx context.Context, workshopID string) {
	enrollments, err := r.st.EnrollmentsByWorkshop(ctx, workshopID)
	if err != nil {
		r.log.Error("notify: could not list who paid", "workshop_id", workshopID, "error", err)
		return
	}
	paid := 0
	for _, en := range enrollments {
		if en.Status == domain.EnrollmentPaid {
			paid++
			r.notifyEnrollment(ctx, en.ID, NotifyConfirmed)
		}
	}
	p, err := r.st.ActiveProposal(ctx, workshopID)
	if err != nil || p == nil {
		r.log.Error("notify: no proposal to total the payments", "workshop_id", workshopID, "error", err)
		return
	}
	r.notifyOwnerConfirmed(ctx, workshopID, paid, domain.Money{Minor: int64(paid) * p.PriceMinor, Currency: p.Currency})
	r.schedulePayout(ctx, workshopID, p.StartsAt)
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
