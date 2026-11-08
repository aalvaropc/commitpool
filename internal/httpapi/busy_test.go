package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/domain"
)

var errModelBusy = &ai.BusyError{Wait: 7*time.Minute + 30*time.Second}

type busyWritingAI struct{ ai.Fake }

func (busyWritingAI) Explain(context.Context, ai.ExplainInput) (string, ai.Meta, error) {
	return "", ai.Meta{}, errModelBusy
}

func (busyWritingAI) Suggest(context.Context, ai.SuggestInput) (string, ai.Meta, error) {
	return "", ai.Meta{}, errModelBusy
}

func (busyWritingAI) Draft(context.Context, ai.DraftInput) (ai.Draft, ai.Meta, error) {
	return ai.Draft{}, ai.Meta{}, errModelBusy
}

type busyAI struct{ busyWritingAI }

func (busyAI) Interpret(context.Context, ai.InterpretInput) (domain.Interpretation, ai.Meta, error) {
	return domain.Interpretation{}, ai.Meta{}, errModelBusy
}

func TestAvailability_ABusyModelIs429WithRetryAfter(t *testing.T) {
	e := newEnv(t, withAI(busyAI{}))
	h := e.createWorkshop()
	ana := e.join(h, "Ana")
	rec := e.request(http.MethodPost, ana.path+"/availability", map[string]string{"message": "Any day"})
	got := e.wantError(http.MethodPost, ana.path+"/availability", map[string]string{"message": "Any day"}, http.StatusTooManyRequests, "ai_busy")
	if got.Message != "Claude is reading a lot of messages right now. Please try again in a few minutes." {
		t.Errorf("message = %q", got.Message)
	}
	if after := rec.Header().Get("Retry-After"); after != "450" {
		t.Errorf("Retry-After = %q, want 450 seconds", after)
	}
	if v := e.owner(h); len(v.Availabilities) != 0 {
		t.Errorf("availabilities after a busy model = %+v", v.Availabilities)
	}
}

func TestOwner_ABusyModelExplainsInPlainText(t *testing.T) {
	e := newEnv(t, withAI(busyWritingAI{}))
	h := e.createWorkshop()
	e.joinAndSay(h, "Ana", "Any day works for me")
	exp := e.settledOwner(h).Recommendation.Explanation
	if exp == nil || !strings.Contains(*exp, "Sat, Nov 14") || !strings.Contains(*exp, "Ana") {
		t.Fatalf("explanation with a busy model = %v, want the plain explanation", exp)
	}
	if !strings.Contains(e.logs.String(), "explain recommendation") {
		t.Errorf("the busy model is not logged:\n%s", e.logs)
	}
}

func TestSuggestions_ABusyModelSendsThePlainMessage(t *testing.T) {
	e := newEnv(t, withAI(busyWritingAI{}))
	h := e.createWorkshop()
	e.joinAndSay(h, "Grace Liu", "I can make it either day, ideally in the afternoon.")
	v := e.settledOwner(h)
	if len(v.Suggestions) != 1 || !strings.HasPrefix(v.Suggestions[0].Message, "Hi Grace!") {
		t.Errorf("suggestions with a busy model = %+v, want the plain message", v.Suggestions)
	}
}

func TestDrafts_ABusyModelAnswersWithAnEmptyDraft(t *testing.T) {
	e := newEnv(t, withAI(busyWritingAI{}))
	var got draftResponseJSON
	e.call(http.MethodPost, draftsPath, draftBody(mayaDescription), http.StatusOK, &got)
	if got.Drafted || got.Draft.Title != "" || len(got.Draft.Missing) != 10 {
		t.Errorf("draft with a busy model = %+v, want an empty draft", got)
	}
}

type busyAtFirstAI struct {
	ai.Fake
	refusals atomic.Int32
}

func (b *busyAtFirstAI) Interpret(ctx context.Context, in ai.InterpretInput) (domain.Interpretation, ai.Meta, error) {
	if b.refusals.Add(-1) >= 0 {
		return domain.Interpretation{}, ai.Meta{}, errModelBusy
	}
	return b.Fake.Interpret(ctx, in)
}

func TestDemo_AnswersWaitOutABusyModel(t *testing.T) {
	model := &busyAtFirstAI{}
	model.refusals.Store(8)
	e := newEnv(t, withDemoMode(), withAI(model))
	var d demoJSON
	e.call(http.MethodPost, "/api/v1/demo/workshops", nil, http.StatusCreated, &d)
	e.background.Wait()
	var v ownerViewJSON
	e.call(http.MethodGet, "/api/v1/owner/"+strings.TrimPrefix(d.OwnerURL, appBaseURL+"/owner/"), nil, http.StatusOK, &v)
	if len(v.Availabilities) != 6 {
		t.Errorf("%d of 6 answers arrived after the model was busy for a while", len(v.Availabilities))
	}
}
