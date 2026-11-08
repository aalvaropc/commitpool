package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/present"
)

type DateOption struct {
	ID       string
	Label    string
	StartsAt time.Time
}

type EarlierAnswer struct {
	Message   string
	Questions []string
}

type InterpretInput struct {
	WorkshopTitle string
	Timezone      string
	Dates         []DateOption
	Message       string
	Earlier       *EarlierAnswer
}

type Meta struct {
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	Fallback     bool   `json:"fallback"`
}

type ExplainInput struct {
	WorkshopTitle  string
	Dates          []DateOption
	Recommendation domain.Recommendation
	Names          map[string]string
	Minimum        int
}

type Interpreter interface {
	Interpret(ctx context.Context, in InterpretInput) (domain.Interpretation, Meta, error)
	Explain(ctx context.Context, in ExplainInput) (string, Meta, error)
	Draft(ctx context.Context, in DraftInput) (Draft, Meta, error)
	Suggest(ctx context.Context, in SuggestInput) (string, Meta, error)
}

func PlainExplanation(in ExplainInput) string {
	day := DateName(in)
	if day == "" {
		return "There is no date to recommend yet."
	}
	names := attendees(in)
	switch {
	case len(names) == 0:
		return day + " is the earliest date, but nobody can come yet."
	case in.Recommendation.MeetsMinimum:
		return fmt.Sprintf("%s works for %s: %s. You need %d.", day, people(len(names)), joinNames(names), in.Minimum)
	}
	return fmt.Sprintf("%s works for the most people so far: %s. You need %d.", day, joinNames(names), in.Minimum)
}

func DateName(in ExplainInput) string {
	date, ok := recommendedDate(in)
	if !ok {
		return ""
	}
	loc := date.StartsAt.Location()
	day := present.Day(date.StartsAt, loc)
	for _, other := range in.Dates {
		if other.ID != date.ID && present.Day(other.StartsAt, loc) == day {
			return day + " at " + present.Clock(date.StartsAt.In(loc))
		}
	}
	return day
}

func recommendedDate(in ExplainInput) (DateOption, bool) {
	for _, d := range in.Dates {
		if d.ID == in.Recommendation.DateID {
			return d, true
		}
	}
	return DateOption{}, false
}

func people(n int) string {
	if n == 1 {
		return "1 person"
	}
	return fmt.Sprintf("%d people", n)
}

func joinNames(names []string) string {
	if len(names) <= 1 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

func attendees(in ExplainInput) []string {
	var names []string
	for _, s := range in.Recommendation.PerDate {
		if s.DateID != in.Recommendation.DateID {
			continue
		}
		for _, id := range s.Compatible {
			names = append(names, displayName(in.Names, id))
		}
	}
	return names
}

func displayName(names map[string]string, id string) string {
	if name, ok := names[id]; ok {
		return name
	}
	return id
}

func ExplainFacts(in ExplainInput) string {
	date, _ := recommendedDate(in)
	label := date.Label
	var sb strings.Builder
	fmt.Fprintf(&sb, "Workshop title: %s\n", jsonString(in.WorkshopTitle))
	fmt.Fprintf(&sb, "Minimum participants to run it: %d\n", in.Minimum)
	fmt.Fprintf(&sb, "Recommended date: %s\n", label)
	fmt.Fprintf(&sb, "Call the date: %s\n", DateName(in))
	reach := "no"
	if in.Recommendation.MeetsMinimum {
		reach = "yes"
	}
	fmt.Fprintf(&sb, "The recommended date reaches the minimum: %s\n", reach)
	var others []string
	for _, s := range in.Recommendation.PerDate {
		if s.DateID != in.Recommendation.DateID {
			others = append(others, fmt.Sprint(len(s.Compatible)))
			continue
		}
		names := make([]string, len(s.Compatible))
		for i, id := range s.Compatible {
			names[i] = displayName(in.Names, id)
		}
		fmt.Fprintf(&sb, "Participants who can attend it (%d): %s\n", len(names), jsonList(names))
		blocked := make([]string, 0, len(s.Blockers))
		for id, reason := range s.Blockers {
			blocked = append(blocked, jsonString(displayName(in.Names, id))+" ("+reason+")")
		}
		slices.Sort(blocked)
		if len(blocked) > 0 {
			fmt.Fprintf(&sb, "Participants who cannot attend it, with the reason: %s\n", strings.Join(blocked, "; "))
		}
	}
	if len(others) > 0 {
		fmt.Fprintf(&sb, "Participants who can attend each other candidate date: %s\n", strings.Join(others, ", "))
	}
	return sb.String()
}

func jsonString(s string) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}

func jsonList(items []string) string {
	if items == nil {
		items = []string{}
	}
	raw, _ := json.Marshal(items)
	return string(raw)
}
