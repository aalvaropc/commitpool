package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/clock"
	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/httpapi"
)

type cappedAI struct {
	ai.Fake
	wait     time.Duration
	readings bool
	explains atomic.Int32
	nudges   atomic.Int32
}

func (c *cappedAI) capped() error { return &ai.BusyError{Wait: c.wait} }

func (c *cappedAI) Interpret(ctx context.Context, in ai.InterpretInput) (domain.Interpretation, ai.Meta, error) {
	if c.readings {
		return domain.Interpretation{}, ai.Meta{}, c.capped()
	}
	return c.Fake.Interpret(ctx, in)
}

func (c *cappedAI) Explain(context.Context, ai.ExplainInput) (string, ai.Meta, error) {
	c.explains.Add(1)
	return "", ai.Meta{}, c.capped()
}

func (c *cappedAI) Suggest(context.Context, ai.SuggestInput) (string, ai.Meta, error) {
	c.nudges.Add(1)
	return "", ai.Meta{}, c.capped()
}

func (c *cappedAI) Draft(context.Context, ai.DraftInput) (ai.Draft, ai.Meta, error) {
	return ai.Draft{}, ai.Meta{}, c.capped()
}

func noErrorLogged(t *testing.T, e *env) {
	t.Helper()
	if logs := e.logs.String(); strings.Contains(logs, "level=ERROR") {
		t.Errorf("a capped model is logged as an error:\n%s", logs)
	}
}

func TestAvailability_ACappedModelIs429WithTheRealWait(t *testing.T) {
	cases := []struct {
		name    string
		wait    time.Duration
		after   string
		message string
	}{
		{"minutes", 7*time.Minute + 30*time.Second, "450", "Claude has read a lot of messages this hour. Please try again in 8 minutes."},
		{"later today", 5 * time.Hour, "18000", "Claude has read a lot of messages today. Please try again later today."},
		{"tomorrow", 22 * time.Hour, "79200", "Claude has read a lot of messages today. Please try again tomorrow."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, withAI(&cappedAI{wait: tc.wait, readings: true}))
			h := e.createWorkshop()
			ana := e.join(h, "Ana")
			rec := e.request(http.MethodPost, ana.path+"/availability", map[string]string{"message": "Any day"})
			if after := rec.Header().Get("Retry-After"); after != tc.after {
				t.Errorf("Retry-After = %q, want %s seconds", after, tc.after)
			}
			got := e.wantError(http.MethodPost, ana.path+"/availability", map[string]string{"message": "Any day"}, http.StatusTooManyRequests, "ai_capped")
			if got.Message != tc.message {
				t.Errorf("message = %q, want %q", got.Message, tc.message)
			}
			if v := e.owner(h); len(v.Availabilities) != 0 {
				t.Errorf("availabilities after a capped model = %+v", v.Availabilities)
			}
			if logs := e.logs.String(); !strings.Contains(logs, "level=WARN") || !strings.Contains(logs, "interpret availability") {
				t.Errorf("the capped reading is not logged as a warning:\n%s", logs)
			}
			noErrorLogged(t, e)
		})
	}
}

func TestOwner_ACappedModelExplainsInPlainTextUntilTheWaitIsOver(t *testing.T) {
	model := &cappedAI{wait: 10 * time.Minute}
	e := newEnv(t, withAI(model))
	h := e.createWorkshop()
	e.joinAndSay(h, "Ana", "Any day works for me")
	exp := e.settledOwner(h).Recommendation.Explanation
	if exp == nil || !strings.Contains(*exp, "Sat, Nov 14") || !strings.Contains(*exp, "Ana") {
		t.Fatalf("explanation with a capped model = %v, want the plain explanation", exp)
	}
	e.clock.Set(e.clock.Now().Add(httpapi.ExplanationRetryAfter + time.Second))
	wantPending(t, e.owner(h).Recommendation, false)
	if n := model.explains.Load(); n != 1 {
		t.Errorf("Explain calls a minute later = %d, want 1: a capped model is asked again only after its wait", n)
	}
	e.clock.Set(e.clock.Now().Add(10 * time.Minute))
	e.settledOwner(h)
	if n := model.explains.Load(); n != 2 {
		t.Errorf("Explain calls after the wait = %d, want 2", n)
	}
	if !strings.Contains(e.logs.String(), "explain recommendation") {
		t.Errorf("the capped model is not logged:\n%s", e.logs)
	}
	noErrorLogged(t, e)
}

func TestSuggestions_ACappedModelSendsThePlainMessage(t *testing.T) {
	model := &cappedAI{wait: 10 * time.Minute}
	e := newEnv(t, withAI(model))
	h := e.createWorkshop()
	e.joinAndSay(h, "Grace Liu", "I can make it either day, ideally in the afternoon.")
	v := e.settledOwner(h)
	if len(v.Suggestions) != 1 || !strings.HasPrefix(v.Suggestions[0].Message, "Hi Grace!") {
		t.Errorf("suggestions with a capped model = %+v, want the plain message", v.Suggestions)
	}
	e.clock.Set(e.clock.Now().Add(httpapi.ExplanationRetryAfter + time.Second))
	e.settledOwner(h)
	if n := model.nudges.Load(); n != 1 {
		t.Errorf("Suggest calls a minute later = %d, want 1", n)
	}
	noErrorLogged(t, e)
}

func TestDrafts_ACappedModelAnswersWithAnEmptyDraft(t *testing.T) {
	e := newEnv(t, withAI(&cappedAI{wait: time.Minute}))
	var got draftResponseJSON
	e.call(http.MethodPost, draftsPath, draftBody(classDescription), http.StatusOK, &got)
	if got.Drafted || got.Draft.Title != "" || len(got.Draft.Missing) != 10 {
		t.Errorf("draft with a capped model = %+v, want an empty draft", got)
	}
	noErrorLogged(t, e)
}

type cappedAtFirstAI struct {
	ai.Fake
	refusals atomic.Int32
}

func (c *cappedAtFirstAI) Interpret(ctx context.Context, in ai.InterpretInput) (domain.Interpretation, ai.Meta, error) {
	if c.refusals.Add(-1) >= 0 {
		return domain.Interpretation{}, ai.Meta{}, &ai.BusyError{Wait: time.Second}
	}
	return c.Fake.Interpret(ctx, in)
}

func TestDemo_AnswersWaitAsLongAsTheAPIAsks(t *testing.T) {
	model := &cappedAtFirstAI{}
	model.refusals.Store(2)
	e := newEnv(t, withDemoMode(), withAI(model))
	start := time.Now()
	_, ownerPath := e.startDemo()
	if waited := time.Since(start); waited < time.Second {
		t.Errorf("the answers arrived after %v, want them to wait the second the API asked for", waited)
	}
	var v ownerViewJSON
	e.call(http.MethodGet, ownerPath, nil, http.StatusOK, &v)
	if len(v.Availabilities) != 6 {
		t.Errorf("%d of 6 answers arrived after the model was capped for a while", len(v.Availabilities))
	}
}

func TestDemo_RestsWhileTheBudgetHasNoRoomForOne(t *testing.T) {
	cases := []struct {
		name          string
		hourly, daily int
		later         time.Duration
		after         string
		message       string
	}{
		{"this hour", 30, 1000, 15 * time.Minute, "2700", "Claude has read many messages this hour. Try the demo again in 45 minutes, or create your own workshop."},
		{"today", 100, 30, 5 * time.Hour, "68400", "Claude has read many messages today. Try the demo again in about 19 hours, or create your own workshop."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			budgetClock := &clock.Fake{T: t0}
			budget := ai.NewBudget(ai.Fake{}, ai.BudgetConfig{Hourly: tc.hourly, Daily: tc.daily, Clock: budgetClock})
			for range 6 {
				if _, _, err := budget.Interpret(context.Background(), ai.InterpretInput{Dates: []ai.DateOption{{ID: "d"}}, Message: "any day"}); err != nil {
					t.Fatal(err)
				}
			}
			budgetClock.Set(t0.Add(tc.later))
			e := newEnv(t, withDemoMode(), withRateLimit(120), withAI(ai.NewMemo(budget, ai.MemoSize)))
			rec := e.request(http.MethodPost, "/api/v1/demo/workshops", nil)
			if after := rec.Header().Get("Retry-After"); after != tc.after {
				t.Errorf("Retry-After = %q, want %s seconds", after, tc.after)
			}
			got := e.wantError(http.MethodPost, "/api/v1/demo/workshops", nil, http.StatusTooManyRequests, "demo_resting")
			if got.Message != tc.message {
				t.Errorf("message = %q, want %q", got.Message, tc.message)
			}

			for range 4 {
				e.wantError(http.MethodPost, "/api/v1/demo/workshops", nil, http.StatusTooManyRequests, "demo_resting")
			}
			budgetClock.Set(t0.Add(25 * time.Hour))
			e.call(http.MethodPost, "/api/v1/demo/workshops", nil, http.StatusCreated, nil)
		})
	}
}
