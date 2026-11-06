package httpapi

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
)

func FuzzCleanInterpretation(f *testing.F) {
	f.Add([]byte(`{"per_date":[{"candidate_date_id":"sat","verdict":"available","condition":{"type":"none"}}]}`))
	f.Add([]byte(`{"per_date":[{"candidate_date_id":"mon","verdict":"maybe","condition":{"type":"end_before","time":"25:99"}}],"needs_clarification":true}`))
	f.Add([]byte(`{"per_date":[{"candidate_date_id":"sat"},{"candidate_date_id":"sat","verdict":"unavailable"}]}`))
	start := time.Date(2026, 11, 14, 15, 0, 0, 0, time.UTC)
	dates := []store.CandidateDateRow{
		{ID: "sat", StartsAt: start, EndsAt: start.Add(3 * time.Hour)},
		{ID: "sun", StartsAt: start.Add(24 * time.Hour), EndsAt: start.Add(27 * time.Hour)},
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		var in domain.Interpretation
		if json.Unmarshal(raw, &in) != nil {
			return
		}
		clean, _ := cleanInterpretation(in, dates)
		for _, v := range clean.PerDate {
			if !slices.ContainsFunc(dates, func(d store.CandidateDateRow) bool { return d.ID == v.CandidateDateID }) {
				t.Fatalf("cleaned reading keeps a verdict for %q, not a candidate date", v.CandidateDateID)
			}
		}
	})
}
