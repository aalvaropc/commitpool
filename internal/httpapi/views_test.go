package httpapi

import (
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
)

func TestStatusLabel_CopyForEveryStatus(t *testing.T) {
	funding, confirmed, cancelled := domain.WorkshopFunding, domain.WorkshopConfirmed, domain.WorkshopCancelled
	cases := []struct {
		status   domain.EnrollmentStatus
		workshop domain.WorkshopStatus
		seatHeld bool
		want     string
	}{
		{domain.EnrollmentSeatReserved, funding, true, "Seat reserved"},
		{domain.EnrollmentSeatReserved, confirmed, true, "Seat reserved"},
		{domain.EnrollmentSeatReserved, funding, false, "Seat expired"},
		{domain.EnrollmentSeatReserved, confirmed, false, "Seat expired"},
		{domain.EnrollmentSeatReserved, cancelled, false, "Seat expired"},
		{domain.EnrollmentAuthorized, funding, false, "Hold authorized"},
		{domain.EnrollmentCapturing, funding, false, "Charging…"},
		{domain.EnrollmentCapturePending, confirmed, false, "Payment pending verification"},
		{domain.EnrollmentPaid, confirmed, false, "Seat paid"},
		{domain.EnrollmentPaid, funding, false, "Seat paid"},
		{domain.EnrollmentPaid, cancelled, false, "Refund in progress"},
		{domain.EnrollmentCaptureFailed, funding, false, "Payment failed — you can try again"},
		{domain.EnrollmentCaptureFailed, confirmed, false, "Hold released"},
		{domain.EnrollmentVoiding, cancelled, false, "Hold released"},
		{domain.EnrollmentVoided, cancelled, false, "Hold released"},
		{domain.EnrollmentRefunding, cancelled, false, "Refund in progress"},
		{domain.EnrollmentRefundPending, cancelled, false, "Refund in progress"},
		{domain.EnrollmentRefunded, cancelled, false, "Refund completed"},
		{domain.EnrollmentRefundFailed, cancelled, false, "Refund needs attention"},
		{domain.EnrollmentNeedsAttention, funding, false, "Needs attention"},
		{domain.EnrollmentReleased, funding, false, "Seat expired"},
		{"SOMETHING_NEW", funding, false, "Needs attention"},
	}
	for _, c := range cases {
		if got := statusLabel(c.status, c.workshop, c.seatHeld); got != c.want {
			t.Errorf("statusLabel(%s, %s, seat held %v) = %q, want %q", c.status, c.workshop, c.seatHeld, got, c.want)
		}
	}
}

func TestDateLabel_UsesTheWorkshopClock(t *testing.T) {
	lima, err := time.LoadLocation("America/Lima")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 14, 20, 0, 0, 0, time.UTC)
	got := dateLabel(store.CandidateDateRow{StartsAt: start, EndsAt: start.Add(3 * time.Hour)}, lima)
	if want := "Sat 14 Nov 2026, 15:00–18:00 America/Lima"; got != want {
		t.Errorf("same-day label = %q, want %q", got, want)
	}
	late := time.Date(2026, 11, 15, 3, 0, 0, 0, time.UTC)
	got = dateLabel(store.CandidateDateRow{StartsAt: late, EndsAt: late.Add(3 * time.Hour)}, lima)
	if want := "Sat 14 Nov 2026, 22:00 – Sun 15 Nov 2026, 01:00 America/Lima"; got != want {
		t.Errorf("overnight label = %q, want %q", got, want)
	}
}
