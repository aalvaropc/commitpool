package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/paypal"
	"github.com/aalvaropc/commitpool/internal/store"
	"github.com/aalvaropc/commitpool/internal/worker"
)

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
	returnedPath = "/paypal/returned"
	webhookPath  = "/paypal/webhook"

	maxWebhookBytes = 256 << 10

	returnReconcileDelay = 30 * time.Second
)

var (
	errWebhookNotConfigured = newError(http.StatusServiceUnavailable, "webhook_not_configured",
		"PayPal webhooks are not configured on this server.")
	errWebhookTooLarge   = newError(http.StatusRequestEntityTooLarge, "body_too_large", "The webhook body is larger than 256 KiB.")
	errInvalidSignature  = badRequest("invalid_signature", "The webhook signature could not be verified with PayPal.")
	errInvalidEvent      = badRequest("invalid_event", "The webhook body is not a PayPal event with an id and an event type.")
	errVerifyUnavailable = newError(http.StatusBadGateway, "paypal_unavailable",
		"PayPal could not verify the webhook right now. Please deliver it again later.")
	errSeatExpired = conflict("seat_expired", "Your reservation expired and could not be renewed.")
)

type checkout struct {
	enrollment store.EnrollmentRow
	attempt    store.AttemptRow
	log        *slog.Logger
}

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

func (s *server) paypalCancel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	c, result := s.findCheckout(ctx, "paypal cancel", q.Get("e"), q.Get("token"))
	if result == "" {
		result = s.cancelCheckout(ctx, c)
	}
	redirectReturned(w, r, c.enrollment.ID, result)
}

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

func (s *server) completeReturn(ctx context.Context, c *checkout) string {
	key := store.JobKey(store.KindAuthorize, c.attempt.ID)
	if c.enrollment.NeedsAttentionAt != nil {
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

type seatState int

const (
	seatHeld seatState = iota
	seatGone
	seatClosed
)

var beforeSeatRenewal = func(context.Context, string) {}

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

func seatLive(en store.EnrollmentRow, now time.Time) bool {
	if en.Status == domain.EnrollmentSeatReserved {
		return !domain.SeatExpired(en.SeatExpiresAt, now)
	}
	return en.Status.Occupies()
}

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
	key := store.JobKey(store.KindReconcile, c.attempt.ID+":return:"+store.Minute(now))
	if err := s.Worker.Enqueue(ctx, store.KindReconcile, key, &c.enrollment.WorkshopID, &c.attempt.ID, nil, now.Add(returnReconcileDelay)); err != nil {
		c.log.Error("paypal return: enqueue the reconcile", "error", err)
		return resultError
	}
	return resultPending
}

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
		return s.byWindow(ctx, c, en, resultPending)
	}
	debugID := ""
	if j.DebugID != nil {
		debugID = *j.DebugID
	}
	c.log.Warn("paypal return: PayPal refused the authorization", "issue", issue, "debug_id", debugID)
	return resultDeclined
}

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

var webhookSignatureHeaders = []string{
	"PAYPAL-TRANSMISSION-ID", "PAYPAL-TRANSMISSION-TIME", "PAYPAL-TRANSMISSION-SIG", "PAYPAL-CERT-URL", "PAYPAL-AUTH-ALGO",
}

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
	if why := implausibleDelivery(r.Header, s.Clock.Now()); why != "" {
		s.Log.Warn("webhook: delivery refused before asking PayPal", "why", why)
		return errInvalidSignature
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

func implausibleDelivery(h http.Header, now time.Time) string {
	cert, err := url.Parse(h.Get("PAYPAL-CERT-URL"))
	if err != nil || cert.Scheme != "https" || (cert.Hostname() != "paypal.com" && !strings.HasSuffix(cert.Hostname(), ".paypal.com")) {
		return "certificate URL is not https on paypal.com"
	}
	sent, err := time.Parse(time.RFC3339, h.Get("PAYPAL-TRANSMISSION-TIME"))
	if err != nil || sent.After(now.Add(5*time.Minute)) {
		return "transmission time is not a time in the past"
	}
	return ""
}
