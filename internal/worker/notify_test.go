package worker_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/mail"
	"github.com/aalvaropc/commitpool/internal/store"
)

func (e *env) setEmail(en store.EnrollmentRow, email string) {
	e.t.Helper()
	if err := e.st.SetParticipantEmail(e.ctx, en.ParticipantID, email); err != nil {
		e.t.Fatalf("SetParticipantEmail: %v", err)
	}
}

func (e *env) sentTo(subject string) []string {
	e.t.Helper()
	var to []string
	for _, m := range e.mailer.Sent() {
		if strings.Contains(m.Subject, subject) {
			to = append(to, m.To)
		}
	}
	return to
}

func TestNotify_ConfirmedOncePerParticipantWithEmail(t *testing.T) {
	e := newEnv(t)
	ana, _ := e.paid("Ana")
	ben, _ := e.paid("Ben")
	e.setEmail(ana, "ana@example.com")
	e.setEmail(ben, "ben@example.com")

	e.paid("Cleo")
	e.runUntilIdle()
	if err := e.runner.HealWorkshop(e.ctx, e.workshop.ID); err != nil {
		t.Fatal(err)
	}
	e.runUntilIdle()

	got := e.sentTo("It’s on")
	if len(got) != 2 || !strings.Contains(strings.Join(got, ","), "ana@example.com") || !strings.Contains(strings.Join(got, ","), "ben@example.com") {
		t.Fatalf("confirmed sent to %v, want Ana and Ben once each", got)
	}
}

func TestNotify_CancelledAndRefundCompleted(t *testing.T) {
	e := newEnv(t)
	ana, _ := e.paid("Ana")
	e.setEmail(ana, "ana@example.com")
	e.clock.Set(e.proposal.ClosesAt)

	e.runKey(e.closeKey())
	e.runUntilIdle()

	if got := e.sentTo("cancelled"); len(got) != 1 {
		t.Errorf("cancelled sent to %v, want Ana once", got)
	}
	if got := e.sentTo("refund"); len(got) != 1 {
		t.Errorf("refund completed sent to %v, want Ana once", got)
	}
}

func TestNotify_RetriesATransientFailure(t *testing.T) {
	e := newEnv(t)
	ana, _ := e.paid("Ana")
	e.setEmail(ana, "ana@example.com")
	e.mailer.FailNext(errors.New("connection reset"))
	e.mailer.FailNext(errors.New("connection reset"))
	e.clock.Set(e.proposal.ClosesAt)
	e.runKey(e.closeKey())
	e.runUntilIdle()
	for range 3 {
		e.advance(time.Minute)
		e.runUntilIdle()
	}
	if got := e.sentTo("cancelled"); len(got) != 1 {
		t.Fatalf("cancelled after two transient failures sent to %v, want Ana once", got)
	}
}

func TestNotify_RejectedRecipientIsNotRetried(t *testing.T) {
	e := newEnv(t)
	ben, _ := e.paid("Ben")
	e.setEmail(ben, "not-verified@example.com")
	e.mailer.FailNext(mail.ErrRejected)
	if err := e.runner.Notify(e.ctx, ben.ID, "payment_failed"); err != nil {
		t.Fatal(err)
	}
	e.runUntilIdle()
	n, err := e.st.NotificationByKey(e.ctx, "notify:payment_failed:"+ben.ID)
	if err != nil || n == nil || n.Status != "failed" {
		t.Fatalf("rejected notification = %+v, %v; want failed and not retried", n, err)
	}
}

func (e *env) setOwnerEmail(email string) {
	e.t.Helper()
	if _, err := e.st.Pool.Exec(e.ctx, `update workshops set owner_email = $2 where id = $1`, e.workshop.ID, email); err != nil {
		e.t.Fatal(err)
	}
}

func TestNotify_ReservedOnceTheHoldLands(t *testing.T) {
	e := newEnv(t)
	en, a := e.withOrder("Ana")
	e.setEmail(en, "ana@example.com")
	e.authorize(en, a)
	e.runUntilIdle()
	got := e.sentTo("Spot reserved")
	if len(got) != 1 || got[0] != "ana@example.com" {
		t.Fatalf("reserved sent to %v, want Ana once", got)
	}
	for _, m := range e.mailer.Sent() {
		if m.HTML == "" || !strings.Contains(m.Text, "Nothing is charged yet") {
			t.Errorf("message %q has no HTML part or the wrong words:\n%s", m.Subject, m.Text)
		}
	}
}

func TestNotify_OrganizerHearsItIsOnOnce(t *testing.T) {
	e := newEnv(t)
	e.setOwnerEmail("maya@example.com")
	e.paid("Ana")
	e.paid("Ben")
	e.paid("Cleo")
	e.runUntilIdle()
	if err := e.runner.HealWorkshop(e.ctx, e.workshop.ID); err != nil {
		t.Fatal(err)
	}
	e.runUntilIdle()
	var owner []string
	for _, m := range e.mailer.Sent() {
		if m.To == "maya@example.com" {
			owner = append(owner, m.Subject+" | "+m.Text)
		}
	}
	if len(owner) != 1 || !strings.Contains(owner[0], "It’s on") || !strings.Contains(owner[0], "3 people paid") {
		t.Fatalf("organizer messages = %v, want one saying 3 paid", owner)
	}
}

func TestNotify_OrganizerHearsOfTheCancellation(t *testing.T) {
	e := newEnv(t)
	e.setOwnerEmail("maya@example.com")
	e.paid("Ana")
	e.clock.Set(e.proposal.ClosesAt)
	e.runKey(e.closeKey())
	e.runUntilIdle()
	var owner []mail.Message
	for _, m := range e.mailer.Sent() {
		if m.To == "maya@example.com" {
			owner = append(owner, m)
		}
	}
	if len(owner) != 1 || !strings.Contains(owner[0].Subject, "is cancelled") || !strings.Contains(owner[0].Text, "Every hold is released") {
		t.Fatalf("organizer messages = %+v", owner)
	}
}
