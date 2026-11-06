package httpapi

import (
	"context"
	"testing"
	"time"
)

func SetCheckoutWait(t testing.TB, wait time.Duration, pause func(context.Context, time.Duration) error) {
	oldWait, oldPause := checkoutWait, checkoutPause
	checkoutWait, checkoutPause = wait, pause
	t.Cleanup(func() { checkoutWait, checkoutPause = oldWait, oldPause })
}

const ExplanationRetryAfter = explanationRetryAfter

func SetAfterExplanation(t testing.TB, f func()) {
	old := afterExplanation.Load()
	afterExplanation.Store(&f)
	t.Cleanup(func() { afterExplanation.Store(old) })
}

func SetBeforeSeatRenewal(t testing.TB, f func(ctx context.Context, enrollmentID string)) {
	old := beforeSeatRenewal
	beforeSeatRenewal = f
	t.Cleanup(func() { beforeSeatRenewal = old })
}

func SetLimits(t testing.TB, participants, submissions, slots int, wait time.Duration) {
	oldP, oldS, oldSlots, oldWait := maxParticipantsPerWorkshop, maxSubmissionsPerParticipant, aiSlots, aiWait
	maxParticipantsPerWorkshop, maxSubmissionsPerParticipant, aiSlots, aiWait = participants, submissions, slots, wait
	t.Cleanup(func() {
		maxParticipantsPerWorkshop, maxSubmissionsPerParticipant, aiSlots, aiWait = oldP, oldS, oldSlots, oldWait
	})
}
