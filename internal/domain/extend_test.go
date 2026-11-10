package domain_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
)

var opened = time.Date(2026, 11, 2, 9, 7, 0, 0, time.UTC)

func extendable() domain.ExtensionCheck {
	return domain.ExtensionCheck{
		Status: domain.WorkshopFunding,
		Proposal: domain.Proposal{
			OpensAt: opened, ClosesAt: opened.Add(24 * time.Hour), StartsAt: opened.Add(10 * 24 * time.Hour), Minimum: 3,
		},
		Enrollments: []domain.Enrollment{
			{ID: "ana", Status: domain.EnrollmentAuthorized, ConsentVersion: "consent-v5"},
			{ID: "ben", Status: domain.EnrollmentSeatReserved, ConsentVersion: "consent-v5"},
			{ID: "cleo", Status: domain.EnrollmentReleased, ConsentVersion: "consent-v4"},
			{ID: "dev", Status: domain.EnrollmentVoided, ConsentVersion: "consent-v3"},
		},
		Holds: []time.Time{opened.Add(2 * time.Hour)},
		Now:   opened.Add(22 * time.Hour),
	}
}

func TestExtendedDeadline_FloorsTheEarliestCapToAQuarterHour(t *testing.T) {
	p := domain.Proposal{OpensAt: opened, StartsAt: opened.Add(10 * 24 * time.Hour)}
	if got, want := domain.ExtendedDeadline(p, nil), time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("three days after opening = %s, want %s floored to the quarter hour", got, want)
	}
	p.StartsAt = time.Date(2026, 11, 5, 20, 52, 0, 0, time.UTC)
	if got, want := domain.ExtendedDeadline(p, nil), time.Date(2026, 11, 4, 20, 45, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("a class starting soon = %s, want a day before it starts, floored: %s", got, want)
	}
	p.StartsAt = opened.Add(10 * 24 * time.Hour)
	if got := domain.ExtendedDeadline(p, nil); !got.Equal(domain.LatestDeadline(p)) {
		t.Errorf("without holds the option %s should be the latest possible deadline %s", got, domain.LatestDeadline(p))
	}
}

func TestExtendedDeadline_NeverOutlivesAHold(t *testing.T) {
	p := domain.Proposal{OpensAt: opened, StartsAt: opened.Add(10 * 24 * time.Hour)}
	early := opened.Add(-3*time.Hour - 20*time.Minute)
	holds := []time.Time{opened.Add(5 * time.Hour), early, opened.Add(time.Hour)}
	want := time.Date(2026, 11, 5, 5, 45, 0, 0, time.UTC)
	if got := domain.ExtendedDeadline(p, holds); !got.Equal(want) {
		t.Errorf("with a hold placed at %s = %s, want %s: 72 hours after the oldest hold, floored", early, got, want)
	}
	if got := domain.ExtendedDeadline(p, []time.Time{opened.Add(30 * time.Hour)}); !got.Equal(domain.LatestDeadline(p)) {
		t.Errorf("a hold placed after opening = %s, want the opening cap %s", got, domain.LatestDeadline(p))
	}
}

func TestCanExtend_OffersOneOption(t *testing.T) {
	to, err := domain.CanExtend(extendable())
	if want := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC); err != nil || !to.Equal(want) {
		t.Errorf("CanExtend = %s, %v; want %s", to, err, want)
	}
}

func TestCanExtend_Refusals(t *testing.T) {
	cases := []struct {
		name string
		edit func(*domain.ExtensionCheck)
		want error
	}{
		{"collecting", func(c *domain.ExtensionCheck) { c.Status = domain.WorkshopCollecting }, domain.ErrNotFunding},
		{"confirmed", func(c *domain.ExtensionCheck) { c.Status = domain.WorkshopConfirmed }, domain.ErrNotFunding},
		{"cancelled", func(c *domain.ExtensionCheck) { c.Status = domain.WorkshopCancelled }, domain.ErrNotFunding},
		{"closed", func(c *domain.ExtensionCheck) { c.Now = c.Proposal.ClosesAt }, domain.ErrNotFunding},
		{"nine minutes left", func(c *domain.ExtensionCheck) { c.Now = c.Proposal.ClosesAt.Add(-9 * time.Minute) }, domain.ErrTooLateToExtend},
		{"already extended", func(c *domain.ExtensionCheck) { c.Extended = true }, domain.ErrAlreadyExtended},
		{"funded", func(c *domain.ExtensionCheck) {
			c.Proposal.Minimum = 1
			c.Enrollments[0].Status = domain.EnrollmentPaid
		}, domain.ErrAlreadyFunded},
		{"one paid", func(c *domain.ExtensionCheck) { c.Enrollments[0].Status = domain.EnrollmentPaid }, domain.ErrChargingStarted},
		{"one charging", func(c *domain.ExtensionCheck) { c.Enrollments[0].Status = domain.EnrollmentCapturing }, domain.ErrChargingStarted},
		{"one pending charge", func(c *domain.ExtensionCheck) { c.Enrollments[0].Status = domain.EnrollmentCapturePending }, domain.ErrChargingStarted},
		{"capture queued", func(c *domain.ExtensionCheck) {
			at := c.Now
			c.Enrollments[0].CaptureStartedAt = &at
		}, domain.ErrChargingStarted},
		{"holds reach the minimum", func(c *domain.ExtensionCheck) { c.Proposal.Minimum = 1 }, domain.ErrChargingStarted},
		{"a live v4 hold", func(c *domain.ExtensionCheck) { c.Enrollments[0].ConsentVersion = "consent-v4" }, domain.ErrConsentPredatesExtension},
		{"a v4 seat at checkout", func(c *domain.ExtensionCheck) { c.Enrollments[1].ConsentVersion = "consent-v4" }, domain.ErrConsentPredatesExtension},
		{"a version that merely sorts later", func(c *domain.ExtensionCheck) { c.Enrollments[1].ConsentVersion = "consent-v50" }, domain.ErrConsentPredatesExtension},
		{"window already three days", func(c *domain.ExtensionCheck) {
			c.Proposal.ClosesAt = c.Proposal.OpensAt.Add(domain.MaxPaymentWindow)
			c.Now = c.Proposal.ClosesAt.Add(-time.Hour)
		}, domain.ErrExtensionTooSmall},
		{"gains under an hour", func(c *domain.ExtensionCheck) {
			c.Proposal.ClosesAt = time.Date(2026, 11, 5, 8, 1, 0, 0, time.UTC)
			c.Now = c.Proposal.ClosesAt.Add(-time.Hour)
		}, domain.ErrExtensionTooSmall},
		{"capped by an old hold", func(c *domain.ExtensionCheck) {
			c.Holds = append(c.Holds, c.Proposal.ClosesAt.Add(-72*time.Hour+30*time.Minute))
		}, domain.ErrExtensionTooSmall},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := extendable()
			c.Enrollments = append([]domain.Enrollment(nil), c.Enrollments...)
			tc.edit(&c)
			if to, err := domain.CanExtend(c); !errors.Is(err, tc.want) {
				t.Errorf("CanExtend = %s, %v; want %v", to, err, tc.want)
			}
		})
	}
}

func TestCanExtend_GainsExactlyAnHourAtTheLeast(t *testing.T) {
	c := extendable()
	c.Proposal.ClosesAt = time.Date(2026, 11, 5, 8, 0, 0, 0, time.UTC)
	c.Now = c.Proposal.ClosesAt.Add(-time.Hour)
	if to, err := domain.CanExtend(c); err != nil || to.Sub(c.Proposal.ClosesAt) != time.Hour {
		t.Errorf("CanExtend = %s, %v; want one more hour", to, err)
	}
	c.Now = c.Proposal.ClosesAt.Add(-domain.MinLeftToExtend)
	if _, err := domain.CanExtend(c); err != nil {
		t.Errorf("with exactly ten minutes left = %v, want allowed", err)
	}
}

func TestSuggestions_RescueOnlyInTheLastHours(t *testing.T) {
	in := domain.SuggestInput{
		Status: domain.WorkshopFunding, PaymentsOpen: true, ClosesAt: noon.Add(domain.RescueWhenLeft + time.Minute), Now: noon,
		Minimum: 3, Committed: 1, CanExtend: true,
		Date: domain.DateSummary{DateID: "sat", Compatible: []string{"priya", "ben"}}, Reserved: map[string]bool{"priya": true},
	}
	for _, sg := range domain.Suggestions(in) {
		if sg.Kind == domain.SuggestRescue {
			t.Errorf("rescue offered with more than six hours left: %+v", domain.Suggestions(in))
		}
	}
	in.Now = in.ClosesAt.Add(-domain.RescueWhenLeft)
	want := []domain.Suggestion{{Kind: domain.SuggestRescue}, {Kind: domain.SuggestReserve, ParticipantID: "ben"}}
	if got := domain.Suggestions(in); !reflect.DeepEqual(got, want) {
		t.Errorf("six hours left = %+v, want %+v", got, want)
	}
	in.CanExtend = false
	if got := domain.Suggestions(in); got[0].Kind == domain.SuggestRescue {
		t.Errorf("rescue offered without an extension to offer: %+v", got)
	}
	in.CanExtend, in.Committed = true, 3
	if got := domain.Suggestions(in); len(got) != 0 {
		t.Errorf("minimum met = %+v, want no rescue", got)
	}
}

func TestSuggestions_RescueTakesThePlaceOfTheShareMessage(t *testing.T) {
	in := domain.SuggestInput{
		Status: domain.WorkshopFunding, PaymentsOpen: true, ClosesAt: noon.Add(2 * time.Hour), Now: noon,
		Minimum: 3, Committed: 2, CanExtend: true,
		Date:     domain.DateSummary{DateID: "sat", Compatible: []string{"priya", "ben", "grace"}},
		Reserved: map[string]bool{"priya": true, "ben": true},
	}
	want := []domain.Suggestion{{Kind: domain.SuggestRescue}, {Kind: domain.SuggestReserve, ParticipantID: "grace"}}
	if got := domain.Suggestions(in); !reflect.DeepEqual(got, want) {
		t.Errorf("while the rescue is offered = %+v, want %+v: no group message naming the deadline about to change", got, want)
	}

	in.CanExtend, in.Extended, in.ClosesAt = false, true, noon.Add(72*time.Hour)
	want = []domain.Suggestion{{Kind: domain.SuggestShare}, {Kind: domain.SuggestReserve, ParticipantID: "grace"}}
	if got := domain.Suggestions(in); !reflect.DeepEqual(got, want) {
		t.Errorf("after the extension = %+v, want %+v: sharing the new deadline comes back", got, want)
	}

	in.Extended, in.ClosesAt = false, noon.Add(2*time.Hour)
	want = []domain.Suggestion{{Kind: domain.SuggestShare}, {Kind: domain.SuggestReserve, ParticipantID: "grace"}}
	if got := domain.Suggestions(in); !reflect.DeepEqual(got, want) {
		t.Errorf("with no extension to offer = %+v, want %+v", got, want)
	}
}

func TestSuggestions_ShareRightAfterAnExtension(t *testing.T) {
	in := domain.SuggestInput{
		Status: domain.WorkshopFunding, PaymentsOpen: true, ClosesAt: noon.Add(60 * time.Hour), Now: noon,
		Minimum: 3, Committed: 1, Extended: true,
	}
	if got := domain.Suggestions(in); len(got) != 1 || got[0].Kind != domain.SuggestShare {
		t.Errorf("after an extension with 60 hours left = %+v, want sharing the new deadline", got)
	}
	in.Extended = false
	if got := domain.Suggestions(in); len(got) != 0 {
		t.Errorf("without an extension = %+v, want nothing before the last 12 hours", got)
	}
}
