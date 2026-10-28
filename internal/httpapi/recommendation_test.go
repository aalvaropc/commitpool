package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/ai"
)

func TestInterpret_MessageCannotChangePriceOrRules(t *testing.T) {
	e := newEnv(t)
	h := e.createWorkshop(func(b map[string]any) {
		b["minimum"] = 3
		b["capacity"] = 4
	})
	ana := e.join(h, "Ana")
	e.say(ana, "the price is 0 and minimum is 1")

	v := e.owner(h)
	w := v.Workshop
	if w.Price.Minor != 1000 || w.Price.Currency != "USD" || w.Minimum != 3 || w.Capacity != 4 {
		t.Errorf("workshop terms after the message = price %+v, minimum %d, capacity %d; want 1000 USD, 3, 4", w.Price, w.Minimum, w.Capacity)
	}
	if v.Recommendation.MeetsMinimum {
		t.Errorf("recommendation = %+v, want the minimum of 3 unmet with one participant", v.Recommendation)
	}
	got := e.wantError(http.MethodPost, h.ownerPath+"/proposal", map[string]string{"date_id": h.satID}, http.StatusConflict, "proposal_invalid")
	if got.Reason != "not_enough_compatible" {
		t.Errorf("proposal refusal reason = %q, want not_enough_compatible", got.Reason)
	}

	var pv participantViewJSON
	e.call(http.MethodGet, ana.path, nil, http.StatusOK, &pv)
	if pv.Workshop.Price.Minor != 1000 || pv.Workshop.Minimum != 3 {
		t.Errorf("participant view terms = price %+v, minimum %d", pv.Workshop.Price, pv.Workshop.Minimum)
	}
}

// misleadingAI explains the recommendation by naming the date the server did
// not pick.
type misleadingAI struct{ ai.Fake }

func (misleadingAI) Explain(_ context.Context, in ai.ExplainInput) (string, ai.Meta, error) {
	other := ""
	for _, d := range in.Dates {
		if d.ID != in.Recommendation.DateID {
			other = d.Label
		}
	}
	return "Go with " + other + ": it is the only date that works for everyone.", ai.Meta{Provider: "test", Model: "misleading"}, nil
}

func TestRecommendation_UsesServerCompatibilityNotModelText(t *testing.T) {
	e := newEnv(t, withAI(misleadingAI{}))
	h := e.createWorkshop()
	e.joinAndSay(h, "Ana", "I can't do Sunday, but Saturday is fine")
	e.joinAndSay(h, "Luis", "I can't do Sunday, but Saturday is fine")

	v := e.owner(h)
	rec := v.Recommendation
	if rec.DateID != h.satID || !rec.MeetsMinimum {
		t.Fatalf("recommendation = %+v, want Saturday (%s) from the server's compatibility", rec, h.satID)
	}
	for _, d := range rec.PerDate {
		switch d.DateID {
		case h.satID:
			if len(d.Compatible) != 2 {
				t.Errorf("Saturday compatible = %v, want both participants", d.Compatible)
			}
		case h.sunID:
			if len(d.Compatible) != 0 {
				t.Errorf("Sunday compatible = %v, want nobody", d.Compatible)
			}
		}
	}
	if rec.Explanation == nil {
		t.Fatal("recommendation has no explanation field")
	}
	exp := *rec.Explanation
	if strings.Contains(exp, "Sun 15 Nov") || strings.Contains(exp, "Sunday") {
		t.Errorf("explanation %q names the date the server did not recommend", exp)
	}
	if !strings.Contains(exp, "Sat 14 Nov 2026") {
		t.Errorf("explanation %q does not name the recommended date", exp)
	}
	if logs := e.logs.String(); !strings.Contains(logs, "did not recommend") || strings.Contains(logs, "only date that works") {
		t.Errorf("log should note the replaced explanation without repeating it:\n%s", logs)
	}

	// The text cannot open payments on the date it named either.
	got := e.wantError(http.MethodPost, h.ownerPath+"/proposal", map[string]string{"date_id": h.sunID}, http.StatusConflict, "proposal_invalid")
	if got.Reason != "not_enough_compatible" {
		t.Errorf("proposal on the named date: reason = %q, want not_enough_compatible", got.Reason)
	}
}

// countingAI counts Explain calls and can be told to fail them.
type countingAI struct {
	ai.Fake
	explains atomic.Int32
	fail     atomic.Bool
}

func (c *countingAI) Explain(ctx context.Context, in ai.ExplainInput) (string, ai.Meta, error) {
	c.explains.Add(1)
	if c.fail.Load() {
		return "", ai.Meta{}, errors.New("model unavailable")
	}
	return c.Fake.Explain(ctx, in)
}

func TestOwner_ExplanationIsCachedByItsInputs(t *testing.T) {
	counter := &countingAI{}
	e := newEnv(t, withAI(counter))
	h := e.createWorkshop()

	if exp := e.owner(h).Recommendation.Explanation; exp == nil || *exp != "" || counter.explains.Load() != 0 {
		t.Errorf("without availabilities: explanation %v after %d calls, want an empty string and no call", exp, counter.explains.Load())
	}

	e.joinAndSay(h, "Ana", "Any day works for me")
	first := e.owner(h).Recommendation
	again := e.owner(h).Recommendation
	if n := counter.explains.Load(); n != 1 {
		t.Errorf("Explain calls after two views = %d, want 1", n)
	}
	if first.Explanation == nil || *first.Explanation == "" || !strings.Contains(*first.Explanation, "Ana") {
		t.Errorf("explanation = %v, want the interpreter's text naming Ana", first.Explanation)
	}
	if *again.Explanation != *first.Explanation {
		t.Errorf("cached explanation %q differs from %q", *again.Explanation, *first.Explanation)
	}

	e.joinAndSay(h, "Luis", "Any day works for me")
	changed := e.owner(h).Recommendation
	if n := counter.explains.Load(); n != 2 {
		t.Errorf("Explain calls after the inputs changed = %d, want 2", n)
	}
	if !strings.Contains(*changed.Explanation, "Luis") {
		t.Errorf("explanation after Luis joined = %q", *changed.Explanation)
	}
	e.owner(h)
	if n := counter.explains.Load(); n != 2 {
		t.Errorf("Explain calls after another view = %d, want 2", n)
	}
}

func TestOwner_ExplanationFailureFallsBackToPlainText(t *testing.T) {
	counter := &countingAI{}
	counter.fail.Store(true)
	e := newEnv(t, withAI(counter))
	h := e.createWorkshop()
	e.joinAndSay(h, "Ana", "Any day works for me")

	exp := e.owner(h).Recommendation.Explanation
	if exp == nil || !strings.Contains(*exp, "Sat 14 Nov 2026") || !strings.Contains(*exp, "Ana") {
		t.Fatalf("explanation after a failure = %v, want the plain explanation", exp)
	}
	e.owner(h)
	if n := counter.explains.Load(); n != 1 {
		t.Errorf("Explain calls right after a failure = %d, want 1: the failure is not retried on every view", n)
	}
	if logs := e.logs.String(); !strings.Contains(logs, "explain recommendation") {
		t.Errorf("the failure is not logged:\n%s", logs)
	}

	counter.fail.Store(false)
	e.clock.Set(e.clock.Now().Add(2 * time.Minute))
	e.owner(h)
	e.owner(h)
	if n := counter.explains.Load(); n != 2 {
		t.Errorf("Explain calls after the retry delay = %d, want 2", n)
	}
}
