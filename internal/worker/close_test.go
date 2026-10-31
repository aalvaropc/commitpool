package worker_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/paypal"
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

// authorizeJustBeforeClose plays a buyer who approves a minute before
// payments close, while they still take authorizations. It leaves the clock
// there.
func (e *env) authorizeJustBeforeClose(en store.EnrollmentRow, a store.AttemptRow) store.AttemptRow {
	e.t.Helper()
	e.clock.Set(e.proposal.ClosesAt.Add(-time.Minute))
	return e.authorize(en, a)
}

// dropJob deletes the job, as if the step that enqueues it had failed after
// the move that called for it.
func (e *env) dropJob(key string) {
	e.t.Helper()
	tag, err := e.st.Pool.Exec(e.ctx, `delete from jobs where idempotency_key = $1`, key)
	if err != nil || tag.RowsAffected() != 1 {
		e.t.Fatalf("delete job %s: %v (%d rows)", key, err, tag.RowsAffected())
	}
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
	a3 = e.authorizeJustBeforeClose(e3, a3)
	// Two paid and Cleo authorized reach the minimum, so the round enqueues
	// Cleo's capture; the worker has not run it when payments close.
	e.wantJob("capture:"+a3.ID, "pending")
	e.clock.Set(e.proposal.ClosesAt)

	// Opening the proposal scheduled its close; enqueueing it again adds nothing.
	e.wantJob(e.closeKey(), "pending")
	if e.enqueueClose() {
		t.Fatal("enqueueing the close job again created a second one")
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
	e.runKey("capture:" + a3.ID)
	e.wantJob("capture:"+a3.ID, "failed")

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
	a3 = e.authorizeJustBeforeClose(cleo, a3)
	e.clock.Set(e.proposal.ClosesAt)

	e.wantJob(e.closeKey(), "pending")
	if e.enqueueClose() {
		t.Fatal("close enqueue after opening the proposal: created = true, want false")
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

	e.runKey("void:" + a3.ID)
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
	eveAttempt = e.authorizeJustBeforeClose(eve, eveAttempt)
	e.dropJob("capture:" + eveAttempt.ID)
	e.clock.Set(e.proposal.ClosesAt)

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

func TestAuthorize_SkippedOncePaymentsClosed(t *testing.T) {
	e := newEnv(t)
	e.nearClose()
	en, a := e.withOrder("Ana")
	e.fake.Approve(*a.OrderID)
	key := e.enqueue("authorize", a)
	e.clock.Set(e.proposal.ClosesAt.Add(time.Minute))
	e.enqueueClose()
	e.runKey(e.closeKey())
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionResolving)

	e.runKey(key)

	j := e.wantJob(key, "failed")
	if j.LastError == nil || *j.LastError != "skipped: payments closed" {
		t.Errorf("authorize last_error = %v, want \"skipped: payments closed\"", deref(j.LastError))
	}
	if n := len(e.fake.Requests("authorize")); n != 0 {
		t.Errorf("authorize requests = %d, want 0", n)
	}
	e.wantStatus(en.ID, domain.EnrollmentSeatReserved)
	e.wantNoJob("void:" + a.ID)

	e.advance(10 * time.Minute)
	e.runKey(e.releaseSeatsNow())
	e.wantStatus(en.ID, domain.EnrollmentReleased)
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionSettled)
}

func TestReconcile_AuthorizationFoundAfterCloseIsVoided(t *testing.T) {
	e := newEnv(t, withClientTimeout(time.Second))
	e.nearClose()
	en, a := e.withOrder("Ana")
	e.fake.Approve(*a.OrderID)
	e.fake.FailNext("authorize", 0, "")
	key := e.enqueue("authorize", a)
	e.runKey(key)
	e.fake.WaitHung()
	e.wantJob(key, "unknown")
	e.wantStatus(en.ID, domain.EnrollmentSeatReserved)

	e.clock.Set(e.proposal.ClosesAt.Add(time.Minute))
	e.enqueueClose()
	e.runKey(e.closeKey())
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionResolving)
	e.runKey("reconcile:" + a.ID + ":1")

	e.wantStatus(en.ID, domain.EnrollmentAuthorized)
	e.wantJob(key, "succeeded")
	e.wantJob("void:"+a.ID, "pending")

	e.runUntilIdle()
	e.wantStatus(en.ID, domain.EnrollmentVoided)
	wantString(t, "authorization_status", e.attempt(a.ID).AuthorizationStatus, "VOIDED")
	if reqs := e.fake.Requests("void"); len(reqs) != 1 || reqs[0].RequestID != "void:"+a.ID {
		t.Errorf("void requests = %+v, want one under void:%s", reqs, a.ID)
	}
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionSettled)
}

// moveWorkshop rewrites the workshop's state as a step the test stands in
// for would have, through the store's compare-and-set.
func (e *env) moveWorkshop(from domain.WorkshopStatus, to domain.WorkshopStatus, resolution domain.Resolution) {
	e.t.Helper()
	ok, err := e.st.TransitionWorkshop(e.ctx, e.workshop.ID, []domain.WorkshopStatus{from}, nil, to, resolution)
	if err != nil || !ok {
		e.t.Fatalf("TransitionWorkshop(%s -> %s/%s) = %v, %v", from, to, resolution, ok, err)
	}
}

// allJobKeys lists every job key in the database.
func (e *env) allJobKeys() map[string]bool {
	e.t.Helper()
	rows, err := e.st.Pool.Query(e.ctx, `select idempotency_key from jobs`)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	keys := map[string]bool{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			e.t.Fatal(err)
		}
		keys[k] = true
	}
	return keys
}

// waitForRowLockWaiter returns once some statement waits on a row lock, or
// fails when done reports first or nothing waits within five seconds.
func (e *env) waitForRowLockWaiter(done <-chan error) {
	e.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case err := <-done:
			e.t.Fatalf("finished (%v) without waiting on the locked row", err)
		default:
		}
		var waiting bool
		if err := e.st.Pool.QueryRow(e.ctx,
			`select exists (select 1 from pg_locks where not granted and locktype in ('transactionid', 'tuple'))`).Scan(&waiting); err != nil {
			e.t.Fatal(err)
		}
		if waiting {
			return
		}
		if time.Now().After(deadline) {
			e.t.Fatal("nothing waited on the locked row within 5s")
		}
	}
}

func TestClose_OnConfirmedWorkshopEnqueuesNoRefunds(t *testing.T) {
	e := newEnv(t, withClientTimeout(time.Second))
	_, a1 := e.paid("Ana")
	_, a2 := e.paid("Ben")
	cleo, a3 := e.authorized("Cleo")
	e.capturePending(cleo, a3)
	e.clock.Set(e.proposal.ClosesAt)
	e.enqueueClose()

	// Cleo's capture completes and confirms the workshop in a transaction
	// that is still open when close decides: close reads two payments and
	// cancels, but its write has to wait for that transaction.
	tx, err := e.st.Pool.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(e.ctx) }()
	if _, err := tx.Exec(e.ctx, `update enrollments set status = 'PAID' where id = $1`, cleo.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(e.ctx, `update workshops set status = 'CONFIRMED' where id = $1`, e.workshop.ID); err != nil {
		t.Fatal(err)
	}
	closed := make(chan error, 1)
	go func() { closed <- e.runner.RunKey(e.ctx, e.closeKey()) }()
	e.waitForRowLockWaiter(closed)
	if err := tx.Commit(e.ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-closed; err != nil {
		t.Fatalf("RunKey(close): %v", err)
	}

	e.wantJob(e.closeKey(), "succeeded")
	e.wantWorkshop(domain.WorkshopConfirmed, domain.ResolutionOpen)
	for _, a := range []store.AttemptRow{a1, a2, a3} {
		e.wantNoJob("refund:" + a.ID)
		e.wantNoJob("void:" + a.ID)
	}
	e.runUntilIdle()
	if n := len(e.fake.Requests("refund")); n != 0 {
		t.Errorf("refund requests = %d, want 0", n)
	}
	e.wantWorkshop(domain.WorkshopConfirmed, domain.ResolutionOpen)
}

func TestClose_RetryOnCancelledResolvingEnqueuesSameKeys(t *testing.T) {
	e := newEnv(t, withClientTimeout(time.Second))
	ana, a1 := e.paid("Ana")
	ben, a2 := e.paid("Ben")
	dan, a4 := e.authorized("Dan")
	e.capturePending(dan, a4)
	e.nearClose()
	cleo, a3 := e.withOrder("Cleo")
	a3 = e.authorizeJustBeforeClose(cleo, a3)
	e.clock.Set(e.proposal.ClosesAt.Add(time.Minute))
	e.enqueueClose()
	// A first close recorded the cancellation and stopped before enqueuing
	// what it owes.
	e.moveWorkshop(domain.WorkshopFunding, domain.WorkshopCancelled, domain.ResolutionResolving)
	before := e.allJobKeys()

	e.runKey(e.closeKey())

	after := e.allJobKeys()
	var added []string
	for k := range after {
		if !before[k] {
			added = append(added, k)
		}
	}
	slices.Sort(added)
	want := []string{"refund:" + a1.ID, "refund:" + a2.ID, "void:" + a3.ID, "reconcile:" + a4.ID + ":close"}
	slices.Sort(want)
	if !slices.Equal(added, want) {
		t.Fatalf("jobs added by the retried close = %v, want %v", added, want)
	}
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionResolving)

	e.runUntilIdle()
	e.wantStatus(ana.ID, domain.EnrollmentRefunded)
	e.wantStatus(ben.ID, domain.EnrollmentRefunded)
	e.wantStatus(cleo.ID, domain.EnrollmentCaptureFailed)
	wantString(t, "Cleo's authorization", e.attempt(a3.ID).AuthorizationStatus, "VOIDED")
	e.wantStatus(dan.ID, domain.EnrollmentCapturePending)
	if n := len(e.fake.Requests("void")); n != 1 {
		t.Errorf("void requests = %d, want 1", n)
	}
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionResolving)
}

func TestSettle_WaitsForPendingVoidAfterCaptureFailed(t *testing.T) {
	setup := func(t *testing.T) (*env, store.EnrollmentRow, store.AttemptRow) {
		e := newEnv(t)
		ana, a := e.authorized("Ana")
		e.setMockCode(a, "TRANSACTION_REFUSED")
		e.runKey(e.enqueue("capture", a))
		e.wantStatus(ana.ID, domain.EnrollmentCaptureFailed)
		e.wantJob("void:"+a.ID, "pending")
		e.clock.Set(e.proposal.ClosesAt)
		e.enqueueClose()
		e.runKey(e.closeKey())
		e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionResolving)
		return e, ana, a
	}

	t.Run("the void succeeds", func(t *testing.T) {
		e, ana, a := setup(t)
		if err := e.runner.SettleIfResolved(e.ctx, e.workshop.ID); err != nil {
			t.Fatal(err)
		}
		e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionResolving)

		e.runKey("void:" + a.ID)

		e.wantStatus(ana.ID, domain.EnrollmentCaptureFailed)
		wantString(t, "authorization_status", e.attempt(a.ID).AuthorizationStatus, "VOIDED")
		e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionSettled)
	})

	t.Run("the void is refused after the workshop settled", func(t *testing.T) {
		e, ana, a := setup(t)
		ok, err := e.st.TransitionWorkshop(e.ctx, e.workshop.ID, nil, []domain.Resolution{domain.ResolutionResolving},
			domain.WorkshopCancelled, domain.ResolutionSettled)
		if err != nil || !ok {
			t.Fatalf("settle the workshop by hand = %v, %v", ok, err)
		}
		e.fake.FailNext("void", 422, "PREVIOUSLY_CAPTURED")

		e.runKey("void:" + a.ID)

		e.wantStatus(ana.ID, domain.EnrollmentNeedsAttention)
		e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionNeedsAttention)
	})
}

func TestEscalate_CloseJobExhaustionFlagsWorkshop(t *testing.T) {
	e := newEnv(t)
	w, _, _, err := e.st.CreateWorkshop(e.ctx, store.CreateWorkshopParams{
		Title: "No proposal", Description: "Payments were never opened.", DurationMin: 60, Timezone: "UTC",
		Minimum: 1, Capacity: 2, PriceMinor: 1000, Currency: "USD",
		Dates: []struct{ StartsAt, EndsAt time.Time }{{StartsAt: t0.Add(72 * time.Hour), EndsAt: t0.Add(73 * time.Hour)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	key := "close:00000000-0000-4000-8000-000000000000"
	if err := e.runner.Enqueue(e.ctx, "close", key, &w.ID, nil, nil, e.clock.Now()); err != nil {
		t.Fatal(err)
	}

	for range 5 {
		e.clock.Set(e.job(key).RunAt)
		e.runKey(key)
	}

	e.wantJob(key, "needs_attention")
	got, err := e.st.WorkshopByID(e.ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.WorkshopCollecting || got.Resolution != domain.ResolutionNeedsAttention {
		t.Errorf("workshop = %s/%s, want COLLECTING/NEEDS_ATTENTION", got.Status, got.Resolution)
	}
	e.wantWorkshop(domain.WorkshopFunding, domain.ResolutionOpen)
}

func TestHeal_OrphanHoldAfterCaptureFailedIsVoided(t *testing.T) {
	e := newEnv(t)
	ana, a := e.authorized("Ana")
	// The capture failure was recorded but the void it calls for was lost.
	e.transition(ana.ID, domain.EnrollmentAuthorized, domain.EnrollmentCaptureFailed)
	e.wantNoJob("void:" + a.ID)
	e.clock.Set(e.proposal.ClosesAt)
	e.enqueueClose()

	e.runKey(e.closeKey())

	e.wantJob("void:"+a.ID, "pending")
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionResolving)
	if err := e.runner.HealWorkshop(e.ctx, e.workshop.ID); err != nil {
		t.Fatalf("HealWorkshop: %v", err)
	}
	e.runUntilIdle()

	wantString(t, "authorization_status", e.attempt(a.ID).AuthorizationStatus, "VOIDED")
	e.wantStatus(ana.ID, domain.EnrollmentCaptureFailed)
	if reqs := e.fake.Requests("void"); len(reqs) != 1 || reqs[0].RequestID != "void:"+a.ID {
		t.Errorf("void requests = %+v, want one under void:%s", reqs, a.ID)
	}
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionSettled)
}

func TestHeal_FundingAtMinimumConfirms(t *testing.T) {
	e := newEnv(t)
	e.confirmed()
	// The confirmation after the third payment was lost.
	e.moveWorkshop(domain.WorkshopConfirmed, domain.WorkshopFunding, domain.ResolutionOpen)

	if err := e.runner.HealWorkshop(e.ctx, e.workshop.ID); err != nil {
		t.Fatalf("HealWorkshop: %v", err)
	}

	e.wantWorkshop(domain.WorkshopConfirmed, domain.ResolutionOpen)
	e.wantIdle()
}

func TestHeal_CancelledPaidWithoutRefundJobEnqueuesRefund(t *testing.T) {
	e := newEnv(t)
	ana, a := e.paid("Ana")
	// The workshop was cancelled but the refund it owes Ana was never enqueued.
	e.moveWorkshop(domain.WorkshopFunding, domain.WorkshopCancelled, domain.ResolutionResolving)

	e.runKey(e.reconcileNow(a))

	e.wantJob("refund:"+a.ID, "pending")
	e.runUntilIdle()
	e.wantStatus(ana.ID, domain.EnrollmentRefunded)
	if n := e.requestsWithID("refund", "refund:"+a.ID); n != 1 {
		t.Errorf("refund requests = %d, want 1", n)
	}
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionSettled)
}

func TestHeal_LeftoverAuthorizationIsVoidedOncePaymentsClosed(t *testing.T) {
	t.Run("confirmed workshop", func(t *testing.T) {
		e := newEnv(t)
		dan, danAttempt := e.withOrder("Dan")
		e.confirmed()
		danAttempt = e.authorize(dan, danAttempt)
		// The capture this authorization called for was lost.
		e.dropJob("capture:" + danAttempt.ID)
		e.clock.Set(e.proposal.ClosesAt.Add(time.Minute))

		e.runKey(e.reconcileNow(danAttempt))

		e.wantJob("void:"+danAttempt.ID, "pending")
		e.runUntilIdle()
		e.wantStatus(dan.ID, domain.EnrollmentVoided)
		e.wantNoJob("capture:" + danAttempt.ID)
		e.wantWorkshop(domain.WorkshopConfirmed, domain.ResolutionOpen)
	})

	t.Run("cancelled workshop", func(t *testing.T) {
		e := newEnv(t)
		ana, a := e.authorized("Ana")
		// The workshop was cancelled but the void it owes Ana was never enqueued.
		e.moveWorkshop(domain.WorkshopFunding, domain.WorkshopCancelled, domain.ResolutionResolving)

		e.runKey(e.reconcileNow(a))

		e.wantJob("void:"+a.ID, "pending")
		e.runUntilIdle()
		e.wantStatus(ana.ID, domain.EnrollmentVoided)
		e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionSettled)
	})
}

func TestSettle_WaitsForAuthorizationOnReleasedSeat(t *testing.T) {
	e := newEnv(t, withClientTimeout(time.Second))
	e.nearClose()
	en, a := e.withOrder("Ana")
	e.fake.Approve(*a.OrderID)
	e.fake.FailNext("authorize", 0, "")
	e.runKey(e.enqueue("authorize", a))
	e.fake.WaitHung()
	e.clock.Set(e.proposal.ClosesAt.Add(time.Minute))
	e.enqueueClose()
	e.runKey(e.closeKey())
	e.advance(10 * time.Minute)
	e.runKey(e.releaseSeatsNow())
	e.wantStatus(en.ID, domain.EnrollmentReleased)
	// The seat is gone but the authorize's outcome is still unknown: PayPal
	// may hold the buyer's money.
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionResolving)

	e.runKey("reconcile:" + a.ID + ":1")

	e.wantStatus(en.ID, domain.EnrollmentReleased)
	wantString(t, "authorization_status", e.attempt(a.ID).AuthorizationStatus, "CREATED")
	e.wantJob("void:"+a.ID, "pending")
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionResolving)

	e.runKey("void:" + a.ID)
	wantString(t, "authorization_status", e.attempt(a.ID).AuthorizationStatus, "VOIDED")
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionSettled)
}

func TestApply_AuthorizationAfterCloseEnqueuesVoid(t *testing.T) {
	e := newEnv(t, withClientTimeout(time.Second))
	e.nearClose()
	en, a := e.withOrder("Ana")
	e.fake.Approve(*a.OrderID)
	e.fake.FailNext("authorize", 0, "")
	e.runKey(e.enqueue("authorize", a))
	e.fake.WaitHung()
	e.clock.Set(e.proposal.ClosesAt.Add(time.Minute))
	e.enqueueClose()
	e.runKey(e.closeKey())

	if err := e.runner.ApplyOrder(e.ctx, a.ID, e.order(*a.OrderID)); err != nil {
		t.Fatalf("ApplyOrder: %v", err)
	}

	e.wantStatus(en.ID, domain.EnrollmentAuthorized)
	e.wantJob("void:"+a.ID, "pending")
}

func TestHeal_RevivesASkippedVoidOfAHoldStillHeld(t *testing.T) {
	e := newEnv(t)
	ana, a := e.authorized("Ana")
	e.transition(ana.ID, domain.EnrollmentAuthorized, domain.EnrollmentCapturing)
	voidKey := e.enqueue("void", a)
	e.runKey(voidKey)
	e.wantJob(voidKey, "failed")
	// The capture failed, but the revive of the void it calls for was lost.
	e.transition(ana.ID, domain.EnrollmentCapturing, domain.EnrollmentCaptureFailed)

	if err := e.runner.HealWorkshop(e.ctx, e.workshop.ID); err != nil {
		t.Fatalf("HealWorkshop: %v", err)
	}

	e.wantJob(voidKey, "pending")
	e.runKey(voidKey)
	wantString(t, "authorization_status", e.attempt(a.ID).AuthorizationStatus, "VOIDED")
	if n := e.requestsWithID("void", voidKey); n != 1 {
		t.Errorf("void requests = %d, want 1 under %s", n, voidKey)
	}
}

// landPaymentDuringCancellation makes the enrollment's move to PAID commit
// in the statement that cancels its workshop, as a capture hook landing
// between close's read of the workshop and its compare-and-set would.
func (e *env) landPaymentDuringCancellation(enrollmentID string) {
	e.t.Helper()
	for _, stmt := range []string{
		`create or replace function test_land_payment() returns trigger language plpgsql as $$
		 begin update enrollments set status = 'PAID' where id = '` + enrollmentID + `'; return new; end $$`,
		`drop trigger if exists test_land_payment on workshops`,
		`create trigger test_land_payment before update on workshops for each row
		 when (old.status = 'FUNDING' and new.status = 'CANCELLED') execute function test_land_payment()`,
	} {
		if _, err := e.st.Pool.Exec(e.ctx, stmt); err != nil {
			e.t.Fatalf("install the payment trigger: %v", err)
		}
	}
	e.t.Cleanup(func() {
		_, _ = e.st.Pool.Exec(context.Background(), `drop trigger if exists test_land_payment on workshops`)
		_, _ = e.st.Pool.Exec(context.Background(), `drop function if exists test_land_payment()`)
	})
}

func TestClose_PaymentLandingWhileTheCancellationIsWrittenIsRefundedByTheClose(t *testing.T) {
	e := newEnv(t)
	ana, a := e.authorized("Ana")
	e.transition(ana.ID, domain.EnrollmentAuthorized, domain.EnrollmentCapturing)
	c, err := e.api.CaptureAuthorization(e.ctx, paypal.CaptureParams{AuthorizationID: *a.AuthorizationID, RequestID: "capture:" + a.ID})
	if err != nil {
		t.Fatalf("capture at PayPal: %v", err)
	}
	// PayPal captured and the attempt holds the capture; the enrollment's
	// move to PAID lands only while close writes the cancellation.
	if _, err := e.st.UpdateAttemptFunc(e.ctx, a.ID, func(row *store.AttemptRow) error {
		row.CaptureID, row.CaptureStatus = &c.ID, &c.Status
		return nil
	}); err != nil {
		t.Fatalf("record the capture: %v", err)
	}
	e.landPaymentDuringCancellation(ana.ID)
	e.clock.Set(e.proposal.ClosesAt)

	e.runKey(e.closeKey())

	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionResolving)
	e.wantStatus(ana.ID, domain.EnrollmentPaid)
	e.wantJob("refund:"+a.ID, "pending")
	e.wantNoJob("reconcile:" + a.ID + ":close")
}

func TestHeal_StartsRoundMissedByFundingHook(t *testing.T) {
	// missedRound authorizes three holds, the workshop's minimum, and drops
	// the captures the third authorization's hook enqueued, as if the hook
	// had failed after the AUTHORIZED move was written.
	missedRound := func(e *env) []store.AttemptRow {
		e.t.Helper()
		var attempts []store.AttemptRow
		for _, name := range []string{"Ana", "Ben", "Cleo"} {
			_, a := e.authorized(name)
			e.wantJob("authorize:"+a.ID, "succeeded")
			attempts = append(attempts, a)
		}
		for _, a := range attempts {
			e.dropJob("capture:" + a.ID)
		}
		e.wantWorkshop(domain.WorkshopFunding, domain.ResolutionOpen)
		return attempts
	}
	wantRound := func(e *env, attempts []store.AttemptRow) {
		e.t.Helper()
		for _, a := range attempts {
			if j := e.wantJob("capture:"+a.ID, "pending"); !j.RunAt.Equal(e.clock.Now()) {
				e.t.Errorf("capture:%s run_at = %s, want now (%s)", a.ID, j.RunAt, e.clock.Now())
			}
		}
	}

	t.Run("heal", func(t *testing.T) {
		e := newEnv(t)
		attempts := missedRound(e)
		before := len(e.allJobKeys())

		if err := e.runner.HealWorkshop(e.ctx, e.workshop.ID); err != nil {
			t.Fatalf("HealWorkshop: %v", err)
		}
		wantRound(e, attempts)
		if got := len(e.allJobKeys()); got != before+3 {
			t.Errorf("jobs after heal = %d, want %d", got, before+3)
		}

		if err := e.runner.HealWorkshop(e.ctx, e.workshop.ID); err != nil {
			t.Fatalf("HealWorkshop again: %v", err)
		}
		if got := len(e.allJobKeys()); got != before+3 {
			t.Errorf("jobs after a second heal = %d, want %d", got, before+3)
		}

		e.runUntilIdle()
		e.wantWorkshop(domain.WorkshopConfirmed, domain.ResolutionOpen)
		for _, a := range attempts {
			if n := e.requestsWithID("capture", "capture:"+a.ID); n != 1 {
				t.Errorf("capture requests under capture:%s = %d, want 1", a.ID, n)
			}
		}
	})

	t.Run("periodic tick", func(t *testing.T) {
		e := newEnv(t)
		attempts := missedRound(e)

		e.runPeriodic()

		wantRound(e, attempts)
	})

	t.Run("confirmed workshop", func(t *testing.T) {
		e := newEnv(t)
		dan, danAttempt := e.withOrder("Dan")
		e.confirmed()
		danAttempt = e.authorize(dan, danAttempt)
		// The capture this authorization called for was lost.
		e.dropJob("capture:" + danAttempt.ID)

		if err := e.runner.HealWorkshop(e.ctx, e.workshop.ID); err != nil {
			t.Fatalf("HealWorkshop: %v", err)
		}

		wantRound(e, []store.AttemptRow{danAttempt})
		e.wantWorkshop(domain.WorkshopConfirmed, domain.ResolutionOpen)
	})
}
