package worker

import (
	"context"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
)

func (r *Runner) CancelWorkshop(ctx context.Context, workshopID, reason string) error {
	for range casRetries {
		now := r.clock.Now()
		w, err := r.st.WorkshopByID(ctx, workshopID)
		if err != nil {
			return err
		}
		p, err := r.st.ActiveProposal(ctx, workshopID)
		if err != nil {
			return err
		}
		var starts *time.Time
		if p != nil {
			starts = &p.StartsAt
		}
		if err := domain.CanCancel(w.Status, starts, now); err != nil {
			return err
		}
		resolution := domain.ResolutionResolving
		switch {
		case w.Resolution == domain.ResolutionNeedsAttention:
			resolution = domain.ResolutionNeedsAttention
		case w.Status == domain.WorkshopCollecting:
			resolution = domain.ResolutionSettled
		}
		ok, err := r.st.CancelWorkshop(ctx, workshopID, w.Status, w.Resolution, resolution, reason, now)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		r.log.Info("workshop cancelled by its organizer", "workshop_id", workshopID, "from", w.Status, "resolution", resolution)
		r.notifyCancelled(ctx, workshopID)
		if p == nil {
			return nil
		}
		if _, err := r.st.ReleaseReservedSeats(ctx, workshopID, now); err != nil {
			return err
		}
		return r.HealWorkshop(ctx, workshopID)
	}
	return errWorkshopMoving
}
