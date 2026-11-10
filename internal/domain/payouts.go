package domain

import (
	"slices"
	"strings"
	"time"
)

type PayoutStatus string

const (
	PayoutCreated   PayoutStatus = "created"
	PayoutSent      PayoutStatus = "sent"
	PayoutSucceeded PayoutStatus = "succeeded"
	PayoutUnclaimed PayoutStatus = "unclaimed"
	PayoutFailed    PayoutStatus = "failed"
	PayoutUnknown   PayoutStatus = "unknown"
)

func (s PayoutStatus) Final() bool { return s == PayoutSucceeded || s == PayoutFailed }

var (
	payoutItemFailures  = []string{"FAILED", "RETURNED", "BLOCKED", "REFUNDED", "REVERSED"}
	payoutBatchFailures = []string{"DENIED", "CANCELED", "CANCELLED"}
)

func PayoutState(batchStatus, itemStatus string) PayoutStatus {
	item, batch := strings.ToUpper(itemStatus), strings.ToUpper(batchStatus)
	switch {
	case item == "SUCCESS":
		return PayoutSucceeded
	case slices.Contains(payoutItemFailures, item), slices.Contains(payoutBatchFailures, batch):
		return PayoutFailed
	case item == "UNCLAIMED":
		return PayoutUnclaimed
	}
	return PayoutSent
}

func NextPayoutStatus(current, read PayoutStatus) PayoutStatus {
	if current.Final() && !read.Final() {
		return current
	}
	return read
}

var PayoutHoldStatuses = []EnrollmentStatus{
	EnrollmentAuthorized, EnrollmentCapturing, EnrollmentCapturePending,
	EnrollmentVoiding, EnrollmentRefunding, EnrollmentRefundPending,
}

type PayoutFacts struct {
	Status             WorkshopStatus
	Resolution         Resolution
	PayoutEmail        string
	ClosesAt, StartsAt time.Time
	InFlight, Flagged  int
	Paid, MissingNet   int
	Net                int64
}

const (
	PayoutNotConfirmed   = "not_confirmed"
	PayoutNeedsAttention = "needs_attention"
	PayoutNoEmail        = "no_payout_email"
	PayoutPaymentsOpen   = "payments_open"
	PayoutNotStarted     = "not_started"
	PayoutInFlight       = "payments_in_flight"
	PayoutFlagged        = "payment_flagged"
	PayoutFeesMissing    = "fees_missing"
	PayoutNothingToPay   = "nothing_to_pay"
)

func PayoutBlocker(f PayoutFacts, now time.Time, early bool) string {
	switch {
	case f.Status != WorkshopConfirmed:
		return PayoutNotConfirmed
	case f.Resolution == ResolutionNeedsAttention:
		return PayoutNeedsAttention
	case f.PayoutEmail == "":
		return PayoutNoEmail
	case now.Before(f.ClosesAt):
		return PayoutPaymentsOpen
	case !early && now.Before(f.StartsAt):
		return PayoutNotStarted
	case f.InFlight > 0:
		return PayoutInFlight
	case f.Flagged > 0:
		return PayoutFlagged
	case f.MissingNet > 0:
		return PayoutFeesMissing
	case f.Paid == 0 || f.Net <= 0:
		return PayoutNothingToPay
	}
	return ""
}
