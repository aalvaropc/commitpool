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

	e.paid("Cleo") // no email; the third payment confirms the workshop
	e.runUntilIdle()
	if err := e.runner.HealWorkshop(e.ctx, e.workshop.ID); err != nil {
		t.Fatal(err)
	}
	e.runUntilIdle()

	got := e.sentTo("confirmed")
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
