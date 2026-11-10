package worker

import "testing"

func SetFeeBackfillBatch(t testing.TB, n int) {
	old := feeBackfillBatch
	feeBackfillBatch = n
	t.Cleanup(func() { feeBackfillBatch = old })
}
