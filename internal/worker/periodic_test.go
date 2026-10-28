package worker_test

import (
	"context"
	"errors"
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

	for _, a := range []store.AttemptRow{anaAttempt, benAttempt, cleoAttempt} {
		if j := e.wantJob(e.periodicKey(a), "pending"); !j.RunAt.Equal(e.clock.Now()) {
			t.Errorf("%s run_at = %s, want now (%s)", j.IdempotencyKey, j.RunAt, e.clock.Now())
		}
	}
	for _, a := range []store.AttemptRow{danAttempt, eveAttempt, fayAttempt} {
		e.wantNoJob(e.periodicKey(a))
	}
	if after := len(e.allJobKeys()); after != before+3 {
		t.Errorf("jobs after RunPeriodic = %d, want %d (three periodic reconciles)", after, before+3)
	}

	e.runPeriodic()
	if again := len(e.allJobKeys()); again != before+3 {
		t.Errorf("jobs after a second RunPeriodic within the minute = %d, want %d", again, before+3)
	}

	e.fake.SetCaptureStatus(captureID, "COMPLETED", "")
	e.runKey(e.periodicKey(anaAttempt))
	e.wantStatus(ana.ID, domain.EnrollmentPaid)
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
	e.runner.Register("release_seats", func(ctx context.Context, job *store.Job) (*int, error) {
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
