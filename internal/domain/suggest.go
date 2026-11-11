package domain

import (
	"maps"
	"slices"
	"strings"
	"time"
)

type SuggestionKind string

const (
	SuggestClarify   SuggestionKind = "clarify"
	SuggestCondition SuggestionKind = "condition"
	SuggestReserve   SuggestionKind = "reserve"
	SuggestShare     SuggestionKind = "share"
	SuggestRescue    SuggestionKind = "rescue"
)

const (
	MaxPersonalSuggestions = 4
	ShareWhenLeft          = 12 * time.Hour
)

type Suggestion struct {
	Kind          SuggestionKind
	ParticipantID string
	Blocker       string
}

type SuggestInput struct {
	Status       WorkshopStatus
	PaymentsOpen bool
	ClosesAt     time.Time
	Now          time.Time
	Minimum      int
	Committed    int
	Capacity     int
	SeatsTaken   int
	Date         DateSummary
	Unclear      []string
	Reserved     map[string]bool
	CanExtend    bool
	Extended     bool
}

func Suggestions(in SuggestInput) []Suggestion {
	var personal []Suggestion
	var share, rescue bool
	switch {
	case in.Status == WorkshopCollecting:
		for _, id := range in.Unclear {
			personal = append(personal, Suggestion{Kind: SuggestClarify, ParticipantID: id})
		}
		for _, id := range slices.Sorted(maps.Keys(in.Date.Blockers)) {
			if b := in.Date.Blockers[id]; strings.HasPrefix(b, "ends after ") || strings.HasPrefix(b, "starts before ") {
				personal = append(personal, Suggestion{Kind: SuggestCondition, ParticipantID: id, Blocker: b})
			}
		}
	case in.Status == WorkshopFunding && in.PaymentsOpen && in.Committed < in.Minimum && (in.Capacity == 0 || in.SeatsTaken < in.Capacity):
		for _, id := range in.Date.Compatible {
			if !in.Reserved[id] {
				personal = append(personal, Suggestion{Kind: SuggestReserve, ParticipantID: id})
			}
		}
		left := in.ClosesAt.Sub(in.Now)
		rescue = in.CanExtend && left <= RescueWhenLeft
		share = !rescue && (left <= ShareWhenLeft || in.Extended)
	}
	var out []Suggestion
	if rescue {
		out = append(out, Suggestion{Kind: SuggestRescue})
	}
	if share {
		out = append(out, Suggestion{Kind: SuggestShare})
	}
	return append(out, personal[:min(len(personal), MaxPersonalSuggestions)]...)
}
