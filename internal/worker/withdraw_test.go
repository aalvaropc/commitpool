package worker_test

import (
	"errors"
	"testing"

	"github.com/aalvaropc/commitpool/internal/domain"
)

func (e *env) withdraw(enrollmentID string) (domain.EnrollmentStatus, error) {
	e.t.Helper()
	st, _, err := e.runner.Withdraw(e.ctx, enrollmentID)
	return st, err
}

func TestWithdraw_ReservedSeatReleased(t *testing.T) {
	e := newEnv(t)
	en, _ := e.enroll("Ana")

	st, err := e.withdraw(en.ID)

	if err != nil || st != domain.EnrollmentReleased {
		t.Fatalf("Withdraw = %s, %v; want RELEASED", st, err)
	}
	got, _ := e.st.EnrollmentByID(e.ctx, en.ID)
	if got.Status != domain.EnrollmentReleased || got.WithdrawnAt == nil {
		t.Errorf("enrollment = %s withdrawn %v; want RELEASED and withdrawn", got.Status, got.WithdrawnAt)
	}
}

func TestWithdraw_AuthorizedHoldVoided(t *testing.T) {
	e := newEnv(t)
	en, a := e.authorized("Ana")

	st, err := e.withdraw(en.ID)
	if err != nil || st != domain.EnrollmentVoiding {
		t.Fatalf("Withdraw = %s, %v; want VOIDING", st, err)
	}
	e.runUntilIdle()

	e.wantStatus(en.ID, domain.EnrollmentVoided)
	wantString(t, "authorization_status", e.attempt(a.ID).AuthorizationStatus, "VOIDED")
}

func TestWithdraw_BeatsAQueuedCapture(t *testing.T) {
	e := newEnv(t)
	en, a := e.authorized("Ana")
	e.enqueue("capture", a)

	if _, err := e.withdraw(en.ID); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	e.runUntilIdle()

	e.wantStatus(en.ID, domain.EnrollmentVoided)
	if n := len(e.fake.Requests("capture")); n != 0 {
		t.Errorf("capture requests = %d, want 0: the withdrawal came first", n)
	}
}

func TestWithdraw_RefusedOnceMoneyIsMovingOrUnderReview(t *testing.T) {
	e := newEnv(t)
	capturing, _ := e.authorized("Ana")
	e.transition(capturing.ID, domain.EnrollmentAuthorized, domain.EnrollmentCapturing)
	paid, _ := e.paid("Ben")
	flagged, _ := e.authorized("Cleo")
	e.flag(flagged.ID)
	review, reviewAttempt := e.enroll("Dan")
	if _, err := e.st.Pool.Exec(e.ctx, `update payment_attempts set authorization_status = 'PENDING' where id = $1`, reviewAttempt.ID); err != nil {
		t.Fatal(err)
	}
	released, _ := e.enroll("Eve")
	e.transition(released.ID, domain.EnrollmentSeatReserved, domain.EnrollmentReleased)

	for _, c := range []struct {
		id   string
		want error
	}{
		{capturing.ID, domain.ErrCaptureInProgress},
		{paid.ID, domain.ErrPaidNoSelfRefund},
		{flagged.ID, domain.ErrNeedsOperator},
		{review.ID, domain.ErrHoldUnderReview},
		{released.ID, domain.ErrNothingToWithdraw},
	} {
		if _, err := e.withdraw(c.id); !errors.Is(err, c.want) {
			t.Errorf("Withdraw(%s) = %v, want %v", c.id, err, c.want)
		}
	}
	e.wantStatus(paid.ID, domain.EnrollmentPaid)
	e.wantStatus(capturing.ID, domain.EnrollmentCapturing)
}

func TestReReserveSeat_RefusesAWithdrawnEnrollment(t *testing.T) {
	e := newEnv(t)
	en, _ := e.enroll("Ana")
	if _, err := e.withdraw(en.ID); err != nil {
		t.Fatal(err)
	}

	reserved, err := e.st.ReReserveSeat(e.ctx, en.ID, e.clock.Now(), 30*60e9)

	if err != nil || reserved {
		t.Errorf("ReReserveSeat after a withdrawal = %v, %v; want false: the person gave the seat up", reserved, err)
	}
}
