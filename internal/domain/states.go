package domain

import "time"

type WorkshopStatus string

const (
	WorkshopCollecting WorkshopStatus = "COLLECTING"
	WorkshopFunding    WorkshopStatus = "FUNDING"
	WorkshopConfirmed  WorkshopStatus = "CONFIRMED"
	WorkshopCancelled  WorkshopStatus = "CANCELLED"
)

type Resolution string

const (
	ResolutionOpen           Resolution = "OPEN"
	ResolutionResolving      Resolution = "RESOLVING"
	ResolutionSettled        Resolution = "SETTLED"
	ResolutionNeedsAttention Resolution = "NEEDS_ATTENTION"
)

type EnrollmentStatus string

const (
	EnrollmentSeatReserved   EnrollmentStatus = "SEAT_RESERVED"
	EnrollmentAuthorized     EnrollmentStatus = "AUTHORIZED"
	EnrollmentCapturing      EnrollmentStatus = "CAPTURING"
	EnrollmentCapturePending EnrollmentStatus = "CAPTURE_PENDING"
	EnrollmentPaid           EnrollmentStatus = "PAID"
	EnrollmentCaptureFailed  EnrollmentStatus = "CAPTURE_FAILED"
	EnrollmentVoiding        EnrollmentStatus = "VOIDING"
	EnrollmentVoided         EnrollmentStatus = "VOIDED"
	EnrollmentRefunding      EnrollmentStatus = "REFUNDING"
	EnrollmentRefundPending  EnrollmentStatus = "REFUND_PENDING"
	EnrollmentRefunded       EnrollmentStatus = "REFUNDED"
	EnrollmentRefundFailed   EnrollmentStatus = "REFUND_FAILED"
	EnrollmentNeedsAttention EnrollmentStatus = "NEEDS_ATTENTION"
	EnrollmentReleased       EnrollmentStatus = "RELEASED"
)

func (s EnrollmentStatus) Terminal() bool {
	switch s {
	case EnrollmentVoided, EnrollmentRefunded, EnrollmentRefundFailed,
		EnrollmentNeedsAttention, EnrollmentReleased, EnrollmentCaptureFailed:
		return true
	}
	return false
}

func SeatExpired(expiresAt, now time.Time) bool { return now.After(expiresAt) }

func (s EnrollmentStatus) Occupies() bool {
	switch s {
	case EnrollmentSeatReserved, EnrollmentAuthorized, EnrollmentCapturing,
		EnrollmentCapturePending, EnrollmentPaid:
		return true
	}
	return false
}

type Workshop struct {
	ID, Title, Timezone string
	Minimum, Capacity   int
	Price               Money
	Status              WorkshopStatus
	Resolution          Resolution
	PaymentWindow       time.Duration
}

type CandidateDate struct {
	ID               string
	StartsAt, EndsAt time.Time
}

type Proposal struct {
	ID                          string
	Version                     int
	DateID                      string
	OpensAt, ClosesAt, StartsAt time.Time
	Price                       Money
	Minimum, Capacity           int
}

type Enrollment struct {
	ID, ParticipantID string
	Status            EnrollmentStatus
	SeatExpiresAt     time.Time
	CaptureStartedAt  *time.Time
	NeedsAttention    bool
}

func (e Enrollment) NeedsOperator() bool {
	return e.NeedsAttention || e.Status == EnrollmentRefundFailed || e.Status == EnrollmentNeedsAttention
}

type Tally struct {
	Reserved, Authorized, Capturing, Pending, Paid, Failed int
	Occupying                                              int
}
