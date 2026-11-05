package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/paypal"
	"github.com/aalvaropc/commitpool/internal/store"
	"github.com/aalvaropc/commitpool/internal/worker"
)

// The outcomes the web app's /paypal/returned page receives as result.
const (
	resultAuthorized  = "authorized"
	resultPending     = "pending"
	resultMismatch    = "mismatch"
	resultSeatExpired = "seat_expired"
	resultClosed      = "closed"
	resultExpired     = "expired"
	resultDeclined    = "declined"
	resultCancelled   = "cancelled"
	resultError       = "error"
)

const (
	// returnedPath is the web app's page for a buyer back from PayPal.
	returnedPath = "/paypal/returned"
	webhookPath  = "/paypal/webhook"

	// maxWebhookBytes caps a webhook body instead of maxBodyBytes.
	maxWebhookBytes = 256 << 10

	// returnReconcileDelay is how long a return PayPal did not answer waits
	// for the reconcile that reads the checkout back.
	returnReconcileDelay = 30 * time.Second
	minuteLayout         = "2006-01-02T15:04"
)

var (
	errWebhookNotConfigured = newError(http.StatusServiceUnavailable, "webhook_not_configured",
		"PayPal webhooks are not configured on this server.")
	errWebhookTooLarge   = newError(http.StatusRequestEntityTooLarge, "body_too_large", "The webhook body is larger than 256 KiB.")
	errInvalidSignature  = badRequest("invalid_signature", "The webhook signature could not be verified with PayPal.")
	errInvalidEvent      = badRequest("invalid_event", "The webhook body is not a PayPal event with an id and an event type.")
	errVerifyUnavailable = newError(http.StatusBadGateway, "paypal_unavailable",
		"PayPal could not verify the webhook right now. Please deliver it again later.")
	errSeatExpired = conflict("seat_expired", "Your seat reservation expired and could not be renewed.")
)

// checkout is the enrollment a buyer comes back for, with the latest
// attempt, whose order PayPal named.
type checkout struct {
	enrollment store.EnrollmentRow
	attempt    store.AttemptRow
	log        *slog.Logger
}

// paypalReturn is where PayPal sends the buyer after approving a checkout:
// /paypal/return?e={enrollment}&token={order}. The query only finds the
// checkout. The order must be the enrollment's latest attempt's, and PayPal
// must report it for this enrollment, this attempt and the proposal's price
// and currency, before the worker is asked to authorize it under
// authorize:{attempt}; then the buyer is redirected to the web app's
// /paypal/returned page with the outcome. The redirect carries the
// enrollment and the outcome only, never a token or a PayPal issue.
func (s *server) paypalReturn(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	c, result := s.findCheckout(ctx, "paypal return", q.Get("e"), q.Get("token"))
	if result == "" {
		result = s.completeReturn(ctx, c)
	}
	c.log.Info("paypal return", "result", result)
	redirectReturned(w, r, c.enrollment.ID, result)
}

// paypalCancel is where PayPal sends a buyer who cancelled the checkout:
// /paypal/cancel?e={enrollment}&token={order}. The cancel is recorded on the
// enrollment for the participant's view; the seat is kept until it expires
// on its own, so the buyer can still approve the same checkout. PayPal's
// token must be the latest attempt's order: without it, or with another
// order, nothing is marked (mismatch).
func (s *server) paypalCancel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	c, result := s.findCheckout(ctx, "paypal cancel", q.Get("e"), q.Get("token"))
	if result == "" {
		result = s.cancelCheckout(ctx, c)
	}
	redirectReturned(w, r, c.enrollment.ID, result)
}

// cancelCheckout records the cancel on a SEAT_RESERVED enrollment. One past
// checkout, its hold authorized from another tab say, keeps what it has and
// the result says what that is.
func (s *server) cancelCheckout(ctx context.Context, c *checkout) string {
	marked, err := s.Store.MarkApprovalCancelled(ctx, c.enrollment.ID, s.Clock.Now())
	if err != nil {
		c.log.Error("paypal cancel: record the cancel", "error", err)
		return resultError
	}
	if marked {
		c.log.Info("paypal cancel: the buyer cancelled the checkout; the seat is kept until it expires")
		return resultCancelled
	}
	en, err := s.Store.EnrollmentByID(ctx, c.enrollment.ID)
	if err != nil {
		c.log.Error("paypal cancel: reload the enrollment", "error", err)
		return resultError
	}
	c.log.Info("paypal cancel: the enrollment is past checkout; nothing to cancel", "status", en.Status)
	return settledResult(en.Status)
}

// findEnrollment loads the enrollment a PayPal redirect names. An unknown or
// malformed ID is a mismatch, with an empty enrollment in the checkout.
func (s *server) findEnrollment(ctx context.Context, op, enrollmentID string) (*checkout, string) {
	c := &checkout{log: s.Log}
	en, err := s.Store.EnrollmentByID(ctx, enrollmentID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		c.log.Warn(op + ": the enrollment is unknown")
		return c, resultMismatch
	case err != nil:
		c.log.Error(op+": load the enrollment", "error", err)
		return c, resultError
	}
	c.enrollment, c.log = en, s.Log.With("enrollment_id", en.ID)
	return c, ""
}

// findCheckout is findEnrollment plus the latest attempt, which must hold
// the order PayPal named; any other order is a mismatch.
func (s *server) findCheckout(ctx context.Context, op, enrollmentID, orderID string) (*checkout, string) {
	c, result := s.findEnrollment(ctx, op, enrollmentID)
	if result != "" {
		return c, result
	}
	a, err := s.Store.LatestAttempt(ctx, c.enrollment.ID)
	if err != nil {
		c.log.Error(op+": load the latest attempt", "error", err)
		return c, resultError
	}
	if a == nil || a.OrderID == nil || orderID == "" || *a.OrderID != orderID {
		c.log.Warn(op + ": the order is not the enrollment's latest checkout")
		return c, resultMismatch
	}
	c.attempt, c.log = *a, c.log.With("attempt_id", a.ID)
	return c, ""
}

// completeReturn decides and does what the return of a known checkout calls
// for. A checkout whose hold is already recorded, or whose enrollment is past
// checkout, is answered from the stored state without asking PayPal, so a
// repeated return sends nothing twice.
func (s *server) completeReturn(ctx context.Context, c *checkout) string {
	key := store.JobKey(store.KindAuthorize, c.attempt.ID)
	if c.enrollment.NeedsAttentionAt != nil {
		// Its money state is uncertain: no hold is added until an operator looks.
		c.log.Warn("paypal return: the enrollment is flagged for an operator; not authorizing")
		return resultError
	}
	if st := c.enrollment.Status; c.attempt.AuthorizationID != nil ||
		(st != domain.EnrollmentSeatReserved && st != domain.EnrollmentReleased) {
		return s.returnOutcome(ctx, c, key)
	}
	ws, err := s.Store.WorkshopByID(ctx, c.enrollment.WorkshopID)
	if err != nil {
		c.log.Error("paypal return: load the workshop", "error", err)
		return resultError
	}
	p, err := s.Store.ProposalByID(ctx, c.enrollment.ProposalID)
	if err != nil {
		c.log.Error("paypal return: load the proposal", "error", err)
		return resultError
	}
	if p == nil {
		c.log.Error("paypal return: the enrollment's proposal is missing", "proposal_id", c.enrollment.ProposalID)
		return resultError
	}

	o, err := s.PayPal.GetOrder(ctx, *c.attempt.OrderID)
	if err != nil {
		return s.orderUnreadable(ctx, c, err)
	}
	if field := worker.OrderMismatch(o, c.attempt, c.enrollment.ID, *p); field != "" {
		c.log.Warn("paypal return: PayPal reports the order for another checkout; not authorizing it", "field", field)
		return resultMismatch
	}
	now := s.Clock.Now()
	switch o.Status {
	case paypal.StatusApproved:
	case paypal.StatusCompleted:
		// Authorized at PayPal, not recorded here yet: a job is finishing it.
		// Once payments have closed, or on a released seat, that hold is
		// voided when it is recorded.
		c.log.Info("paypal return: PayPal already shows the order authorized", "status", c.enrollment.Status)
		switch {
		case !paymentsOpen(ws, p, now):
			return resultClosed
		case c.enrollment.Status == domain.EnrollmentReleased:
			return resultSeatExpired
		}
		return resultAuthorized
	case paypal.StatusCreated, paypal.StatusPayerActionRequired:
		return resultPending
	default:
		c.log.Warn("paypal return: the order is in a status the return does not authorize", "order_status", o.Status)
		return resultError
	}

	if !paymentsOpen(ws, p, now) {
		c.log.Info("paypal return: order approved after payments closed; not authorizing it", "workshop_status", ws.Status)
		return resultClosed
	}
	seat, err := s.holdSeat(ctx, c.enrollment, now)
	switch {
	case err != nil:
		c.log.Error("paypal return: renew the seat", "error", err)
		return resultError
	case seat == seatClosed:
		c.log.Info("paypal return: payments closed before the expired seat could be renewed; not authorizing it")
		return resultClosed
	case seat == seatGone:
		c.log.Info("paypal return: order approved after the seat expired and no seat is left; not authorizing it")
		return resultSeatExpired
	}
	if err := s.authorizeNow(ctx, c, key, now); err != nil {
		c.log.Error("paypal return: run the authorization", "error", err)
	}
	return s.returnOutcome(ctx, c, key)
}

// authorizeNow has the worker authorize the attempt under key, enqueued or
// revived and run inline: a job skipped earlier is revived automatically,
// and one PayPal refused as ORDER_NOT_APPROVED is revived because the buyer
// is back with the order approved. Any other refusal, or a job running,
// unknown or waiting for its retry, is left alone.
func (s *server) authorizeNow(ctx context.Context, c *checkout, key string, now time.Time) error {
	created, err := s.Worker.EnqueueOrRevive(ctx, store.KindAuthorize, key, &c.enrollment.WorkshopID, &c.attempt.ID, nil, now)
	if err != nil {
		return err
	}
	if !created {
		j, err := s.Store.JobByKey(ctx, key)
		if err != nil {
			return err
		}
		if j != nil && j.Status == store.JobFailed && j.ProviderIssue != nil && *j.ProviderIssue == paypal.IssueOrderNotApproved {
			c.log.Info("paypal return: the buyer approved since PayPal refused the authorization; reviving it")
			if _, err := s.Worker.Revive(ctx, key); err != nil {
				return err
			}
		}
	}
	return s.Worker.RunKey(ctx, key)
}

// seatState is what holdSeat found.
type seatState int

const (
	seatHeld   seatState = iota // the enrollment holds a live seat, or its hold is placed
	seatGone                    // expired, and no seat could be given back
	seatClosed                  // expired, and payments closed before it could be renewed
)

// beforeSeatRenewal runs as holdSeat starts renewing an expired seat. It does
// nothing; tests replace it to let another request act first.
var beforeSeatRenewal = func(context.Context, string) {}

// holdSeat makes sure the enrollment holds a live seat at now. A seat past
// its expiry, released by the seat tick or not yet, is released and taken
// again under the capacity rule (store.ReReserveSeat) for the configured
// seat TTL, as a new enrollment gets. When that is refused the enrollment is
// read again: another request or a reconcile may have renewed the seat
// first, or placed the hold, and then the seat is held; so is a seat the
// release kept because its authorization is pending at PayPal. Only an
// enrollment still RELEASED has lost it: seatClosed when payments have
// closed since, seatGone otherwise.
func (s *server) holdSeat(ctx context.Context, en store.EnrollmentRow, now time.Time) (seatState, error) {
	if !seatLive(en, now) && (en.Status == domain.EnrollmentSeatReserved || en.Status == domain.EnrollmentReleased) {
		beforeSeatRenewal(ctx, en.ID)
		if _, err := s.Store.ReleaseExpiredSeats(ctx, en.WorkshopID, now); err != nil {
			return seatGone, err
		}
		renewed, err := s.Store.ReReserveSeat(ctx, en.ID, now, s.Cfg.SeatTTL)
		if err != nil || renewed {
			return seatHeld, err
		}
		if en, err = s.Store.EnrollmentByID(ctx, en.ID); err != nil {
			return seatGone, err
		}
	}
	switch {
	case seatLive(en, now):
		return seatHeld, nil
	case en.Status == domain.EnrollmentSeatReserved:
		// The release kept the seat: its authorization is pending at PayPal.
		return seatHeld, nil
	case en.Status != domain.EnrollmentReleased:
		return seatGone, nil
	}
	ws, err := s.Store.WorkshopByID(ctx, en.WorkshopID)
	if err != nil {
		return seatGone, err
	}
	p, err := s.Store.ProposalByID(ctx, en.ProposalID)
	if err != nil {
		return seatGone, err
	}
	if !paymentsOpen(ws, p, now) {
		return seatClosed, nil
	}
	return seatGone, nil
}

// seatLive reports whether the enrollment occupies a seat at now: reserved
// and not expired, or past checkout with its hold placed or charged.
func seatLive(en store.EnrollmentRow, now time.Time) bool {
	if en.Status == domain.EnrollmentSeatReserved {
		return !domain.SeatExpired(en.SeatExpiresAt, now)
	}
	return en.Status.Occupies()
}

// orderUnreadable answers a return whose order PayPal would not show. An
// order PayPal no longer knows expired unapproved: the attempt records it
// and the participant's next checkout opens a new one. A refusal that is
// final is an error. Otherwise PayPal may answer later, so a reconcile reads
// the checkout back shortly (reconcile:{attempt}:return:{minute}) and
// authorizes it if the buyer approved, and the buyer is told it is pending.
func (s *server) orderUnreadable(ctx context.Context, c *checkout, err error) string {
	var pe *paypal.Error
	if errors.As(err, &pe) {
		c.log = c.log.With("http_status", pe.HTTPStatus, "issue", pe.Issue, "debug_id", pe.DebugID)
	}
	if pe != nil && pe.HTTPStatus == http.StatusNotFound && pe.Issue == paypal.IssueInvalidResourceID {
		c.log.Info("paypal return: PayPal no longer has the order; the checkout expired")
		if _, err := s.Store.MarkOrderExpired(ctx, c.attempt.ID); err != nil {
			c.log.Error("paypal return: record the expired order", "error", err)
			return resultError
		}
		return resultExpired
	}
	if paypal.Definitive(err) != nil {
		c.log.Error("paypal return: PayPal refused to show the order")
		return resultError
	}
	c.log.Warn("paypal return: PayPal did not show the order; reconciling the checkout shortly", "error", err)
	now := s.Clock.Now()
	key := store.JobKey(store.KindReconcile, c.attempt.ID+":return:"+now.UTC().Format(minuteLayout))
	if err := s.Worker.Enqueue(ctx, store.KindReconcile, key, &c.enrollment.WorkshopID, &c.attempt.ID, nil, now.Add(returnReconcileDelay)); err != nil {
		c.log.Error("paypal return: enqueue the reconcile", "error", err)
		return resultError
	}
	return resultPending
}

// returnOutcome is the result for the checkout as it stands now, read back
// from the store after the worker had its turn.
func (s *server) returnOutcome(ctx context.Context, c *checkout, key string) string {
	en, err := s.Store.EnrollmentByID(ctx, c.enrollment.ID)
	if err != nil {
		c.log.Error("paypal return: reload the enrollment", "error", err)
		return resultError
	}
	if en.NeedsAttentionAt != nil {
		return resultError
	}
	switch en.Status {
	case domain.EnrollmentSeatReserved:
	case domain.EnrollmentAuthorized:
		// A hold still uncharged once payments have closed is voided.
		return s.byWindow(ctx, c, en, resultAuthorized)
	default:
		return settledResult(en.Status)
	}
	j, err := s.Store.JobByKey(ctx, key)
	if err != nil {
		c.log.Error("paypal return: read the authorize job", "error", err)
		return resultError
	}
	if j == nil {
		return resultPending
	}
	switch j.Status {
	case store.JobNeedsAttention:
		return resultError
	case store.JobFailed:
	default:
		return resultPending
	}
	issue := ""
	if j.ProviderIssue != nil {
		issue = *j.ProviderIssue
	}
	switch {
	case issue == paypal.IssueOrderNotApproved:
		return resultPending
	case issue == paypal.IssueInvalidResourceID:
		return resultExpired
	case issue == "" && j.HTTPStatus == nil:
		// The worker skipped it: payments closed, or the seat or the
		// checkout changed under the return.
		return s.byWindow(ctx, c, en, resultPending)
	}
	debugID := ""
	if j.DebugID != nil {
		debugID = *j.DebugID
	}
	c.log.Warn("paypal return: PayPal refused the authorization", "issue", issue, "debug_id", debugID)
	return resultDeclined
}

// settledResult is the result an enrollment past SEAT_RESERVED stands for: a
// hold placed (or charged since) is authorized, a released seat expired, a
// denied hold or failed charge declined, a hold released at close closed. A
// seat still reserved is pending.
func settledResult(status domain.EnrollmentStatus) string {
	switch status {
	case domain.EnrollmentSeatReserved:
		return resultPending
	case domain.EnrollmentReleased:
		return resultSeatExpired
	case domain.EnrollmentCaptureFailed:
		return resultDeclined
	case domain.EnrollmentVoiding, domain.EnrollmentVoided:
		return resultClosed
	case domain.EnrollmentNeedsAttention:
		return resultError
	}
	return resultAuthorized
}

// byWindow is open while the enrollment's payments are open, and closed once
// they have closed.
func (s *server) byWindow(ctx context.Context, c *checkout, en store.EnrollmentRow, open string) string {
	ws, err := s.Store.WorkshopByID(ctx, en.WorkshopID)
	if err != nil {
		c.log.Error("paypal return: load the workshop", "error", err)
		return resultError
	}
	p, err := s.Store.ProposalByID(ctx, en.ProposalID)
	if err != nil {
		c.log.Error("paypal return: load the proposal", "error", err)
		return resultError
	}
	if !paymentsOpen(ws, p, s.Clock.Now()) {
		return resultClosed
	}
	return open
}

func redirectReturned(w http.ResponseWriter, r *http.Request, enrollmentID, result string) {
	q := url.Values{"result": {result}}
	if enrollmentID != "" {
		q.Set("e", enrollmentID)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, returnedPath+"?"+q.Encode(), http.StatusFound)
}

// webhookSignatureHeaders are the transmission headers PayPal signs a
// delivery with; verify-webhook-signature needs every one of them.
var webhookSignatureHeaders = []string{
	"PAYPAL-TRANSMISSION-ID", "PAYPAL-TRANSMISSION-TIME", "PAYPAL-TRANSMISSION-SIG", "PAYPAL-CERT-URL", "PAYPAL-AUTH-ALGO",
}

// webhookEvent is what the webhook reads from a PayPal event; the body is
// stored whole.
type webhookEvent struct {
	ID           string `json:"id"`
	EventType    string `json:"event_type"`
	ResourceType string `json:"resource_type"`
	Resource     struct {
		ID                string `json:"id"`
		SupplementaryData struct {
			RelatedIDs struct {
				OrderID         string `json:"order_id"`
				AuthorizationID string `json:"authorization_id"`
				CaptureID       string `json:"capture_id"`
			} `json:"related_ids"`
		} `json:"supplementary_data"`
	} `json:"resource"`
}

// paypalWebhook takes PayPal's event deliveries. A delivery is verified with
// PayPal (verify-webhook-signature, with its transmission headers and the
// body byte for byte) before anything is stored, and an event is stored
// once by its id. The webhook never applies a payment state: when the
// event's resource belongs to a payment attempt it enqueues
// reconcile:{attempt}:webhook:{event_id}, due now, and the reconcile reads
// PayPal and applies what it finds, so an early or repeated event cannot
// count a payment twice. It answers 200 {"inserted"} for every verified
// event, a repeat included. The log names a verified event and its attempt,
// never the body, and nothing of a delivery PayPal did not verify.
//
// Errors: 503 webhook_not_configured without a webhook ID; 413
// body_too_large; 400 invalid_json or invalid_event for a body that is not a
// PayPal event; 400 invalid_signature when one of the five PAYPAL-*
// transmission headers is missing or empty, without asking PayPal, or when
// PayPal does not verify the delivery; 502 paypal_unavailable when PayPal
// could not be asked.
func (s *server) paypalWebhook(w http.ResponseWriter, r *http.Request) error {
	if s.Cfg.PayPalWebhookID == "" {
		return errWebhookNotConfigured
	}
	body, err := io.ReadAll(r.Body)
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		return errWebhookTooLarge
	case err != nil:
		return err
	case !json.Valid(body):
		return errInvalidJSON
	}
	var ev webhookEvent
	if err := json.Unmarshal(body, &ev); err != nil || ev.ID == "" || ev.EventType == "" {
		return errInvalidEvent
	}
	for _, h := range webhookSignatureHeaders {
		if r.Header.Get(h) == "" {
			s.Log.Warn("webhook: a signature header is missing; event dropped", "header", h)
			return errInvalidSignature
		}
	}
	ctx := r.Context()
	verified, err := s.PayPal.VerifyWebhookSignature(ctx, r.Header, body)
	if err != nil {
		s.Log.Error("webhook: PayPal could not verify the delivery", "error", err)
		return errVerifyUnavailable
	}
	if !verified {
		s.Log.Warn("webhook: the signature did not verify; event dropped")
		return errInvalidSignature
	}
	// Only a delivery PayPal verified names its event in the log.
	log := s.Log.With("event_id", ev.ID, "event_type", ev.EventType)

	inserted, err := s.Store.InsertProviderEvent(ctx, ev.ID, ev.EventType, ev.ResourceType, ev.Resource.ID, true, body)
	if err != nil {
		return err
	}
	a, err := s.eventAttempt(ctx, ev)
	if err != nil {
		return err
	}
	if a != nil {
		log = log.With("attempt_id", a.ID)
		en, err := s.Store.EnrollmentByID(ctx, a.EnrollmentID)
		if err != nil {
			return err
		}
		key := store.JobKey(store.KindReconcile, a.ID+":webhook:"+ev.ID)
		if err := s.Worker.Enqueue(ctx, store.KindReconcile, key, &en.WorkshopID, &a.ID, nil, s.Clock.Now()); err != nil {
			return err
		}
	}
	if err := s.Store.MarkEventProcessed(ctx, ev.ID); err != nil {
		return err
	}
	log.Info("webhook: event recorded", "inserted", inserted, "matched", a != nil)
	writeJSON(w, http.StatusOK, map[string]bool{"inserted": inserted})
	return nil
}

// eventAttempt is the payment attempt the event's resource belongs to: the
// first of the resource itself and its related capture, authorization and
// order that an attempt holds, or nil. The related IDs find the attempt when
// PayPal tells of a resource before the worker recorded it, such as a
// capture whose answer was lost.
func (s *server) eventAttempt(ctx context.Context, ev webhookEvent) (*store.AttemptRow, error) {
	related := ev.Resource.SupplementaryData.RelatedIDs
	for _, id := range []string{ev.Resource.ID, related.CaptureID, related.AuthorizationID, related.OrderID} {
		if id == "" {
			continue
		}
		a, err := s.Store.AttemptByResourceID(ctx, id)
		if err != nil || a != nil {
			return a, err
		}
	}
	return nil, nil
}
