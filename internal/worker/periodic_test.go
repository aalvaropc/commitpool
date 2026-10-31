package worker_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
)

// unknownCapture runs the authorized attempt's capture into an ambiguous 503,
// which leaves the job unknown.
func (e *env) unknownCapture(a store.AttemptRow) string {
	e.t.Helper()
	e.fake.FailNext("capture", 503, "")
	key := e.enqueue("capture", a)
	e.runKey(key)
	e.wantJob(key, "unknown")
	return key
}

// unknownAuthorize runs the attempt's authorize into an ambiguous 503 after
// the buyer approved, which leaves the job unknown and the seat reserved.
func (e *env) unknownAuthorize(a store.AttemptRow) string {
	e.t.Helper()
	e.fake.Approve(*a.OrderID)
	e.fake.FailNext("authorize", 503, "")
	key := e.enqueue("authorize", a)
	e.runKey(key)
	e.wantJob(key, "unknown")
	return key
}

// ageJob makes the job look last touched d ago by the database clock, which
// is the clock RunPeriodic ages unknown jobs by.
func (e *env) ageJob(key string, d time.Duration) {
	e.t.Helper()
	tag, err := e.st.Pool.Exec(e.ctx, `update jobs set updated_at = now() - make_interval(secs => $2) where idempotency_key = $1`, key, d.Seconds())
	if err != nil || tag.RowsAffected() != 1 {
		e.t.Fatalf("age job %s: %v (%d rows)", key, err, tag.RowsAffected())
	}
}

func (e *env) runPeriodic() {
	e.t.Helper()
	if err := e.runner.RunPeriodic(e.ctx); err != nil {
		e.t.Fatalf("RunPeriodic: %v", err)
	}
}

func TestPeriodicReconcile_EnqueuesPendingAttempts(t *testing.T) {
	e := newEnv(t, withClientTimeout(time.Second))
	ana, anaAttempt := e.authorized("Ana")
	captureID := e.capturePending(ana, anaAttempt)
	ben, benAttempt := e.paid("Ben")
	e.enqueue("refund", benAttempt)
	e.refundPending(ben, benAttempt)
	_, cleoAttempt := e.withOrder("Cleo")
	e.ageJob(e.unknownAuthorize(cleoAttempt), 6*time.Minute)
	_, danAttempt := e.withOrder("Dan")
	e.ageJob(e.unknownAuthorize(danAttempt), 4*time.Minute)
	_, eveAttempt := e.withOrder("Eve")
	e.ageJob(e.unknownAuthorize(eveAttempt), time.Hour)
	// Eve's last reconcile generation already ran and left the job to an
	// operator: no periodic reconcile may send it again.
	e.enqueueKey("reconcile", "reconcile:"+eveAttempt.ID+":5", eveAttempt, e.clock.Now().Add(-time.Hour))
	_, fayAttempt := e.authorized("Fay")
	before := len(e.allJobKeys())

	e.runPeriodic()

	for _, key := range []string{e.pendingKey(anaAttempt), e.pendingKey(benAttempt), e.periodicKey(cleoAttempt)} {
		if j := e.wantJob(key, "pending"); !j.RunAt.Equal(e.clock.Now()) {
			t.Errorf("%s run_at = %s, want now (%s)", j.IdempotencyKey, j.RunAt, e.clock.Now())
		}
	}
	for _, a := range []store.AttemptRow{danAttempt, eveAttempt, fayAttempt} {
		e.wantNoJob(e.periodicKey(a))
		e.wantNoJob(e.pendingKey(a))
	}
	wantJobs := func(when string, want int) {
		t.Helper()
		if got := len(e.allJobKeys()); got != want {
			t.Errorf("jobs after RunPeriodic %s = %d, want %d", when, got, want)
		}
	}
	wantJobs("once", before+3)

	e.runPeriodic()
	wantJobs("again within the minute", before+3)

	// Later in the same hour only the unknown job is reconciled again: a
	// pending capture or refund gets one periodic reconcile per hour.
	e.advance(10 * time.Minute)
	e.runPeriodic()
	wantJobs("later in the hour", before+4)
	e.wantJob(e.periodicKey(cleoAttempt), "pending")

	e.clock.Set(e.clock.Now().Truncate(time.Hour).Add(time.Hour))
	e.runPeriodic()
	wantJobs("in the next hour", before+7)
	e.wantJob(e.pendingKey(anaAttempt), "pending")
	e.wantJob(e.pendingKey(benAttempt), "pending")

	e.fake.SetCaptureStatus(captureID, "COMPLETED", "")
	e.runKey(e.pendingKey(anaAttempt))
	e.wantStatus(ana.ID, domain.EnrollmentPaid)
}

func TestPeriodic_HealsACancelledWorkshopThatNeedsAttention(t *testing.T) {
	e := newEnv(t)
	en, a := e.withOrder("Ana")
	e.fake.Approve(*a.OrderID)
	e.transition(en.ID, domain.EnrollmentSeatReserved, domain.EnrollmentReleased)
	o, err := e.api.AuthorizeOrder(e.ctx, *a.OrderID, "authorize:"+a.ID)
	if err != nil {
		t.Fatalf("authorize at PayPal: %v", err)
	}
	auth := o.PurchaseUnits[0].Payments.Authorizations[0]
	// The hold landed on the released seat, but the void it calls for was
	// never enqueued, and the workshop was cancelled straight into
	// NEEDS_ATTENTION.
	if _, err := e.st.UpdateAttemptFunc(e.ctx, a.ID, func(row *store.AttemptRow) error {
		row.AuthorizationID, row.AuthorizationStatus = &auth.ID, &auth.Status
		return nil
	}); err != nil {
		t.Fatalf("record the authorization: %v", err)
	}
	e.moveWorkshop(domain.WorkshopFunding, domain.WorkshopCancelled, domain.ResolutionNeedsAttention)

	e.runPeriodic()

	e.wantJob("void:"+a.ID, "pending")
}

func TestPeriodic_HealsFundingAndResolvingWorkshops(t *testing.T) {
	e := newEnv(t)
	e.confirmed()
	// The confirmation after the third payment was lost.
	e.moveWorkshop(domain.WorkshopConfirmed, domain.WorkshopFunding, domain.ResolutionOpen)
	cancelled, cancelledProposal := e.fundingWorkshop()
	pt, _, err := e.st.CreateParticipant(e.ctx, cancelled.ID, "Gus")
	if err != nil {
		t.Fatal(err)
	}
	gus, _, err := e.st.ReserveSeatAndCreateEnrollment(e.ctx, store.ReserveParams{
		WorkshopID: cancelled.ID, ParticipantID: pt.ID, ProposalID: cancelledProposal.ID,
		ConsentVersion: "consent-v1", Now: e.clock.Now(), SeatTTL: 30 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	// The workshop was cancelled and Gus's seat released, but the settlement
	// that should have followed never ran.
	if ok, err := e.st.TransitionEnrollment(e.ctx, gus.ID, []domain.EnrollmentStatus{domain.EnrollmentSeatReserved}, domain.EnrollmentReleased); err != nil || !ok {
		t.Fatalf("release Gus = %v, %v", ok, err)
	}
	if ok, err := e.st.TransitionWorkshop(e.ctx, cancelled.ID, nil, nil, domain.WorkshopCancelled, domain.ResolutionResolving); err != nil || !ok {
		t.Fatalf("cancel the second workshop = %v, %v", ok, err)
	}

	e.runPeriodic()

	e.wantWorkshop(domain.WorkshopConfirmed, domain.ResolutionOpen)
	got, err := e.st.WorkshopByID(e.ctx, cancelled.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.WorkshopCancelled || got.Resolution != domain.ResolutionSettled {
		t.Errorf("cancelled workshop = %s/%s, want CANCELLED/SETTLED", got.Status, got.Resolution)
	}
}

func TestWindow_UsesEarlierOfCloseAndAuthExpiry(t *testing.T) {
	e := newEnv(t)
	en, a := e.authorized("Ana")
	expires := e.proposal.ClosesAt.Add(-20 * time.Hour)
	if _, err := e.st.UpdateAttemptFunc(e.ctx, a.ID, func(row *store.AttemptRow) error {
		row.AuthExpiresAt = &expires
		return nil
	}); err != nil {
		t.Fatalf("set auth_expires_at: %v", err)
	}
	key := e.enqueue("capture", a)
	e.clock.Set(expires.Add(time.Minute))

	e.runKey(key)

	if n := len(e.fake.Requests("capture")); n != 0 {
		t.Fatalf("capture requests = %d, want 0 past the authorization's expiry", n)
	}
	e.wantStatus(en.ID, domain.EnrollmentCaptureFailed)
	wantString(t, "capture_issue", e.attempt(a.ID).CaptureIssue, "AUTHORIZATION_EXPIRED_LOCAL")
	j := e.wantJob(key, "failed")
	wantString(t, "provider_issue", j.ProviderIssue, "AUTHORIZATION_EXPIRED_LOCAL")
	e.wantJob("void:"+a.ID, "pending")
	e.wantWorkshop(domain.WorkshopFunding, domain.ResolutionOpen)
}

func TestRun_LetsTheJobInFlightFinishOnShutdown(t *testing.T) {
	e := newEnv(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	jobErr := make(chan error, 1)
	var first sync.Once
	e.runner.Register("release_seats", func(ctx context.Context, job *store.Job) (*int, error) {
		held := false
		first.Do(func() { held = true })
		if !held {
			return nil, nil
		}
		close(entered)
		<-release
		jobErr <- ctx.Err()
		return nil, nil
	})
	if err := e.runner.Enqueue(e.ctx, "release_seats", "release_seats:in-flight", &e.workshop.ID, nil, nil, e.clock.Now()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(e.ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- e.runner.Run(ctx) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not start the due job within 5s")
	}

	cancel()
	close(release)

	if err := <-jobErr; err != nil {
		t.Errorf("job context after shutdown began = %v, want it still live", err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run returned %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the job in flight finished")
	}
	e.wantJob("release_seats:in-flight", "succeeded")
}
