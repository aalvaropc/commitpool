package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/httpapi"
	"github.com/aalvaropc/commitpool/internal/paypal"
	"github.com/aalvaropc/commitpool/internal/store"
)

const consentText = "You authorize a temporary hold. We will attempt to charge it once enough people have authorized. " +
	"The workshop is confirmed when the minimum number of completed payments is reached. " +
	"If the minimum is not reached by the deadline, the workshop is cancelled and we request a refund of any charge already made. " +
	"Refunds can take time and you will be able to follow their status."

var tokenShape = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

// fundingWorkshop creates the default workshop (minimum 2, capacity 3), adds
// two participants who can both make Saturday and opens payments on it.
func (e *env) fundingWorkshop(edit ...func(map[string]any)) (workshopHandle, proposalJSON, []participantHandle) {
	e.t.Helper()
	h := e.createWorkshop(edit...)
	ps := []participantHandle{
		e.joinAndSay(h, "Ana", "Saturday works if we finish before six"),
		e.joinAndSay(h, "Luis", "Any day works for me"),
	}
	return h, e.openProposal(h, h.satID), ps
}

func TestWorkshops_CreateReturnsOwnerAndShareURLs(t *testing.T) {
	e := newEnv(t)
	h := e.createWorkshop()

	if !strings.HasPrefix(h.created.OwnerURL, appBaseURL+"/owner/") || !tokenShape.MatchString(h.ownerToken) {
		t.Errorf("owner_url = %q, want %s/owner/{43 base64url chars}", h.created.OwnerURL, appBaseURL)
	}
	if !strings.HasPrefix(h.created.ShareURL, appBaseURL+"/w/") || !tokenShape.MatchString(h.shareToken) {
		t.Errorf("share_url = %q, want %s/w/{43 base64url chars}", h.created.ShareURL, appBaseURL)
	}
	if h.ownerToken == h.shareToken {
		t.Errorf("owner and share tokens are the same")
	}
	w := h.created.Workshop
	if w.Price != (moneyJSON{Minor: 1000, Currency: "USD", Value: "10.00"}) || w.Currency != "USD" {
		t.Errorf("workshop price = %+v %s, want 1000 USD", w.Price, w.Currency)
	}
	if w.Status != "COLLECTING" || w.Resolution != "OPEN" || w.PaymentWindowMin != 1440 || w.Minimum != 2 || w.Capacity != 3 {
		t.Errorf("workshop = %+v", w)
	}
	if h.satID == "" || h.sunID == "" {
		t.Fatalf("owner view does not list both candidate dates")
	}

	e.wantError(http.MethodPost, "/api/v1/workshops", withField("minimum", 4), http.StatusBadRequest, "minimum_above_capacity")
}

func withField(name string, value any) map[string]any {
	body := workshopRequest()
	body[name] = value
	return body
}

func TestWorkshops_CreateRejectsInvalidTerms(t *testing.T) {
	e := newEnv(t)
	past := t0.Add(-time.Hour)
	cases := []struct {
		name  string
		body  map[string]any
		code  string
		field string
	}{
		{"zero price", withField("price", "0.00"), "invalid_price", ""},
		{"negative price", withField("price", "-10.00"), "invalid_price", ""},
		{"three decimals", withField("price", "10.001"), "invalid_price", ""},
		{"bad currency", withField("currency", "US"), "invalid_price", ""},
		{"unsupported currency", withField("currency", "EUR"), "unsupported_currency", ""},
		{"empty title", withField("title", "  "), "invalid_title", ""},
		{"zero duration", withField("duration_min", 0), "invalid_duration", ""},
		{"unknown timezone", withField("timezone", "Mars/Olympus"), "invalid_timezone", ""},
		{"local timezone", withField("timezone", "Local"), "invalid_timezone", ""},
		{"zero minimum", withField("minimum", 0), "invalid_minimum", ""},
		{"zero window", withField("payment_window_min", 0), "invalid_payment_window", ""},
		{"no dates", withField("dates", []any{}), "invalid_dates", ""},
		{"end before start", withField("dates", []map[string]string{{"starts_at": satStart.Format(time.RFC3339), "ends_at": satStart.Add(-time.Hour).Format(time.RFC3339)}}), "invalid_dates", ""},
		{"date in the past", withField("dates", []map[string]string{{"starts_at": past.Format(time.RFC3339), "ends_at": t0.Format(time.RFC3339)}}), "invalid_dates", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e.t = t
			e.wantError(http.MethodPost, "/api/v1/workshops", c.body, http.StatusBadRequest, c.code)
		})
	}
	e.t = t
	e.wantError(http.MethodPost, "/api/v1/workshops", `{"title":`, http.StatusBadRequest, "invalid_json")
	e.wantError(http.MethodPost, "/api/v1/workshops", `{"title":"a"} {"title":"b"}`, http.StatusBadRequest, "invalid_json")

	var n int
	if err := e.st.Pool.QueryRow(e.ctx, `select count(*) from workshops`).Scan(&n); err != nil || n != 0 {
		t.Errorf("workshops stored after refused requests = %d, %v; want 0", n, err)
	}
}

func TestProposal_OpenLocksTermsAndSchedulesClose(t *testing.T) {
	e := newEnv(t)
	h := e.createWorkshop()
	e.joinAndSay(h, "Ana", "Saturday works if we finish before six")
	e.joinAndSay(h, "Luis", "Any day works for me")

	p := e.openProposal(h, h.satID)
	if !p.OpensAt.Equal(t0) || !p.ClosesAt.Equal(t0.Add(24*time.Hour)) || !p.StartsAt.Equal(satStart) {
		t.Errorf("proposal times = opens %s, closes %s, starts %s", p.OpensAt, p.ClosesAt, p.StartsAt)
	}
	if p.DateID != h.satID || p.Version != 1 || !p.PaymentsOpen || p.Price.Minor != 1000 || p.Minimum != 2 || p.Capacity != 3 {
		t.Errorf("proposal = %+v", p)
	}

	closeJob := e.job("close:" + p.ID)
	if closeJob.Kind != "close" || closeJob.Status != "pending" || !closeJob.RunAt.Equal(p.ClosesAt) || closeJob.AttemptID != nil {
		t.Errorf("close job = %+v, want pending close at %s", closeJob, p.ClosesAt)
	}
	if closeJob.WorkshopID == nil || *closeJob.WorkshopID != h.created.Workshop.ID {
		t.Errorf("close job workshop = %v, want %s", closeJob.WorkshopID, h.created.Workshop.ID)
	}

	e.wantError(http.MethodPost, h.ownerPath+"/proposal", map[string]string{"date_id": h.sunID}, http.StatusConflict, "proposal_already_open")

	// Terms are locked by design: nothing edits a workshop after creation.
	for _, method := range []string{http.MethodPut, http.MethodPatch, http.MethodPost} {
		e.wantError(method, h.ownerPath, withField("price", "1.00"), http.StatusNotFound, "not_found")
	}
	e.wantError(http.MethodPatch, "/api/v1/workshops", withField("price", "1.00"), http.StatusNotFound, "not_found")

	v := e.owner(h)
	if v.Workshop.Status != "FUNDING" || v.Workshop.Price.Minor != 1000 || v.Proposal == nil || v.Proposal.ID != p.ID {
		t.Errorf("owner view after opening = workshop %+v, proposal %+v", v.Workshop, v.Proposal)
	}
}

func TestProposal_RefusesUnknownDatesAndTooFewCompatible(t *testing.T) {
	e := newEnv(t)
	h := e.createWorkshop()
	other := e.createWorkshop()
	e.joinAndSay(h, "Ana", "Saturday works if we finish before six")

	e.wantError(http.MethodPost, h.ownerPath+"/proposal", map[string]string{"date_id": other.satID}, http.StatusBadRequest, "invalid_date")
	e.wantError(http.MethodPost, h.ownerPath+"/proposal", map[string]string{"date_id": "nope"}, http.StatusBadRequest, "invalid_date")
	got := e.wantError(http.MethodPost, h.ownerPath+"/proposal", map[string]string{"date_id": h.satID}, http.StatusConflict, "proposal_invalid")
	if got.Reason != "not_enough_compatible" {
		t.Errorf("reason = %q, want not_enough_compatible", got.Reason)
	}

	soon := e.createWorkshop(func(b map[string]any) {
		start := t0.Add(36 * time.Hour)
		b["dates"] = []map[string]string{{"starts_at": start.Format(time.RFC3339), "ends_at": start.Add(time.Hour).Format(time.RFC3339)}}
		b["minimum"] = 1
	})
	e.joinAndSay(soon, "Ana", "Any day works for me")
	view := e.owner(soon)
	got = e.wantError(http.MethodPost, soon.ownerPath+"/proposal", map[string]string{"date_id": view.Dates[0].ID}, http.StatusConflict, "proposal_invalid")
	if got.Reason != "starts_too_soon" {
		t.Errorf("reason = %q, want starts_too_soon", got.Reason)
	}

	if v := e.owner(h); v.Workshop.Status != "COLLECTING" || v.Proposal != nil {
		t.Errorf("refused proposals changed the workshop: %+v, proposal %+v", v.Workshop, v.Proposal)
	}
}

func TestEnrollment_RequiresConsentVersionAndLockedProposal(t *testing.T) {
	e := newEnv(t)
	h := e.createWorkshop()
	ana := e.joinAndSay(h, "Ana", "Saturday works if we finish before six")
	e.joinAndSay(h, "Luis", "Any day works for me")

	e.wantError(http.MethodPost, ana.path+"/enrollment", enrollBody(""), http.StatusConflict, "proposal_not_open")

	p := e.openProposal(h, h.satID)
	e.wantError(http.MethodPost, ana.path+"/enrollment",
		map[string]string{"consent_version": "x", "proposal_id": p.ID}, http.StatusConflict, "consent_version_mismatch")
	e.wantError(http.MethodPost, ana.path+"/enrollment",
		map[string]string{"proposal_id": p.ID}, http.StatusConflict, "consent_version_mismatch")
	e.wantError(http.MethodPost, ana.path+"/enrollment", enrollBody("00000000-0000-4000-8000-000000000000"), http.StatusConflict, "proposal_not_open")

	e.clock.Set(p.ClosesAt)
	e.wantError(http.MethodPost, ana.path+"/enrollment", enrollBody(p.ID), http.StatusConflict, "proposal_not_open")

	if n := len(e.fake.Requests("create_order")); n != 0 {
		t.Errorf("create_order requests = %d, want none for refused enrollments", n)
	}
	rows, err := e.st.EnrollmentsByWorkshop(e.ctx, h.created.Workshop.ID)
	if err != nil || len(rows) != 0 {
		t.Errorf("enrollments = %d, %v; want none", len(rows), err)
	}
}

func TestEnrollment_ReturnsApproveURLAndIsIdempotentPerParticipant(t *testing.T) {
	e := newEnv(t)
	_, p, ps := e.fundingWorkshop()

	first := e.enroll(ps[0], p.ID, http.StatusCreated)
	if first.EnrollmentID == "" || !strings.HasPrefix(first.ApproveURL, e.fake.URL+"/approve?order=") {
		t.Fatalf("first enrollment = %+v, want an approval link on the fake", first)
	}
	if first.Status != "SEAT_RESERVED" || first.StatusLabel == "" {
		t.Errorf("first enrollment status = %q %q", first.Status, first.StatusLabel)
	}
	second := e.enroll(ps[0], p.ID, http.StatusOK)
	if second.EnrollmentID != first.EnrollmentID || second.ApproveURL != first.ApproveURL {
		t.Errorf("second tab = %+v, want the first enrollment %+v", second, first)
	}

	orders := e.fake.Requests("create_order")
	if len(orders) != 1 {
		t.Fatalf("create_order requests = %d, want 1", len(orders))
	}
	a := e.latestAttempt(first.EnrollmentID)
	if orders[0].RequestID != "create_order:"+a.ID || a.AttemptNo != 1 {
		t.Errorf("create_order request id = %q for attempt %+v", orders[0].RequestID, a)
	}
	if a.AmountMinor != 1000 || a.Currency != "USD" {
		t.Errorf("attempt amount = %d %s, want the proposal's 1000 USD", a.AmountMinor, a.Currency)
	}
	en, err := e.st.EnrollmentByID(e.ctx, first.EnrollmentID)
	if err != nil || en.ConsentVersion != httpapi.ConsentVersion || !en.ConsentAt.Equal(t0) || !en.SeatExpiresAt.Equal(t0.Add(30*time.Minute)) {
		t.Errorf("enrollment = %+v, %v; want consent-v1 at t0 and a 30-minute seat", en, err)
	}
}

func TestEnrollment_CapacityFullIs409(t *testing.T) {
	e := newEnv(t)
	h, p, ps := e.fundingWorkshop(func(b map[string]any) { b["capacity"] = 2 })
	third := e.join(h, "Marta")

	e.enroll(ps[0], p.ID, http.StatusCreated)
	e.enroll(ps[1], p.ID, http.StatusCreated)
	e.wantError(http.MethodPost, third.path+"/enrollment", enrollBody(p.ID), http.StatusConflict, "capacity_full")

	if n := len(e.fake.Requests("create_order")); n != 2 {
		t.Errorf("create_order requests = %d, want 2", n)
	}
	// A participant who already holds a seat still gets it back.
	e.enroll(ps[0], p.ID, http.StatusOK)
}

func TestEnrollment_PayPalFailureIs502AndTheRetryKeepsTheKey(t *testing.T) {
	e := newEnv(t)
	h, p, ps := e.fundingWorkshop()

	e.fake.FailNext("create_order", http.StatusTooManyRequests, "RATE_LIMIT_REACHED")
	e.wantError(http.MethodPost, ps[0].path+"/enrollment", enrollBody(p.ID), http.StatusBadGateway, "paypal_unavailable")
	rows, err := e.st.EnrollmentsByWorkshop(e.ctx, h.created.Workshop.ID)
	if err != nil || len(rows) != 1 || rows[0].Status != domain.EnrollmentSeatReserved {
		t.Fatalf("enrollments after a PayPal failure = %+v, %v; want one SEAT_RESERVED", rows, err)
	}
	a := e.latestAttempt(rows[0].ID)

	// The retry is not due yet: the same attempt waits for its backoff.
	e.wantError(http.MethodPost, ps[0].path+"/enrollment", enrollBody(p.ID), http.StatusBadGateway, "paypal_unavailable")
	e.clock.Set(t0.Add(5 * time.Second))
	got := e.enroll(ps[0], p.ID, http.StatusOK)
	if got.EnrollmentID != rows[0].ID || got.ApproveURL == "" {
		t.Errorf("enrollment after the backoff = %+v, want %s with a link", got, rows[0].ID)
	}
	orders := e.fake.Requests("create_order")
	if len(orders) != 2 {
		t.Fatalf("create_order requests = %d, want the failed one and its retry", len(orders))
	}
	for _, o := range orders {
		if o.RequestID != "create_order:"+a.ID {
			t.Errorf("create_order request id = %q, want create_order:%s for every try", o.RequestID, a.ID)
		}
	}
	if b := e.latestAttempt(rows[0].ID); b.ID != a.ID {
		t.Errorf("a retry opened attempt %d, want attempt %d reused", b.AttemptNo, a.AttemptNo)
	}
}

func TestEnrollment_StoreFailureRunningTheCheckoutIs500(t *testing.T) {
	e := newEnv(t)
	_, p, ps := e.fundingWorkshop()
	// The database refuses to claim the create_order job the request runs.
	for _, stmt := range []string{
		`create function test_refuse_claim() returns trigger language plpgsql as $$
		 begin raise exception 'claim refused by the test'; end $$`,
		`create trigger test_refuse_claim before update on jobs for each row
		 when (new.status = 'running' and new.kind = 'create_order') execute function test_refuse_claim()`,
	} {
		if _, err := e.st.Pool.Exec(e.ctx, stmt); err != nil {
			t.Fatalf("install the failing trigger: %v", err)
		}
	}
	t.Cleanup(func() {
		_, _ = e.st.Pool.Exec(context.Background(), `drop trigger if exists test_refuse_claim on jobs`)
		_, _ = e.st.Pool.Exec(context.Background(), `drop function if exists test_refuse_claim()`)
	})

	e.wantError(http.MethodPost, ps[0].path+"/enrollment", enrollBody(p.ID), http.StatusInternalServerError, "internal_error")

	if n := len(e.fake.Requests("create_order")); n != 0 {
		t.Errorf("create_order requests = %d, want 0", n)
	}
}

func TestEnrollment_ExpiredOrderGetsANewCheckout(t *testing.T) {
	e := newEnv(t)
	h, p, ps := e.fundingWorkshop()
	first := e.enroll(ps[0], p.ID, http.StatusCreated)
	a := e.latestAttempt(first.EnrollmentID)

	// The buyer never approved and PayPal dropped the order; the return's
	// authorize finds it gone and the worker records it EXPIRED.
	e.fake.ExpireOrder(*a.OrderID)
	e.runPayment("authorize", h.created.Workshop.ID, a)
	if got := e.latestAttempt(first.EnrollmentID); got.OrderStatus == nil || *got.OrderStatus != paypal.StatusExpired {
		t.Fatalf("order status after PayPal dropped it = %v, want EXPIRED", got.OrderStatus)
	}

	again := e.enroll(ps[0], p.ID, http.StatusOK)
	if again.EnrollmentID != first.EnrollmentID || again.ApproveURL == first.ApproveURL || again.ApproveURL == "" {
		t.Errorf("checkout after the order expired = %+v, want the same enrollment with a new link (was %q)", again, first.ApproveURL)
	}
	if b := e.latestAttempt(first.EnrollmentID); b.AttemptNo != 2 {
		t.Errorf("latest attempt = %d, want 2", b.AttemptNo)
	}
}

func TestEnrollment_AuthorizedHoldReturnsItsEnrollment(t *testing.T) {
	e := newEnv(t)
	h, p, ps := e.fundingWorkshop(func(b map[string]any) { b["capacity"] = 3 })
	first := e.enroll(ps[0], p.ID, http.StatusCreated)
	e.authorize(h.created.Workshop.ID, first.EnrollmentID)

	again := e.enroll(ps[0], p.ID, http.StatusOK)
	if again.EnrollmentID != first.EnrollmentID || again.ApproveURL != "" || again.Status != "AUTHORIZED" || again.StatusLabel != "Hold authorized" {
		t.Errorf("enrollment behind an authorization = %+v, want %s AUTHORIZED without a link", again, first.EnrollmentID)
	}
	if n := len(e.fake.Requests("create_order")); n != 1 {
		t.Errorf("create_order requests = %d, want 1", n)
	}
}

func TestOwner_CountsInterestedAuthorizedPaidSeparately(t *testing.T) {
	e := newEnv(t)
	h := e.createWorkshop(func(b map[string]any) {
		b["minimum"] = 3
		b["capacity"] = 4
	})
	wid := h.created.Workshop.ID
	ana := e.joinAndSay(h, "Ana", "Any day works for me")
	luis := e.joinAndSay(h, "Luis", "Any day works for me")
	e.joinAndSay(h, "Marta", "Any day works for me")
	p := e.openProposal(h, h.satID)

	paid := e.enroll(ana, p.ID, http.StatusCreated)
	held := e.enroll(luis, p.ID, http.StatusCreated)
	e.runPayment("capture", wid, e.authorize(wid, paid.EnrollmentID))
	e.wantStatus(paid.EnrollmentID, domain.EnrollmentPaid)
	e.authorize(wid, held.EnrollmentID)

	v := e.settledOwner(h)
	tl := v.Tally
	if tl.Interested != 3 || tl.Authorized != 1 || tl.Paid != 1 || tl.Minimum != 3 || tl.Capacity != 4 {
		t.Errorf("tally = %+v, want interested 3, authorized 1, paid 1, minimum 3, capacity 4", tl)
	}
	if tl.Reserved == nil || *tl.Reserved != 0 || tl.SeatsTaken == nil || *tl.SeatsTaken != 2 {
		t.Errorf("owner tally detail = reserved %v, seats taken %v; want 0 and 2", tl.Reserved, tl.SeatsTaken)
	}
	if len(v.Availabilities) != 3 || len(v.Participants) != 3 {
		t.Errorf("availabilities = %d, participants = %d; want 3 and 3", len(v.Availabilities), len(v.Participants))
	}
	if len(v.Enrollments) != 2 {
		t.Fatalf("enrollments = %+v, want 2", v.Enrollments)
	}
	byID := map[string]enrollmentJSON{}
	for _, en := range v.Enrollments {
		byID[en.ID] = en
	}
	if got := byID[paid.EnrollmentID]; got.Status != "PAID" || got.StatusLabel != "Seat paid" || got.DisplayName != "Ana" ||
		got.Payment == nil || got.Payment.CaptureID == nil || got.Payment.CaptureStatus == nil || *got.Payment.CaptureStatus != "COMPLETED" {
		t.Errorf("paid enrollment = %+v (payment %+v)", got, got.Payment)
	}
	if got := byID[held.EnrollmentID]; got.Status != "AUTHORIZED" || got.StatusLabel != "Hold authorized" ||
		got.Payment == nil || got.Payment.AuthorizationID == nil || got.Payment.CaptureID != nil {
		t.Errorf("authorized enrollment = %+v (payment %+v)", got, got.Payment)
	}
	if exp := v.Recommendation.Explanation; exp == nil || !strings.Contains(*exp, "Sat 14 Nov 2026") || !strings.Contains(*exp, "Marta") {
		t.Errorf("recommendation explanation = %v, want the interpreter's words for Saturday and its people", exp)
	}
}

func TestParticipant_SeesOnlyOwnDataAndAggregate(t *testing.T) {
	e := newEnv(t)
	h, p, ps := e.fundingWorkshop()
	ana, luis := ps[0], ps[1]
	luisEnrollment := e.enroll(luis, p.ID, http.StatusCreated)

	rec := e.request(http.MethodGet, ana.path, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET participant = %d %s", rec.Code, rec.Body.String())
	}
	raw := rec.Body.String()
	owner := e.owner(h)
	var luisID string
	for _, pt := range owner.Participants {
		if pt.DisplayName == "Luis" {
			luisID = pt.ID
		}
	}
	for _, secret := range []string{"Luis", "Any day works for me", luisID, luisEnrollment.EnrollmentID, h.ownerToken, h.shareToken, luis.token, ana.token, "owner_url", "token"} {
		if secret != "" && strings.Contains(raw, secret) {
			t.Errorf("participant view contains %q:\n%s", secret, raw)
		}
	}

	var v participantViewJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v.Participant.DisplayName != "Ana" || v.Availability == nil || v.Availability.Message != "Saturday works if we finish before six" {
		t.Errorf("own data = %+v, availability %+v", v.Participant, v.Availability)
	}
	if v.Tally.Interested != 2 || v.Tally.Minimum != 2 || v.Tally.Capacity != 3 || v.Tally.Reserved != nil || v.Tally.SeatsTaken != nil {
		t.Errorf("tally = %+v, want the aggregate only", v.Tally)
	}
	if v.Consent.Version != httpapi.ConsentVersion || v.Consent.Text != consentText || httpapi.ConsentText != consentText {
		t.Errorf("consent = %+v", v.Consent)
	}
	if v.Proposal == nil || v.Proposal.ID != p.ID || v.Enrollment != nil || v.Workshop.ID != "" || v.Workshop.Title != "Go for operators" {
		t.Errorf("view = proposal %+v, enrollment %+v, workshop %+v", v.Proposal, v.Enrollment, v.Workshop)
	}

	own := e.enroll(ana, p.ID, http.StatusCreated)
	e.call(http.MethodGet, ana.path, nil, http.StatusOK, &v)
	if v.Enrollment == nil || v.Enrollment.ID != own.EnrollmentID || v.Enrollment.Status != "SEAT_RESERVED" || v.Enrollment.StatusLabel != "Seat reserved" ||
		v.Enrollment.Payment == nil || v.Enrollment.Payment.OrderID == nil {
		t.Errorf("own enrollment = %+v", v.Enrollment)
	}
}

func TestAvailability_FakeInterpreterAndCorrection(t *testing.T) {
	e := newEnv(t)
	h := e.createWorkshop(func(b map[string]any) { b["minimum"] = 1 })
	ana := e.join(h, "Ana")

	saved := e.say(ana, "Saturday works if we finish before six")
	if saved.ID == "" || saved.NeedsClarification {
		t.Fatalf("availability = %+v", saved)
	}
	var sat domain.DateVerdict
	for _, v := range saved.Interpretation.PerDate {
		if v.CandidateDateID == h.satID {
			sat = v
		}
	}
	if sat.Verdict != domain.VerdictConditional || sat.Condition != (domain.Condition{Type: domain.ConditionEndBefore, Time: "18:00"}) {
		t.Errorf("Saturday reading = %+v, want conditional end_before 18:00", sat)
	}
	if len(saved.Interpretation.PerDate) != 2 {
		t.Errorf("per_date = %+v, want one verdict per candidate date", saved.Interpretation.PerDate)
	}

	before := e.owner(h)
	if before.Recommendation.DateID != h.satID || !before.Recommendation.MeetsMinimum {
		t.Errorf("recommendation before the correction = %+v, want Saturday", before.Recommendation)
	}

	corrected := domain.Interpretation{
		PerDate: []domain.DateVerdict{
			{CandidateDateID: h.satID, Verdict: domain.VerdictUnavailable, Condition: domain.Condition{Type: domain.ConditionNone}, Evidence: "I misread my calendar"},
			{CandidateDateID: h.sunID, Verdict: domain.VerdictAvailable, Condition: domain.Condition{Type: domain.ConditionNone}},
		},
	}
	e.call(http.MethodPut, ana.path+"/availability/"+saved.ID, map[string]any{"corrected": corrected}, http.StatusOK, nil)

	after := e.owner(h)
	if len(after.Availabilities) != 1 {
		t.Fatalf("availabilities = %+v", after.Availabilities)
	}
	got := after.Availabilities[0]
	if got.Corrected == nil || got.Corrected.PerDate[0].Verdict != domain.VerdictUnavailable || got.Message != "Saturday works if we finish before six" || got.DisplayName != "Ana" {
		t.Errorf("owner availability = %+v", got)
	}
	if got.Interpretation.PerDate[0].Verdict != domain.VerdictConditional {
		t.Errorf("the raw interpretation changed: %+v", got.Interpretation)
	}
	if after.Recommendation.DateID != h.sunID {
		t.Errorf("recommendation after the correction = %+v, want Sunday", after.Recommendation)
	}
	for _, d := range after.Recommendation.PerDate {
		if d.DateID == h.satID && d.Blockers[got.ParticipantID] != "unavailable" {
			t.Errorf("Saturday blockers = %+v, want the participant unavailable", d.Blockers)
		}
	}

	vague := e.say(e.join(h, "Luis"), "afternoon")
	if !vague.NeedsClarification {
		t.Errorf("%q = %+v, want needs_clarification", "afternoon", vague)
	}
}

func TestAvailability_CorrectionIsValidatedAndOwnOnly(t *testing.T) {
	e := newEnv(t)
	h := e.createWorkshop()
	ana := e.join(h, "Ana")
	luis := e.join(h, "Luis")
	saved := e.say(ana, "Any day works for me")
	path := ana.path + "/availability/" + saved.ID

	bad := []domain.Interpretation{
		{PerDate: []domain.DateVerdict{{CandidateDateID: "someone-else", Verdict: domain.VerdictAvailable}}},
		{PerDate: []domain.DateVerdict{{CandidateDateID: h.satID, Verdict: "maybe"}}},
		{PerDate: []domain.DateVerdict{{CandidateDateID: h.satID, Verdict: domain.VerdictConditional, Condition: domain.Condition{Type: domain.ConditionEndBefore, Time: "6pm"}}}},
		{PerDate: []domain.DateVerdict{{CandidateDateID: h.satID, Verdict: domain.VerdictAvailable}, {CandidateDateID: h.satID, Verdict: domain.VerdictUnavailable}}},
	}
	for _, in := range bad {
		e.wantError(http.MethodPut, path, map[string]any{"corrected": in}, http.StatusBadRequest, "invalid_correction")
	}
	e.wantError(http.MethodPut, path, map[string]any{}, http.StatusBadRequest, "invalid_correction")
	e.wantError(http.MethodPut, luis.path+"/availability/"+saved.ID,
		map[string]any{"corrected": domain.Interpretation{}}, http.StatusNotFound, "not_found")
	e.wantError(http.MethodPost, ana.path+"/availability", map[string]string{"message": " "}, http.StatusBadRequest, "invalid_message")

	v := e.owner(h)
	if len(v.Availabilities) != 1 || v.Availabilities[0].Corrected != nil {
		t.Errorf("refused corrections were stored: %+v", v.Availabilities)
	}
}

func TestDemoEndpoints_DisabledWithoutFlags(t *testing.T) {
	t.Run("without flags", func(t *testing.T) {
		e := newEnv(t)
		h, _, ps := e.fundingWorkshop()
		en := e.enroll(ps[0], e.owner(h).Proposal.ID, http.StatusCreated)
		e.wantError(http.MethodPost, h.ownerPath+"/demo/close-now", nil, http.StatusNotFound, "not_found")
		e.wantError(http.MethodPost, h.ownerPath+"/demo/fail-next-capture", map[string]string{"enrollment_id": en.EnrollmentID}, http.StatusNotFound, "not_found")
		if v := e.owner(h); v.Demo.DemoMode || v.Demo.SandboxMocks {
			t.Errorf("demo flags = %+v, want both off", v.Demo)
		}
	})

	t.Run("demo mode closes now", func(t *testing.T) {
		e := newEnv(t, withDemoMode())
		h, p, _ := e.fundingWorkshop()
		now := t0.Add(2 * time.Hour)
		e.clock.Set(now)
		e.call(http.MethodPost, h.ownerPath+"/demo/close-now", nil, http.StatusAccepted, nil)

		j := e.job("close:" + p.ID)
		if j.Status != "pending" || j.RunAt.After(now) {
			t.Errorf("close job = %s at %s, want pending at or before %s", j.Status, j.RunAt, now)
		}
		v := e.owner(h)
		if v.Proposal == nil || !v.Proposal.ClosesAt.Equal(now) || v.Proposal.PaymentsOpen || !v.Demo.DemoMode {
			t.Errorf("proposal after close-now = %+v, demo %+v; want closed at %s", v.Proposal, v.Demo, now)
		}
		if v.Workshop.Status != "FUNDING" {
			t.Errorf("workshop = %s, want FUNDING until the close job runs", v.Workshop.Status)
		}
		e.call(http.MethodPost, h.ownerPath+"/demo/close-now", nil, http.StatusAccepted, nil)
		if again := e.job("close:" + p.ID); again.ID != j.ID {
			t.Errorf("a second close-now replaced the close job")
		}

		if err := e.runner.RunKey(e.ctx, "close:"+p.ID); err != nil {
			t.Fatalf("RunKey(close): %v", err)
		}
		if v := e.owner(h); v.Workshop.Status != "CANCELLED" {
			t.Errorf("workshop after the close job = %s, want CANCELLED with nobody paid", v.Workshop.Status)
		}
	})

	t.Run("close-now before payments open", func(t *testing.T) {
		e := newEnv(t, withDemoMode())
		h := e.createWorkshop()
		e.wantError(http.MethodPost, h.ownerPath+"/demo/close-now", nil, http.StatusConflict, "proposal_not_open")
	})

	t.Run("sandbox mocks fail the next capture", func(t *testing.T) {
		e := newEnv(t, withSandboxMocks())
		h, p, ps := e.fundingWorkshop(func(b map[string]any) { b["minimum"] = 1 })
		wid := h.created.Workshop.ID
		_, otherP, otherPs := e.fundingWorkshop()
		foreign := e.enroll(otherPs[0], otherP.ID, http.StatusCreated)

		en := e.enroll(ps[0], p.ID, http.StatusCreated)
		e.wantError(http.MethodPost, h.ownerPath+"/demo/fail-next-capture", map[string]string{"enrollment_id": foreign.EnrollmentID}, http.StatusNotFound, "not_found")
		e.wantError(http.MethodPost, h.ownerPath+"/demo/fail-next-capture", map[string]string{"enrollment_id": en.EnrollmentID, "mock_code": "INTERNAL_SERVER_ERROR"}, http.StatusBadRequest, "unsupported_mock_code")
		e.call(http.MethodPost, h.ownerPath+"/demo/fail-next-capture", map[string]string{"enrollment_id": en.EnrollmentID}, http.StatusAccepted, nil)

		a := e.latestAttempt(en.EnrollmentID)
		if a.MockCode == nil || *a.MockCode != "AUTHORIZATION_EXPIRED" {
			t.Fatalf("mock code = %v, want AUTHORIZATION_EXPIRED", a.MockCode)
		}
		e.runPayment("capture", wid, e.authorize(wid, en.EnrollmentID))
		captures := e.fake.Requests("capture")
		if len(captures) == 0 || !strings.Contains(captures[0].Headers.Get("PayPal-Mock-Response"), "AUTHORIZATION_EXPIRED") {
			t.Fatalf("capture requests = %+v, want the mock header", captures)
		}
		e.wantStatus(en.EnrollmentID, domain.EnrollmentCaptureFailed)
		e.wantError(http.MethodPost, h.ownerPath+"/demo/fail-next-capture", map[string]string{"enrollment_id": en.EnrollmentID}, http.StatusConflict, "capture_already_started")
	})
}

func TestHealthz_FailsWithinItsTimeoutWhenTheDatabaseHangs(t *testing.T) {
	e := newEnv(t)
	if rec := e.request(http.MethodGet, "/healthz", nil); rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"ok":true}` {
		t.Fatalf("GET /healthz = %d %s, want 200 {\"ok\":true}", rec.Code, rec.Body.String())
	}

	// A database that accepts connections and never answers.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()
	pool, err := pgxpool.New(e.ctx, "postgres://commitpool:commitpool@"+ln.Addr().String()+"/commitpool?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = ln.Close()
		mu.Lock()
		for _, c := range conns {
			_ = c.Close()
		}
		mu.Unlock()
		pool.Close()
	})
	h := httpapi.New(httpapi.Deps{Store: &store.Store{Pool: pool}, Log: slog.New(slog.DiscardHandler), Dist: fstest.MapFS{}})

	start := time.Now()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(e.ctx, http.MethodGet, "/healthz", nil))
		done <- rec
	}()
	select {
	case rec := <-done:
		if rec.Code != http.StatusServiceUnavailable || strings.TrimSpace(rec.Body.String()) != `{"ok":false}` {
			t.Errorf("GET /healthz on a hung database = %d %s, want 503 {\"ok\":false}", rec.Code, rec.Body.String())
		}
		if took := time.Since(start); took > 5*time.Second {
			t.Errorf("GET /healthz took %s, want it bounded by its ping timeout", took)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("GET /healthz on a hung database did not answer within 10s")
	}
}

func TestSPA_FallbackServesIndex(t *testing.T) {
	e := newEnv(t)
	for _, path := range []string{"/", "/owner/abc", "/p/xyz", "/w/abc", "/new", "/paypal/returned?e=1&result=authorized", "/.keep"} {
		rec := e.request(http.MethodGet, path, nil)
		if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") || rec.Body.String() != indexHTML {
			t.Errorf("GET %s = %d %q %q, want the embedded index.html", path, rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
		}
	}
	if rec := e.request(http.MethodGet, "/assets/app.js", nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "commitpool") {
		t.Errorf("GET /assets/app.js = %d %q", rec.Code, rec.Body.String())
	}
	for _, path := range []string{"/assets/missing.js", "/favicon.ico", "/owner/abc/logo.png"} {
		if rec := e.request(http.MethodGet, path, nil); rec.Code != http.StatusNotFound || rec.Body.String() == indexHTML {
			t.Errorf("GET %s = %d %q, want 404 for a missing file", path, rec.Code, rec.Body.String())
		}
	}
	for _, path := range []string{"/api/v1/nope", "/api/", "/api/v1/workshops"} {
		e.wantError(http.MethodGet, path, nil, http.StatusNotFound, "not_found")
	}
	e.wantError(http.MethodPost, "/owner/abc", nil, http.StatusMethodNotAllowed, "method_not_allowed")

	var health struct {
		OK bool `json:"ok"`
	}
	e.call(http.MethodGet, "/healthz", nil, http.StatusOK, &health)
	if !health.OK {
		t.Errorf("healthz ok = false")
	}
}

func TestTokens_OnlyHashesAreStoredAndUnknownTokensAre404(t *testing.T) {
	e := newEnv(t)
	h := e.createWorkshop()
	ana := e.join(h, "Ana")

	var hashed int
	err := e.st.Pool.QueryRow(e.ctx,
		`select count(*) from workshops where owner_token_hash = $1 and share_token_hash = $2`,
		store.HashToken(h.ownerToken), store.HashToken(h.shareToken)).Scan(&hashed)
	if err != nil || hashed != 1 {
		t.Errorf("workshops matching the token hashes = %d, %v; want 1", hashed, err)
	}
	var plain int
	err = e.st.Pool.QueryRow(e.ctx,
		`select count(*) from participants where access_token_hash = $1 or access_token_hash = convert_to($2, 'UTF8')`,
		store.HashToken(ana.token), ana.token).Scan(&plain)
	if err != nil || plain != 1 {
		t.Errorf("participants matching the participant token = %d, %v; want only its hash", plain, err)
	}

	unknown := strings.Repeat("A", 43)
	for _, path := range []string{
		"/api/v1/owner/" + unknown, "/api/v1/w/" + unknown, "/api/v1/p/" + unknown,
		"/api/v1/owner/short", "/api/v1/p/" + h.ownerToken, "/api/v1/owner/" + h.shareToken, "/api/v1/w/" + ana.token,
	} {
		e.wantError(http.MethodGet, path, nil, http.StatusNotFound, "not_found")
	}
	e.wantError(http.MethodPost, "/api/v1/w/"+unknown+"/participants", map[string]string{"display_name": "Eve"}, http.StatusNotFound, "not_found")
	e.wantError(http.MethodPost, "/api/v1/p/"+unknown+"/enrollment", enrollBody(""), http.StatusNotFound, "not_found")
}

func TestJoin_PublicViewAndNameRules(t *testing.T) {
	e := newEnv(t)
	h := e.createWorkshop()
	e.joinAndSay(h, "Ana", "Any day works for me")

	rec := e.request(http.MethodGet, h.sharePath, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET public view = %d %s", rec.Code, rec.Body.String())
	}
	for _, secret := range []string{"Ana", "Any day works", h.ownerToken, "owner_url"} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Errorf("public view contains %q: %s", secret, rec.Body.String())
		}
	}
	var v struct {
		Workshop workshopJSON  `json:"workshop"`
		Dates    []dateJSON    `json:"dates"`
		Proposal *proposalJSON `json:"proposal"`
		Tally    tallyJSON     `json:"tally"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.Workshop.Title != "Go for operators" || v.Workshop.ID != "" || len(v.Dates) != 2 || v.Proposal != nil || v.Tally.Interested != 1 {
		t.Errorf("public view = %+v", v)
	}
	if v.Dates[0].Label != "Sat 14 Nov 2026, 15:00–18:00 America/Lima" {
		t.Errorf("date label = %q", v.Dates[0].Label)
	}

	e.wantError(http.MethodPost, h.sharePath+"/participants", map[string]string{"display_name": "  "}, http.StatusBadRequest, "invalid_display_name")
	e.wantError(http.MethodPost, h.sharePath+"/participants", map[string]string{"display_name": strings.Repeat("x", 81)}, http.StatusBadRequest, "invalid_display_name")
}

func TestRequests_BodyOver64KiBIs413(t *testing.T) {
	e := newEnv(t)
	e.wantError(http.MethodPost, "/api/v1/workshops", withField("description", strings.Repeat("x", 70<<10)), http.StatusRequestEntityTooLarge, "body_too_large")
	h := e.createWorkshop()
	ana := e.join(h, "Ana")
	e.wantError(http.MethodPost, ana.path+"/availability", map[string]string{"message": strings.Repeat("x", 65<<10)}, http.StatusRequestEntityTooLarge, "body_too_large")
}

type panickingAI struct{ ai.Fake }

func (panickingAI) Interpret(context.Context, ai.InterpretInput) (domain.Interpretation, ai.Meta, error) {
	panic("interpreter exploded")
}

type failingAI struct{ ai.Fake }

func (failingAI) Interpret(context.Context, ai.InterpretInput) (domain.Interpretation, ai.Meta, error) {
	return domain.Interpretation{}, ai.Meta{}, errors.New("model unavailable")
}

func TestRequests_PanicIs500JSONAndLogged(t *testing.T) {
	e := newEnv(t, withAI(panickingAI{}))
	h := e.createWorkshop()
	ana := e.join(h, "Ana")
	e.wantError(http.MethodPost, ana.path+"/availability", map[string]string{"message": "Any day"}, http.StatusInternalServerError, "internal_error")
	if logs := e.logs.String(); !strings.Contains(logs, "interpreter exploded") || strings.Contains(logs, ana.token) {
		t.Errorf("panic log = %s", logs)
	}
	// The server keeps serving.
	e.owner(h)
}

func TestAvailability_InterpreterFailureIs502(t *testing.T) {
	e := newEnv(t, withAI(failingAI{}))
	h := e.createWorkshop()
	ana := e.join(h, "Ana")
	e.wantError(http.MethodPost, ana.path+"/availability", map[string]string{"message": "Any day"}, http.StatusBadGateway, "ai_unavailable")
	if v := e.owner(h); len(v.Availabilities) != 0 {
		t.Errorf("availabilities after a failed interpretation = %+v", v.Availabilities)
	}
}

func TestLogs_NeverContainTokensMessagesOrApprovalLinks(t *testing.T) {
	e := newEnv(t)
	h, p, ps := e.fundingWorkshop()
	en := e.enroll(ps[0], p.ID, http.StatusCreated)
	e.request(http.MethodGet, ps[0].path, nil)
	e.request(http.MethodGet, "/api/v1/p/"+strings.Repeat("B", 43), nil)

	logs := e.logs.String()
	if !strings.Contains(logs, "/api/v1/p/{token}") {
		t.Fatalf("request log does not name routes; the check would be vacuous:\n%s", logs)
	}
	secrets := []string{h.ownerToken, h.shareToken, ps[0].token, ps[1].token, strings.Repeat("B", 43),
		"Saturday works if we finish", "Any day works", en.ApproveURL, "approve?order=", "fake-secret", "Bearer"}
	for _, secret := range secrets {
		if strings.Contains(logs, secret) {
			t.Errorf("log contains %q", secret)
		}
	}
}

func TestParticipant_ReservedSeatInAConfirmedWorkshopUntilClose(t *testing.T) {
	e := newEnv(t)
	h, p, ps := e.fundingWorkshop(func(b map[string]any) { b["minimum"] = 1 })
	wid := h.created.Workshop.ID
	luis := e.enroll(ps[1], p.ID, http.StatusCreated)
	ana := e.enroll(ps[0], p.ID, http.StatusCreated)
	marta := e.join(h, "Marta")
	e.runPayment("capture", wid, e.authorize(wid, ana.EnrollmentID))
	e.wantStatus(ana.EnrollmentID, domain.EnrollmentPaid)

	label := func() (string, bool) {
		t.Helper()
		var v participantViewJSON
		e.call(http.MethodGet, ps[1].path, nil, http.StatusOK, &v)
		if v.Workshop.Status != "CONFIRMED" || v.Enrollment == nil || v.Enrollment.ID != luis.EnrollmentID || v.Proposal == nil {
			t.Fatalf("view = workshop %+v, enrollment %+v", v.Workshop, v.Enrollment)
		}
		return v.Enrollment.StatusLabel, v.Proposal.PaymentsOpen
	}
	if got, open := label(); got != "Seat reserved" || !open {
		t.Errorf("confirmed before close: label %q, payments_open %v; want Seat reserved and open", got, open)
	}
	// The confirmed workshop still charges a hold authorized before close.
	e.authorize(wid, luis.EnrollmentID)
	e.runPayment("capture", wid, e.latestAttempt(luis.EnrollmentID))
	e.wantStatus(luis.EnrollmentID, domain.EnrollmentPaid)

	// A confirmed workshop keeps selling seats until payments close.
	e.enroll(marta, p.ID, http.StatusCreated)
}

func TestParticipant_ReservedSeatExpiresWithTheSeatOrTheWindow(t *testing.T) {
	e := newEnv(t)
	h, p, ps := e.fundingWorkshop(func(b map[string]any) { b["minimum"] = 1 })
	wid := h.created.Workshop.ID
	luis := e.enroll(ps[1], p.ID, http.StatusCreated)
	ana := e.enroll(ps[0], p.ID, http.StatusCreated)
	e.runPayment("capture", wid, e.authorize(wid, ana.EnrollmentID))

	view := func() participantViewJSON {
		t.Helper()
		var v participantViewJSON
		e.call(http.MethodGet, ps[1].path, nil, http.StatusOK, &v)
		return v
	}
	e.clock.Set(t0.Add(31 * time.Minute))
	if v := view(); v.Enrollment == nil || v.Enrollment.ID != luis.EnrollmentID || v.Enrollment.StatusLabel != "Seat expired" || !v.Proposal.PaymentsOpen {
		t.Errorf("seat past its expiry: %+v, proposal %+v; want Seat expired with payments still open", v.Enrollment, v.Proposal)
	}
	e.clock.Set(p.ClosesAt)
	if v := view(); v.Enrollment.StatusLabel != "Seat expired" || v.Proposal.PaymentsOpen {
		t.Errorf("after close: %+v, proposal %+v; want Seat expired, payments closed", v.Enrollment, v.Proposal)
	}
}

func TestEnrollment_RefusedOrderOpensANewAttempt(t *testing.T) {
	e := newEnv(t)
	_, p, ps := e.fundingWorkshop()

	e.fake.FailNext("create_order", http.StatusUnprocessableEntity, "INVALID_PARAMETER_VALUE")
	e.wantError(http.MethodPost, ps[0].path+"/enrollment", enrollBody(p.ID), http.StatusBadGateway, "paypal_unavailable")

	got := e.enroll(ps[0], p.ID, http.StatusOK)
	a := e.latestAttempt(got.EnrollmentID)
	if a.AttemptNo != 2 || a.OrderID == nil || !strings.HasSuffix(got.ApproveURL, *a.OrderID) {
		t.Errorf("checkout after the refusal = %+v, attempt %+v; want attempt 2's link", got, a)
	}
	orders := e.fake.Requests("create_order")
	if len(orders) != 2 || orders[0].RequestID == orders[1].RequestID || orders[1].RequestID != "create_order:"+a.ID {
		t.Errorf("create_order request ids = %v, want two different keys, the second create_order:%s", requestIDs(orders), a.ID)
	}
}

func requestIDs(reqs []paypal.RecordedRequest) []string {
	ids := make([]string, len(reqs))
	for i, r := range reqs {
		ids[i] = r.RequestID
	}
	return ids
}

func TestOwner_ShareURLCanBeRecovered(t *testing.T) {
	e := newEnv(t)
	h := e.createWorkshop()
	v := e.owner(h)
	if v.ShareURL == "" || v.ShareURL != h.created.ShareURL {
		t.Fatalf("owner view share_url = %q, want the creation's %q", v.ShareURL, h.created.ShareURL)
	}
	var public struct {
		Workshop workshopJSON `json:"workshop"`
	}
	e.call(http.MethodGet, "/api/v1/w/"+strings.TrimPrefix(v.ShareURL, appBaseURL+"/w/"), nil, http.StatusOK, &public)
	if public.Workshop.Title != "Go for operators" {
		t.Errorf("share_url resolves to %+v", public.Workshop)
	}
	ana := e.join(h, "Ana")
	for _, path := range []string{ana.path, h.sharePath} {
		if body := e.request(http.MethodGet, path, nil).Body.String(); strings.Contains(body, h.shareToken) || strings.Contains(body, "share_url") {
			t.Errorf("GET %s exposes the share link: %s", path, body)
		}
	}
}

func TestEnrollment_DoubleClickWaitsForTheRunningCheckout(t *testing.T) {
	gate := newGate(t)
	e := newEnv(t, withPayPal(gate.wrap))
	_, p, ps := e.fundingWorkshop()

	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() { firstDone <- e.request(http.MethodPost, ps[0].path+"/enrollment", enrollBody(p.ID)) }()
	<-gate.entered

	var first *httptest.ResponseRecorder
	waits := 0
	httpapi.SetCheckoutWait(t, 3*time.Second, func(context.Context, time.Duration) error {
		waits++
		gate.open()
		if first == nil {
			first = <-firstDone
		}
		return nil
	})
	var second enrollJSON
	e.call(http.MethodPost, ps[0].path+"/enrollment", enrollBody(p.ID), http.StatusOK, &second)

	var firstBody enrollJSON
	if first == nil || first.Code != http.StatusCreated || json.Unmarshal(first.Body.Bytes(), &firstBody) != nil {
		t.Fatalf("first click = %+v", first)
	}
	if waits == 0 || second.EnrollmentID != firstBody.EnrollmentID || second.ApproveURL == "" || second.ApproveURL != firstBody.ApproveURL {
		t.Errorf("second click after %d waits = %+v, want the first click's %+v", waits, second, firstBody)
	}
	if n := len(e.fake.Requests("create_order")); n != 1 {
		t.Errorf("create_order requests = %d, want 1", n)
	}
}

func TestEnrollment_DoubleClickWhileTheCheckoutStillRunsIs409(t *testing.T) {
	gate := newGate(t)
	e := newEnv(t, withPayPal(gate.wrap))
	_, p, ps := e.fundingWorkshop()

	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() { firstDone <- e.request(http.MethodPost, ps[0].path+"/enrollment", enrollBody(p.ID)) }()
	<-gate.entered

	waits := 0
	httpapi.SetCheckoutWait(t, 300*time.Millisecond, func(context.Context, time.Duration) error {
		waits++
		return nil
	})
	e.wantError(http.MethodPost, ps[0].path+"/enrollment", enrollBody(p.ID), http.StatusConflict, "checkout_in_progress")
	if waits == 0 {
		t.Errorf("the second click did not wait for the running checkout")
	}

	gate.open()
	first := <-firstDone
	var firstBody enrollJSON
	if first.Code != http.StatusCreated || json.Unmarshal(first.Body.Bytes(), &firstBody) != nil || firstBody.ApproveURL == "" {
		t.Fatalf("first click = %d %s", first.Code, first.Body.String())
	}
	third := e.enroll(ps[0], p.ID, http.StatusOK)
	if third.ApproveURL != firstBody.ApproveURL {
		t.Errorf("third click = %+v, want the first click's link", third)
	}
	if n := len(e.fake.Requests("create_order")); n != 1 {
		t.Errorf("create_order requests = %d, want 1", n)
	}
}

func TestDemoFailNextCapture_RefusedWhenTheCaptureStartsMeanwhile(t *testing.T) {
	e := newEnv(t, withSandboxMocks())
	h, p, ps := e.fundingWorkshop()
	en := e.enroll(ps[0], p.ID, http.StatusCreated)
	e.authorize(h.created.Workshop.ID, en.EnrollmentID)
	// A capture is moving the enrollment to CAPTURING and holds its row; the
	// attempt has no capture yet.
	tx, err := e.st.Pool.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(e.ctx, `update enrollments set status = 'CAPTURING' where id = $1`, en.EnrollmentID); err != nil {
		t.Fatal(err)
	}
	body := `{"enrollment_id":"` + en.EnrollmentID + `"}`
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- e.request(http.MethodPost, h.ownerPath+"/demo/fail-next-capture", body) }()
	deadline := time.Now().Add(5 * time.Second)
	for waiting := false; !waiting; {
		select {
		case rec := <-done:
			t.Fatalf("answered %d %s without waiting for the capture holding the enrollment", rec.Code, rec.Body.String())
		default:
		}
		if err := e.st.Pool.QueryRow(e.ctx,
			`select exists (select 1 from pg_locks where not granted and locktype in ('transactionid', 'tuple'))`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("the request never waited on the enrollment row")
		}
	}
	if err := tx.Commit(e.ctx); err != nil {
		t.Fatal(err)
	}

	rec := <-done
	var got errorJSON
	if rec.Code != http.StatusConflict || json.Unmarshal(rec.Body.Bytes(), &got) != nil || got.Error != "capture_already_started" {
		t.Fatalf("fail-next-capture = %d %s, want 409 capture_already_started", rec.Code, rec.Body.String())
	}
	if a := e.latestAttempt(en.EnrollmentID); a.MockCode != nil {
		t.Errorf("mock code = %q, want none once the capture started", *a.MockCode)
	}
}

func TestDemoFailNextCapture_RefusedOnceTheAttemptHoldsACapture(t *testing.T) {
	e := newEnv(t, withSandboxMocks())
	h, p, ps := e.fundingWorkshop()
	en := e.enroll(ps[0], p.ID, http.StatusCreated)
	a := e.authorize(h.created.Workshop.ID, en.EnrollmentID)
	// The capture is recorded on the attempt while the enrollment has not
	// moved yet, as when the request lands in the middle of a capture.
	if _, err := e.st.UpdateAttemptFunc(e.ctx, a.ID, func(row *store.AttemptRow) error {
		id := "CAPTURE-IN-FLIGHT"
		row.CaptureID = &id
		return nil
	}); err != nil {
		t.Fatalf("UpdateAttemptFunc: %v", err)
	}
	e.wantError(http.MethodPost, h.ownerPath+"/demo/fail-next-capture", map[string]string{"enrollment_id": en.EnrollmentID}, http.StatusConflict, "capture_already_started")
	if got := e.latestAttempt(en.EnrollmentID); got.MockCode != nil {
		t.Errorf("mock code = %q, want none once a capture exists", *got.MockCode)
	}
}

func TestDemoCloseNow_RevivesAStuckCloseJob(t *testing.T) {
	for _, stuck := range []string{"failed", "needs_attention"} {
		t.Run(stuck, func(t *testing.T) {
			e := newEnv(t, withDemoMode())
			h, p, _ := e.fundingWorkshop()
			key := store.JobKey(store.KindClose, p.ID)
			if _, err := e.st.AdvanceJob(e.ctx, key, t0); err != nil {
				t.Fatal(err)
			}
			job, err := e.st.ClaimJobByKey(e.ctx, key, "w1", t0)
			if err != nil || job == nil {
				t.Fatalf("ClaimJobByKey = %+v, %v", job, err)
			}
			status := 500
			if err := e.st.FailJob(e.ctx, job.ID, "close failed", &status, nil, nil); err != nil {
				t.Fatal(err)
			}
			if stuck == "needs_attention" {
				if _, err := e.st.Pool.Exec(e.ctx, `update jobs set status = 'needs_attention' where id = $1`, job.ID); err != nil {
					t.Fatal(err)
				}
			}

			now := t0.Add(time.Hour)
			e.clock.Set(now)
			e.call(http.MethodPost, h.ownerPath+"/demo/close-now", nil, http.StatusAccepted, nil)
			got := e.job(key)
			if got.ID != job.ID || got.Status != "pending" || got.RunAt.After(now) {
				t.Errorf("close job after close-now = %s at %s, want the same job pending by %s", got.Status, got.RunAt, now)
			}
		})
	}
}

func TestEnrollment_DatabaseFailureIs500NotPayPal(t *testing.T) {
	e := newEnv(t)
	h, p, ps := e.fundingWorkshop()
	// Only this workshop's create_order inserts fail, as a database fault
	// right where the checkout enqueues its order.
	for _, stmt := range []string{
		`create or replace function test_refuse_create_order() returns trigger language plpgsql as $$
		 begin raise exception 'create_order insert refused by the test'; end $$`,
		`create trigger test_refuse_create_order before insert on jobs for each row
		 when (new.kind = 'create_order' and new.workshop_id = '` + h.created.Workshop.ID + `'::uuid)
		 execute function test_refuse_create_order()`,
	} {
		if _, err := e.st.Pool.Exec(e.ctx, stmt); err != nil {
			t.Fatalf("install the fault: %v", err)
		}
	}
	t.Cleanup(func() {
		_, _ = e.st.Pool.Exec(context.Background(), `drop trigger if exists test_refuse_create_order on jobs`)
		_, _ = e.st.Pool.Exec(context.Background(), `drop function if exists test_refuse_create_order()`)
	})

	e.wantError(http.MethodPost, ps[0].path+"/enrollment", enrollBody(p.ID), http.StatusInternalServerError, "internal_error")
	if n := len(e.fake.Requests("create_order")); n != 0 {
		t.Errorf("create_order requests = %d, want none", n)
	}
}

func TestEnrollment_LateJoinerInConfirmedWorkshopGetsApproveURL(t *testing.T) {
	e := newEnv(t)
	h, p, ps := e.fundingWorkshop(func(b map[string]any) { b["minimum"] = 1 })
	wid := h.created.Workshop.ID
	ana := e.enroll(ps[0], p.ID, http.StatusCreated)
	e.runPayment("capture", wid, e.authorize(wid, ana.EnrollmentID))
	if v := e.owner(h); v.Workshop.Status != "CONFIRMED" {
		t.Fatalf("workshop = %s, want CONFIRMED after the minimum paid", v.Workshop.Status)
	}

	marta := e.join(h, "Marta")
	e.say(marta, "Saturday works for me")
	late := e.enroll(marta, p.ID, http.StatusCreated)
	if late.ApproveURL == "" || late.Status != "SEAT_RESERVED" || late.StatusLabel != "Seat reserved" {
		t.Fatalf("late enrollment = %+v, want a reserved seat with an approval link", late)
	}

	a := e.authorize(wid, late.EnrollmentID)
	// The confirmed workshop's round enqueues this capture by itself.
	key := store.JobKey(store.KindCapture, a.ID)
	if j := e.job(key); j.Status != "pending" {
		t.Fatalf("capture job of the late joiner = %s, want pending", j.Status)
	}
	if err := e.runner.RunKey(e.ctx, key); err != nil {
		t.Fatalf("RunKey(%s): %v", key, err)
	}
	e.wantStatus(late.EnrollmentID, domain.EnrollmentPaid)
	e.wantStatus(ana.EnrollmentID, domain.EnrollmentPaid)
	captures := e.fake.Requests("capture")
	if len(captures) != 2 || captures[1].RequestID != key {
		t.Errorf("capture request ids = %v, want Ana's and then only %s", requestIDs(captures), key)
	}
	v := e.owner(h)
	if v.Workshop.Status != "CONFIRMED" || v.Tally.Paid != 2 {
		t.Errorf("workshop after the late payment = %s, paid %d; want CONFIRMED with 2 paid", v.Workshop.Status, v.Tally.Paid)
	}

	nora := e.join(h, "Nora")
	e.clock.Set(p.ClosesAt)
	e.wantError(http.MethodPost, nora.path+"/enrollment", enrollBody(p.ID), http.StatusConflict, "proposal_not_open")
	e.wantError(http.MethodPost, h.sharePath+"/participants", map[string]string{"display_name": "Omar"}, http.StatusConflict, "workshop_closed")
}
