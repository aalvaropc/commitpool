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
