// Package paypaltest is an in-memory PayPal sandbox for tests and for
// end-to-end runs of a binary built with -tags e2e.
package paypaltest

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/aalvaropc/commitpool/internal/paypal"
)

const (
	fakePrefix = "/sandbox"
	fakeToken  = "fake-token"
	fakeHang   = 5 * time.Second

	headerRequestID    = "PayPal-Request-Id"
	headerMockResponse = "PayPal-Mock-Response"
)

// Order, authorization, capture and refund statuses the fake moves through.
const (
	orderPayerActionRequired = paypal.StatusPayerActionRequired
	orderApproved            = paypal.StatusApproved
	orderCompleted           = paypal.StatusCompleted

	authCreated  = paypal.StatusCreated
	authCaptured = paypal.StatusCaptured
	authVoided   = paypal.StatusVoided

	captureCompleted = paypal.StatusCompleted
	captureRefunded  = paypal.StatusRefunded

	refundCompleted = paypal.StatusCompleted
)

var (
	// fakeOps lists every operation Requests and FailNext accept.
	fakeOps = map[string]bool{
		"token": true, "create_order": true, "authorize": true, "capture": true,
		"void": true, "refund": true, "get_order": true, "get_authorization": true,
		"get_capture": true, "get_refund": true, "verify_webhook": true,
	}
	// replayedOps record a successful answer per PayPal-Request-Id.
	replayedOps = map[string]bool{"create_order": true, "authorize": true, "capture": true, "void": true, "refund": true}
)

// RecordedRequest is one API request the fake received.
type RecordedRequest struct {
	RequestID string
	Headers   http.Header
	Body      []byte
}

// Server is an in-memory PayPal sandbox. Point a paypal.Client at URL;
// the client then runs the same code paths as against the real sandbox.
//
// URL ends in "/sandbox" and every route is mounted under that prefix, so a
// Client with SandboxMocks accepts it without special cases.
//
// Besides Orders v2 and Payments v2 it serves the buyer-facing pages
// GET /approve?order=ID (an HTML form), POST /approve (marks the order
// APPROVED and redirects to the return URL) and GET /cancel?order=ID.
// Operations are named create_order, authorize, capture, void, refund,
// get_order, get_authorization, get_capture, get_refund, verify_webhook and
// token (OAuth).
//
// All methods are safe for concurrent use. The Approve, ExpireOrder, Set*
// helpers panic on an identifier the fake never issued, and FailNext and
// Requests panic on a misspelled operation, so a typo fails loudly.
type Server struct {
	URL string

	srv  *httptest.Server
	done chan struct{}
	once sync.Once

	mu          sync.Mutex
	orders      map[string]*fakeOrder
	auths       map[string]*fakeAuth
	captures    map[string]*fakeCapture
	refunds     map[string]*fakeRefund
	requests    map[string][]RecordedRequest
	failures    map[string][]fakeFailure
	replays     map[replayKey]fakeResponse
	verify      bool
	expireToken bool
	nextAuth    string
	pending     int
	closed      bool
	idle        *sync.Cond
}

type fakeOrder struct {
	id          string
	status      string
	referenceID string
	customID    string
	amount      paypal.Money
	reported    *paypal.Money
	returnURL   string
	cancelURL   string
	expired     bool
	authIDs     []string
	captureIDs  []string
	refundIDs   []string
}

type fakeAuth struct {
	paypal.Authorization
	orderID string
}

type fakeCapture struct {
	paypal.Capture
	orderID string
}

type fakeRefund struct {
	paypal.Refund
	orderID string
}

type fakeFailure struct {
	status int
	issue  string
	canned bool
	body   []byte
}

type replayKey struct{ op, requestID string }

type fakeResponse struct {
	status int
	body   []byte
}

// NewServer starts the server; call Close when done.
func NewServer() *Server {
	f := &Server{
		done:     make(chan struct{}),
		orders:   map[string]*fakeOrder{},
		auths:    map[string]*fakeAuth{},
		captures: map[string]*fakeCapture{},
		refunds:  map[string]*fakeRefund{},
		requests: map[string][]RecordedRequest{},
		failures: map[string][]fakeFailure{},
		replays:  map[replayKey]fakeResponse{},
		verify:   true,
	}
	f.idle = sync.NewCond(&f.mu)
	f.srv = httptest.NewUnstartedServer(f.routes())
	f.URL = "http://" + f.srv.Listener.Addr().String() + fakePrefix
	f.srv.Start()
	return f
}

// Close stops the server. A request hanging because of FailNext(op, 0, "") is
// released without executing, so Close returns promptly.
func (f *Server) Close() {
	f.once.Do(func() {
		close(f.done)
		f.mu.Lock()
		f.closed = true
		f.idle.Broadcast()
		f.mu.Unlock()
	})
	f.srv.Close()
}

// Approve plays the buyer approving the order without going through HTTP.
func (f *Server) Approve(orderID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.approveLocked(f.order(orderID))
}

// ExpireOrder makes the order vanish, as an unapproved order does at PayPal
// after a few hours: GET and authorize answer 404 INVALID_RESOURCE_ID.
func (f *Server) ExpireOrder(orderID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.order(orderID).expired = true
}

// ExpireToken makes the next API call answer 401 once, as when PayPal revokes
// the cached access token before its expiry.
func (f *Server) ExpireToken() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.expireToken = true
}

// FailNext queues a one-shot failure for the next request of op; several
// calls for the same op are consumed in order. A status of 400 or above is
// answered without running the operation: 5xx with an INTERNAL_SERVER_ERROR
// body and anything else with the 422 shape carrying issue (a 409 defaults to
// PREVIOUS_REQUEST_IN_PROGRESS). Any other status except 0 is a programming
// error and panics: a "failure" must not look like a success.
//
// A status of 0 hangs the request, then runs the operation normally: the
// client times out while PayPal still does the work, the ambiguous case
// callers must reconcile. The operation runs once the server notices the
// client has gone, or after five seconds, whichever comes first. That is not
// instantaneous, so a Captures() or GET issued right after the client's
// timeout can still miss it: call WaitHung first.
func (f *Server) FailNext(op string, status int, issue string) {
	mustBeOp(op)
	if status < 0 || (status > 0 && status < 400) {
		panic(fmt.Sprintf("paypal fake: FailNext needs status 0 or an HTTP error status, got %d", status))
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures[op] = append(f.failures[op], fakeFailure{status: status, issue: issue})
}

// RespondOnce answers the next request of op with status and body exactly as
// given, without running the operation or recording a replay. It is how a test
// makes PayPal answer with a body of an unexpected shape, such as a 201 that
// does not identify the resource. Like FailNext it is queued per op.
func (f *Server) RespondOnce(op string, status int, body []byte) {
	mustBeOp(op)
	if status <= 0 {
		panic(fmt.Sprintf("paypal fake: RespondOnce needs an HTTP status, got %d", status))
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures[op] = append(f.failures[op], fakeFailure{status: status, canned: true, body: append([]byte(nil), body...)})
}

// WaitHung blocks until every request held by FailNext(op, 0, "") has run its
// operation, so a test can read the result deterministically after the client
// timed out. It returns at once when none is pending, and also after Close.
func (f *Server) WaitHung() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for f.pending > 0 && !f.closed {
		f.idle.Wait()
	}
}

// Requests returns every request received for op, in arrival order. A request
// answered with an injected failure or a 401 is included.
func (f *Server) Requests(op string) []RecordedRequest {
	mustBeOp(op)
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]RecordedRequest(nil), f.requests[op]...)
}

// Captures is the number of captures actually created.
func (f *Server) Captures() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.captures)
}

// SetCaptureStatus moves a capture to status, for example PENDING to
// COMPLETED. A non-empty reason is reported as status_details.reason.
func (f *Server) SetCaptureStatus(captureID, status, reason string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp, ok := f.captures[captureID]
	if !ok {
		panic("paypal fake: unknown capture " + captureID)
	}
	cp.Status = status
	cp.StatusDetails = nil
	if reason != "" {
		cp.StatusDetails = &paypal.StatusDetails{Reason: reason}
	}
}

// SetNextAuthorizationStatus makes the next authorization the fake creates
// start in status, such as PENDING for one held for review, instead of
// CREATED.
func (f *Server) SetNextAuthorizationStatus(status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextAuth = status
}

// SetAuthorizationStatus moves an authorization to status, for example
// PENDING to CREATED or DENIED.
func (f *Server) SetAuthorizationStatus(authorizationID, status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.auths[authorizationID]
	if !ok {
		panic("paypal fake: unknown authorization " + authorizationID)
	}
	a.Status = status
}

func (f *Server) SetRefundStatus(refundID, status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.refunds[refundID]
	if !ok {
		panic("paypal fake: unknown refund " + refundID)
	}
	r.Status = status
}

// SetOrderAmount changes only the amount GET order reports, to exercise the
// mismatch check; authorizations and captures keep the original amount.
func (f *Server) SetOrderAmount(orderID, value, currency string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.order(orderID).reported = &paypal.Money{CurrencyCode: currency, Value: value}
}

// SetVerify sets the outcome of verify-webhook-signature (true by default).
func (f *Server) SetVerify(result bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.verify = result
}

func mustBeOp(op string) {
	if !fakeOps[op] {
		panic("paypal fake: unknown operation " + op)
	}
}

func (f *Server) order(id string) *fakeOrder {
	o, ok := f.orders[id]
	if !ok {
		panic("paypal fake: unknown order " + id)
	}
	return o
}

func (f *Server) approveLocked(o *fakeOrder) {
	if o.status == orderPayerActionRequired {
		o.status = orderApproved
	}
}

type opFunc func(f *Server, r *http.Request, body []byte) fakeResponse

func (f *Server) routes() http.Handler {
	mux := http.NewServeMux()
	route := func(method, path, op string, authed bool, h opFunc) {
		mux.HandleFunc(method+" "+fakePrefix+path, f.handler(op, authed, h))
	}
	route("POST", "/v1/oauth2/token", "token", false, (*Server).token)
	route("POST", "/v1/notifications/verify-webhook-signature", "verify_webhook", true, (*Server).verifyWebhook)
	route("POST", "/v2/checkout/orders", "create_order", true, (*Server).createOrder)
	route("GET", "/v2/checkout/orders/{id}", "get_order", true, (*Server).getOrder)
	route("POST", "/v2/checkout/orders/{id}/authorize", "authorize", true, (*Server).authorize)
	route("GET", "/v2/payments/authorizations/{id}", "get_authorization", true, (*Server).getAuthorization)
	route("POST", "/v2/payments/authorizations/{id}/capture", "capture", true, (*Server).capture)
	route("POST", "/v2/payments/authorizations/{id}/void", "void", true, (*Server).void)
	route("GET", "/v2/payments/captures/{id}", "get_capture", true, (*Server).getCapture)
	route("POST", "/v2/payments/captures/{id}/refund", "refund", true, (*Server).refund)
	route("GET", "/v2/payments/refunds/{id}", "get_refund", true, (*Server).getRefund)
	mux.HandleFunc("GET "+fakePrefix+"/approve", f.approvePage)
	mux.HandleFunc("POST "+fakePrefix+"/approve", f.approveSubmit)
	mux.HandleFunc("GET "+fakePrefix+"/cancel", f.cancelPage)
	return mux
}

// handler wraps one API operation: it records the request, applies the
// bearer check and any queued failure, replays a recorded answer for a
// repeated PayPal-Request-Id, and otherwise runs h under the state lock.
func (f *Server) handler(op string, authed bool, h opFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))

		f.mu.Lock()
		f.requests[op] = append(f.requests[op], RecordedRequest{
			RequestID: r.Header.Get(headerRequestID),
			Headers:   r.Header.Clone(),
			Body:      body,
		})
		unauthorized := false
		if authed {
			if f.expireToken {
				f.expireToken = false
				unauthorized = true
			} else if r.Header.Get("Authorization") != "Bearer "+fakeToken {
				unauthorized = true
			}
		}
		var fail fakeFailure
		failing := false
		if !unauthorized && len(f.failures[op]) > 0 {
			fail, failing = f.failures[op][0], true
			f.failures[op] = f.failures[op][1:]
		}
		hung := failing && fail.status == 0
		if hung {
			f.pending++
		}
		f.mu.Unlock()

		if unauthorized {
			writeResponse(w, errorResponse(http.StatusUnauthorized, "AUTHENTICATION_FAILURE", "",
				"Authentication failed due to invalid authentication credentials or a missing Authorization header."))
			return
		}
		if failing && fail.canned {
			writeResponse(w, fakeResponse{status: fail.status, body: fail.body})
			return
		}
		if failing && fail.status > 0 {
			writeResponse(w, failureResponse(fail.status, fail.issue))
			return
		}
		if hung && !f.hang(r) {
			f.mu.Lock()
			f.pending--
			f.idle.Broadcast()
			f.mu.Unlock()
			panic(http.ErrAbortHandler)
		}

		resp := func() fakeResponse {
			f.mu.Lock()
			defer f.mu.Unlock()
			if hung {
				defer func() {
					f.pending--
					f.idle.Broadcast()
				}()
			}
			return f.runLocked(op, r, body, h)
		}()
		writeResponse(w, resp)
	}
}

// hang waits out a status-0 failure. It reports false when the server is
// closing, in which case the operation must not run. A client that gave up
// does not stop it: PayPal would still have processed the request.
func (f *Server) hang(r *http.Request) bool {
	timer := time.NewTimer(fakeHang)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-r.Context().Done():
	case <-f.done:
		return false
	}
	return true
}

func (f *Server) runLocked(op string, r *http.Request, body []byte, h opFunc) fakeResponse {
	key := replayKey{op: op, requestID: r.Header.Get(headerRequestID)}
	replayable := replayedOps[op] && key.requestID != ""
	if replayable {
		if resp, ok := f.replays[key]; ok {
			return resp
		}
	}
	resp := h(f, r, body)
	if replayable && resp.status >= 200 && resp.status < 300 {
		f.replays[key] = resp
	}
	return resp
}

func writeResponse(w http.ResponseWriter, r fakeResponse) {
	if len(r.body) > 0 {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(r.status)
	if len(r.body) > 0 {
		_, _ = w.Write(r.body)
	}
}

func jsonResponse(status int, v any) fakeResponse {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("paypal fake: marshal %T: %v", v, err))
	}
	return fakeResponse{status: status, body: b}
}

type fakeErrorBody struct {
	Name    string            `json:"name"`
	Details []fakeErrorDetail `json:"details,omitempty"`
	Message string            `json:"message,omitempty"`
	DebugID string            `json:"debug_id"`
}

type fakeErrorDetail struct {
	Issue       string `json:"issue"`
	Description string `json:"description"`
}

func errorResponse(status int, name, issue, message string) fakeResponse {
	body := fakeErrorBody{Name: name, Message: message, DebugID: randomHex()}
	if issue != "" {
		body.Details = []fakeErrorDetail{{Issue: issue, Description: "The request failed business validation."}}
	}
	return jsonResponse(status, body)
}

func unprocessable(issue string) fakeResponse {
	return statusUnprocessable(http.StatusUnprocessableEntity, issue)
}

func statusUnprocessable(status int, issue string) fakeResponse {
	return errorResponse(status, "UNPROCESSABLE_ENTITY", issue,
		"The requested action could not be performed, semantically incorrect, or failed business validation.")
}

func notFound() fakeResponse {
	body := fakeErrorBody{
		Name: "RESOURCE_NOT_FOUND",
		Details: []fakeErrorDetail{{
			Issue:       "INVALID_RESOURCE_ID",
			Description: "Specified resource ID does not exist. Please check the resource ID and try again.",
		}},
		DebugID: randomHex(),
	}
	return jsonResponse(http.StatusNotFound, body)
}

func failureResponse(status int, issue string) fakeResponse {
	if status >= 500 {
		return jsonResponse(status, fakeErrorBody{Name: "INTERNAL_SERVER_ERROR", DebugID: randomHex()})
	}
	if status == http.StatusConflict && issue == "" {
		issue = "PREVIOUS_REQUEST_IN_PROGRESS"
	}
	return statusUnprocessable(status, issue)
}

// newID returns 17 uppercase alphanumerics, the shape of PayPal identifiers.
func newID() string { return rand.Text()[:17] }

func randomHex() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
