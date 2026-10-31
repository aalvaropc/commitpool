package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/httpapi"
	"github.com/aalvaropc/commitpool/internal/paypal"
	"github.com/aalvaropc/commitpool/internal/store"
)

// returned is where the app sent the buyer back from PayPal: the web app's
// /paypal/returned page with the enrollment and the outcome.
type returned struct {
	enrollmentID string
	result       string
	location     string
}

// fromPayPal plays the browser arriving from PayPal at path, a /paypal/return
// or /paypal/cancel address on this app, and checks it is redirected to the
// returned page with nothing but the enrollment and the outcome.
func (e *env) fromPayPal(path string) returned {
	e.t.Helper()
	rec := e.request(http.MethodGet, path, nil)
	if rec.Code != http.StatusFound {
		e.t.Fatalf("GET %s = %d %s, want 302", path, rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	u, err := url.Parse(loc)
	if err != nil || u.Path != "/paypal/returned" || u.Host != "" || u.Scheme != "" {
		e.t.Fatalf("GET %s redirects to %q, want /paypal/returned on this app", path, loc)
	}
	q := u.Query()
	for k := range q {
		if k != "e" && k != "result" {
			e.t.Errorf("redirect %q carries %q; want only e and result", loc, k)
		}
	}
	return returned{enrollmentID: q.Get("e"), result: q.Get("result"), location: loc}
}

func (e *env) returnFor(enrollmentID, orderID string) returned {
	e.t.Helper()
	return e.fromPayPal("/paypal/return?" + url.Values{"e": {enrollmentID}, "token": {orderID}}.Encode())
}

// wantReturn plays PayPal's return for the enrollment's order and checks the
// outcome the returned page gets.
func (e *env) wantReturn(enrollmentID, orderID, want string) returned {
	e.t.Helper()
	got := e.returnFor(enrollmentID, orderID)
	if got.result != want || got.enrollmentID != enrollmentID {
		e.t.Fatalf("return of %s = %+v, want result %s for that enrollment", enrollmentID, got, want)
	}
	return got
}

// buyer plays the buyer on one of the fake PayPal's pages and returns the
// address on this app PayPal sends them back to.
func (e *env) buyer(method, page string, form url.Values) string {
	e.t.Helper()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	var (
		resp *http.Response
		err  error
	)
	if method == http.MethodPost {
		resp, err = client.PostForm(e.fake.URL+page, form)
	} else {
		resp, err = client.Get(e.fake.URL + page + "?" + form.Encode())
	}
	if err != nil {
		e.t.Fatalf("%s %s on the fake PayPal: %v", method, page, err)
	}
	defer resp.Body.Close()
	loc := resp.Header.Get("Location")
	path, ok := strings.CutPrefix(loc, appBaseURL)
	if resp.StatusCode != http.StatusFound || !ok {
		e.t.Fatalf("%s %s on the fake PayPal = %d to %q, want a redirect to %s", method, page, resp.StatusCode, loc, appBaseURL)
	}
	return path
}

// checkout enrolls the participant and returns the enrollment and its attempt.
func (e *env) checkout(p participantHandle, proposalID string) (enrollJSON, store.AttemptRow) {
	e.t.Helper()
	en := e.enroll(p, proposalID, http.StatusCreated)
	return en, e.latestAttempt(en.EnrollmentID)
}

func (e *env) runKey(key string) {
	e.t.Helper()
	if err := e.runner.RunKey(e.ctx, key); err != nil {
		e.t.Fatalf("RunKey(%s): %v", key, err)
	}
}

// releaseSeats runs the seat release the worker's tick enqueues this minute.
func (e *env) releaseSeats(h workshopHandle) {
	e.t.Helper()
	if err := e.runner.EnqueueSeatReleases(e.ctx); err != nil {
		e.t.Fatalf("EnqueueSeatReleases: %v", err)
	}
	e.runKey("release_seats:" + h.created.Workshop.ID + ":" + e.clock.Now().UTC().Format("2006-01-02T15:04"))
}

func (e *env) enrollment(id string) store.EnrollmentRow {
	e.t.Helper()
	en, err := e.st.EnrollmentByID(e.ctx, id)
	if err != nil {
		e.t.Fatalf("EnrollmentByID(%s): %v", id, err)
	}
	return en
}

func (e *env) count(query string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.st.Pool.QueryRow(e.ctx, query, args...).Scan(&n); err != nil {
		e.t.Fatalf("%s: %v", query, err)
	}
	return n
}

func (e *env) noJob(key string) {
	e.t.Helper()
	if j, err := e.st.JobByKey(e.ctx, key); err != nil || j != nil {
		e.t.Errorf("JobByKey(%s) = %+v, %v; want no job", key, j, err)
	}
}

func (e *env) wantRequests(op string, n int) []paypal.RecordedRequest {
	e.t.Helper()
	reqs := e.fake.Requests(op)
	if len(reqs) != n {
		e.t.Errorf("%s requests = %v, want %d", op, requestIDs(reqs), n)
	}
	return reqs
}

func TestReturn_AuthorizesAndRedirectsToReturnedPage(t *testing.T) {
	e := newEnv(t)
	_, p, ps := e.fundingWorkshop()
	en, a := e.checkout(ps[0], p.ID)

	back := e.buyer(http.MethodPost, "/approve", url.Values{"order": {*a.OrderID}})
	if !strings.HasPrefix(back, "/paypal/return?e="+en.EnrollmentID+"&") {
		t.Fatalf("PayPal sends the buyer back to %q, want this enrollment's return", back)
	}
	got := e.fromPayPal(back)
	if got.result != "authorized" || got.enrollmentID != en.EnrollmentID {
		t.Fatalf("return = %+v, want authorized for %s", got, en.EnrollmentID)
	}
	for _, secret := range []string{ps[0].token, *a.OrderID} {
		if strings.Contains(got.location, secret) {
			t.Errorf("redirect %q carries %q", got.location, secret)
		}
	}
	e.wantStatus(en.EnrollmentID, domain.EnrollmentAuthorized)
	if reqs := e.wantRequests("authorize", 1); len(reqs) == 1 && reqs[0].RequestID != "authorize:"+a.ID {
		t.Errorf("authorize PayPal-Request-Id = %q, want authorize:%s", reqs[0].RequestID, a.ID)
	}
	if j := e.job("authorize:" + a.ID); j.Status != store.JobSucceeded {
		t.Errorf("authorize job = %s, want succeeded", j.Status)
	}
	if held := e.latestAttempt(en.EnrollmentID); held.ID != a.ID || held.AuthorizationID == nil {
		t.Errorf("attempt after the return = %+v, want attempt %d holding an authorization", held, a.AttemptNo)
	}

	// The returned page is the web app's own route.
	if rec := e.request(http.MethodGet, got.location, nil); rec.Code != http.StatusOK || rec.Body.String() != indexHTML {
		t.Errorf("GET %s = %d %q, want the web app", got.location, rec.Code, rec.Body.String())
	}
}

func TestReturn_RejectsAmountMismatch(t *testing.T) {
	e := newEnv(t)
	_, p, ps := e.fundingWorkshop()
	en, a := e.checkout(ps[0], p.ID)
	e.fake.Approve(*a.OrderID)
	e.fake.SetOrderAmount(*a.OrderID, "9.00", "USD")

	e.wantReturn(en.EnrollmentID, *a.OrderID, "mismatch")
	e.wantRequests("authorize", 0)
	e.noJob("authorize:" + a.ID)
	if got := e.enrollment(en.EnrollmentID); got.Status != domain.EnrollmentSeatReserved || !got.SeatExpiresAt.Equal(t0.Add(30*time.Minute)) {
		t.Errorf("enrollment after a mismatch = %s until %s, want SEAT_RESERVED as it was", got.Status, got.SeatExpiresAt)
	}
}

// reshapedOrders rewrites what GET order reports, as a PayPal answering for
// a checkout other than the one the attempt opened.
type reshapedOrders struct {
	paypal.API
	mu   sync.Mutex
	edit func(*paypal.Order)
}

func (r *reshapedOrders) wrap(api paypal.API) paypal.API {
	r.API = api
	return r
}

func (r *reshapedOrders) set(edit func(*paypal.Order)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.edit = edit
}

func (r *reshapedOrders) GetOrder(ctx context.Context, id string) (paypal.Order, error) {
	o, err := r.API.GetOrder(ctx, id)
	r.mu.Lock()
	edit := r.edit
	r.mu.Unlock()
	if err == nil && edit != nil {
		edit(&o)
	}
	return o, err
}

func TestReturn_RejectsOrderThatDoesNotMatchTheCheckout(t *testing.T) {
	reshape := &reshapedOrders{}
	e := newEnv(t, withPayPal(reshape.wrap))
	_, p, ps := e.fundingWorkshop()
	en, a := e.checkout(ps[0], p.ID)
	e.fake.Approve(*a.OrderID)

	cases := map[string]func(*paypal.Order){
		"another enrollment": func(o *paypal.Order) { o.PurchaseUnits[0].CustomID = "00000000-0000-4000-8000-000000000001" },
		"another attempt":    func(o *paypal.Order) { o.PurchaseUnits[0].ReferenceID = "00000000-0000-4000-8000-000000000002" },
		"another currency":   func(o *paypal.Order) { o.PurchaseUnits[0].Amount.CurrencyCode = "EUR" },
		"another amount":     func(o *paypal.Order) { o.PurchaseUnits[0].Amount.Value = "10.01" },
		"another order":      func(o *paypal.Order) { o.ID = "ANOTHER-ORDER" },
		"no purchase unit":   func(o *paypal.Order) { o.PurchaseUnits = nil },
		"two purchase units": func(o *paypal.Order) { o.PurchaseUnits = append(o.PurchaseUnits, o.PurchaseUnits[0]) },
	}
	for name, edit := range cases {
		reshape.set(edit)
		if got := e.returnFor(en.EnrollmentID, *a.OrderID); got.result != "mismatch" {
			t.Errorf("%s: return = %+v, want mismatch", name, got)
		}
	}
	e.wantRequests("authorize", 0)
	e.wantStatus(en.EnrollmentID, domain.EnrollmentSeatReserved)

	reshape.set(nil)
	e.wantReturn(en.EnrollmentID, *a.OrderID, "authorized")
}

func TestReturn_RejectsForeignOrderToken(t *testing.T) {
	e := newEnv(t)
	_, p, ps := e.fundingWorkshop()
	ana, anaAttempt := e.checkout(ps[0], p.ID)
	luis, luisAttempt := e.checkout(ps[1], p.ID)
	e.fake.Approve(*anaAttempt.OrderID)
	e.fake.Approve(*luisAttempt.OrderID)

	for _, token := range []string{*luisAttempt.OrderID, "NOT-AN-ORDER", ""} {
		e.wantReturn(ana.EnrollmentID, token, "mismatch")
	}
	// An enrollment the app does not know is a mismatch too, and is not echoed.
	for _, id := range []string{"00000000-0000-4000-8000-000000000000", "not-an-enrollment", ""} {
		if got := e.returnFor(id, *anaAttempt.OrderID); got.result != "mismatch" || got.enrollmentID != "" {
			t.Errorf("return for enrollment %q = %+v, want mismatch without an enrollment", id, got)
		}
	}
	e.wantRequests("get_order", 0)
	e.wantRequests("authorize", 0)
	e.wantStatus(ana.EnrollmentID, domain.EnrollmentSeatReserved)
	e.wantStatus(luis.EnrollmentID, domain.EnrollmentSeatReserved)
}

func TestReturn_ExpiredSeatIsReReservedOrRefused(t *testing.T) {
	lapsed := t0.Add(31 * time.Minute)

	t.Run("released, with a free seat", func(t *testing.T) {
		e := newEnv(t)
		h, p, ps := e.fundingWorkshop()
		en, a := e.checkout(ps[0], p.ID)
		e.fake.Approve(*a.OrderID)
		e.clock.Set(lapsed)
		e.releaseSeats(h)
		e.wantStatus(en.EnrollmentID, domain.EnrollmentReleased)

		e.wantReturn(en.EnrollmentID, *a.OrderID, "authorized")
		e.wantStatus(en.EnrollmentID, domain.EnrollmentAuthorized)
		if reqs := e.wantRequests("authorize", 1); len(reqs) == 1 && reqs[0].RequestID != "authorize:"+a.ID {
			t.Errorf("authorize PayPal-Request-Id = %q, want authorize:%s", reqs[0].RequestID, a.ID)
		}
	})

	t.Run("lapsed, not released yet", func(t *testing.T) {
		e := newEnv(t)
		_, p, ps := e.fundingWorkshop()
		en, a := e.checkout(ps[0], p.ID)
		e.fake.Approve(*a.OrderID)
		e.clock.Set(lapsed)

		e.wantReturn(en.EnrollmentID, *a.OrderID, "authorized")
		e.wantStatus(en.EnrollmentID, domain.EnrollmentAuthorized)
		if got := e.enrollment(en.EnrollmentID); !got.SeatReservedAt.Equal(lapsed) {
			t.Errorf("seat reserved at %s, want it taken again at %s", got.SeatReservedAt, lapsed)
		}
	})

	t.Run("released, every seat taken", func(t *testing.T) {
		e := newEnv(t)
		h, p, ps := e.fundingWorkshop(func(b map[string]any) { b["capacity"] = 2 })
		marta := e.join(h, "Marta")
		en, a := e.checkout(ps[0], p.ID)
		e.fake.Approve(*a.OrderID)
		e.clock.Set(lapsed)
		e.releaseSeats(h)
		e.enroll(ps[1], p.ID, http.StatusCreated)
		e.enroll(marta, p.ID, http.StatusCreated)

		e.wantReturn(en.EnrollmentID, *a.OrderID, "seat_expired")
		e.wantStatus(en.EnrollmentID, domain.EnrollmentReleased)
		e.wantRequests("authorize", 0)
		e.noJob("authorize:" + a.ID)
	})

	t.Run("released in a confirmed workshop before close", func(t *testing.T) {
		e := newEnv(t)
		h, p, ps := e.fundingWorkshop(func(b map[string]any) { b["minimum"] = 1 })
		wid := h.created.Workshop.ID
		luis, a := e.checkout(ps[1], p.ID)
		ana := e.enroll(ps[0], p.ID, http.StatusCreated)
		e.runPayment("capture", wid, e.authorize(wid, ana.EnrollmentID))
		if v := e.owner(h); v.Workshop.Status != "CONFIRMED" {
			t.Fatalf("workshop = %s, want CONFIRMED", v.Workshop.Status)
		}
		e.fake.Approve(*a.OrderID)
		e.clock.Set(lapsed)
		e.releaseSeats(h)
		e.wantStatus(luis.EnrollmentID, domain.EnrollmentReleased)

		e.wantReturn(luis.EnrollmentID, *a.OrderID, "authorized")
		e.wantStatus(luis.EnrollmentID, domain.EnrollmentAuthorized)
		// The confirmed workshop charges the new hold by itself.
		if j := e.job("capture:" + a.ID); j.Status != store.JobPending {
			t.Errorf("capture job of the re-reserved seat = %s, want pending", j.Status)
		}
	})
}

func TestReturn_DoubleReturnIsIdempotent(t *testing.T) {
	e := newEnv(t)
	_, p, ps := e.fundingWorkshop()
	en, a := e.checkout(ps[0], p.ID)
	e.fake.Approve(*a.OrderID)

	e.wantReturn(en.EnrollmentID, *a.OrderID, "authorized")
	e.wantReturn(en.EnrollmentID, *a.OrderID, "authorized")

	e.wantRequests("authorize", 1)
	// The second return finds the hold recorded and asks PayPal nothing.
	e.wantRequests("get_order", 1)
	e.wantStatus(en.EnrollmentID, domain.EnrollmentAuthorized)
}

func TestReturn_CompletedOrderIsAuthorizedWithoutAskingAgain(t *testing.T) {
	lost := &lostAnswers{}
	e := newEnv(t, withPayPal(lost.wrap))
	h, p, ps := e.fundingWorkshop()
	en, a := e.checkout(ps[0], p.ID)
	e.fake.Approve(*a.OrderID)
	// The reconcile's authorization reached PayPal; its answer did not.
	lost.loseNext("authorize")
	e.runPayment("authorize", h.created.Workshop.ID, a)

	e.wantReturn(en.EnrollmentID, *a.OrderID, "authorized")
	e.wantRequests("authorize", 1)
	if j := e.job("authorize:" + a.ID); j.Status != store.JobUnknown {
		t.Errorf("authorize job = %s, want it left unknown for its reconcile", j.Status)
	}
	e.wantStatus(en.EnrollmentID, domain.EnrollmentSeatReserved)
}

func TestReturn_HoldLandedOnAReleasedSeatIsNotAuthorizedAgain(t *testing.T) {
	lost := &lostAnswers{}
	e := newEnv(t, withPayPal(lost.wrap))
	h, p, ps := e.fundingWorkshop()
	en, a := e.checkout(ps[0], p.ID)
	e.fake.Approve(*a.OrderID)
	// PayPal authorizes, the answer is lost, and the seat expires before the
	// reconcile records the hold, which nobody can use now and is voided.
	lost.loseNext("authorize")
	e.runPayment("authorize", h.created.Workshop.ID, a)
	if j := e.job("authorize:" + a.ID); j.Status != store.JobUnknown {
		t.Fatalf("authorize job after the lost answer = %s, want unknown", j.Status)
	}
	e.clock.Set(t0.Add(31 * time.Minute))
	e.releaseSeats(h)
	// Back before the hold is recorded: PayPal shows the order authorized.
	e.wantReturn(en.EnrollmentID, *a.OrderID, "seat_expired")

	e.runKey("reconcile:" + a.ID + ":1")
	if got := e.latestAttempt(en.EnrollmentID); got.AuthorizationID == nil {
		t.Fatalf("the reconcile did not record the hold")
	}
	e.wantStatus(en.EnrollmentID, domain.EnrollmentReleased)

	e.wantReturn(en.EnrollmentID, *a.OrderID, "seat_expired")
	e.wantStatus(en.EnrollmentID, domain.EnrollmentReleased)
	e.wantRequests("authorize", 1)
	if j := e.job("void:" + a.ID); j.Status != store.JobPending {
		t.Errorf("void of the unused hold = %s, want pending", j.Status)
	}
}

// orderReads runs a hook once, on the next GET order, before PayPal answers
// it: what another request does while the return reads the order.
type orderReads struct {
	paypal.API
	mu   sync.Mutex
	next func()
}

func (o *orderReads) wrap(api paypal.API) paypal.API {
	o.API = api
	return o
}

func (o *orderReads) before(f func()) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.next = f
}

func (o *orderReads) GetOrder(ctx context.Context, id string) (paypal.Order, error) {
	o.mu.Lock()
	f := o.next
	o.next = nil
	o.mu.Unlock()
	if f != nil {
		f()
	}
	return o.API.GetOrder(ctx, id)
}

// approvedEvent is PayPal's CHECKOUT.ORDER.APPROVED for the order.
func approvedEvent(id, orderID string) map[string]any {
	return event(id, "CHECKOUT.ORDER.APPROVED", "checkout-order", map[string]any{"id": orderID, "status": "APPROVED", "intent": "AUTHORIZE"})
}

func TestReturn_SeatReReservedByConcurrentReconcileIsNotReportedExpired(t *testing.T) {
	reads := &orderReads{}
	e := newEnv(t, withWebhookID(), withPayPal(reads.wrap))
	h, p, ps := e.fundingWorkshop()
	en, a := e.checkout(ps[0], p.ID)
	e.fake.Approve(*a.OrderID)
	e.clock.Set(t0.Add(31 * time.Minute))
	e.releaseSeats(h)
	e.wantStatus(en.EnrollmentID, domain.EnrollmentReleased)

	// PayPal's approval webhook comes with the buyer's redirect, and its
	// reconcile takes the seat again while the return reads the order.
	e.deliver(approvedEvent("WH-APPROVED-RACE-1", *a.OrderID))
	reads.before(func() { e.runKey("reconcile:" + a.ID + ":webhook:WH-APPROVED-RACE-1") })

	e.wantReturn(en.EnrollmentID, *a.OrderID, "authorized")
	e.wantStatus(en.EnrollmentID, domain.EnrollmentAuthorized)
	if reqs := e.wantRequests("authorize", 1); len(reqs) == 1 && reqs[0].RequestID != "authorize:"+a.ID {
		t.Errorf("authorize PayPal-Request-Id = %q, want authorize:%s", reqs[0].RequestID, a.ID)
	}
}

func TestReturn_PaymentsClosingWhileTheSeatIsRenewedIsClosed(t *testing.T) {
	reads := &orderReads{}
	e := newEnv(t, withPayPal(reads.wrap))
	h, p, ps := e.fundingWorkshop()
	en, a := e.checkout(ps[0], p.ID)
	e.fake.Approve(*a.OrderID)
	lapsed := t0.Add(31 * time.Minute)
	e.clock.Set(lapsed)
	e.releaseSeats(h)
	// The owner closes payments while the return reads the order.
	reads.before(func() {
		if _, err := e.st.ShortenPaymentWindow(e.ctx, p.ID, lapsed); err != nil {
			t.Fatal(err)
		}
	})

	e.wantReturn(en.EnrollmentID, *a.OrderID, "closed")
	e.wantStatus(en.EnrollmentID, domain.EnrollmentReleased)
	e.wantRequests("authorize", 0)
}

func TestReturn_CompletedOrderAfterPaymentsCloseIsClosed(t *testing.T) {
	lost := &lostAnswers{}
	e := newEnv(t, withPayPal(lost.wrap))
	h, p, ps := e.fundingWorkshop()
	en, a := e.checkout(ps[0], p.ID)
	e.fake.Approve(*a.OrderID)
	lost.loseNext("authorize")
	e.runPayment("authorize", h.created.Workshop.ID, a)
	e.clock.Set(p.ClosesAt)

	e.wantReturn(en.EnrollmentID, *a.OrderID, "closed")
	e.wantRequests("authorize", 1)

	// The reconcile records the hold and, payments being closed, voids it.
	e.runKey("reconcile:" + a.ID + ":1")
	if j := e.job("void:" + a.ID); j.Status != store.JobPending {
		t.Errorf("void of the hold recorded after close = %s, want pending", j.Status)
	}
	e.wantReturn(en.EnrollmentID, *a.OrderID, "closed")
}

func TestReturn_OrderNotApprovedIsPending(t *testing.T) {
	e := newEnv(t)
	_, p, ps := e.fundingWorkshop()
	en, a := e.checkout(ps[0], p.ID)

	e.wantReturn(en.EnrollmentID, *a.OrderID, "pending")
	e.wantRequests("authorize", 0)
	e.noJob("authorize:" + a.ID)
	if got := e.enrollment(en.EnrollmentID); got.Status != domain.EnrollmentSeatReserved || !got.SeatExpiresAt.Equal(t0.Add(30*time.Minute)) {
		t.Errorf("enrollment = %s until %s, want SEAT_RESERVED as it was", got.Status, got.SeatExpiresAt)
	}
}

func TestReturn_AfterPaymentsCloseIsClosed(t *testing.T) {
	e := newEnv(t)
	_, p, ps := e.fundingWorkshop()
	en, a := e.checkout(ps[0], p.ID)
	e.fake.Approve(*a.OrderID)
	e.clock.Set(p.ClosesAt)

	e.wantReturn(en.EnrollmentID, *a.OrderID, "closed")
	e.wantRequests("authorize", 0)
	e.noJob("authorize:" + a.ID)
	e.wantStatus(en.EnrollmentID, domain.EnrollmentSeatReserved)
}

func TestReturn_ExpiredOrderIsExpired(t *testing.T) {
	e := newEnv(t)
	_, p, ps := e.fundingWorkshop()
	en, a := e.checkout(ps[0], p.ID)
	e.fake.ExpireOrder(*a.OrderID)

	e.wantReturn(en.EnrollmentID, *a.OrderID, "expired")
	e.wantRequests("authorize", 0)
	if got := e.latestAttempt(en.EnrollmentID); got.OrderStatus == nil || *got.OrderStatus != paypal.StatusExpired {
		t.Errorf("order status = %v, want EXPIRED", got.OrderStatus)
	}
	e.wantStatus(en.EnrollmentID, domain.EnrollmentSeatReserved)

	// The participant's next click opens a new order.
	again := e.enroll(ps[0], p.ID, http.StatusOK)
	if b := e.latestAttempt(en.EnrollmentID); b.AttemptNo != 2 || again.ApproveURL == en.ApproveURL || again.ApproveURL == "" {
		t.Errorf("checkout after the order expired = %+v (attempt %d), want attempt 2 with a new link", again, b.AttemptNo)
	}
}

func TestReturn_DeclinedAuthorizationIsDeclined(t *testing.T) {
	e := newEnv(t)
	_, p, ps := e.fundingWorkshop()
	en, a := e.checkout(ps[0], p.ID)
	e.fake.Approve(*a.OrderID)
	e.fake.FailNext("authorize", http.StatusUnprocessableEntity, "INSTRUMENT_DECLINED")

	got := e.wantReturn(en.EnrollmentID, *a.OrderID, "declined")
	if strings.Contains(got.location, "INSTRUMENT_DECLINED") {
		t.Errorf("redirect %q carries the PayPal issue", got.location)
	}
	if !strings.Contains(e.logs.String(), "INSTRUMENT_DECLINED") {
		t.Errorf("the log does not name PayPal's issue")
	}
	e.wantStatus(en.EnrollmentID, domain.EnrollmentSeatReserved)
	j := e.job("authorize:" + a.ID)
	if j.Status != store.JobFailed || j.ProviderIssue == nil || *j.ProviderIssue != "INSTRUMENT_DECLINED" {
		t.Errorf("authorize job = %s %v, want failed with INSTRUMENT_DECLINED", j.Status, j.ProviderIssue)
	}

	// Coming back again does not send the refused authorization again.
	e.wantReturn(en.EnrollmentID, *a.OrderID, "declined")
	e.wantRequests("authorize", 1)
}

func TestReturn_RevivesAuthorizeRefusedBeforeApproval(t *testing.T) {
	e := newEnv(t)
	h, p, ps := e.fundingWorkshop()
	en, a := e.checkout(ps[0], p.ID)
	key := "authorize:" + a.ID
	// Asked before the buyer approved, PayPal refused it.
	e.runPayment("authorize", h.created.Workshop.ID, a)
	refused := e.job(key)
	if refused.Status != store.JobFailed || refused.ProviderIssue == nil || *refused.ProviderIssue != "ORDER_NOT_APPROVED" {
		t.Fatalf("authorize job before approval = %s %v, want failed ORDER_NOT_APPROVED", refused.Status, refused.ProviderIssue)
	}

	e.fake.Approve(*a.OrderID)
	e.wantReturn(en.EnrollmentID, *a.OrderID, "authorized")
	e.wantStatus(en.EnrollmentID, domain.EnrollmentAuthorized)
	for _, r := range e.wantRequests("authorize", 2) {
		if r.RequestID != key {
			t.Errorf("authorize PayPal-Request-Id = %q, want %s for both", r.RequestID, key)
		}
	}
	if j := e.job(key); j.ID != refused.ID || j.Status != store.JobSucceeded {
		t.Errorf("authorize job = %s (%s), want the refused job %s succeeded", j.ID, j.Status, refused.ID)
	}
}

func TestReturn_PayPalOutageIsPendingAndReconciled(t *testing.T) {
	e := newEnv(t)
	_, p, ps := e.fundingWorkshop()
	en, a := e.checkout(ps[0], p.ID)
	e.fake.Approve(*a.OrderID)
	e.fake.FailNext("get_order", http.StatusServiceUnavailable, "")

	e.wantReturn(en.EnrollmentID, *a.OrderID, "pending")
	e.wantRequests("authorize", 0)
	e.wantStatus(en.EnrollmentID, domain.EnrollmentSeatReserved)
	key := "reconcile:" + a.ID + ":return:" + t0.Format("2006-01-02T15:04")
	if j := e.job(key); j.Status != store.JobPending || !j.RunAt.Equal(t0.Add(30*time.Second)) {
		t.Fatalf("reconcile after the outage = %s at %s, want pending at %s", j.Status, j.RunAt, t0.Add(30*time.Second))
	}

	// The reconcile reads the approved order back and authorizes it.
	e.clock.Set(t0.Add(30 * time.Second))
	e.runKey(key)
	e.runKey("authorize:" + a.ID)
	e.wantStatus(en.EnrollmentID, domain.EnrollmentAuthorized)
}

func TestCancel_MarksApprovalCancelledAndKeepsSeatUntilExpiry(t *testing.T) {
	e := newEnv(t)
	h, p, ps := e.fundingWorkshop()
	en, a := e.checkout(ps[0], p.ID)
	cancelledAt := t0.Add(2 * time.Minute)
	e.clock.Set(cancelledAt)

	// A cancel without PayPal's token cannot be told apart from a forged one.
	if got := e.fromPayPal("/paypal/cancel?e=" + en.EnrollmentID); got.result != "mismatch" || got.enrollmentID != en.EnrollmentID {
		t.Errorf("cancel without a token = %+v, want mismatch", got)
	}
	if row := e.enrollment(en.EnrollmentID); row.ApprovalCancelledAt != nil {
		t.Fatalf("a cancel without a token marked the enrollment at %v", row.ApprovalCancelledAt)
	}

	back := e.buyer(http.MethodGet, "/cancel", url.Values{"order": {*a.OrderID}})
	if !strings.HasPrefix(back, "/paypal/cancel?e="+en.EnrollmentID+"&") {
		t.Fatalf("PayPal sends the cancelling buyer to %q, want this enrollment's cancel", back)
	}
	got := e.fromPayPal(back)
	if got.result != "cancelled" || got.enrollmentID != en.EnrollmentID {
		t.Fatalf("cancel = %+v, want cancelled for %s", got, en.EnrollmentID)
	}
	row := e.enrollment(en.EnrollmentID)
	if row.Status != domain.EnrollmentSeatReserved || row.ApprovalCancelledAt == nil || !row.ApprovalCancelledAt.Equal(cancelledAt) ||
		!row.SeatExpiresAt.Equal(t0.Add(30*time.Minute)) {
		t.Errorf("enrollment after the cancel = %s, cancelled at %v, seat until %s; want SEAT_RESERVED, %s, %s",
			row.Status, row.ApprovalCancelledAt, row.SeatExpiresAt, cancelledAt, t0.Add(30*time.Minute))
	}
	var v participantViewJSON
	e.call(http.MethodGet, ps[0].path, nil, http.StatusOK, &v)
	if v.Enrollment == nil || v.Enrollment.ApprovalCancelledAt == nil || !v.Enrollment.ApprovalCancelledAt.Equal(cancelledAt) ||
		v.Enrollment.StatusLabel != "Seat reserved" {
		t.Errorf("participant view after the cancel = %+v, want the cancel shown on a reserved seat", v.Enrollment)
	}

	// The buyer can still pay with the same checkout.
	if again := e.enroll(ps[0], p.ID, http.StatusOK); again.ApproveURL != en.ApproveURL {
		t.Errorf("checkout after the cancel = %+v, want the same link %q", again, en.ApproveURL)
	}
	// A cancel naming another checkout's order marks nothing.
	luis, _ := e.checkout(ps[1], p.ID)
	if got := e.fromPayPal("/paypal/cancel?" + url.Values{"e": {luis.EnrollmentID}, "token": {*a.OrderID}}.Encode()); got.result != "mismatch" {
		t.Errorf("cancel with another order = %+v, want mismatch", got)
	}
	if row := e.enrollment(luis.EnrollmentID); row.ApprovalCancelledAt != nil {
		t.Errorf("a foreign cancel marked %s at %v", luis.EnrollmentID, row.ApprovalCancelledAt)
	}

	// The seat expires on its own.
	e.clock.Set(t0.Add(31 * time.Minute))
	e.releaseSeats(h)
	e.wantStatus(en.EnrollmentID, domain.EnrollmentReleased)
	e.wantRequests("authorize", 0)
}

func TestCancel_AfterTheHoldIsAuthorizedKeepsIt(t *testing.T) {
	e := newEnv(t)
	_, p, ps := e.fundingWorkshop()
	en, a := e.checkout(ps[0], p.ID)
	e.fake.Approve(*a.OrderID)
	e.wantReturn(en.EnrollmentID, *a.OrderID, "authorized")

	// A tab left on PayPal's page cancels after the hold was placed.
	got := e.fromPayPal("/paypal/cancel?" + url.Values{"e": {en.EnrollmentID}, "token": {*a.OrderID}}.Encode())
	if got.result != "authorized" || got.enrollmentID != en.EnrollmentID {
		t.Errorf("cancel after the authorization = %+v, want authorized", got)
	}
	if row := e.enrollment(en.EnrollmentID); row.Status != domain.EnrollmentAuthorized || row.ApprovalCancelledAt != nil {
		t.Errorf("enrollment = %s, cancelled at %v; want AUTHORIZED and no cancel", row.Status, row.ApprovalCancelledAt)
	}
	e.wantRequests("void", 0)
}

func TestEnrollment_LapsedSeatIsReReservedOrRefused(t *testing.T) {
	lapsed := t0.Add(31 * time.Minute)

	t.Run("a seat is free", func(t *testing.T) {
		e := newEnv(t)
		_, p, ps := e.fundingWorkshop()
		first := e.enroll(ps[0], p.ID, http.StatusCreated)
		e.clock.Set(lapsed)

		again := e.enroll(ps[0], p.ID, http.StatusOK)
		if again.EnrollmentID != first.EnrollmentID || again.ApproveURL != first.ApproveURL ||
			again.Status != "SEAT_RESERVED" || again.StatusLabel != "Seat reserved" {
			t.Errorf("repeated POST on a lapsed seat = %+v, want %s live again with its link", again, first.EnrollmentID)
		}
		if row := e.enrollment(first.EnrollmentID); row.Status != domain.EnrollmentSeatReserved ||
			!row.SeatReservedAt.Equal(lapsed) || !row.SeatExpiresAt.Equal(lapsed.Add(30*time.Minute)) {
			t.Errorf("seat = %s from %s until %s, want SEAT_RESERVED from %s for 30 minutes", row.Status, row.SeatReservedAt, row.SeatExpiresAt, lapsed)
		}
		e.wantRequests("create_order", 1)
	})

	t.Run("the seat is taken while it is renewed", func(t *testing.T) {
		e := newEnv(t)
		h, p, ps := e.fundingWorkshop(func(b map[string]any) { b["capacity"] = 2 })
		first := e.enroll(ps[0], p.ID, http.StatusCreated)
		e.clock.Set(t0.Add(10 * time.Minute))
		e.enroll(ps[1], p.ID, http.StatusCreated)
		e.clock.Set(lapsed)
		// Another request takes the last seat between this one's release of
		// the lapsed seat and its re-reservation; it is written straight to
		// the table, past the capacity check it would race with.
		marta := e.join(h, "Marta")
		mp, err := e.st.ParticipantByToken(e.ctx, marta.token)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.st.Pool.Exec(e.ctx,
			`insert into enrollments (workshop_id, participant_id, proposal_id, status, consent_at, consent_version, seat_reserved_at, seat_expires_at)
			 values ($1, $2, $3, 'SEAT_RESERVED', $4, 'consent-v1', $4, $5)`,
			h.created.Workshop.ID, mp.ID, p.ID, lapsed, lapsed.Add(30*time.Minute)); err != nil {
			t.Fatal(err)
		}

		e.wantError(http.MethodPost, ps[0].path+"/enrollment", enrollBody(p.ID), http.StatusConflict, "seat_expired")
		e.wantStatus(first.EnrollmentID, domain.EnrollmentReleased)
		e.wantRequests("create_order", 2)
	})
}

func TestEnrollment_LapsedSeatRenewedByAConcurrentReconcileKeepsTheLink(t *testing.T) {
	e := newEnv(t, withWebhookID())
	_, p, ps := e.fundingWorkshop()
	first, a := e.checkout(ps[0], p.ID)
	e.fake.Approve(*a.OrderID)
	e.clock.Set(t0.Add(31 * time.Minute))
	e.deliver(approvedEvent("WH-APPROVED-RACE-2", *a.OrderID))
	// The approval webhook's reconcile renews the seat while this request
	// is about to.
	ran := false
	httpapi.SetBeforeSeatRenewal(t, func(context.Context, string) {
		if !ran {
			ran = true
			e.runKey("reconcile:" + a.ID + ":webhook:WH-APPROVED-RACE-2")
		}
	})

	again := e.enroll(ps[0], p.ID, http.StatusOK)
	if !ran || again.EnrollmentID != first.EnrollmentID || again.ApproveURL != first.ApproveURL || again.Status != "SEAT_RESERVED" {
		t.Errorf("repeated POST while the reconcile renewed the seat = %+v (hook ran %v), want the same link", again, ran)
	}
	if j := e.job("authorize:" + a.ID); j.Status != store.JobPending {
		t.Errorf("the reconcile's authorize = %s, want pending", j.Status)
	}
	e.wantRequests("create_order", 1)
}

func TestEnrollment_LapsedSeatWithAPendingAuthorizationIsKept(t *testing.T) {
	e := newEnv(t)
	h, p, ps := e.fundingWorkshop()
	first, a := e.checkout(ps[0], p.ID)
	e.fake.Approve(*a.OrderID)
	// PayPal holds the authorization for review.
	e.fake.SetNextAuthorizationStatus("PENDING")
	e.runPayment("authorize", h.created.Workshop.ID, a)
	e.wantStatus(first.EnrollmentID, domain.EnrollmentSeatReserved)
	e.clock.Set(t0.Add(31 * time.Minute))

	again := e.enroll(ps[0], p.ID, http.StatusOK)
	if again.EnrollmentID != first.EnrollmentID || again.Status != "SEAT_RESERVED" {
		t.Errorf("repeated POST while the authorization is pending = %+v, want %s still SEAT_RESERVED", again, first.EnrollmentID)
	}
	e.wantStatus(first.EnrollmentID, domain.EnrollmentSeatReserved)
	e.wantReturn(first.EnrollmentID, *a.OrderID, "pending")
	e.wantRequests("authorize", 1)
}

func TestEnrollment_LapsedSeatAfterPaymentsCloseIsProposalNotOpen(t *testing.T) {
	e := newEnv(t)
	_, p, ps := e.fundingWorkshop()
	first := e.enroll(ps[0], p.ID, http.StatusCreated)
	lapsed := t0.Add(31 * time.Minute)
	e.clock.Set(lapsed)
	// The owner closes payments while the seat is being renewed.
	httpapi.SetBeforeSeatRenewal(t, func(ctx context.Context, _ string) {
		if _, err := e.st.ShortenPaymentWindow(ctx, p.ID, lapsed); err != nil {
			t.Fatal(err)
		}
	})

	e.wantError(http.MethodPost, ps[0].path+"/enrollment", enrollBody(p.ID), http.StatusConflict, "proposal_not_open")
	e.wantStatus(first.EnrollmentID, domain.EnrollmentReleased)
}

// lostAnswers lets PayPal authorize or capture but loses the answer to the
// next request of an operation it is told to, as a timeout after PayPal
// processed the request does: the job is left unknown while PayPal holds the
// result.
type lostAnswers struct {
	paypal.API
	mu      sync.Mutex
	lose    map[string]bool
	capture paypal.Capture
}

func (l *lostAnswers) wrap(api paypal.API) paypal.API {
	l.API = api
	return l
}

// loseNext loses the answer to the next "authorize" or "capture".
func (l *lostAnswers) loseNext(op string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lose == nil {
		l.lose = map[string]bool{}
	}
	l.lose[op] = true
}

func (l *lostAnswers) take(op string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	lost := l.lose[op]
	delete(l.lose, op)
	return lost
}

// captured is the capture whose answer was lost.
func (l *lostAnswers) captured() paypal.Capture {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.capture
}

func (l *lostAnswers) AuthorizeOrder(ctx context.Context, orderID, requestID string) (paypal.Order, error) {
	o, err := l.API.AuthorizeOrder(ctx, orderID, requestID)
	if err != nil || !l.take("authorize") {
		return o, err
	}
	return paypal.Order{}, context.DeadlineExceeded
}

func (l *lostAnswers) CaptureAuthorization(ctx context.Context, p paypal.CaptureParams) (paypal.Capture, error) {
	c, err := l.API.CaptureAuthorization(ctx, p)
	if err != nil || !l.take("capture") {
		return c, err
	}
	l.mu.Lock()
	l.capture = c
	l.mu.Unlock()
	return paypal.Capture{}, context.DeadlineExceeded
}

// Webhook deliveries carry PayPal's transmission headers.
var webhookHeaders = map[string]string{
	"PAYPAL-TRANSMISSION-ID":   "b0a5c6e0-transmission",
	"PAYPAL-TRANSMISSION-TIME": "2026-11-02T09:00:05Z",
	"PAYPAL-TRANSMISSION-SIG":  "c2lnbmF0dXJl",
	"PAYPAL-CERT-URL":          "https://api-m.sandbox.paypal.com/v1/notifications/certs/CERT-test",
	"PAYPAL-AUTH-ALGO":         "SHA256withRSA",
}

func (e *env) webhook(body []byte) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequestWithContext(e.ctx, http.MethodPost, "/paypal/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range webhookHeaders {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

// deliver posts the event, wants 200 and returns whether it was new.
func (e *env) deliver(event map[string]any) bool {
	e.t.Helper()
	rec := e.webhook(mustJSON(e.t, event))
	if rec.Code != http.StatusOK {
		e.t.Fatalf("POST /paypal/webhook = %d %s, want 200", rec.Code, rec.Body.String())
	}
	var out struct {
		Inserted *bool `json:"inserted"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Inserted == nil {
		e.t.Fatalf("webhook answer %q, want {inserted}", rec.Body.String())
	}
	return *out.Inserted
}

// refuseDelivery posts body and wants the JSON error code with status.
func (e *env) refuseDelivery(body []byte, status int, code string) {
	e.t.Helper()
	rec := e.webhook(body)
	var got errorJSON
	if rec.Code != status || json.Unmarshal(rec.Body.Bytes(), &got) != nil || got.Error != code || got.Message == "" {
		e.t.Fatalf("POST /paypal/webhook = %d %s, want %d %s", rec.Code, rec.Body.String(), status, code)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func event(id, eventType, resourceType string, resource map[string]any) map[string]any {
	ev := map[string]any{
		"id":            id,
		"event_version": "1.0",
		"create_time":   "2026-11-02T09:00:04.000Z",
		"resource_type": resourceType,
		"event_type":    eventType,
		"summary":       "Payment event",
	}
	if resource != nil {
		ev["resource"] = resource
	}
	return ev
}

// captureCompleted is PayPal's PAYMENT.CAPTURE.COMPLETED for a capture of
// the authorization of the order.
func captureCompleted(id, captureID, orderID, authorizationID string) map[string]any {
	return event(id, "PAYMENT.CAPTURE.COMPLETED", "capture", map[string]any{
		"id":            captureID,
		"status":        "COMPLETED",
		"final_capture": true,
		"amount":        map[string]string{"currency_code": "USD", "value": "10.00"},
		"supplementary_data": map[string]any{
			"related_ids": map[string]string{"order_id": orderID, "authorization_id": authorizationID},
		},
	})
}

type storedEvent struct {
	EventType  string
	ResourceID *string
	Verified   bool
	Processed  bool
}

func (e *env) storedEvent(id string) storedEvent {
	e.t.Helper()
	var ev storedEvent
	err := e.st.Pool.QueryRow(e.ctx,
		`select event_type, resource_id, verified, processed_at is not null from provider_events where id = $1`, id).
		Scan(&ev.EventType, &ev.ResourceID, &ev.Verified, &ev.Processed)
	if err != nil {
		e.t.Fatalf("provider event %s: %v", id, err)
	}
	return ev
}

func TestWebhook_InvalidSignatureIs400AndNotStored(t *testing.T) {
	e := newEnv(t, withWebhookID())
	e.fake.SetVerify(false)
	body := mustJSON(t, captureCompleted("WH-FORGED-1", "CAPTURE-1", "ORDER-1", "AUTH-1"))

	e.refuseDelivery(body, http.StatusBadRequest, "invalid_signature")
	if n := e.count(`select count(*) from provider_events`); n != 0 {
		t.Errorf("provider events = %d, want none", n)
	}
	if logs := e.logs.String(); strings.Contains(logs, "WH-FORGED-1") || strings.Contains(logs, "PAYMENT.CAPTURE.COMPLETED") {
		t.Errorf("the log carries fields of an unverified event:\n%s", logs)
	}
	if n := e.count(`select count(*) from jobs where kind = 'reconcile'`); n != 0 {
		t.Errorf("reconcile jobs = %d, want none", n)
	}

	// PayPal was asked about this delivery, with its headers, the configured
	// webhook and the body byte for byte.
	reqs := e.wantRequests("verify_webhook", 1)
	if len(reqs) != 1 {
		return
	}
	var asked struct {
		AuthAlgo         string          `json:"auth_algo"`
		CertURL          string          `json:"cert_url"`
		TransmissionID   string          `json:"transmission_id"`
		TransmissionSig  string          `json:"transmission_sig"`
		TransmissionTime string          `json:"transmission_time"`
		WebhookID        string          `json:"webhook_id"`
		WebhookEvent     json.RawMessage `json:"webhook_event"`
	}
	if err := json.Unmarshal(reqs[0].Body, &asked); err != nil {
		t.Fatalf("verify request %q: %v", reqs[0].Body, err)
	}
	if asked.WebhookID != testWebhookID || asked.AuthAlgo != webhookHeaders["PAYPAL-AUTH-ALGO"] ||
		asked.CertURL != webhookHeaders["PAYPAL-CERT-URL"] || asked.TransmissionID != webhookHeaders["PAYPAL-TRANSMISSION-ID"] ||
		asked.TransmissionSig != webhookHeaders["PAYPAL-TRANSMISSION-SIG"] || asked.TransmissionTime != webhookHeaders["PAYPAL-TRANSMISSION-TIME"] ||
		!bytes.Equal(asked.WebhookEvent, body) {
		t.Errorf("verify request = %s, want the delivery's headers, webhook %s and the raw body", reqs[0].Body, testWebhookID)
	}
}

func TestWebhook_DuplicateAndEarlyEventsDoNotDoubleCount(t *testing.T) {
	lost := &lostAnswers{}
	e := newEnv(t, withWebhookID(), withPayPal(lost.wrap))
	h, p, ps := e.fundingWorkshop()
	wid := h.created.Workshop.ID
	en := e.enroll(ps[0], p.ID, http.StatusCreated)
	a := e.authorize(wid, en.EnrollmentID)

	// Our capture reaches PayPal but its answer is lost.
	lost.loseNext("capture")
	e.runPayment("capture", wid, a)
	captureKey := "capture:" + a.ID
	if j := e.job(captureKey); j.Status != store.JobUnknown {
		t.Fatalf("capture job after the lost answer = %s, want unknown", j.Status)
	}
	e.wantStatus(en.EnrollmentID, domain.EnrollmentCapturing)
	c := lost.captured()
	if c.ID == "" || e.fake.Captures() != 1 {
		t.Fatalf("PayPal captured %+v (%d captures), want one capture", c, e.fake.Captures())
	}

	// PayPal's webhook for that capture arrives first.
	const eventID = "WH-EARLY-CAPTURE-1"
	ev := captureCompleted(eventID, c.ID, *a.OrderID, *a.AuthorizationID)
	if !e.deliver(ev) {
		t.Fatalf("first delivery: inserted = false, want true")
	}
	if got := e.storedEvent(eventID); got.EventType != "PAYMENT.CAPTURE.COMPLETED" || got.ResourceID == nil ||
		*got.ResourceID != c.ID || !got.Verified || !got.Processed {
		t.Errorf("stored event = %+v, want the verified capture event, processed", got)
	}
	reconcileKey := "reconcile:" + a.ID + ":webhook:" + eventID
	if j := e.job(reconcileKey); j.Status != store.JobPending || !j.RunAt.Equal(t0) ||
		j.AttemptID == nil || *j.AttemptID != a.ID || j.WorkshopID == nil || *j.WorkshopID != wid {
		t.Errorf("webhook reconcile = %+v, want pending now for attempt %s of workshop %s", j, a.ID, wid)
	}
	// The webhook only records and enqueues: nothing is applied yet.
	e.wantStatus(en.EnrollmentID, domain.EnrollmentCapturing)
	if got := e.latestAttempt(en.EnrollmentID); got.CaptureID != nil {
		t.Errorf("capture id after the webhook = %s, want none until the reconcile", *got.CaptureID)
	}

	// PayPal delivers the same event again.
	if e.deliver(ev) {
		t.Errorf("repeated delivery: inserted = true, want false")
	}
	if n := e.count(`select count(*) from provider_events`); n != 1 {
		t.Errorf("provider events = %d, want 1", n)
	}
	if n := e.count(`select count(*) from jobs where idempotency_key like $1`, "reconcile:"+a.ID+":webhook:%"); n != 1 {
		t.Errorf("webhook reconciles = %d, want 1", n)
	}

	e.runKey(reconcileKey)
	e.wantStatus(en.EnrollmentID, domain.EnrollmentPaid)
	if j := e.job(captureKey); j.Status != store.JobSucceeded {
		t.Errorf("capture job after the reconcile = %s, want succeeded", j.Status)
	}
	if v := e.owner(h); v.Tally.Paid != 1 {
		t.Errorf("tally paid = %d, want 1", v.Tally.Paid)
	}

	// The reconcile the lost answer scheduled finds nothing left to do.
	e.clock.Set(t0.Add(31 * time.Second))
	e.runKey("reconcile:" + a.ID + ":1")
	if v := e.owner(h); v.Tally.Paid != 1 {
		t.Errorf("tally paid after the second reconcile = %d, want 1", v.Tally.Paid)
	}
	e.wantRequests("capture", 1)
	if e.fake.Captures() != 1 {
		t.Errorf("captures at PayPal = %d, want 1", e.fake.Captures())
	}
}

func TestWebhook_UnknownResourceIsStoredAndIgnored(t *testing.T) {
	e := newEnv(t, withWebhookID())
	_, p, ps := e.fundingWorkshop()
	e.enroll(ps[0], p.ID, http.StatusCreated)

	ev := captureCompleted("WH-UNKNOWN-1", "CAPTURE-NOBODY", "ORDER-NOBODY", "AUTH-NOBODY")
	ev["resource"].(map[string]any)["payee"] = map[string]string{"email_address": "payee-marker@example.com"}
	if !e.deliver(ev) {
		t.Fatalf("inserted = false, want true")
	}
	if got := e.storedEvent("WH-UNKNOWN-1"); !got.Verified || !got.Processed || got.ResourceID == nil || *got.ResourceID != "CAPTURE-NOBODY" {
		t.Errorf("stored event = %+v, want it verified and processed", got)
	}

	// An event about no resource at all is kept the same way.
	if !e.deliver(event("WH-NO-RESOURCE-1", "CUSTOMER.DISPUTE.CREATED", "dispute", nil)) {
		t.Fatalf("event without a resource: inserted = false, want true")
	}
	if got := e.storedEvent("WH-NO-RESOURCE-1"); !got.Processed || got.ResourceID != nil {
		t.Errorf("stored event without a resource = %+v, want processed without a resource id", got)
	}

	if n := e.count(`select count(*) from jobs where kind = 'reconcile'`); n != 0 {
		t.Errorf("reconcile jobs = %d, want none", n)
	}
	logs := e.logs.String()
	if !strings.Contains(logs, "WH-UNKNOWN-1") || strings.Contains(logs, "payee-marker@example.com") {
		t.Errorf("webhook log must name the event and never carry its body:\n%s", logs)
	}
}

func TestWebhook_WithoutWebhookIDIs503(t *testing.T) {
	e := newEnv(t)
	e.refuseDelivery(mustJSON(t, captureCompleted("WH-1", "CAPTURE-1", "ORDER-1", "AUTH-1")), http.StatusServiceUnavailable, "webhook_not_configured")
	e.wantRequests("verify_webhook", 0)
	if n := e.count(`select count(*) from provider_events`); n != 0 {
		t.Errorf("provider events = %d, want none", n)
	}
}

func TestWebhook_OrderApprovedEventLeadsToTheAuthorization(t *testing.T) {
	e := newEnv(t, withWebhookID())
	_, p, ps := e.fundingWorkshop()
	en, a := e.checkout(ps[0], p.ID)
	// The buyer approved at PayPal and closed the tab.
	e.fake.Approve(*a.OrderID)

	if !e.deliver(event("WH-ORDER-APPROVED-1", "CHECKOUT.ORDER.APPROVED", "checkout-order",
		map[string]any{"id": *a.OrderID, "status": "APPROVED", "intent": "AUTHORIZE"})) {
		t.Fatalf("inserted = false, want true")
	}
	e.wantStatus(en.EnrollmentID, domain.EnrollmentSeatReserved)
	e.runKey("reconcile:" + a.ID + ":webhook:WH-ORDER-APPROVED-1")
	e.runKey("authorize:" + a.ID)
	e.wantStatus(en.EnrollmentID, domain.EnrollmentAuthorized)
}

func TestWebhook_BodyLimitIs256KiB(t *testing.T) {
	e := newEnv(t, withWebhookID())
	padded := func(id string, size int) []byte {
		ev := event(id, "CUSTOMER.DISPUTE.CREATED", "dispute", nil)
		ev["summary"] = strings.Repeat("x", size)
		return mustJSON(t, ev)
	}

	// Larger than the API's 64 KiB, within the webhook's 256 KiB.
	if rec := e.webhook(padded("WH-LARGE-1", 200<<10)); rec.Code != http.StatusOK {
		t.Fatalf("200 KiB event = %d %s, want 200", rec.Code, rec.Body.String())
	}
	e.refuseDelivery(padded("WH-TOO-LARGE-1", 260<<10), http.StatusRequestEntityTooLarge, "body_too_large")
	if n := e.count(`select count(*) from provider_events where id = 'WH-TOO-LARGE-1'`); n != 0 {
		t.Errorf("the oversized event was stored")
	}
}

func TestWebhook_RejectsBodiesThatAreNotEvents(t *testing.T) {
	e := newEnv(t, withWebhookID())
	e.refuseDelivery([]byte("not json"), http.StatusBadRequest, "invalid_json")
	e.refuseDelivery([]byte(`{"event_type":"PAYMENT.CAPTURE.COMPLETED","resource":{"id":"CAPTURE-1"}}`), http.StatusBadRequest, "invalid_event")
	e.refuseDelivery([]byte(`{"id":"WH-1"}`), http.StatusBadRequest, "invalid_event")
	e.wantRequests("verify_webhook", 0)
	if n := e.count(`select count(*) from provider_events`); n != 0 {
		t.Errorf("provider events = %d, want none", n)
	}
}

func TestWebhook_VerificationOutageIs502AndNotStored(t *testing.T) {
	e := newEnv(t, withWebhookID())
	e.fake.FailNext("verify_webhook", http.StatusServiceUnavailable, "")
	e.refuseDelivery(mustJSON(t, captureCompleted("WH-UNVERIFIED-MARKER", "CAPTURE-1", "ORDER-1", "AUTH-1")), http.StatusBadGateway, "paypal_unavailable")
	if n := e.count(`select count(*) from provider_events`); n != 0 {
		t.Errorf("provider events = %d, want none", n)
	}
	if strings.Contains(e.logs.String(), "WH-UNVERIFIED-MARKER") {
		t.Errorf("the log carries the id of an event PayPal did not verify")
	}
}
