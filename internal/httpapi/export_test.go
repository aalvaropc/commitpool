package httpapi

import (
	"context"
	"testing"
	"time"
)

// SetCheckoutWait replaces, for the rest of the test, how long an enrollment
// waits for a create_order another request is running and how it pauses
// between looks.
func SetCheckoutWait(t testing.TB, wait time.Duration, pause func(context.Context, time.Duration) error) {
	oldWait, oldPause := checkoutWait, checkoutPause
	checkoutWait, checkoutPause = wait, pause
	t.Cleanup(func() { checkoutWait, checkoutPause = oldWait, oldPause })
}

// ExplanationRetryAfter is how long a failed explanation's plain text is
// served before the interpreter is asked again.
const ExplanationRetryAfter = explanationRetryAfter

// SetAfterExplanation runs f, for the rest of the test, each time a
// background explanation has been stored.
func SetAfterExplanation(t testing.TB, f func()) {
	old := afterExplanation.Load()
	afterExplanation.Store(&f)
	t.Cleanup(func() { afterExplanation.Store(old) })
}

// SetBeforeSeatRenewal replaces, for the rest of the test, what runs as an
// expired seat starts being renewed, so a test can let another request act
// first.
func SetBeforeSeatRenewal(t testing.TB, f func(ctx context.Context, enrollmentID string)) {
	old := beforeSeatRenewal
	beforeSeatRenewal = f
	t.Cleanup(func() { beforeSeatRenewal = old })
}
