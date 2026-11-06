package ai_test

import (
	"testing"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/domain"
)

func plainInput(compatible []string, meets bool) ai.ExplainInput {
	return ai.ExplainInput{
		WorkshopTitle: "Wheel-throwing for beginners",
		Dates:         dates,
		Recommendation: domain.Recommendation{
			DateID:       "sat",
			MeetsMinimum: meets,
			PerDate:      []domain.DateSummary{{DateID: "sat", Compatible: compatible}},
		},
		Names:   map[string]string{"p1": "Priya", "p2": "Ben", "p3": "Lucía"},
		Minimum: 3,
	}
}

func TestPlainExplanation_SpeaksToTheOrganizer(t *testing.T) {
	cases := []struct {
		name string
		in   ai.ExplainInput
		want string
	}{
		{"reaches the minimum", plainInput([]string{"p1", "p2", "p3"}, true), "Sat, Nov 14 works for 3 people: Priya, Ben and Lucía. You need 3."},
		{"short of the minimum", plainInput([]string{"p1", "p2"}, false), "Sat, Nov 14 works for the most people so far: Priya and Ben. You need 3."},
		{"one person", plainInput([]string{"p1"}, false), "Sat, Nov 14 works for the most people so far: Priya. You need 3."},
		{"nobody yet", plainInput(nil, false), "Sat, Nov 14 is the earliest date, but nobody can come yet."},
		{"no date", ai.ExplainInput{Dates: dates}, "There is no date to recommend yet."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ai.PlainExplanation(c.in); got != c.want {
				t.Errorf("PlainExplanation = %q, want %q", got, c.want)
			}
		})
	}
}
