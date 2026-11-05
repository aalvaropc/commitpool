package worker

import (
	"context"
	"fmt"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
)

// Withdraw gives a participant's seat up (store.Withdraw): a reserved seat is
// released, and a hold not yet charged goes to VOIDING with void:{attempt}
// enqueued due now; voidKey names that job, for a caller that runs it inline,
// and is empty otherwise.
func (r *Runner) Withdraw(ctx context.Context, enrollmentID string) (status domain.EnrollmentStatus, voidKey string, err error) {
	status, err = r.st.Withdraw(ctx, enrollmentID, r.clock.Now())
	if err != nil {
		return "", "", err
	}
	en, err := r.st.EnrollmentByID(ctx, enrollmentID)
	if err != nil {
		return status, "", err
	}
	r.log.Info("participant withdrew", "workshop_id", en.WorkshopID, "enrollment_id", en.ID, "to", status)
	if status != domain.EnrollmentVoiding {
		return status, "", nil
	}
	a, err := r.st.LatestAttempt(ctx, enrollmentID)
	if err != nil {
		return status, "", err
	}
	if a == nil {
		return status, "", fmt.Errorf("enrollment %s: no attempt holds its authorization: %w", enrollmentID, store.ErrNotFound)
	}
	voidKey = store.JobKey(store.KindVoid, a.ID)
	if _, err := r.EnqueueOrRevive(ctx, store.KindVoid, voidKey, &en.WorkshopID, &a.ID, nil, r.clock.Now()); err != nil {
		return status, "", err
	}
	return status, voidKey, nil
}
