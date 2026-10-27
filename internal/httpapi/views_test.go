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
		want     string
	}{
		{domain.EnrollmentSeatReserved, funding, "Seat reserved"},
		{domain.EnrollmentSeatReserved, cancelled, "Seat expired"},
		{domain.EnrollmentAuthorized, funding, "Hold authorized"},
		{domain.EnrollmentCapturing, funding, "Charging…"},
		{domain.EnrollmentCapturePending, confirmed, "Payment pending verification"},
		{domain.EnrollmentPaid, confirmed, "Seat paid"},
		{domain.EnrollmentPaid, funding, "Seat paid"},
		{domain.EnrollmentPaid, cancelled, "Refund in progress"},
		{domain.EnrollmentCaptureFailed, funding, "Payment failed — you can try again"},
		{domain.EnrollmentCaptureFailed, confirmed, "Hold released"},
		{domain.EnrollmentVoiding, cancelled, "Hold released"},
		{domain.EnrollmentVoided, cancelled, "Hold released"},
		{domain.EnrollmentRefunding, cancelled, "Refund in progress"},
		{domain.EnrollmentRefundPending, cancelled, "Refund in progress"},
		{domain.EnrollmentRefunded, cancelled, "Refund completed"},
		{domain.EnrollmentRefundFailed, cancelled, "Refund needs attention"},
		{domain.EnrollmentNeedsAttention, funding, "Needs attention"},
		{domain.EnrollmentReleased, funding, "Seat expired"},
		{"SOMETHING_NEW", funding, "Needs attention"},
	}
	for _, c := range cases {
		if got := statusLabel(c.status, c.workshop); got != c.want {
			t.Errorf("statusLabel(%s, %s) = %q, want %q", c.status, c.workshop, got, c.want)
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
