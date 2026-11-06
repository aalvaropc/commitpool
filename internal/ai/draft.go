package ai

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

type DraftInput struct {
	Text     string
	Timezone string
	Today    time.Time
}

type DraftDate struct {
	Date  string `json:"date"`
	Start string `json:"start"`
}

type Draft struct {
	Title       string      `json:"title"`
	Description string      `json:"description"`
	HostName    string      `json:"host_name"`
	Location    string      `json:"location"`
	DurationMin int         `json:"duration_min"`
	Price       string      `json:"price"`
	VenueCost   string      `json:"venue_cost"`
	Minimum     int         `json:"minimum"`
	Capacity    int         `json:"capacity"`
	Dates       []DraftDate `json:"dates"`
}

func closedObject(props map[string]any) map[string]any {
	required := make([]string, 0, len(props))
	for name := range props {
		required = append(required, name)
	}
	slices.Sort(required)
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}

func DraftSchema() map[string]any {
	text := func(about string) map[string]any { return map[string]any{"type": "string", "description": about} }
	whole := func(about string) map[string]any { return map[string]any{"type": "integer", "description": about} }
	date := closedObject(map[string]any{
		"date":  map[string]any{"type": "string", "format": "date", "description": "YYYY-MM-DD in the workshop's time zone"},
		"start": text("HH:MM on the 24-hour clock in the workshop's time zone"),
	})
	return closedObject(map[string]any{
		"title":        text(`short name for the class, or ""`),
		"description":  text(`one or two sentences for the people invited, or ""`),
		"host_name":    text(`the organizer's name, or ""`),
		"location":     text(`where the class happens, or ""`),
		"duration_min": whole("minutes one session lasts, or 0"),
		"price":        text(`US dollars each person pays, such as "45", or ""`),
		"venue_cost":   text(`US dollars the place costs per session, or ""`),
		"minimum":      whole("people who must pay for the class to happen, or 0"),
		"capacity":     whole("most people who can take part, or 0"),
		"dates":        map[string]any{"type": "array", "items": date},
	})
}

var draftSchema = sync.OnceValues(func() (*jsonschema.Schema, error) {
	raw, err := json.Marshal(DraftSchema())
	if err != nil {
		return nil, fmt.Errorf("draft schema: %w", err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("draft schema: %w", err)
	}
	const url = "draft.json"
	c := jsonschema.NewCompiler()
	if err := c.AddResource(url, doc); err != nil {
		return nil, fmt.Errorf("draft schema: %w", err)
	}
	return c.Compile(url)
})

func ParseDraft(raw []byte) (Draft, error) {
	schema, err := draftSchema()
	if err != nil {
		return Draft{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return Draft{}, errors.New("draft: the answer is not a JSON value")
	}
	if _, err := dec.Token(); err != io.EOF {
		return Draft{}, errors.New("draft: the answer has more than one JSON value")
	}
	if err := schema.Validate(doc); err != nil {
		return Draft{}, errors.New("draft: the answer does not match the schema")
	}
	strict := json.NewDecoder(bytes.NewReader(raw))
	strict.DisallowUnknownFields()
	var out Draft
	if err := strict.Decode(&out); err != nil {
		return Draft{}, errors.New("draft: the answer does not decode into a draft")
	}
	return out, nil
}

func draftSystem(in DraftInput) string {
	var sb strings.Builder
	sb.WriteString(draftSystemPrompt)
	fmt.Fprintf(&sb, "\nToday: %s\nTime zone: %s\n", in.Today.Format("2006-01-02 (Monday)"), in.Timezone)
	return sb.String()
}
