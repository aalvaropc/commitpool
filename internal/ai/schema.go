package ai

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/aalvaropc/commitpool/internal/domain"
)

// InterpretationSchema is the JSON schema a reading must match: the shape of
// domain.Interpretation with every object closed (additionalProperties false)
// and every field required, and candidate_date_id limited to the workshop's
// dateIDs. It has no field for the price, the currency, the minimum, the
// capacity or any state: the model has no way to state them, and the server
// takes them from the workshop alone. The model is held to it through
// structured output and the server checks it again in ParseInterpretation.
func InterpretationSchema(dateIDs []string) map[string]any {
	str := map[string]any{"type": "string"}
	texts := map[string]any{"type": "array", "items": str}
	object := func(props map[string]any) map[string]any {
		required := make([]string, 0, len(props))
		for name := range props {
			required = append(required, name)
		}
		slices.Sort(required)
		return map[string]any{
			"type":                 "object",
			"properties":           props,
			"required":             required,
			"additionalProperties": false,
		}
	}
	condition := object(map[string]any{
		"type": map[string]any{"type": "string", "enum": []string{domain.ConditionNone, domain.ConditionEndBefore, domain.ConditionStartAfter}},
		"time": map[string]any{"type": "string", "description": `HH:MM on the 24-hour clock in the workshop's time zone for end_before and start_after; "" otherwise`},
	})
	verdict := object(map[string]any{
		"candidate_date_id": map[string]any{"type": "string", "enum": slices.Clone(dateIDs)},
		"verdict": map[string]any{"type": "string", "enum": []string{
			string(domain.VerdictAvailable), string(domain.VerdictUnavailable), string(domain.VerdictConditional), string(domain.VerdictUnknown),
		}},
		"condition": condition,
		"evidence":  str,
	})
	ambiguity := object(map[string]any{"question": str, "why": str})
	return object(map[string]any{
		"per_date":            map[string]any{"type": "array", "items": verdict},
		"hard_constraints":    texts,
		"preferences":         texts,
		"ambiguities":         map[string]any{"type": "array", "items": ambiguity},
		"needs_clarification": map[string]any{"type": "boolean"},
	})
}

// ParseInterpretation reads a model's answer: exactly one JSON object that
// matches InterpretationSchema(dateIDs). Anything else, an extra field or a
// date the workshop does not have included, is refused. Errors name only the
// failing location and rule, never the text of the answer. The clarification
// fields are then made to agree (see genericAmbiguity).
func ParseInterpretation(raw []byte, dateIDs []string) (domain.Interpretation, error) {
	if len(dateIDs) == 0 {
		return domain.Interpretation{}, errors.New("interpretation: the workshop has no candidate dates")
	}
	schema, err := compiledSchema(dateIDs)
	if err != nil {
		return domain.Interpretation{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return domain.Interpretation{}, errors.New("interpretation: the answer is not a JSON value")
	}
	if _, err := dec.Token(); err != io.EOF {
		return domain.Interpretation{}, errors.New("interpretation: the answer has more than one JSON value")
	}
	if err := schema.Validate(doc); err != nil {
		return domain.Interpretation{}, schemaViolation(err)
	}

	strict := json.NewDecoder(bytes.NewReader(raw))
	strict.DisallowUnknownFields()
	var out domain.Interpretation
	if err := strict.Decode(&out); err != nil {
		return domain.Interpretation{}, errors.New("interpretation: the answer does not decode into a reading")
	}
	// The two clarification fields must agree: a reading that needs
	// clarification asks at least one question, and a question means it does.
	switch {
	case out.NeedsClarification && len(out.Ambiguities) == 0:
		out.Ambiguities = []domain.Ambiguity{genericAmbiguity}
	case !out.NeedsClarification && len(out.Ambiguities) > 0:
		out.NeedsClarification = true
	}
	return out, nil
}

// genericAmbiguity stands in when a reading needs clarification but asks
// nothing.
var genericAmbiguity = domain.Ambiguity{
	Question: "Which of the dates and times work for you?",
	Why:      "The message did not make the availability clear.",
}

// schemaCacheSize bounds the compiled schemas kept, one per set of dates.
const schemaCacheSize = 256

var schemas = struct {
	mu      sync.Mutex
	byDates map[string]*jsonschema.Schema
}{byDates: make(map[string]*jsonschema.Schema)}

// compiledSchema is InterpretationSchema compiled for a set of dates, cached
// by the set (order does not matter to validation). A compiled schema is
// read-only, so callers may validate with it concurrently.
func compiledSchema(dateIDs []string) (*jsonschema.Schema, error) {
	ids := slices.Sorted(slices.Values(dateIDs))
	key := strings.Join(ids, "\x00")
	schemas.mu.Lock()
	cached, ok := schemas.byDates[key]
	schemas.mu.Unlock()
	if ok {
		return cached, nil
	}
	schema, err := compileSchema(ids)
	if err != nil {
		return nil, err
	}
	schemas.mu.Lock()
	defer schemas.mu.Unlock()
	if len(schemas.byDates) >= schemaCacheSize {
		for k := range schemas.byDates {
			delete(schemas.byDates, k)
			break
		}
	}
	schemas.byDates[key] = schema
	return schema, nil
}

func compileSchema(dateIDs []string) (*jsonschema.Schema, error) {
	// The compiler takes decoded JSON ([]any, not []string), so the schema
	// goes through a JSON round trip first.
	raw, err := json.Marshal(InterpretationSchema(dateIDs))
	if err != nil {
		return nil, fmt.Errorf("interpretation schema: %w", err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("interpretation schema: %w", err)
	}
	const url = "interpretation.json"
	c := jsonschema.NewCompiler()
	if err := c.AddResource(url, doc); err != nil {
		return nil, fmt.Errorf("interpretation schema: %w", err)
	}
	return c.Compile(url)
}

// schemaFields are the property names of InterpretationSchema; a location in
// an error shows only these and array indexes.
var schemaFields = map[string]bool{
	"per_date": true, "hard_constraints": true, "preferences": true, "ambiguities": true, "needs_clarification": true,
	"candidate_date_id": true, "verdict": true, "condition": true, "evidence": true,
	"type": true, "time": true, "question": true, "why": true,
}

// schemaViolation reports where the answer broke the schema and which rule
// it broke, without the library's message, which can quote the answer; a
// key the schema does not know shows as "?".
func schemaViolation(err error) error {
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return errors.New("interpretation: the answer does not match the schema")
	}
	var found []string
	var walk func(*jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) == 0 {
			at := make([]string, len(e.InstanceLocation))
			for i, seg := range e.InstanceLocation {
				if _, err := strconv.Atoi(seg); err != nil && !schemaFields[seg] {
					seg = "?"
				}
				at[i] = seg
			}
			found = append(found, fmt.Sprintf("at /%s: %s", strings.Join(at, "/"), strings.Join(e.ErrorKind.KeywordPath(), "/")))
			return
		}
		for _, c := range e.Causes {
			walk(c)
		}
	}
	walk(ve)
	if len(found) > 3 {
		found = append(found[:3], fmt.Sprintf("and %d more", len(found)-3))
	}
	return fmt.Errorf("interpretation: the answer does not match the schema (%s)", strings.Join(found, "; "))
}
