package worker

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/aalvaropc/commitpool/internal/paypal"
	"github.com/aalvaropc/commitpool/internal/store"
)

var feeBackfillBatch = 20

const (
	feeBackfillRetryAfter = time.Hour
	feeBackfillTimeout    = time.Minute
)

var errNoFeesToRecord = errors.New("worker: no fees to record from this capture")

type feeRetries struct {
	mu    sync.Mutex
	until map[string]time.Time
}

func (f *feeRetries) waiting(now time.Time) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	ids := []string{}
	for id, at := range f.until {
		if now.Before(at) {
			ids = append(ids, id)
		} else {
			delete(f.until, id)
		}
	}
	return ids
}

func (f *feeRetries) later(id string, at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.until == nil {
		f.until = map[string]time.Time{}
	}
	f.until[id] = at
}

func (r *Runner) backfillFees(ctx context.Context, now time.Time) error {
	attempts, err := r.st.AttemptsMissingFees(ctx, feeBackfillBatch, r.retries.waiting(now))
	if err != nil || len(attempts) == 0 {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, feeBackfillTimeout)
	defer cancel()
	recorded := 0
	for _, a := range attempts {
		log := r.log.With("attempt_id", a.ID, "capture_id", *a.CaptureID)
		c, err := r.pp.GetCapture(ctx, *a.CaptureID)
		if err != nil {
			r.retries.later(a.ID, now.Add(feeBackfillRetryAfter))
			log.Warn("fee backfill: PayPal did not return the capture; trying again later", "error", err)
			continue
		}
		problem := ""
		_, err = r.st.UpdateAttemptFunc(ctx, a.ID, func(row *store.AttemptRow) error {
			switch {
			case row.CaptureFeeMinor != nil || row.CaptureNetMinor != nil:
				return errNoFeesToRecord
			case row.CaptureID == nil || *row.CaptureID != c.ID:
				problem = "PayPal returned another capture"
			case c.Status != paypal.StatusCompleted && c.Status != paypal.StatusRefunded && c.Status != paypal.StatusPartiallyRefunded:
				problem = "PayPal reports the capture " + c.Status
			default:
				problem = fillFees(row, c.SellerReceivableBreakdown)
			}
			if problem != "" || row.CaptureFeeMinor == nil {
				return errNoFeesToRecord
			}
			return nil
		})
		switch {
		case errors.Is(err, errNoFeesToRecord):
			if problem != "" {
				r.retries.later(a.ID, now.Add(feeBackfillRetryAfter))
				log.Warn("fee backfill: fees not recorded: " + problem)
			}
		case err != nil:
			return err
		default:
			recorded++
		}
	}
	if recorded > 0 {
		r.log.Info("fee backfill: recorded PayPal's fees for captures recorded without them", "count", recorded)
	}
	return nil
}
