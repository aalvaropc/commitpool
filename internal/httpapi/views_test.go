package httpapi

import (
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
)

func TestStatusLabel_CopyForEveryStatus(t *testing.T) {
	open := func(w domain.WorkshopStatus) labelState {
		return labelState{workshop: w, paymentsOpen: true, seatLive: true}
	}
	closed := func(w domain.WorkshopStatus) labelState { return labelState{workshop: w} }
	funding, confirmed, cancelled := domain.WorkshopFunding, domain.WorkshopConfirmed, domain.WorkshopCancelled
	cases := []struct {
		status domain.EnrollmentStatus
		state  labelState
		want   string
	}{
		{domain.EnrollmentSeatReserved, open(funding), "Seat reserved"},
		{domain.EnrollmentSeatReserved, open(confirmed), "Seat reserved"},
		{domain.EnrollmentSeatReserved, labelState{workshop: funding, paymentsOpen: true}, "Seat expired"},
		{domain.EnrollmentSeatReserved, labelState{workshop: confirmed, paymentsOpen: true}, "Seat expired"},
		{domain.EnrollmentSeatReserved, labelState{workshop: confirmed, seatLive: true}, "Seat expired"},
		{domain.EnrollmentSeatReserved, closed(cancelled), "Seat expired"},
		{domain.EnrollmentSeatReserved, labelState{workshop: funding, paymentsOpen: true, authPending: true}, "Hold pending verification"},
		{domain.EnrollmentSeatReserved, labelState{workshop: confirmed, authPending: true}, "Hold pending verification"},
		{domain.EnrollmentAuthorized, open(funding), "Hold authorized"},
		{domain.EnrollmentCapturing, open(funding), "Charging…"},
		{domain.EnrollmentCapturePending, closed(confirmed), "Payment pending verification"},
		{domain.EnrollmentPaid, closed(confirmed), "Seat paid"},
		{domain.EnrollmentPaid, open(funding), "Seat paid"},
		{domain.EnrollmentPaid, closed(cancelled), "Refund in progress"},
		{domain.EnrollmentCaptureFailed, open(funding), "Payment failed — you can try again"},
		{domain.EnrollmentCaptureFailed, open(confirmed), "Payment failed — you can try again"},
		{domain.EnrollmentCaptureFailed, closed(confirmed), "Hold released"},
		{domain.EnrollmentCaptureFailed, closed(cancelled), "Hold released"},
		{domain.EnrollmentVoiding, closed(cancelled), "Hold released"},
		{domain.EnrollmentVoided, closed(cancelled), "Hold released"},
		{domain.EnrollmentRefunding, closed(cancelled), "Refund in progress"},
		{domain.EnrollmentRefundPending, closed(cancelled), "Refund in progress"},
		{domain.EnrollmentRefunded, closed(cancelled), "Refund completed"},
		{domain.EnrollmentRefundFailed, closed(cancelled), "Refund needs attention"},
		{domain.EnrollmentNeedsAttention, open(funding), "Needs attention"},
		{domain.EnrollmentReleased, open(funding), "Seat expired"},
		{"SOMETHING_NEW", open(funding), "Needs attention"},
	}
	for _, c := range cases {
		if got := statusLabel(c.status, c.state); got != c.want {
			t.Errorf("statusLabel(%s, %+v) = %q, want %q", c.status, c.state, got, c.want)
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
