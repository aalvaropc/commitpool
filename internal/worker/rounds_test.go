package worker_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
)

// confirmed brings the env workshop to CONFIRMED through a real capture round:
// three participants authorize, the third authorization starts the round and
// the three captures complete. Payments stay open, but a confirmed workshop
// takes no new reservations, so anyone else must have enrolled before.
func (e *env) confirmed() []store.AttemptRow {
	e.t.Helper()
	var attempts []store.AttemptRow
	for _, name := range []string{"Ana", "Ben", "Cleo"} {
		_, a := e.authorized(name)
		attempts = append(attempts, a)
	}
	e.runUntilIdle()
	e.wantWorkshop(domain.WorkshopConfirmed, domain.ResolutionOpen)
	return attempts
}

func TestRound_ThreePaidOneFailedConfirms(t *testing.T) {
	e := newEnv(t)
	var enrollments []store.EnrollmentRow
	var attempts []store.AttemptRow
	for _, name := range []string{"Ana", "Ben", "Cleo"} {
		en, a := e.authorized(name)
		enrollments = append(enrollments, en)
		attempts = append(attempts, a)
	}
	dan, danAttempt := e.withOrder("Dan")
	e.setMockCode(danAttempt, "TRANSACTION_REFUSED")
	e.fake.Approve(*danAttempt.OrderID)
	e.runKey(e.enqueue("authorize", danAttempt))
	e.wantStatus(dan.ID, domain.EnrollmentAuthorized)

	for _, a := range append(attempts, danAttempt) {
		e.wantJob("capture:"+a.ID, "pending")
	}
	if n := len(e.fake.Requests("capture")); n != 0 {
		t.Fatalf("capture requests before the round ran = %d, want 0", n)
	}

	e.runUntilIdle()

	for _, en := range enrollments {
		e.wantStatus(en.ID, domain.EnrollmentPaid)
	}
	e.wantStatus(dan.ID, domain.EnrollmentCaptureFailed)
	wantString(t, "refused capture issue", e.attempt(danAttempt.ID).CaptureIssue, "TRANSACTION_REFUSED")
	e.wantJob("void:"+danAttempt.ID, "succeeded")
	wantString(t, "refused capture authorization", e.attempt(danAttempt.ID).AuthorizationStatus, "VOIDED")
	e.wantWorkshop(domain.WorkshopConfirmed, domain.ResolutionOpen)
	if got := e.fake.Captures(); got != 3 {
		t.Errorf("captures at PayPal = %d, want 3", got)
	}
	for _, a := range append(attempts, danAttempt) {
		if n := e.requestsWithID("capture", "capture:"+a.ID); n != 1 {
			t.Errorf("capture requests under capture:%s = %d, want 1", a.ID, n)
		}
	}
	if n := len(e.fake.Requests("refund")); n != 0 {
		t.Errorf("refund requests = %d, want 0", n)
	}
}

func TestRound_NotStartedBelowMinimumAuthorized(t *testing.T) {
	e := newEnv(t)
	_, ana := e.authorized("Ana")
	_, ben := e.authorized("Ben")
	e.enroll("Cleo")

	if err := e.runner.StartCaptureRoundIfReady(e.ctx, e.workshop.ID); err != nil {
		t.Fatalf("StartCaptureRoundIfReady: %v", err)
	}
	e.runUntilIdle()

	e.wantNoJob("capture:" + ana.ID)
	e.wantNoJob("capture:" + ben.ID)
	if n := len(e.fake.Requests("capture")); n != 0 {
		t.Errorf("capture requests = %d, want 0", n)
	}
	e.wantWorkshop(domain.WorkshopFunding, domain.ResolutionOpen)
}

func TestConfirmed_NewAuthorizationCapturesAlone(t *testing.T) {
	e := newEnv(t)
	dan, danAttempt := e.withOrder("Dan")
	first := e.confirmed()

	danAttempt = e.authorize(dan, danAttempt)
	e.wantJob("capture:"+danAttempt.ID, "pending")
	e.runUntilIdle()

	e.wantStatus(dan.ID, domain.EnrollmentPaid)
	reqs := e.fake.Requests("capture")
	if len(reqs) != 4 || reqs[3].RequestID != "capture:"+danAttempt.ID {
		t.Fatalf("capture requests = %+v, want the round's three and then capture:%s", reqs, danAttempt.ID)
	}
	for _, a := range first {
		if n := e.requestsWithID("capture", "capture:"+a.ID); n != 1 {
			t.Errorf("capture requests under capture:%s = %d, want 1", a.ID, n)
		}
	}
	if got := e.fake.Captures(); got != 4 {
		t.Errorf("captures at PayPal = %d, want 4", got)
	}
	e.wantNoJob("refund:" + danAttempt.ID)
	e.wantWorkshop(domain.WorkshopConfirmed, domain.ResolutionOpen)
}

func TestRound_ConfirmedWorkshopKeepsAuthorizedWhoseCaptureStartedAtClose(t *testing.T) {
	e := newEnv(t)
	e.nearClose()
	dan, danAttempt := e.withOrder("Dan")
	eve, eveAttempt := e.withOrder("Eve")
	e.confirmed()
	danAttempt = e.authorize(dan, danAttempt)
	e.wantJob("capture:"+danAttempt.ID, "pending")
	eveAttempt = e.authorizeJustBeforeClose(eve, eveAttempt)
	e.dropJob("capture:" + eveAttempt.ID)
	e.clock.Set(e.proposal.ClosesAt)

	e.enqueueClose()
	e.runKey(e.closeKey())

	e.wantJob(e.closeKey(), "succeeded")
	e.wantWorkshop(domain.WorkshopConfirmed, domain.ResolutionOpen)
	e.wantNoJob("void:" + danAttempt.ID)
	e.wantJob("void:"+eveAttempt.ID, "pending")

	e.runUntilIdle()

	e.wantStatus(dan.ID, domain.EnrollmentPaid)
	e.wantStatus(eve.ID, domain.EnrollmentVoided)
	e.wantNoJob("refund:" + danAttempt.ID)
	if got := e.fake.Captures(); got != 4 {
		t.Errorf("captures at PayPal = %d, want 4", got)
	}
	e.wantWorkshop(domain.WorkshopConfirmed, domain.ResolutionOpen)
}

func TestLateCapture_AfterConfirmKeepsSeatBeforeStart(t *testing.T) {
	e := newEnv(t, withClientTimeout(time.Second))
	dan, danAttempt := e.withOrder("Dan")
	eve, eveAttempt := e.withOrder("Eve")
	e.confirmed()
	danAttempt = e.authorize(dan, danAttempt)
	danCapture := e.capturePending(dan, danAttempt)

	e.fake.SetCaptureStatus(danCapture, "COMPLETED", "")
	e.runKey(e.reconcileNow(danAttempt))

	e.wantStatus(dan.ID, domain.EnrollmentPaid)
	e.wantNoJob("refund:" + danAttempt.ID)
	e.wantWorkshop(domain.WorkshopConfirmed, domain.ResolutionOpen)

	eveAttempt = e.authorize(eve, eveAttempt)
	eveCapture := e.capturePending(eve, eveAttempt)
	e.clock.Set(e.proposal.StartsAt)
	e.fake.SetCaptureStatus(eveCapture, "COMPLETED", "")
	e.runKey(e.reconcileNow(eveAttempt))

	e.wantStatus(eve.ID, domain.EnrollmentPaid)
	e.wantJob("refund:"+eveAttempt.ID, "pending")
	e.runUntilIdle()
	e.wantStatus(eve.ID, domain.EnrollmentRefunded)
	e.wantStatus(dan.ID, domain.EnrollmentPaid)
	if reqs := e.fake.Requests("refund"); len(reqs) != 1 || reqs[0].RequestID != "refund:"+eveAttempt.ID {
		t.Errorf("refund requests = %+v, want only refund:%s", reqs, eveAttempt.ID)
	}
	e.wantWorkshop(domain.WorkshopConfirmed, domain.ResolutionOpen)
}

func TestLateCapture_AfterCancelEnqueuesRefund(t *testing.T) {
	e := newEnv(t, withClientTimeout(time.Second))
	ana, _ := e.paid("Ana")
	ben, benAttempt := e.authorized("Ben")
	benCapture := e.capturePending(ben, benAttempt)
	e.clock.Set(e.proposal.ClosesAt)
	e.enqueueClose()
	e.runUntilIdle()
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionResolving)
	e.wantStatus(ana.ID, domain.EnrollmentRefunded)
	e.wantStatus(ben.ID, domain.EnrollmentCapturePending)
	e.wantJob("reconcile:"+benAttempt.ID+":close", "succeeded")
	e.wantNoJob("refund:" + benAttempt.ID)

	e.fake.SetCaptureStatus(benCapture, "COMPLETED", "")
	e.runKey(e.reconcileNow(benAttempt))

	e.wantStatus(ben.ID, domain.EnrollmentPaid)
	e.wantJob("refund:"+benAttempt.ID, "pending")
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionResolving)

	e.runUntilIdle()
	e.wantStatus(ben.ID, domain.EnrollmentRefunded)
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionSettled)
	if got := e.fake.Captures(); got != 2 {
		t.Errorf("captures at PayPal = %d, want 2", got)
	}
	if n := e.requestsWithID("refund", "refund:"+benAttempt.ID); n != 1 {
		t.Errorf("refund requests for the late capture = %d, want 1", n)
	}
}

func TestReconcile_AuthorizesApprovedOrderWithoutReturn(t *testing.T) {
	e := newEnv(t)

	ana, anaAttempt := e.withOrder("Ana")
	e.fake.Approve(*anaAttempt.OrderID)
	e.reconcileNow(anaAttempt)
	e.runUntilIdle()

	e.wantStatus(ana.ID, domain.EnrollmentAuthorized)
	e.wantJob("authorize:"+anaAttempt.ID, "succeeded")
	if n := e.requestsWithID("authorize", "authorize:"+anaAttempt.ID); n != 1 {
		t.Errorf("authorize requests = %d, want 1", n)
	}

	ben, benAttempt := e.withOrder("Ben")
	e.advance(31 * time.Minute)
	e.fake.Approve(*benAttempt.OrderID)
	e.reconcileNow(benAttempt)
	e.runUntilIdle()

	e.wantStatus(ben.ID, domain.EnrollmentAuthorized)
	got, err := e.st.EnrollmentByID(e.ctx, ben.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.SeatReservedAt.Equal(e.clock.Now()) || got.ReleasedAt != nil {
		t.Errorf("expired seat reserved at %s (released at %v), want taken again at %s", got.SeatReservedAt, deref(got.ReleasedAt), e.clock.Now())
	}

	cleo, cleoAttempt := e.withOrder("Cleo")
	e.advance(31 * time.Minute)
	e.runKey(e.releaseSeatsNow())
	e.wantStatus(cleo.ID, domain.EnrollmentReleased)
	for i := range 4 {
		if _, err := e.reserve(e.workshop, e.proposal, fmt.Sprintf("Seat %d", i)); err != nil {
			t.Fatalf("reserve seat %d: %v", i, err)
		}
	}
	if _, err := e.reserve(e.workshop, e.proposal, "One too many"); !errors.Is(err, domain.ErrCapacityFull) {
		t.Fatalf("reserve past capacity = %v, want ErrCapacityFull", err)
	}
	e.fake.Approve(*cleoAttempt.OrderID)
	e.reconcileNow(cleoAttempt)
	e.runUntilIdle()

	e.wantStatus(cleo.ID, domain.EnrollmentReleased)
	wantString(t, "order status", e.attempt(cleoAttempt.ID).OrderStatus, "APPROVED")
	e.wantNoJob("authorize:" + cleoAttempt.ID)
	if n := e.requestsWithID("authorize", "authorize:"+cleoAttempt.ID); n != 0 {
		t.Errorf("authorize requests without a seat = %d, want 0", n)
	}

	held, err := e.st.EnrollmentsByWorkshop(e.ctx, e.workshop.ID)
	if err != nil {
		t.Fatal(err)
	}
	e.clock.Set(e.proposal.ClosesAt.Add(-10 * time.Minute))
	for _, en := range held {
		if en.Status == domain.EnrollmentSeatReserved {
			e.transition(en.ID, domain.EnrollmentSeatReserved, domain.EnrollmentReleased)
		}
	}
	dan, danAttempt := e.withOrder("Dan")
	e.clock.Set(e.proposal.ClosesAt)
	e.fake.Approve(*danAttempt.OrderID)
	e.reconcileNow(danAttempt)
	e.runUntilIdle()

	e.wantStatus(dan.ID, domain.EnrollmentSeatReserved)
	e.wantNoJob("authorize:" + danAttempt.ID)
	if n := e.requestsWithID("authorize", "authorize:"+danAttempt.ID); n != 0 {
		t.Errorf("authorize requests once payments closed = %d, want 0", n)
	}
}

func TestReconcile_RevivesAuthorizeRefusedBeforeApproval(t *testing.T) {
	e := newEnv(t)
	en, a := e.withOrder("Ana")
	key := e.enqueue("authorize", a)
	e.runKey(key)
	j := e.wantJob(key, "failed")
	wantString(t, "provider_issue", j.ProviderIssue, "ORDER_NOT_APPROVED")

	e.fake.Approve(*a.OrderID)
	e.reconcileNow(a)
	e.runUntilIdle()

	e.wantStatus(en.ID, domain.EnrollmentAuthorized)
	e.wantJob(key, "succeeded")
	if n := e.requestsWithID("authorize", key); n != 2 {
		t.Errorf("authorize requests under %s = %d, want the refused one and the revived one", key, n)
	}
}

func TestRound_ReevaluatedAfterRefusalAndLateAuthorization(t *testing.T) {
	e := newEnv(t)
	ana, anaAttempt := e.authorized("Ana")
	ben, _ := e.authorized("Ben")
	cleo, _ := e.authorized("Cleo")
	dan, danAttempt := e.withOrder("Dan")
	e.setMockCode(anaAttempt, "TRANSACTION_REFUSED")
	e.runKey("capture:" + anaAttempt.ID)
	e.wantStatus(ana.ID, domain.EnrollmentCaptureFailed)
	// Ben's capture request is in flight: its handler has moved him to CAPTURING.
	e.transition(ben.ID, domain.EnrollmentAuthorized, domain.EnrollmentCapturing)
	danAttempt = e.authorize(dan, danAttempt)
	e.wantNoJob("capture:" + danAttempt.ID)

	e.runUntilIdle()

	e.wantJob("capture:"+danAttempt.ID, "succeeded")
	for _, en := range []store.EnrollmentRow{ben, cleo, dan} {
		e.wantStatus(en.ID, domain.EnrollmentPaid)
	}
	e.wantWorkshop(domain.WorkshopConfirmed, domain.ResolutionOpen)
	if got := e.fake.Captures(); got != 3 {
		t.Errorf("captures at PayPal = %d, want 3", got)
	}
	if n := len(e.fake.Requests("refund")); n != 0 {
		t.Errorf("refund requests = %d, want 0", n)
	}
}
