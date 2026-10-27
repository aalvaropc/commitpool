package worker_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
)

func releaseSeatsKey(workshopID string, now time.Time) string {
	return "release_seats:" + workshopID + ":" + now.UTC().Format("2006-01-02T15:04")
}

// releaseSeatsNow enqueues the env workshop's seat release due now and
// returns its key.
func (e *env) releaseSeatsNow() string {
	e.t.Helper()
	key := releaseSeatsKey(e.workshop.ID, e.clock.Now())
	if err := e.runner.Enqueue(e.ctx, "release_seats", key, &e.workshop.ID, nil, nil, e.clock.Now()); err != nil {
		e.t.Fatalf("Enqueue(%s): %v", key, err)
	}
	return key
}

// runAgain makes the finished job run once more, as when its worker died
// after the handler's effects and before the outcome was recorded, and
// RequeueStale took the job back.
func (e *env) runAgain(key string) {
	e.t.Helper()
	now := e.clock.Now()
	if _, err := e.st.Pool.Exec(e.ctx,
		`update jobs set status = 'running', locked_at = $2, locked_by = 'crashed-worker' where idempotency_key = $1`,
		key, now.Add(-10*time.Minute)); err != nil {
		e.t.Fatalf("mark %s as claimed by a dead worker: %v", key, err)
	}
	if n, err := e.st.RequeueStale(e.ctx, now, 5*time.Minute); err != nil || n != 1 {
		e.t.Fatalf("RequeueStale = %d, %v; want the one job back", n, err)
	}
	e.runKey(key)
	e.wantJob(key, "succeeded")
}

// nearClose moves the clock to 20 minutes before payments close, so a seat
// reserved now is still held when they close.
func (e *env) nearClose() { e.clock.Set(e.proposal.ClosesAt.Add(-20 * time.Minute)) }

// authorizeAtClose plays a buyer who approved just before payments closed and
// whose authorization lands at the close: too late for a capture. It leaves
// the clock at the close.
func (e *env) authorizeAtClose(en store.EnrollmentRow, a store.AttemptRow) store.AttemptRow {
	e.t.Helper()
	e.fake.Approve(*a.OrderID)
	e.clock.Set(e.proposal.ClosesAt)
	e.runKey(e.enqueue("authorize", a))
	e.wantStatus(en.ID, domain.EnrollmentAuthorized)
	e.wantNoJob("capture:" + a.ID)
	return e.attempt(a.ID)
}

func TestClose_TwoPaidCancelsVoidsRefundsAndTracksPending(t *testing.T) {
	e := newEnv(t, withClientTimeout(time.Second))
	e1, a1 := e.paid("Ana")
	e2, a2 := e.paid("Ben")
	e4, a4 := e.authorized("Dan")
	e.wantJob("capture:"+a4.ID, "pending")
	captureID := e.capturePending(e4, a4)
	e.nearClose()
	e3, a3 := e.withOrder("Cleo")
	a3 = e.authorizeAtClose(e3, a3)

	if !e.enqueueClose() {
		t.Fatal("close job was not created")
	}
	e.runKey(e.closeKey())

	e.wantJob(e.closeKey(), "succeeded")
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionResolving)
	for _, key := range []string{"void:" + a3.ID, "refund:" + a1.ID, "refund:" + a2.ID, "reconcile:" + a4.ID + ":close"} {
		e.wantJob(key, "pending")
	}
	for _, key := range []string{"void:" + a1.ID, "void:" + a2.ID, "void:" + a4.ID, "refund:" + a3.ID, "refund:" + a4.ID} {
		e.wantNoJob(key)
	}

	e.runKey("void:" + a3.ID)
	e.runKey("refund:" + a1.ID)
	refundID := e.refundPending(e2, a2)
	e.runKey("reconcile:" + a4.ID + ":close")

	e.wantStatus(e1.ID, domain.EnrollmentRefunded)
	e.wantStatus(e2.ID, domain.EnrollmentRefundPending)
	e.wantStatus(e3.ID, domain.EnrollmentVoided)
	e.wantStatus(e4.ID, domain.EnrollmentCapturePending)
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionResolving)
	e.wantIdle()

	e.fake.SetRefundStatus(refundID, "COMPLETED")
	e.runKey(e.reconcileNow(a2))
	e.wantStatus(e2.ID, domain.EnrollmentRefunded)
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionResolving)

	e.fake.SetCaptureStatus(captureID, "COMPLETED", "")
	e.runKey(e.reconcileNow(a4))
	e.runUntilIdle()

	e.wantStatus(e4.ID, domain.EnrollmentRefunded)
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionSettled)
	if got := e.fake.Captures(); got != 3 {
		t.Errorf("captures at PayPal = %d, want 3", got)
	}
	for _, a := range []store.AttemptRow{a1, a2, a4} {
		if n := e.requestsWithID("refund", "refund:"+a.ID); n != 1 {
			t.Errorf("refund requests under refund:%s = %d, want 1", a.ID, n)
		}
	}
	if n := len(e.fake.Requests("refund")); n != 3 {
		t.Errorf("refund requests = %d, want 3", n)
	}
}

func TestClose_RepeatedCloseJobIsNoop(t *testing.T) {
	e := newEnv(t)
	ana, a1 := e.paid("Ana")
	ben, a2 := e.paid("Ben")
	dan, _ := e.enroll("Dan")
	e.nearClose()
	cleo, a3 := e.withOrder("Cleo")
	a3 = e.authorizeAtClose(cleo, a3)

	if !e.enqueueClose() {
		t.Fatal("first close enqueue: created = false, want true")
	}
	if e.enqueueClose() {
		t.Fatal("second close enqueue: created = true, want false")
	}
	e.runKey(e.closeKey())
	attempts := []store.AttemptRow{a1, a2, a3}
	before := map[string][]string{}
	for _, a := range attempts {
		before[a.ID] = e.jobKeys(a)
	}

	e.runAgain(e.closeKey())
	for _, a := range attempts {
		if got := e.jobKeys(a); !slices.Equal(got, before[a.ID]) {
			t.Errorf("jobs of attempt %s after a second close = %v, want %v", a.ID, got, before[a.ID])
		}
	}

	e.runUntilIdle()
	e.runAgain(e.closeKey())
	e.runUntilIdle()

	for _, a := range attempts {
		if got := e.jobKeys(a); !slices.Equal(got, before[a.ID]) {
			t.Errorf("jobs of attempt %s after its jobs ran and a third close = %v, want %v", a.ID, got, before[a.ID])
		}
	}
	e.wantStatus(ana.ID, domain.EnrollmentRefunded)
	e.wantStatus(ben.ID, domain.EnrollmentRefunded)
	e.wantStatus(cleo.ID, domain.EnrollmentVoided)
	if n := len(e.fake.Requests("refund")); n != 2 {
		t.Errorf("refund requests = %d, want 2", n)
	}
	if n := len(e.fake.Requests("void")); n != 1 {
		t.Errorf("void requests = %d, want 1", n)
	}

	e.wantStatus(dan.ID, domain.EnrollmentSeatReserved)
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionResolving)
	e.runKey(e.releaseSeatsNow())
	e.wantStatus(dan.ID, domain.EnrollmentReleased)
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionSettled)

	e.runAgain(e.closeKey())
	e.wantIdle()
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionSettled)
}

func TestClose_ConfirmedVoidsOnlyUncapturedAuthorizations(t *testing.T) {
	e := newEnv(t, withClientTimeout(time.Second))
	e.nearClose()
	dan, danAttempt := e.withOrder("Dan")
	eve, eveAttempt := e.withOrder("Eve")
	paid := e.confirmed()
	danAttempt = e.authorize(dan, danAttempt)
	e.capturePending(dan, danAttempt)
	eveAttempt = e.authorizeAtClose(eve, eveAttempt)

	e.enqueueClose()
	e.runKey(e.closeKey())

	e.wantWorkshop(domain.WorkshopConfirmed, domain.ResolutionOpen)
	e.wantJob("void:"+eveAttempt.ID, "pending")
	e.wantJob("reconcile:"+danAttempt.ID+":close", "pending")
	e.wantNoJob("void:" + danAttempt.ID)
	e.wantNoJob("refund:" + danAttempt.ID)
	for _, a := range paid {
		e.wantNoJob("void:" + a.ID)
		e.wantNoJob("refund:" + a.ID)
	}

	e.runUntilIdle()

	e.wantStatus(eve.ID, domain.EnrollmentVoided)
	e.wantStatus(dan.ID, domain.EnrollmentCapturePending)
	for _, a := range paid {
		e.wantStatus(a.EnrollmentID, domain.EnrollmentPaid)
	}
	if reqs := e.fake.Requests("void"); len(reqs) != 1 || reqs[0].RequestID != "void:"+eveAttempt.ID {
		t.Errorf("void requests = %+v, want only void:%s", reqs, eveAttempt.ID)
	}
	if n := len(e.fake.Requests("refund")); n != 0 {
		t.Errorf("refund requests = %d, want 0", n)
	}
	e.wantWorkshop(domain.WorkshopConfirmed, domain.ResolutionOpen)
}

func TestSettle_NeedsAttentionWhenARefundFails(t *testing.T) {
	t.Run("PayPal refuses the refund", func(t *testing.T) {
		e := newEnv(t)
		ana, _ := e.paid("Ana")
		ben, benAttempt := e.paid("Ben")
		e.setMockCode(benAttempt, "REFUND_AMOUNT_EXCEEDED")
		e.clock.Set(e.proposal.ClosesAt)
		e.enqueueClose()
		e.runKey(e.closeKey())
		e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionResolving)

		e.runKey("refund:" + benAttempt.ID)

		e.wantStatus(ben.ID, domain.EnrollmentNeedsAttention)
		e.wantStatus(ana.ID, domain.EnrollmentPaid)
		e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionNeedsAttention)

		e.runUntilIdle()
		e.wantStatus(ana.ID, domain.EnrollmentRefunded)
		if err := e.runner.SettleIfResolved(e.ctx, e.workshop.ID); err != nil {
			t.Fatalf("SettleIfResolved: %v", err)
		}
		e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionNeedsAttention)
	})

	t.Run("the refund fails after pending", func(t *testing.T) {
		e := newEnv(t, withClientTimeout(time.Second))
		ana, anaAttempt := e.paid("Ana")
		ben, benAttempt := e.paid("Ben")
		e.clock.Set(e.proposal.ClosesAt)
		e.enqueueClose()
		e.runKey(e.closeKey())
		e.runKey("refund:" + anaAttempt.ID)
		refundID := e.refundPending(ben, benAttempt)
		e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionResolving)

		e.fake.SetRefundStatus(refundID, "FAILED")
		e.runKey(e.reconcileNow(benAttempt))

		e.wantStatus(ben.ID, domain.EnrollmentRefundFailed)
		e.wantStatus(ana.ID, domain.EnrollmentRefunded)
		e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionNeedsAttention)
		e.wantIdle()
	})
}

func TestReleaseSeats_FreesCapacityAfterExpiry(t *testing.T) {
	e := newEnv(t)
	var held []store.EnrollmentRow
	for i := range 6 {
		en, err := e.reserve(e.workshop, e.proposal, fmt.Sprintf("Participant %d", i))
		if err != nil {
			t.Fatalf("reserve %d: %v", i, err)
		}
		held = append(held, en)
	}
	if _, err := e.reserve(e.workshop, e.proposal, "Late"); !errors.Is(err, domain.ErrCapacityFull) {
		t.Fatalf("reserve past capacity = %v, want ErrCapacityFull", err)
	}

	e.advance(31 * time.Minute)
	key := e.releaseSeatsNow()
	e.runUntilIdle()

	e.wantJob(key, "succeeded")
	for _, en := range held {
		e.wantStatus(en.ID, domain.EnrollmentReleased)
	}
	if _, err := e.reserve(e.workshop, e.proposal, "Later"); err != nil {
		t.Fatalf("reserve after the release: %v", err)
	}
	e.wantWorkshop(domain.WorkshopFunding, domain.ResolutionOpen)
}

func TestReleaseSeatsTick_EnqueuesOnePerWorkshopPerMinute(t *testing.T) {
	e := newEnv(t)
	if _, err := e.reserve(e.workshop, e.proposal, "Ana"); err != nil {
		t.Fatal(err)
	}
	other, otherP := e.fundingWorkshop()
	for _, name := range []string{"Ben", "Cleo"} {
		if _, err := e.reserve(other, otherP, name); err != nil {
			t.Fatal(err)
		}
	}
	quiet, quietP := e.fundingWorkshop()
	dan, err := e.reserve(quiet, quietP, "Dan")
	if err != nil {
		t.Fatal(err)
	}
	e.transition(dan.ID, domain.EnrollmentSeatReserved, domain.EnrollmentAuthorized)
	e.fundingWorkshop()

	tick := func() {
		t.Helper()
		if err := e.runner.EnqueueSeatReleases(e.ctx); err != nil {
			t.Fatalf("EnqueueSeatReleases: %v", err)
		}
	}
	releaseJobs := func() int {
		t.Helper()
		var n int
		if err := e.st.Pool.QueryRow(e.ctx, `select count(*) from jobs where kind = 'release_seats'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	wantMinute := func(at time.Time) {
		t.Helper()
		for _, w := range []store.WorkshopRow{e.workshop, other} {
			j := e.wantJob(releaseSeatsKey(w.ID, at), "pending")
			if j.WorkshopID == nil || *j.WorkshopID != w.ID || j.AttemptID != nil || !j.RunAt.Equal(at) {
				t.Errorf("job %s: workshop %v, attempt %v, run_at %s; want workshop %s, no attempt, due %s",
					j.IdempotencyKey, deref(j.WorkshopID), deref(j.AttemptID), j.RunAt, w.ID, at)
			}
		}
		e.wantNoJob(releaseSeatsKey(quiet.ID, at))
	}

	first := t0.Add(30 * time.Second)
	e.clock.Set(first)
	tick()
	e.clock.Set(t0.Add(50 * time.Second))
	tick()
	if n := releaseJobs(); n != 2 {
		t.Fatalf("release_seats jobs after two ticks in one minute = %d, want 2", n)
	}
	wantMinute(first)

	next := t0.Add(70 * time.Second)
	e.clock.Set(next)
	tick()
	if n := releaseJobs(); n != 4 {
		t.Fatalf("release_seats jobs after a tick in the next minute = %d, want 4", n)
	}
	wantMinute(next)

	e.runUntilIdle()
	for _, key := range []string{releaseSeatsKey(e.workshop.ID, first), releaseSeatsKey(other.ID, next)} {
		e.wantJob(key, "succeeded")
	}
}
