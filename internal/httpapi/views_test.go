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
		{domain.EnrollmentSeatReserved, open(funding), "At checkout"},
		{domain.EnrollmentSeatReserved, open(confirmed), "At checkout"},
		{domain.EnrollmentSeatReserved, labelState{workshop: funding, paymentsOpen: true}, "Reservation expired"},
		{domain.EnrollmentSeatReserved, labelState{workshop: confirmed, paymentsOpen: true}, "Reservation expired"},
		{domain.EnrollmentSeatReserved, labelState{workshop: confirmed, seatLive: true}, "Reservation expired"},
		{domain.EnrollmentSeatReserved, closed(cancelled), "Reservation expired"},
		{domain.EnrollmentSeatReserved, labelState{workshop: funding, paymentsOpen: true, authPending: true}, "PayPal is reviewing the hold"},
		{domain.EnrollmentSeatReserved, labelState{workshop: confirmed, authPending: true}, "PayPal is reviewing the hold"},
		{domain.EnrollmentAuthorized, open(funding), "Spot reserved — not charged yet"},
		{domain.EnrollmentAuthorized, open(confirmed), "Spot reserved — not charged yet"},
		{domain.EnrollmentAuthorized, closed(confirmed), "Not charged — releasing the hold"},
		{domain.EnrollmentAuthorized, closed(funding), "Not charged — releasing the hold"},
		{domain.EnrollmentAuthorized, labelState{workshop: cancelled, paymentsOpen: true}, "Not charged — releasing the hold"},
		{domain.EnrollmentCapturing, open(funding), "Charging…"},
		{domain.EnrollmentCapturePending, closed(confirmed), "PayPal is verifying the payment"},
		{domain.EnrollmentPaid, closed(confirmed), "Paid"},
		{domain.EnrollmentPaid, open(funding), "Paid"},
		{domain.EnrollmentPaid, closed(cancelled), "Refund on its way"},
		{domain.EnrollmentCaptureFailed, open(funding), "Payment didn't go through — try again"},
		{domain.EnrollmentCaptureFailed, open(confirmed), "Payment didn't go through — try again"},
		{domain.EnrollmentCaptureFailed, closed(confirmed), "Not charged"},
		{domain.EnrollmentCaptureFailed, closed(cancelled), "Not charged"},
		{domain.EnrollmentVoiding, closed(cancelled), "Releasing the hold…"},
		{domain.EnrollmentVoided, closed(cancelled), "Hold released — never charged"},
		{domain.EnrollmentRefunding, closed(cancelled), "Refund on its way"},
		{domain.EnrollmentRefundPending, closed(cancelled), "Refund on its way"},
		{domain.EnrollmentRefunded, closed(cancelled), "Refunded"},
		{domain.EnrollmentRefundFailed, closed(cancelled), "Refund needs a check"},
		{domain.EnrollmentNeedsAttention, open(funding), "Needs a check"},
		{domain.EnrollmentReleased, open(funding), "Reservation expired"},
		{"SOMETHING_NEW", open(funding), "Needs a check"},
	}
	for _, c := range cases {
		if got := statusLabel(c.status, c.state); got != c.want {
			t.Errorf("statusLabel(%s, %+v) = %q, want %q", c.status, c.state, got, c.want)
		}
	}
}

func TestEnrollmentView_FlaggedShowsNeedsAttentionAndTheMoneyStatus(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	at := now.Add(-time.Minute)
	en := store.EnrollmentRow{ID: "e1", Status: domain.EnrollmentAuthorized, SeatExpiresAt: now.Add(time.Hour), NeedsAttentionAt: &at}
	w := store.WorkshopRow{Status: domain.WorkshopFunding, Resolution: domain.ResolutionOpen}

	got := enrollmentView(en, w, nil, nil, now)

	if got.Status != "NEEDS_ATTENTION" || got.StatusLabel != "Needs a check" || got.MoneyStatus != "AUTHORIZED" || !got.NeedsAttention {
		t.Errorf("flagged view = status %q label %q money %q flag %v; want NEEDS_ATTENTION, Needs a check, AUTHORIZED, true",
			got.Status, got.StatusLabel, got.MoneyStatus, got.NeedsAttention)
	}
}

func TestDateLabel_UsesTheWorkshopClock(t *testing.T) {
	lima, err := time.LoadLocation("America/Lima")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 14, 20, 0, 0, 0, time.UTC)
	got := dateLabel(store.CandidateDateRow{StartsAt: start, EndsAt: start.Add(3 * time.Hour)}, lima)
	if want := "Sat, Nov 14, 3 PM–6 PM (Lima time)"; got != want {
		t.Errorf("same-day label = %q, want %q", got, want)
	}
	late := time.Date(2026, 11, 15, 3, 0, 0, 0, time.UTC)
	got = dateLabel(store.CandidateDateRow{StartsAt: late, EndsAt: late.Add(3 * time.Hour)}, lima)
	if want := "Sat, Nov 14, 10 PM – Sun, Nov 15, 1 AM (Lima time)"; got != want {
		t.Errorf("overnight label = %q, want %q", got, want)
	}
}

func TestTally_CountsChargesInFlight(t *testing.T) {
	st := workshopState{
		w: store.WorkshopRow{Minimum: 3, Capacity: 6},
		enrollments: []store.EnrollmentRow{
			{Status: domain.EnrollmentAuthorized},
			{Status: domain.EnrollmentCapturing},
			{Status: domain.EnrollmentCapturePending},
			{Status: domain.EnrollmentPaid},
		},
	}
	got := st.tally()
	if got.Authorized != 1 || got.Charging != 2 || got.Paid != 1 {
		t.Errorf("tally = %+v, want 1 reserved, 2 being charged and 1 paid", got)
	}
}
