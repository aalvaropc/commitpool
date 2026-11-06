package worker_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
)

func (e *env) parkCapture(a store.AttemptRow) string {
	e.t.Helper()
	key := e.enqueue("capture", a)
	backoff := []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute}
	for i := range 5 {
		e.fake.FailNext("capture", 429, "RATE_LIMIT_REACHED")
		e.runOnce()
		if i < 4 {
			e.advance(backoff[i])
		}
	}
	e.wantJob(key, "needs_attention")
	return key
}

func TestHeal_RevivesAParkedCaptureOnceClosedAndVoidsTheHold(t *testing.T) {
	e := newEnv(t)
	en, a := e.authorized("Ana")
	e.parkCapture(a)
	e.wantFlagged(en.ID, domain.EnrollmentCapturing)

	e.clock.Set(e.proposal.ClosesAt.Add(2 * time.Second))
	e.runUntilIdle()

	e.wantFlagged(en.ID, domain.EnrollmentCaptureFailed)
	wantString(t, "authorization_status", e.attempt(a.ID).AuthorizationStatus, "VOIDED")
	if n := len(e.fake.Requests("capture")); n != 5 {
		t.Errorf("capture requests = %d, want 5: past the deadline nothing is sent again", n)
	}
}

func TestEscalate_VoidThatKeepsFailingFlagsTheWorkshop(t *testing.T) {
	e := newEnv(t)
	_, a := e.holdOnReleasedSeat("Ana")
	backoff := []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute}
	for i := range 5 {
		e.fake.FailNext("void", 429, "RATE_LIMIT_REACHED")
		e.runKey("void:" + a.ID)
		if i < 4 {
			e.advance(backoff[i])
		}
	}

	e.wantJob("void:"+a.ID, "pending")
	e.wantWorkshop(domain.WorkshopFunding, domain.ResolutionNeedsAttention)
}

func TestReconcile_PeriodicKeepsResendingWhileTheLastGenerationWaits(t *testing.T) {
	e := newEnv(t)
	en, a := e.authorized("Ana")
	key := e.enqueue("void", a)
	for gen := 1; gen <= 4; gen++ {
		e.fake.FailNext("void", 503, "")
		e.runKey(key)
		e.wantJob(key, "unknown")
		r := e.job("reconcile:" + a.ID + ":" + strconv.Itoa(gen))
		e.clock.Set(r.RunAt)
		e.runKey(r.IdempotencyKey)
	}
	e.fake.FailNext("void", 503, "")
	e.runKey(key)
	e.wantJob("reconcile:"+a.ID+":5", "pending")

	e.advance(10 * time.Minute)
	e.ageJob(key, 10*time.Minute)
	e.runPeriodic()
	e.runUntilIdle()

	e.wantStatus(en.ID, domain.EnrollmentVoided)
	if got, _ := e.st.EnrollmentByID(e.ctx, en.ID); got.NeedsAttentionAt != nil {
		t.Errorf("enrollment flagged although PayPal recovered before the last generation")
	}
}

func TestEnroll_FlaggedEnrollmentStaysTheParticipantsActiveOne(t *testing.T) {
	t.Run("capture that ran out of retries", func(t *testing.T) {
		e := newEnv(t)
		en, a := e.authorized("Ana")
		e.parkCapture(a)
		e.wantFlagged(en.ID, domain.EnrollmentCapturing)
		e.wantSameEnrollment(en)
	})
	t.Run("failed capture whose void PayPal refused", func(t *testing.T) {
		e := newEnv(t)
		en, a := e.authorized("Ana")
		e.transition(en.ID, domain.EnrollmentAuthorized, domain.EnrollmentCaptureFailed)
		e.fake.FailNext("void", 422, "PREVIOUSLY_CAPTURED")
		e.runKey(e.enqueue("void", a))
		e.wantFlagged(en.ID, domain.EnrollmentCaptureFailed)
		e.wantSameEnrollment(en)
	})
	t.Run("flagged reserved seat past its expiry", func(t *testing.T) {
		e := newEnv(t)
		en, _ := e.enroll("Ana")
		e.flag(en.ID)
		e.clock.Set(en.SeatExpiresAt.Add(time.Minute))
		e.runKey(e.releaseSeatsNow())
		e.wantFlagged(en.ID, domain.EnrollmentSeatReserved)
		e.wantSameEnrollment(en)
	})
}

func (e *env) wantSameEnrollment(en store.EnrollmentRow) {
	e.t.Helper()
	again, created, err := e.st.ReserveSeatAndCreateEnrollment(e.ctx, store.ReserveParams{
		WorkshopID: e.workshop.ID, ParticipantID: en.ParticipantID, ProposalID: e.proposal.ID,
		ConsentVersion: "consent-v1", Now: e.clock.Now(), SeatTTL: 30 * time.Minute,
	})
	if err != nil {
		e.t.Fatalf("ReserveSeatAndCreateEnrollment: %v", err)
	}
	if created || again.ID != en.ID {
		e.t.Fatalf("reserve after a flag = %s (created %v); want the flagged %s back: its money is uncertain", again.ID, created, en.ID)
	}
}
