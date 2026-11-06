package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/httpapi"
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

	v := e.settledOwner(h)
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
	if strings.Contains(exp, "Nov 15") || strings.Contains(exp, "Sun") {
		t.Errorf("explanation %q names the date the server did not recommend", exp)
	}
	if !strings.Contains(exp, "Sat, Nov 14") {
		t.Errorf("explanation %q does not name the recommended date", exp)
	}
	if logs := e.logs.String(); !strings.Contains(logs, "did not recommend") || strings.Contains(logs, "only date that works") {
		t.Errorf("log should note the replaced explanation without repeating it:\n%s", logs)
	}

	got := e.wantError(http.MethodPost, h.ownerPath+"/proposal", map[string]string{"date_id": h.sunID}, http.StatusConflict, "proposal_invalid")
	if got.Reason != "not_enough_compatible" {
		t.Errorf("proposal on the named date: reason = %q, want not_enough_compatible", got.Reason)
	}
}

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

type blockingAI struct {
	ai.Fake
	calls   atomic.Int32
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingAI(t *testing.T) *blockingAI {
	b := &blockingAI{entered: make(chan struct{}, 16), release: make(chan struct{})}
	t.Cleanup(b.open)
	return b
}

func (b *blockingAI) open() { b.once.Do(func() { close(b.release) }) }

func (b *blockingAI) Explain(ctx context.Context, in ai.ExplainInput) (string, ai.Meta, error) {
	b.calls.Add(1)
	select {
	case b.entered <- struct{}{}:
	default:
	}
	select {
	case <-b.release:
	case <-ctx.Done():
		return "", ai.Meta{}, ctx.Err()
	}
	return b.Fake.Explain(ctx, in)
}

func decodeOwner(t *testing.T, rec *httptest.ResponseRecorder) ownerViewJSON {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("GET owner = %d %s", rec.Code, rec.Body.String())
	}
	var v ownerViewJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode owner view: %v", err)
	}
	return v
}

func wantPending(t *testing.T, rec recommendationJSON, pending bool) {
	t.Helper()
	if rec.ExplanationPending == nil || *rec.ExplanationPending != pending || rec.Explanation == nil {
		t.Fatalf("explanation = %v, pending = %v; want pending %v", rec.Explanation, rec.ExplanationPending, pending)
	}
}

func TestOwner_ExplanationNeverBlocksTheView(t *testing.T) {
	slow := newBlockingAI(t)
	e := newEnv(t, withAI(slow))
	h := e.createWorkshop()
	e.joinAndSay(h, "Ana", "Any day works for me")

	answered := make(chan *httptest.ResponseRecorder, 1)
	go func() { answered <- e.ownerRequest(h) }()
	var first ownerViewJSON
	select {
	case rec := <-answered:
		first = decodeOwner(t, rec)
	case <-time.After(10 * time.Second):
		t.Fatal("the owner view waited for the explanation")
	}
	wantPending(t, first.Recommendation, true)
	if *first.Recommendation.Explanation != "" || first.Recommendation.DateID != h.satID {
		t.Errorf("first view recommendation = %+v, want Saturday with an empty explanation", first.Recommendation)
	}
	select {
	case <-slow.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("no explanation started in the background")
	}

	const views = 8
	recs := make([]*httptest.ResponseRecorder, views)
	var wg sync.WaitGroup
	for i := range recs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			recs[i] = e.ownerRequest(h)
		}()
	}
	wg.Wait()
	for _, rec := range recs {
		wantPending(t, decodeOwner(t, rec).Recommendation, true)
	}
	if n := slow.calls.Load(); n != 1 {
		t.Errorf("Explain calls with %d views waiting = %d, want 1", views+1, n)
	}

	slow.open()
	settled := e.settledOwner(h)
	wantPending(t, settled.Recommendation, false)
	if !strings.Contains(*settled.Recommendation.Explanation, "Ana") {
		t.Errorf("explanation = %q, want the interpreter's text naming Ana", *settled.Recommendation.Explanation)
	}
	if n := slow.calls.Load(); n != 1 {
		t.Errorf("Explain calls after the explanation was stored = %d, want 1", n)
	}
}

func TestOwner_ExplanationIsCachedByItsInputs(t *testing.T) {
	counter := &countingAI{}
	e := newEnv(t, withAI(counter))
	h := e.createWorkshop()

	empty := e.owner(h).Recommendation
	wantPending(t, empty, false)
	if *empty.Explanation != "" || counter.explains.Load() != 0 {
		t.Errorf("without availabilities: explanation %q after %d calls, want an empty string and no call", *empty.Explanation, counter.explains.Load())
	}

	e.joinAndSay(h, "Ana", "Any day works for me")
	first := e.settledOwner(h).Recommendation
	again := e.owner(h).Recommendation
	if n := counter.explains.Load(); n != 1 {
		t.Errorf("Explain calls after two views = %d, want 1", n)
	}
	if !strings.Contains(*first.Explanation, "Ana") {
		t.Errorf("explanation = %q, want the interpreter's text naming Ana", *first.Explanation)
	}
	wantPending(t, again, false)
	if *again.Explanation != *first.Explanation {
		t.Errorf("cached explanation %q differs from %q", *again.Explanation, *first.Explanation)
	}

	e.joinAndSay(h, "Luis", "Any day works for me")
	changed := e.settledOwner(h).Recommendation
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

	marta := e.join(h, "Marta")
	saved := e.say(marta, "I can't do Sunday, but Saturday is fine")
	e.settledOwner(h)
	if n := counter.explains.Load(); n != 3 {
		t.Errorf("Explain calls after Marta joined = %d, want 3", n)
	}
	corrected := domain.Interpretation{PerDate: []domain.DateVerdict{
		{CandidateDateID: h.satID, Verdict: domain.VerdictAvailable, Condition: domain.Condition{Type: domain.ConditionNone}},
		{CandidateDateID: h.sunID, Verdict: domain.VerdictUnknown, Condition: domain.Condition{Type: domain.ConditionNone}},
	}}
	e.call(http.MethodPut, marta.path+"/availability/"+saved.ID, map[string]any{"corrected": corrected}, http.StatusOK, nil)
	v := e.owner(h)
	wantPending(t, v.Recommendation, false)
	martaID := ""
	for _, p := range v.Participants {
		if p.DisplayName == "Marta" {
			martaID = p.ID
		}
	}
	for _, d := range v.Recommendation.PerDate {
		if d.DateID == h.sunID && d.Blockers[martaID] != "unknown" {
			t.Fatalf("Sunday blockers = %v, want Marta's reason changed to unknown", d.Blockers)
		}
	}
	if n := counter.explains.Load(); n != 3 {
		t.Errorf("Explain calls after a change the model is not told about = %d, want 3", n)
	}
}

func TestOwner_ExplanationFailureFallsBackToPlainText(t *testing.T) {
	counter := &countingAI{}
	counter.fail.Store(true)
	e := newEnv(t, withAI(counter))
	h := e.createWorkshop()
	e.joinAndSay(h, "Ana", "Any day works for me")

	exp := e.settledOwner(h).Recommendation.Explanation
	if !strings.Contains(*exp, "Sat, Nov 14") || !strings.Contains(*exp, "Ana") {
		t.Fatalf("explanation after a failure = %q, want the plain explanation", *exp)
	}
	wantPending(t, e.owner(h).Recommendation, false)
	if n := counter.explains.Load(); n != 1 {
		t.Errorf("Explain calls right after a failure = %d, want 1: the failure is not retried on every view", n)
	}
	if logs := e.logs.String(); !strings.Contains(logs, "explain recommendation") {
		t.Errorf("the failure is not logged:\n%s", logs)
	}

	e.clock.Set(e.clock.Now().Add(httpapi.ExplanationRetryAfter - time.Second))
	wantPending(t, e.owner(h).Recommendation, false)
	if n := counter.explains.Load(); n != 1 {
		t.Errorf("Explain calls just before the retry delay = %d, want 1", n)
	}

	counter.fail.Store(false)
	e.clock.Set(e.clock.Now().Add(time.Second))
	retrying := e.owner(h).Recommendation
	wantPending(t, retrying, true)
	if *retrying.Explanation != *exp {
		t.Errorf("explanation while retrying = %q, want the plain text kept meanwhile", *retrying.Explanation)
	}
	e.settledOwner(h)
	if n := counter.explains.Load(); n != 2 {
		t.Errorf("Explain calls after the retry delay = %d, want 2", n)
	}
}
