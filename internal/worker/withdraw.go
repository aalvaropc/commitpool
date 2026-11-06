package worker

import (
	"context"
	"fmt"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
)

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
