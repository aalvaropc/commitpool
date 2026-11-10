package domain_test

import (
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
)

func TestPayoutState_MapsPayPalsBatchAndItemStatuses(t *testing.T) {
	cases := []struct {
		batch, item string
		want        domain.PayoutStatus
	}{
		{"SUCCESS", "SUCCESS", domain.PayoutSucceeded},
		{"PROCESSING", "SUCCESS", domain.PayoutSucceeded},
		{"SUCCESS", "UNCLAIMED", domain.PayoutUnclaimed},
		{"SUCCESS", "FAILED", domain.PayoutFailed},
		{"SUCCESS", "RETURNED", domain.PayoutFailed},
		{"SUCCESS", "BLOCKED", domain.PayoutFailed},
		{"SUCCESS", "REFUNDED", domain.PayoutFailed},
		{"SUCCESS", "REVERSED", domain.PayoutFailed},
		{"SUCCESS", "DENIED", domain.PayoutFailed},
		{"SUCCESS", "CANCELED", domain.PayoutFailed},
		{"DENIED", "", domain.PayoutFailed},
		{"CANCELED", "", domain.PayoutFailed},
		{"DENIED", "UNCLAIMED", domain.PayoutFailed},
		{"PENDING", "", domain.PayoutSent},
		{"PROCESSING", "PENDING", domain.PayoutSent},
		{"SUCCESS", "ONHOLD", domain.PayoutSent},
		{"", "", domain.PayoutSent},
		{"success", "success", domain.PayoutSucceeded},
	}
	for _, c := range cases {
		if got := domain.PayoutState(c.batch, c.item); got != c.want {
			t.Errorf("PayoutState(%q, %q) = %s, want %s", c.batch, c.item, got, c.want)
		}
	}
}

func TestNextPayoutStatus_NeverMovesAFinalPayoutBack(t *testing.T) {
	cases := []struct {
		current, read, want domain.PayoutStatus
	}{
		{domain.PayoutSent, domain.PayoutSucceeded, domain.PayoutSucceeded},
		{domain.PayoutUnclaimed, domain.PayoutSent, domain.PayoutSent},
		{domain.PayoutUnknown, domain.PayoutUnclaimed, domain.PayoutUnclaimed},
		{domain.PayoutSucceeded, domain.PayoutSent, domain.PayoutSucceeded},
		{domain.PayoutSucceeded, domain.PayoutUnclaimed, domain.PayoutSucceeded},
		{domain.PayoutSucceeded, domain.PayoutFailed, domain.PayoutFailed},
		{domain.PayoutFailed, domain.PayoutSent, domain.PayoutFailed},
	}
	for _, c := range cases {
		if got := domain.NextPayoutStatus(c.current, c.read); got != c.want {
			t.Errorf("NextPayoutStatus(%s, %s) = %s, want %s", c.current, c.read, got, c.want)
		}
	}
}

func TestPayoutBlocker_EveryRuleRefusesOnItsOwn(t *testing.T) {
	ready := domain.PayoutFacts{
		Status: domain.WorkshopConfirmed, Resolution: domain.ResolutionOpen, PayoutEmail: "iris@example.com",
		ClosesAt: closesAt, StartsAt: startsAt, Paid: 3, Net: 2748,
	}
	afterStart := startsAt.Add(time.Minute)
	if got := domain.PayoutBlocker(ready, afterStart, false); got != "" {
		t.Fatalf("ready facts are blocked by %q", got)
	}
	cases := []struct {
		name  string
		edit  func(*domain.PayoutFacts)
		now   time.Time
		early bool
		want  string
	}{
		{"funding", func(f *domain.PayoutFacts) { f.Status = domain.WorkshopFunding }, afterStart, false, domain.PayoutNotConfirmed},
		{"cancelled", func(f *domain.PayoutFacts) { f.Status = domain.WorkshopCancelled }, afterStart, false, domain.PayoutNotConfirmed},
		{"flagged workshop", func(f *domain.PayoutFacts) { f.Resolution = domain.ResolutionNeedsAttention }, afterStart, false, domain.PayoutNeedsAttention},
		{"no email", func(f *domain.PayoutFacts) { f.PayoutEmail = "" }, afterStart, false, domain.PayoutNoEmail},
		{"payments still open", func(*domain.PayoutFacts) {}, closesAt.Add(-time.Second), true, domain.PayoutPaymentsOpen},
		{"before the start", func(*domain.PayoutFacts) {}, startsAt.Add(-time.Second), false, domain.PayoutNotStarted},
		{"in flight", func(f *domain.PayoutFacts) { f.InFlight = 1 }, afterStart, false, domain.PayoutInFlight},
		{"flagged seat", func(f *domain.PayoutFacts) { f.Flagged = 1 }, afterStart, false, domain.PayoutFlagged},
		{"fees missing", func(f *domain.PayoutFacts) { f.MissingNet = 1 }, afterStart, false, domain.PayoutFeesMissing},
		{"nobody paid", func(f *domain.PayoutFacts) { f.Paid, f.Net = 0, 0 }, afterStart, false, domain.PayoutNothingToPay},
		{"fees took it all", func(f *domain.PayoutFacts) { f.Net = 0 }, afterStart, false, domain.PayoutNothingToPay},
	}
	for _, c := range cases {
		f := ready
		c.edit(&f)
		if got := domain.PayoutBlocker(f, c.now, c.early); got != c.want {
			t.Errorf("%s: PayoutBlocker = %q, want %q", c.name, got, c.want)
		}
	}
	if got := domain.PayoutBlocker(ready, closesAt, true); got != "" {
		t.Errorf("an early payout once payments closed is blocked by %q", got)
	}
	if got := domain.PayoutBlocker(ready, startsAt, false); got != "" {
		t.Errorf("a payout exactly at the start is blocked by %q", got)
	}
}

func TestPayoutWaitIsLong_OnlyForWaitsNoPaymentEndsOnItsOwn(t *testing.T) {
	for reason, want := range map[string]bool{
		domain.PayoutNothingToPay: true, domain.PayoutFlagged: true, domain.PayoutNeedsAttention: true,
		domain.PayoutInFlight: false, domain.PayoutFeesMissing: false, domain.PayoutNotStarted: false, "": false,
	} {
		if got := domain.PayoutWaitIsLong(reason); got != want {
			t.Errorf("PayoutWaitIsLong(%q) = %v, want %v", reason, got, want)
		}
	}
}
