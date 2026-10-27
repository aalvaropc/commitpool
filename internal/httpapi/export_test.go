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

// SetBeforeSeatRenewal replaces, for the rest of the test, what runs as an
// expired seat starts being renewed, so a test can let another request act
// first.
func SetBeforeSeatRenewal(t testing.TB, f func(ctx context.Context, enrollmentID string)) {
	old := beforeSeatRenewal
	beforeSeatRenewal = f
	t.Cleanup(func() { beforeSeatRenewal = old })
}
