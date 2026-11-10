package worker_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
)

func (e *env) extendable() time.Time {
	e.t.Helper()
	starts := t0.Add(10 * 24 * time.Hour)
	if _, err := e.st.Pool.Exec(e.ctx, `update proposals set starts_at = $2 where id = $1`, e.proposal.ID, starts); err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.st.Pool.Exec(e.ctx, `update enrollments set consent_version = 'consent-v5' where workshop_id = $1`, e.workshop.ID); err != nil {
		e.t.Fatal(err)
	}
	e.proposal.StartsAt = starts
	return t0.Add(domain.MaxPaymentWindow)
}

func (e *env) extend(to time.Time) store.ProposalRow {
	e.t.Helper()
	p, err := e.runner.ExtendDeadline(e.ctx, e.proposal.ID, e.proposal.ClosesAt, to)
	if err != nil {
		e.t.Fatalf("ExtendDeadline: %v", err)
	}
	e.proposal = p
	return p
}

func TestExtendDeadline_TellsEveryoneWhoStillHoldsASpot(t *testing.T) {
	e := newEnv(t)
	ana, _ := e.authorized("Ana")
	ben, _ := e.withOrder("Ben")
	cleo, _ := e.enroll("Cleo")
	e.authorized("Dev")
	e.transition(cleo.ID, domain.EnrollmentSeatReserved, domain.EnrollmentReleased)
	e.setEmail(ana, "ana@example.com")
	e.setEmail(ben, "ben@example.com")
	e.setEmail(cleo, "cleo@example.com")
	to := e.extendable()
	e.clock.Set(e.proposal.ClosesAt.Add(-2 * time.Hour))
	from := e.proposal.ClosesAt

	p := e.extend(to)

	if !p.ClosesAt.Equal(to) || p.ExtendedFrom == nil || !p.ExtendedFrom.Equal(from) {
		t.Errorf("proposal = closes %s, extended from %v", p.ClosesAt, p.ExtendedFrom)
	}
	if j := e.job(e.closeKey()); j.Status != store.JobPending || !j.RunAt.Equal(to) {
		t.Errorf("close = %s at %s, want pending at %s", j.Status, j.RunAt, to)
	}
	e.runUntilIdle()
	got := e.sentTo("More time to reserve")
	slices.Sort(got)
	if want := []string{"ana@example.com", "ben@example.com"}; !slices.Equal(got, want) {
		t.Errorf("deadline notices sent to %v, want %v (not Cleo, whose seat lapsed, nor Dev, who left no email)", got, want)
	}
	for _, m := range e.mailer.Sent() {
		if strings.Contains(m.Subject, "More time to reserve") && (!strings.Contains(m.Text, "Thu, Nov 5") || !strings.Contains(m.Text, "Tue, Nov 3")) {
			t.Errorf("notice does not name both deadlines:\n%s", m.Text)
		}
	}
	e.wantWorkshop(domain.WorkshopFunding, domain.ResolutionOpen)
}

func TestExtendDeadline_NoticeSkippedOnceTheClassIsDecided(t *testing.T) {
	e := newEnv(t)
	ana, _ := e.authorized("Ana")
	e.setEmail(ana, "ana@example.com")
	to := e.extendable()
	e.clock.Set(e.proposal.ClosesAt.Add(-2 * time.Hour))
	e.extend(to)
	e.moveWorkshop(domain.WorkshopFunding, domain.WorkshopConfirmed, domain.ResolutionOpen)

	e.runUntilIdle()

	if got := e.notificationStatus("notify:deadline_extended:" + ana.ID); got != "skipped" {
		t.Errorf("notice = %s, want skipped once the class is on", got)
	}
	if sent := e.sentTo("More time to reserve"); len(sent) != 0 {
		t.Errorf("sent to %v, want nobody", sent)
	}
}

func TestClose_RefusedBeforeItsDeadlineWithoutFlaggingTheWorkshop(t *testing.T) {
	e := newEnv(t)
	e.authorized("Ana")
	e.clock.Set(e.proposal.ClosesAt.Add(-time.Hour))
	if _, err := e.st.AdvanceJob(e.ctx, e.closeKey(), e.clock.Now()); err != nil {
		t.Fatal(err)
	}

	e.runOnce()

	j := e.wantJob(e.closeKey(), store.JobFailed)
	if j.LastError == nil || !strings.HasPrefix(*j.LastError, "skipped: payments close at ") || j.ProviderIssue != nil || j.HTTPStatus != nil {
		t.Errorf("refused close = %+v, want a skip with no PayPal answer", j)
	}
	e.wantWorkshop(domain.WorkshopFunding, domain.ResolutionOpen)
	e.runPeriodic()
	e.wantJob(e.closeKey(), store.JobFailed)

	e.clock.Set(e.proposal.ClosesAt.Add(-30 * time.Second))
	if revived, err := e.runner.Revive(e.ctx, e.closeKey()); err != nil || !revived {
		t.Fatalf("Revive = %v, %v", revived, err)
	}
	e.runUntilIdle()
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionSettled)
}

func TestClose_ARefusedEarlyCloseRunsOnceOverdue(t *testing.T) {
	e := newEnv(t)
	e.clock.Set(e.proposal.ClosesAt.Add(-time.Hour))
	if _, err := e.st.AdvanceJob(e.ctx, e.closeKey(), e.clock.Now()); err != nil {
		t.Fatal(err)
	}
	e.runOnce()
	e.wantJob(e.closeKey(), store.JobFailed)

	e.clock.Set(e.proposal.ClosesAt.Add(6 * time.Minute))
	e.runPeriodic()
	e.wantJob(e.closeKey(), store.JobPending)
	e.runUntilIdle()
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionSettled)
}

func TestExtendDeadline_LateHoldsAreChargedUntilTheNewDeadline(t *testing.T) {
	e := newEnv(t)
	ana, _ := e.authorized("Ana")
	ben, _ := e.authorized("Ben")
	to := e.extendable()
	e.clock.Set(e.proposal.ClosesAt.Add(-time.Hour))
	old := e.proposal.ClosesAt
	e.extend(to)

	e.clock.Set(old.Add(2 * time.Hour))
	e.runUntilIdle()
	e.wantWorkshop(domain.WorkshopFunding, domain.ResolutionOpen)
	cleo, _ := e.authorized("Cleo")
	e.runUntilIdle()

	for _, en := range []store.EnrollmentRow{ana, ben, cleo} {
		e.wantStatus(en.ID, domain.EnrollmentPaid)
	}
	e.wantWorkshop(domain.WorkshopConfirmed, domain.ResolutionOpen)
	e.wantJob(e.closeKey(), store.JobPending)

	e.clock.Set(to.Add(time.Second))
	e.runUntilIdle()
	e.wantJob(e.closeKey(), store.JobSucceeded)
	e.wantWorkshop(domain.WorkshopConfirmed, domain.ResolutionOpen)
}
