package worker

import "testing"

func SetFeeBackfillBatch(t testing.TB, n int) {
	old := feeBackfillBatch
	feeBackfillBatch = n
	t.Cleanup(func() { feeBackfillBatch = old })
}

func EnablePayouts(r *Runner) { r.cfg.Payouts = true }
