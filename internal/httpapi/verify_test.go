package httpapi_test

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/httpapi"
	"github.com/aalvaropc/commitpool/internal/store"
)

type verifyCheckJSON struct {
	Resource string `json:"resource"`
	ID       string `json:"id"`
	Local    string `json:"local"`
	PayPal   string `json:"paypal"`
	Match    bool   `json:"match"`
	DebugID  string `json:"debug_id"`
}

type verifySeatJSON struct {
	EnrollmentID string            `json:"enrollment_id"`
	Seat         string            `json:"seat"`
	Checks       []verifyCheckJSON `json:"checks"`
}

type verifyJSON struct {
	CheckedAt time.Time `json:"checked_at"`
	Summary   struct {
		Records  int `json:"records"`
		Matching int `json:"matching"`
		Seats    int `json:"seats"`
		Skipped  int `json:"skipped"`
	} `json:"summary"`
	Seats []verifySeatJSON `json:"seats"`
}

func (e *env) verify(h workshopHandle) verifyJSON {
	e.t.Helper()
	var out verifyJSON
	e.call(http.MethodPost, h.ownerPath+"/paypal/verify", nil, http.StatusOK, &out)
	return out
}

func (e *env) paidSeat(h workshopHandle, p participantHandle, proposalID string) (enrollJSON, store.AttemptRow) {
	e.t.Helper()
	wid := h.created.Workshop.ID
	en, _ := e.checkout(p, proposalID)
	e.runPayment("capture", wid, e.authorize(wid, en.EnrollmentID))
	e.wantStatus(en.EnrollmentID, domain.EnrollmentPaid)
	return en, e.latestAttempt(en.EnrollmentID)
}

type paymentSnapshot struct {
	attempts []store.AttemptRow
	jobs     int
	posts    int
}

func (e *env) paymentSnapshot(enrollmentIDs ...string) paymentSnapshot {
	e.t.Helper()
	snap := paymentSnapshot{jobs: e.count(`select count(*) from jobs`)}
	for _, id := range enrollmentIDs {
		snap.attempts = append(snap.attempts, e.latestAttempt(id))
	}
	for _, op := range []string{"create_order", "authorize", "capture", "void", "refund"} {
		snap.posts += len(e.fake.Requests(op))
	}
	return snap
}

func (e *env) wantUnchanged(before paymentSnapshot, enrollmentIDs ...string) {
	e.t.Helper()
	after := e.paymentSnapshot(enrollmentIDs...)
	if after.jobs != before.jobs || after.posts != before.posts {
		e.t.Errorf("verify wrote jobs (%d -> %d) or sent PayPal actions (%d -> %d); it must only read",
			before.jobs, after.jobs, before.posts, after.posts)
	}
	for i := range before.attempts {
		b, a := before.attempts[i], after.attempts[i]
		if attemptFacts(b) != attemptFacts(a) {
			e.t.Errorf("verify changed attempt %s:\n%s\n->\n%s", b.ID, attemptFacts(b), attemptFacts(a))
		}
	}
}

func text(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func minor(n *int64) string {
	if n == nil {
		return "none"
	}
	return strconv.FormatInt(*n, 10)
}

func attemptFacts(a store.AttemptRow) string {
	return strings.Join([]string{
		text(a.OrderID), text(a.AuthorizationID), text(a.CaptureID), text(a.RefundID),
		text(a.OrderStatus), text(a.AuthorizationStatus), text(a.CaptureStatus), text(a.RefundStatus),
		minor(a.CaptureFeeMinor), minor(a.CaptureNetMinor),
	}, "|")
}

func seatNamed(t *testing.T, out verifyJSON, name string) verifySeatJSON {
	t.Helper()
	i := slices.IndexFunc(out.Seats, func(s verifySeatJSON) bool { return s.Seat == name })
	if i < 0 {
		t.Fatalf("seats = %+v, want one for %s", out.Seats, name)
	}
	return out.Seats[i]
}

func checkOf(t *testing.T, seat verifySeatJSON, resource string) verifyCheckJSON {
	t.Helper()
	i := slices.IndexFunc(seat.Checks, func(c verifyCheckJSON) bool { return c.Resource == resource })
	if i < 0 {
		t.Fatalf("%s's checks = %+v, want one for the %s", seat.Seat, seat.Checks, resource)
	}
	return seat.Checks[i]
}

func TestVerify_EveryRecordMatchesPayPal(t *testing.T) {
	e := newEnv(t)
	h, p, ps := e.fundingWorkshop()
	ana, paid := e.paidSeat(h, ps[0], p.ID)
	luis, _ := e.checkout(ps[1], p.ID)
	held := e.authorize(h.created.Workshop.ID, luis.EnrollmentID)
	before := e.paymentSnapshot(ana.EnrollmentID, luis.EnrollmentID)

	out := e.verify(h)

	if out.Summary.Records != 5 || out.Summary.Matching != 5 || out.Summary.Seats != 2 || out.Summary.Skipped != 0 {
		t.Fatalf("summary = %+v, want 5 of 5 records on 2 seats", out.Summary)
	}
	if !out.CheckedAt.Equal(e.clock.Now()) {
		t.Errorf("checked_at = %v, want %v", out.CheckedAt, e.clock.Now())
	}
	anaSeat := seatNamed(t, out, "Ana")
	if anaSeat.EnrollmentID != ana.EnrollmentID || len(anaSeat.Checks) != 3 {
		t.Errorf("Ana's seat = %+v, want order, authorization and capture", anaSeat)
	}
	for _, c := range anaSeat.Checks {
		if c.DebugID != "" {
			t.Errorf("check %+v carries a debug id though PayPal answered", c)
		}
	}
	capture := checkOf(t, anaSeat, "capture")
	wantCapture := "COMPLETED, 10.00 USD, fee 0.84 USD, net 9.16 USD"
	if capture.ID != *paid.CaptureID || capture.Local != wantCapture || capture.PayPal != wantCapture || !capture.Match {
		t.Errorf("Ana's capture check = %+v, want %q on both sides", capture, wantCapture)
	}
	if c := checkOf(t, anaSeat, "authorization"); c.Local != "CAPTURED, 10.00 USD" || !c.Match {
		t.Errorf("Ana's authorization check = %+v, want CAPTURED expected and found", c)
	}
	if c := checkOf(t, anaSeat, "order"); c.ID != *paid.OrderID || c.Local != "COMPLETED, 10.00 USD" || !c.Match {
		t.Errorf("Ana's order check = %+v", c)
	}
	luisSeat := seatNamed(t, out, "Luis")
	if c := checkOf(t, luisSeat, "authorization"); c.ID != *held.AuthorizationID || c.Local != "CREATED, 10.00 USD" || !c.Match {
		t.Errorf("Luis's authorization check = %+v, want CREATED on both sides", c)
	}
	e.wantUnchanged(before, ana.EnrollmentID, luis.EnrollmentID)
	if n := len(e.fake.Requests("get_capture")); n != 1 {
		t.Errorf("get_capture requests = %d, want one for the one capture", n)
	}
}

func TestVerify_ShowsFeesOnTheOwnerViewOnly(t *testing.T) {
	e := newEnv(t)
	h, p, ps := e.fundingWorkshop()
	e.paidSeat(h, ps[0], p.ID)

	var raw struct {
		Enrollments []struct {
			Payment struct {
				Fee *moneyJSON `json:"fee"`
				Net *moneyJSON `json:"net"`
			} `json:"payment"`
		} `json:"enrollments"`
	}
	e.call(http.MethodGet, h.ownerPath, nil, http.StatusOK, &raw)
	if len(raw.Enrollments) != 1 {
		t.Fatalf("enrollments = %+v, want Ana's", raw.Enrollments)
	}
	pay := raw.Enrollments[0].Payment
	if pay.Fee == nil || *pay.Fee != (moneyJSON{Minor: 84, Currency: "USD", Value: "0.84"}) ||
		pay.Net == nil || *pay.Net != (moneyJSON{Minor: 916, Currency: "USD", Value: "9.16"}) {
		t.Errorf("owner payment fee %+v, net %+v; want 0.84 and 9.16", pay.Fee, pay.Net)
	}

	rec := e.request(http.MethodGet, ps[0].path, nil)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"fee"`) || strings.Contains(rec.Body.String(), `"net"`) {
		t.Errorf("participant view = %d %s, want no fee or net: they are the organizer's", rec.Code, rec.Body.String())
	}
}

func TestVerify_ReportsWhatPayPalSaysDifferentlyAndChangesNothing(t *testing.T) {
	e := newEnv(t)
	h, p, ps := e.fundingWorkshop()
	ana, paid := e.paidSeat(h, ps[0], p.ID)
	e.fake.SetCaptureStatus(*paid.CaptureID, "REFUNDED", "")
	before := e.paymentSnapshot(ana.EnrollmentID)

	out := e.verify(h)

	if out.Summary.Records != 3 || out.Summary.Matching != 2 {
		t.Fatalf("summary = %+v, want 2 of 3 records matching", out.Summary)
	}
	c := checkOf(t, seatNamed(t, out, "Ana"), "capture")
	if c.Match || c.Local != "COMPLETED, 10.00 USD, fee 0.84 USD, net 9.16 USD" || c.PayPal != "REFUNDED, 10.00 USD, fee 0.84 USD, net 9.16 USD" {
		t.Errorf("capture check = %+v, want COMPLETED here and REFUNDED at PayPal", c)
	}
	e.wantUnchanged(before, ana.EnrollmentID)
	e.wantStatus(ana.EnrollmentID, domain.EnrollmentPaid)
}

func TestVerify_ReportsPayPalsDebugIDWhenPayPalFails(t *testing.T) {
	e := newEnv(t)
	h, p, ps := e.fundingWorkshop()
	ana, _ := e.checkout(ps[0], p.ID)
	e.authorize(h.created.Workshop.ID, ana.EnrollmentID)
	e.fake.FailNext("get_authorization", http.StatusInternalServerError, "")
	before := e.paymentSnapshot(ana.EnrollmentID)

	out := e.verify(h)

	seat := seatNamed(t, out, "Ana")
	c := checkOf(t, seat, "authorization")
	if c.Match || c.PayPal != "error INTERNAL_SERVER_ERROR" || c.DebugID == "" {
		t.Errorf("authorization check = %+v, want PayPal's error with its debug id", c)
	}
	if o := checkOf(t, seat, "order"); !o.Match || o.DebugID != "" {
		t.Errorf("order check = %+v, want it still compared and without the authorization's debug id", o)
	}
	if out.Summary.Records != 2 || out.Summary.Matching != 1 {
		t.Errorf("summary = %+v, want 1 of 2", out.Summary)
	}
	e.wantUnchanged(before, ana.EnrollmentID)
}

func TestVerify_APaidSeatWithoutRecordedFeesDoesNotMatch(t *testing.T) {
	e := newEnv(t)
	h, p, ps := e.fundingWorkshop()
	ana, paid := e.paidSeat(h, ps[0], p.ID)
	if _, err := e.st.Pool.Exec(e.ctx, `update payment_attempts set capture_fee_minor = null, capture_net_minor = null where id = $1`, paid.ID); err != nil {
		t.Fatal(err)
	}
	before := e.paymentSnapshot(ana.EnrollmentID)

	out := e.verify(h)

	c := checkOf(t, seatNamed(t, out, "Ana"), "capture")
	if c.Match || c.Local != "COMPLETED, 10.00 USD, fee not recorded, net not recorded" || c.PayPal != "COMPLETED, 10.00 USD, fee 0.84 USD, net 9.16 USD" {
		t.Errorf("capture check = %+v, want the missing fees reported as a difference", c)
	}
	e.wantUnchanged(before, ana.EnrollmentID)
}

func TestVerify_ExpectsTheHoldCapturedOnlyWhenTheChargeWentThrough(t *testing.T) {
	e := newEnv(t)
	h, p, ps := e.fundingWorkshop()
	ana, _ := e.checkout(ps[0], p.ID)
	held := e.authorize(h.created.Workshop.ID, ana.EnrollmentID)
	if _, err := e.st.UpdateAttemptFunc(e.ctx, held.ID, func(a *store.AttemptRow) error {
		a.CaptureID, a.CaptureStatus = ptr("DECLINEDCAPTURE"), ptr("DECLINED")
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	out := e.verify(h)

	if c := checkOf(t, seatNamed(t, out, "Ana"), "authorization"); c.Local != "CREATED, 10.00 USD" || !c.Match {
		t.Errorf("authorization check = %+v, want the hold expected as recorded after a declined charge", c)
	}
}

func ptr[T any](v T) *T { return &v }

func TestVerify_ComparesTheAmountPayPalReports(t *testing.T) {
	e := newEnv(t)
	h, p, ps := e.fundingWorkshop()
	_, a := e.checkout(ps[0], p.ID)
	e.fake.SetOrderAmount(*a.OrderID, "12.00", "USD")

	out := e.verify(h)

	c := checkOf(t, seatNamed(t, out, "Ana"), "order")
	if c.Match || c.Local != "PAYER_ACTION_REQUIRED, 10.00 USD" || c.PayPal != "PAYER_ACTION_REQUIRED, 12.00 USD" {
		t.Errorf("order check = %+v, want the amounts compared", c)
	}
}

func TestVerify_OncePerWorkshopEveryThirtySeconds(t *testing.T) {
	e := newEnv(t)
	h, p, ps := e.fundingWorkshop()
	e.checkout(ps[0], p.ID)
	other := e.createWorkshop()

	e.verify(h)
	rec := e.request(http.MethodPost, h.ownerPath+"/paypal/verify", nil)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "30" {
		t.Fatalf("second verify = %d (Retry-After %q) %s, want 429 asking to wait 30 seconds", rec.Code, rec.Header().Get("Retry-After"), rec.Body.String())
	}
	got := e.wantError(http.MethodPost, h.ownerPath+"/paypal/verify", nil, http.StatusTooManyRequests, "verify_too_soon")
	if !strings.Contains(got.Message, "Try again in 30 seconds") {
		t.Errorf("message = %q, want it to say when to try again", got.Message)
	}
	reads := len(e.fake.Requests("get_order"))

	e.verify(other)
	e.clock.Set(e.clock.Now().Add(29 * time.Second))
	if rec := e.request(http.MethodPost, h.ownerPath+"/paypal/verify", nil); rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "1" {
		t.Errorf("verify after 29s = %d (Retry-After %q), want 429 for one more second", rec.Code, rec.Header().Get("Retry-After"))
	}
	if n := len(e.fake.Requests("get_order")); n != reads {
		t.Errorf("get_order requests = %d after refused verifies, want %d: a refused verify reads nothing", n, reads)
	}
	e.clock.Set(e.clock.Now().Add(time.Second))
	e.verify(h)
}

func TestVerify_AtMostTwelveSeatsACall(t *testing.T) {
	httpapi.SetVerifySeats(t, 1)
	e := newEnv(t)
	h, p, ps := e.fundingWorkshop()
	e.checkout(ps[0], p.ID)
	e.checkout(ps[1], p.ID)

	out := e.verify(h)

	if out.Summary.Seats != 1 || out.Summary.Skipped != 1 || len(out.Seats) != 1 || out.Seats[0].Seat != "Ana" {
		t.Errorf("verify = %+v, want Ana checked and one seat skipped", out)
	}
}

func TestVerify_OnlyTheOwnerAndOnlyTheirSeats(t *testing.T) {
	e := newEnv(t)
	h, p, ps := e.fundingWorkshop()
	e.checkout(ps[0], p.ID)
	other := e.createWorkshop()

	e.wantError(http.MethodPost, "/api/v1/owner/"+h.shareToken+"/paypal/verify", nil, http.StatusNotFound, "not_found")
	e.wantError(http.MethodPost, "/api/v1/owner/"+ps[0].token+"/paypal/verify", nil, http.StatusNotFound, "not_found")
	e.wantError(http.MethodPost, "/api/v1/owner/not-a-token/paypal/verify", nil, http.StatusNotFound, "not_found")
	if rec := e.request(http.MethodGet, h.ownerPath+"/paypal/verify", nil); rec.Code == http.StatusOK {
		t.Errorf("GET verify = %d, want only POST", rec.Code)
	}

	out := e.verify(other)
	if len(out.Seats) != 0 || out.Summary.Records != 0 {
		t.Errorf("another organizer's verify = %+v, want none of this workshop's seats", out)
	}
	if n := len(e.fake.Requests("get_order")); n != 0 {
		t.Errorf("get_order requests = %d, want none for a workshop without payments", n)
	}
}
