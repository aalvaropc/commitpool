// Package ai turns a participant's free-text availability into a structured
// domain.Interpretation and explains a date recommendation in plain words.
// The message is data, never instructions: the server, not the model,
// computes compatibility, amounts and states from the interpretation.
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/aalvaropc/commitpool/internal/domain"
)

// DateOption is one candidate date as the interpreter sees it: its ID and a
// label on the workshop's local clock, such as
// "Sat 14 Nov 2026, 15:00–18:00 America/Lima".
type DateOption struct {
	ID    string
	Label string
}

type InterpretInput struct {
	WorkshopTitle string
	Timezone      string
	Dates         []DateOption
	Message       string
}

// Meta describes the call that produced a result. Model is the model that
// answered. Fallback is true when the configured model could not answer and
// a fallback model or a conservative reading did instead.
type Meta struct {
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	Fallback     bool   `json:"fallback"`
}

// ExplainInput is a recommendation to put into words. Names maps participant
// IDs to display names.
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
}

// PlainExplanation puts a recommendation into words without a model: the
// recommended date and the people who can attend it. It names no other date.
func PlainExplanation(in ExplainInput) string {
	label := ""
	for _, d := range in.Dates {
		if d.ID == in.Recommendation.DateID {
			label = d.Label
		}
	}
	if label == "" {
		return "There is no candidate date to recommend yet."
	}
	names := attendees(in)
	if len(names) == 0 {
		return label + " is the earliest date, but nobody can attend it yet."
	}
	return fmt.Sprintf("%s works for the most people (%d, minimum %d): %s.",
		label, len(names), in.Minimum, strings.Join(names, ", "))
}

// attendees names the participants who can attend the recommended date, in
// the recommendation's order; an ID without a name stands for itself.
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

// ExplainFacts is everything an explanation may draw on, in the words the
// model receives: the workshop title, the minimum, the recommended date's
// label and whether it reaches the minimum, who can attend it, who cannot and
// why, and only how many can attend each other date. Equal facts call for an
// equal explanation, so the server keys its cache on them.
func ExplainFacts(in ExplainInput) string {
	label := ""
	for _, d := range in.Dates {
		if d.ID == in.Recommendation.DateID {
			label = d.Label
		}
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Workshop title: %s\n", jsonString(in.WorkshopTitle))
	fmt.Fprintf(&sb, "Minimum participants to run it: %d\n", in.Minimum)
	fmt.Fprintf(&sb, "Recommended date: %s\n", label)
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

// jsonString quotes s as a JSON string, with <, > and & escaped, so text from
// a participant or an instructor reads as one value.
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
