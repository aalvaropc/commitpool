package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/httpapi"
	"github.com/aalvaropc/commitpool/internal/inproc"
)

var extendTo = time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)

type extensionJSON struct {
	Available  bool       `json:"available"`
	From       time.Time  `json:"from"`
	To         *time.Time `json:"to"`
	Refusal    string     `json:"refusal"`
	Reason     string     `json:"reason"`
	ApproveURL string     `json:"approve_url"`
}

type extendProposalJSON struct {
	ID             string     `json:"id"`
	ClosesAt       time.Time  `json:"closes_at"`
	ExtendedFrom   *time.Time `json:"extended_from"`
	LatestClosesAt time.Time  `json:"latest_closes_at"`
}

type extendViewJSON struct {
	Proposal    *extendProposalJSON `json:"proposal"`
	Extension   *extensionJSON      `json:"extension"`
	Suggestions []suggestionJSON    `json:"suggestions"`
}

func (e *env) extendView(h workshopHandle) extendViewJSON {
	e.t.Helper()
	e.settledOwner(h)
	var v extendViewJSON
	e.call(http.MethodGet, h.ownerPath, nil, http.StatusOK, &v)
	return v
}

func (e *env) shortOfTheMinimum() (workshopHandle, proposalJSON, []participantHandle) {
	e.t.Helper()
	h, p, ps := e.fundingWorkshop()
	ana := e.enroll(ps[0], p.ID, http.StatusCreated)
	e.authorize(e.workshopID(h), ana.EnrollmentID)
	e.clock.Set(p.ClosesAt.Add(-2 * time.Hour))
	return h, p, ps
}

func (e *env) workshopID(h workshopHandle) string {
	e.t.Helper()
	return e.owner(h).Workshop.ID
}

func extendBody(from, to time.Time) map[string]string {
	return map[string]string{"from": from.Format(time.RFC3339Nano), "to": to.Format(time.RFC3339Nano)}
}

func kinds(sgs []suggestionJSON) []string {
	out := make([]string, len(sgs))
	for i, sg := range sgs {
		out[i] = sg.Kind
	}
	return out
}

func TestExtend_TheOrganizerMovesTheDeadlineOnce(t *testing.T) {
	e := newEnv(t)
	h, p, ps := e.shortOfTheMinimum()

	v := e.extendView(h)
	x := v.Extension
	if x == nil || !x.Available || x.To == nil || !x.To.Equal(extendTo) || !x.From.Equal(p.ClosesAt) || x.Refusal != "" {
		t.Fatalf("extension = %+v, want %s on offer", x, extendTo)
	}
	if want := appBaseURL + "/owner/" + h.ownerToken + "?extend=" + strconv.FormatInt(extendTo.Unix(), 10); x.ApproveURL != want {
		t.Errorf("approve_url = %q, want %q", x.ApproveURL, want)
	}
	if !v.Proposal.LatestClosesAt.Equal(extendTo) || v.Proposal.ExtendedFrom != nil {
		t.Errorf("proposal = %+v, want the latest possible deadline %s and no extension yet", v.Proposal, extendTo)
	}
	if got := kinds(v.Suggestions); len(got) < 2 || got[0] != "rescue" || got[1] != "share" {
		t.Fatalf("suggestions = %v, want the rescue first, then sharing", got)
	}
	rescue := v.Suggestions[0]
	if !strings.Contains(rescue.Reason, "1 more is needed") || !strings.Contains(rescue.Reason, "Thu, Nov 5 at 4 AM") {
		t.Errorf("rescue reason = %q", rescue.Reason)
	}
	if !strings.Contains(rescue.Message, "until Thu, Nov 5 at 4 AM") || !strings.HasSuffix(rescue.Message, "\n"+h.created.ShareURL) {
		t.Errorf("rescue message = %q, want the new deadline and the invite link", rescue.Message)
	}

	var out struct {
		Proposal extendProposalJSON `json:"proposal"`
	}
	e.call(http.MethodPost, h.ownerPath+"/extend", extendBody(p.ClosesAt, extendTo), http.StatusOK, &out)
	if !out.Proposal.ClosesAt.Equal(extendTo) || out.Proposal.ExtendedFrom == nil || !out.Proposal.ExtendedFrom.Equal(p.ClosesAt) {
		t.Errorf("extended proposal = %+v", out.Proposal)
	}
	if j := e.job("close:" + p.ID); !j.RunAt.Equal(extendTo) || j.Status != "pending" {
		t.Errorf("close job = %s at %s, want pending at %s", j.Status, j.RunAt, extendTo)
	}

	v = e.extendView(h)
	if v.Extension == nil || v.Extension.Available || v.Extension.Refusal != "already_extended" {
		t.Errorf("extension after extending = %+v, want already_extended", v.Extension)
	}
	if got := kinds(v.Suggestions); len(got) == 0 || got[0] != "share" || slices.Contains(got, "rescue") {
		t.Errorf("suggestions after extending = %v, want sharing the new deadline and no rescue", got)
	}
	if !strings.Contains(v.Suggestions[0].Message, "Thu, Nov 5 at 4 AM") {
		t.Errorf("share after extending = %q, want the new deadline", v.Suggestions[0].Message)
	}

	var mine struct {
		Proposal extendProposalJSON `json:"proposal"`
	}
	e.call(http.MethodGet, ps[1].path, nil, http.StatusOK, &mine)
	if mine.Proposal.ExtendedFrom == nil || !mine.Proposal.ExtendedFrom.Equal(p.ClosesAt) || !mine.Proposal.ClosesAt.Equal(extendTo) || !mine.Proposal.LatestClosesAt.Equal(extendTo) {
		t.Errorf("participant proposal = %+v, want extended from %s to %s", mine.Proposal, p.ClosesAt, extendTo)
	}

	e.wantError(http.MethodPost, h.ownerPath+"/extend", extendBody(extendTo, extendTo), http.StatusConflict, "already_extended")
}

func TestExtend_EachRefusalHasItsOwnCode(t *testing.T) {
	t.Run("assistant", func(t *testing.T) {
		e := newEnv(t)
		h, p, _ := e.shortOfTheMinimum()
		err := inproc.Client{Handler: e.h}.Call(e.ctx, http.MethodPost, h.ownerPath+"/extend", extendBody(p.ClosesAt, extendTo), nil)
		var apiErr *inproc.Error
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusForbidden || apiErr.Code != "approval_required" {
			t.Fatalf("in-process POST /extend = %v, want 403 approval_required", err)
		}
		if v := e.extendView(h); v.Proposal.ExtendedFrom != nil {
			t.Errorf("an assistant extended the deadline: %+v", v.Proposal)
		}
	})
	t.Run("changed option", func(t *testing.T) {
		e := newEnv(t)
		h, p, _ := e.shortOfTheMinimum()
		e.wantError(http.MethodPost, h.ownerPath+"/extend", extendBody(p.ClosesAt, extendTo.Add(15*time.Minute)), http.StatusConflict, "extension_changed")
		e.wantError(http.MethodPost, h.ownerPath+"/extend", extendBody(p.ClosesAt.Add(-time.Hour), extendTo), http.StatusConflict, "extension_changed")
		e.wantError(http.MethodPost, h.ownerPath+"/extend", map[string]string{"to": extendTo.Format(time.RFC3339)}, http.StatusBadRequest, "invalid_extension")
	})
	t.Run("too late", func(t *testing.T) {
		e := newEnv(t)
		h, p, _ := e.shortOfTheMinimum()
		e.clock.Set(p.ClosesAt.Add(-5 * time.Minute))
		e.wantError(http.MethodPost, h.ownerPath+"/extend", extendBody(p.ClosesAt, extendTo), http.StatusConflict, "too_late_to_extend")
	})
	t.Run("someone paid", func(t *testing.T) {
		e := newEnv(t)
		h, p, _ := e.shortOfTheMinimum()
		v := e.owner(h)
		e.runPayment("capture", v.Workshop.ID, e.latestAttempt(v.Enrollments[0].ID))
		e.wantStatus(v.Enrollments[0].ID, domain.EnrollmentPaid)
		e.wantError(http.MethodPost, h.ownerPath+"/extend", extendBody(p.ClosesAt, extendTo), http.StatusConflict, "charging_started")
		if x := e.extendView(h).Extension; x == nil || x.Available || x.Refusal != "charging_started" || x.Reason == "" {
			t.Errorf("extension with a paid seat = %+v", x)
		}
	})
	t.Run("a v4 reservation", func(t *testing.T) {
		e := newEnv(t)
		h, p, _ := e.shortOfTheMinimum()
		if _, err := e.st.Pool.Exec(e.ctx, `update enrollments set consent_version = 'consent-v4'`); err != nil {
			t.Fatal(err)
		}
		e.wantError(http.MethodPost, h.ownerPath+"/extend", extendBody(p.ClosesAt, extendTo), http.StatusConflict, "consent_predates_extension")
		v := e.extendView(h)
		if v.Extension == nil || v.Extension.Refusal != "consent_predates_extension" || slices.Contains(kinds(v.Suggestions), "rescue") {
			t.Errorf("extension = %+v, suggestions %v; want it refused and no rescue offered", v.Extension, kinds(v.Suggestions))
		}
	})
	t.Run("close running", func(t *testing.T) {
		e := newEnv(t)
		h, p, _ := e.shortOfTheMinimum()
		if _, err := e.st.Pool.Exec(e.ctx, `update jobs set status = 'running' where idempotency_key = $1`, "close:"+p.ID); err != nil {
			t.Fatal(err)
		}
		e.wantError(http.MethodPost, h.ownerPath+"/extend", extendBody(p.ClosesAt, extendTo), http.StatusConflict, "close_in_progress")
	})
	t.Run("cancelled", func(t *testing.T) {
		e := newEnv(t)
		h, p, _ := e.shortOfTheMinimum()
		e.call(http.MethodPost, h.ownerPath+"/cancel", map[string]string{}, http.StatusOK, nil)
		e.wantError(http.MethodPost, h.ownerPath+"/extend", extendBody(p.ClosesAt, extendTo), http.StatusConflict, "not_funding")
	})
	t.Run("not open yet", func(t *testing.T) {
		e := newEnv(t)
		h := e.createWorkshop()
		e.wantError(http.MethodPost, h.ownerPath+"/extend", extendBody(t0, extendTo), http.StatusConflict, "proposal_not_open")
		if v := e.extendView(h); v.Extension != nil {
			t.Errorf("extension before reservations open = %+v, want none", v.Extension)
		}
	})
}

func TestExtend_NoRescueUntilTheLastSixHours(t *testing.T) {
	e := newEnv(t)
	h, p, _ := e.shortOfTheMinimum()
	e.clock.Set(p.ClosesAt.Add(-domain.RescueWhenLeft - time.Minute))
	v := e.extendView(h)
	if !v.Extension.Available || slices.Contains(kinds(v.Suggestions), "rescue") {
		t.Errorf("with over six hours left: extension %+v, suggestions %v; want it on offer but not pushed", v.Extension, kinds(v.Suggestions))
	}
}

type rescueAI struct {
	ai.Fake
	text string
}

func (a rescueAI) Suggest(ctx context.Context, in ai.SuggestInput) (string, ai.Meta, error) {
	if in.Kind == string(domain.SuggestRescue) {
		return a.text, ai.Meta{Provider: "test", Model: "test"}, nil
	}
	return a.Fake.Suggest(ctx, in)
}

func TestExtend_ClaudeMayNameTheNewDeadlineButNoOtherDate(t *testing.T) {
	cases := []struct {
		text string
		kept bool
	}{
		{"Go for operators on Sat, Nov 14 got more time: reserve by Thursday, Nov 5 at 4 AM. We need one more, or nobody pays.", true},
		{"Go for operators on Sat, Nov 14 got more time: reserve by Friday, Nov 6. We need one more, or nobody pays.", false},
	}
	for _, tc := range cases {
		t.Run(strconv.FormatBool(tc.kept), func(t *testing.T) {
			e := newEnv(t, withAI(rescueAI{text: tc.text}))
			h, _, _ := e.shortOfTheMinimum()
			v := e.extendView(h)
			got := v.Suggestions[0]
			if got.Kind != "rescue" {
				t.Fatalf("suggestions = %v", kinds(v.Suggestions))
			}
			if kept := strings.HasPrefix(got.Message, tc.text); kept != tc.kept {
				t.Errorf("Claude wrote %q; message = %q, kept %v, want %v", tc.text, got.Message, kept, tc.kept)
			}
		})
	}
}

func TestDemoSkipToLastHours(t *testing.T) {
	t.Run("off outside demo mode", func(t *testing.T) {
		e := newEnv(t)
		h, _, _ := e.fundingWorkshop()
		e.wantError(http.MethodPost, h.ownerPath+"/demo/skip-to-last-hours", nil, http.StatusNotFound, "not_found")
	})
	t.Run("moves the deadline and its close", func(t *testing.T) {
		e := newEnv(t, withDemoMode())
		h, p, _ := e.fundingWorkshop()
		e.clock.Set(t0.Add(time.Hour))
		var out struct {
			Proposal extendProposalJSON `json:"proposal"`
		}
		e.call(http.MethodPost, h.ownerPath+"/demo/skip-to-last-hours", nil, http.StatusAccepted, &out)
		want := t0.Add(3 * time.Hour)
		if !out.Proposal.ClosesAt.Equal(want) {
			t.Errorf("closes_at = %s, want %s", out.Proposal.ClosesAt, want)
		}
		if j := e.job("close:" + p.ID); !j.RunAt.Equal(want) {
			t.Errorf("close runs at %s, want %s", j.RunAt, want)
		}
		v := e.extendView(h)
		if v.Extension == nil || !v.Extension.Available || !slices.Contains(kinds(v.Suggestions), "rescue") {
			t.Errorf("after the skip: extension %+v, suggestions %v; want the rescue", v.Extension, kinds(v.Suggestions))
		}
		e.call(http.MethodPost, h.ownerPath+"/extend", extendBody(want, *v.Extension.To), http.StatusOK, nil)
		e.wantError(http.MethodPost, h.ownerPath+"/demo/skip-to-last-hours", nil, http.StatusConflict, "already_extended")
	})
	t.Run("only while reservations are open", func(t *testing.T) {
		e := newEnv(t, withDemoMode())
		h := e.createWorkshop()
		e.wantError(http.MethodPost, h.ownerPath+"/demo/skip-to-last-hours", nil, http.StatusConflict, "proposal_not_open")
	})
}

func TestConsent_TheCurrentVersionAllowsAnExtension(t *testing.T) {
	if !slices.Contains(domain.ExtensionConsents, httpapi.ConsentVersion) {
		t.Errorf("%s is not in domain.ExtensionConsents %v", httpapi.ConsentVersion, domain.ExtensionConsents)
	}
	if !strings.Contains(httpapi.ConsentText, "The organizer can extend the deadline once, to at most three days after reservations opened.") {
		t.Errorf("consent text does not say the deadline can move: %q", httpapi.ConsentText)
	}
}

func TestConsent_APageLeftOpenOnV4IsAskedAgain(t *testing.T) {
	e := newEnv(t)
	_, p, ps := e.fundingWorkshop()
	e.wantError(http.MethodPost, ps[0].path+"/enrollment", map[string]string{"consent_version": "consent-v4", "proposal_id": p.ID}, http.StatusConflict, "consent_version_mismatch")
}
