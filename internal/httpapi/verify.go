package httpapi

import (
	"context"
	"errors"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/paypal"
	"github.com/aalvaropc/commitpool/internal/store"
)

const (
	verifyEvery    = 30 * time.Second
	verifyTimeout  = 45 * time.Second
	verifyParallel = 4
)

var maxVerifySeats = 12

type verifyQuota struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func newVerifyQuota() *verifyQuota { return &verifyQuota{last: map[string]time.Time{}} }

func (q *verifyQuota) take(workshopID string, now time.Time) time.Duration {
	q.mu.Lock()
	defer q.mu.Unlock()
	for id, at := range q.last {
		if !now.Before(at.Add(verifyEvery)) {
			delete(q.last, id)
		}
	}
	if at, ok := q.last[workshopID]; ok {
		return at.Add(verifyEvery).Sub(now)
	}
	q.last[workshopID] = now
	return 0
}

type verifyCheckDTO struct {
	Resource string `json:"resource"`
	ID       string `json:"id"`
	Local    string `json:"local"`
	PayPal   string `json:"paypal"`
	Match    bool   `json:"match"`
	DebugID  string `json:"debug_id,omitempty"`
}

type verifySeatDTO struct {
	EnrollmentID string           `json:"enrollment_id"`
	Seat         string           `json:"seat"`
	Checks       []verifyCheckDTO `json:"checks"`
}

type verifySummaryDTO struct {
	Records  int `json:"records"`
	Matching int `json:"matching"`
	Seats    int `json:"seats"`
	Skipped  int `json:"skipped"`
}

type verifyDTO struct {
	CheckedAt time.Time        `json:"checked_at"`
	Summary   verifySummaryDTO `json:"summary"`
	Seats     []verifySeatDTO  `json:"seats"`
}

var errPayPalNotConfigured = newError(http.StatusServiceUnavailable, "paypal_unavailable", "PayPal cannot be reached from CommitPool right now.")

func (s *server) verifyWithPayPal(w http.ResponseWriter, r *http.Request) error {
	ws, err := s.ownerWorkshop(r)
	if err != nil {
		return err
	}
	if s.PayPal == nil {
		return errPayPalNotConfigured
	}
	subjects, skipped, err := s.verifiableSeats(r.Context(), ws.ID)
	if err != nil {
		return err
	}
	now := s.Clock.Now()
	if wait := s.verifies.take(ws.ID, now); wait > 0 {
		retryAfter(w, wait)
		return newError(http.StatusTooManyRequests, "verify_too_soon",
			"PayPal was checked for this workshop moments ago. Try again in "+seconds(wait)+".")
	}
	ctx, cancel := context.WithTimeout(r.Context(), verifyTimeout)
	defer cancel()
	out := verifyDTO{CheckedAt: now.UTC(), Seats: make([]verifySeatDTO, len(subjects))}
	slots := make(chan struct{}, verifyParallel)
	var wg sync.WaitGroup
	for i, subject := range subjects {
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			out.Seats[i] = s.verifySeat(ctx, subject)
		})
	}
	wg.Wait()
	out.Summary = verifySummaryDTO{Seats: len(subjects), Skipped: skipped}
	for _, seat := range out.Seats {
		for _, c := range seat.Checks {
			out.Summary.Records++
			if c.Match {
				out.Summary.Matching++
			}
		}
	}
	s.Log.Info("payments verified with PayPal", "workshop_id", ws.ID, "seats", out.Summary.Seats,
		"records", out.Summary.Records, "matching", out.Summary.Matching, "skipped", skipped)
	writeJSON(w, http.StatusOK, out)
	return nil
}

func seconds(d time.Duration) string {
	n := max(1, int(math.Ceil(d.Seconds())))
	if n == 1 {
		return "a second"
	}
	return strconv.Itoa(n) + " seconds"
}

type verifySubject struct {
	enrollmentID string
	name         string
	attempt      store.AttemptRow
}

func (s *server) verifiableSeats(ctx context.Context, workshopID string) ([]verifySubject, int, error) {
	enrollments, err := s.Store.EnrollmentsByWorkshop(ctx, workshopID)
	if err != nil {
		return nil, 0, err
	}
	participants, err := s.Store.ParticipantsByWorkshop(ctx, workshopID)
	if err != nil {
		return nil, 0, err
	}
	latest, err := s.Store.LatestAttemptsByWorkshop(ctx, workshopID)
	if err != nil {
		return nil, 0, err
	}
	names := make(map[string]string, len(participants))
	for _, p := range participants {
		names[p.ID] = p.DisplayName
	}
	var subjects []verifySubject
	for _, en := range enrollments {
		a := latest[en.ID]
		if a == nil || a.OrderID == nil && a.AuthorizationID == nil && a.CaptureID == nil && a.RefundID == nil {
			continue
		}
		subjects = append(subjects, verifySubject{enrollmentID: en.ID, name: names[en.ParticipantID], attempt: *a})
	}
	if len(subjects) > maxVerifySeats {
		return subjects[:maxVerifySeats], len(subjects) - maxVerifySeats, nil
	}
	return subjects, 0, nil
}

type paymentFacts struct {
	status, amount, fee, net string
}

const notRecorded = "not recorded"

func (f paymentFacts) String() string {
	parts := []string{f.status}
	if f.status == "" {
		parts[0] = notRecorded
	}
	if f.amount != "" {
		parts = append(parts, f.amount)
	}
	if f.fee != "" {
		parts = append(parts, "fee "+f.fee)
	}
	if f.net != "" {
		parts = append(parts, "net "+f.net)
	}
	return strings.Join(parts, ", ")
}

func (s *server) verifySeat(ctx context.Context, v verifySubject) verifySeatDTO {
	a := v.attempt
	out := verifySeatDTO{EnrollmentID: v.enrollmentID, Seat: v.name, Checks: []verifyCheckDTO{}}
	price := amountText(domain.Money{Minor: a.AmountMinor, Currency: a.Currency})
	compare := func(resource, id string, local paymentFacts, remote paymentFacts, err error) {
		c := verifyCheckDTO{Resource: resource, ID: id, Local: local.String()}
		if err != nil {
			c.PayPal, c.DebugID = payPalFailure(err)
		} else {
			c.PayPal = remote.String()
			c.Match = c.Local == c.PayPal
		}
		out.Checks = append(out.Checks, c)
	}

	if a.OrderID != nil {
		local := paymentFacts{status: expectedOrderStatus(a)}
		if local.status != store.OrderExpired {
			local.amount = price
		}
		var remote paymentFacts
		o, err := s.PayPal.GetOrder(ctx, *a.OrderID)
		switch {
		case err == nil:
			remote.status = o.Status
			if len(o.PurchaseUnits) > 0 {
				remote.amount = payPalAmount(o.PurchaseUnits[0].Amount)
			}
		case local.status == store.OrderExpired && orderNotFound(err):
			remote.status, err = store.OrderExpired, nil
		}
		compare("order", *a.OrderID, local, remote, err)
	}
	if a.AuthorizationID != nil {
		local := paymentFacts{status: expectedAuthorizationStatus(a), amount: price}
		var remote paymentFacts
		au, err := s.PayPal.GetAuthorization(ctx, *a.AuthorizationID)
		if err == nil {
			remote = paymentFacts{status: au.Status, amount: payPalAmount(au.Amount)}
		}
		compare("authorization", *a.AuthorizationID, local, remote, err)
	}
	if a.CaptureID != nil {
		local := paymentFacts{status: expectedCaptureStatus(a), amount: price}
		var remote paymentFacts
		c, err := s.PayPal.GetCapture(ctx, *a.CaptureID)
		if err == nil {
			remote = paymentFacts{status: c.Status, amount: payPalAmount(c.Amount)}
		}
		switch {
		case a.CaptureFeeMinor != nil && a.CaptureNetMinor != nil:
			local.fee = amountText(domain.Money{Minor: *a.CaptureFeeMinor, Currency: a.Currency})
			local.net = amountText(domain.Money{Minor: *a.CaptureNetMinor, Currency: a.Currency})
			remote.fee, remote.net = reportedFees(c.SellerReceivableBreakdown, a.Currency)
		case is(a.CaptureStatus, paypal.StatusCompleted) || is(a.CaptureStatus, paypal.StatusRefunded):
			local.fee, local.net = notRecorded, notRecorded
			remote.fee, remote.net = reportedFees(c.SellerReceivableBreakdown, a.Currency)
		}
		compare("capture", *a.CaptureID, local, remote, err)
	}
	if a.RefundID != nil {
		local := paymentFacts{status: text(a.RefundStatus), amount: price}
		var remote paymentFacts
		rf, err := s.PayPal.GetRefund(ctx, *a.RefundID)
		if err == nil {
			remote = paymentFacts{status: rf.Status, amount: payPalAmount(rf.Amount)}
		}
		compare("refund", *a.RefundID, local, remote, err)
	}
	return out
}

func expectedOrderStatus(a store.AttemptRow) string {
	switch {
	case is(a.OrderStatus, store.OrderExpired):
		return store.OrderExpired
	case a.AuthorizationID != nil:
		return paypal.StatusCompleted
	}
	return text(a.OrderStatus)
}

var capturedAuthorization = []string{paypal.StatusCompleted, paypal.StatusPending, paypal.StatusRefunded, paypal.StatusPartiallyRefunded}

func expectedAuthorizationStatus(a store.AttemptRow) string {
	if a.CaptureID != nil && a.CaptureStatus != nil && slices.Contains(capturedAuthorization, *a.CaptureStatus) {
		return paypal.StatusCaptured
	}
	return text(a.AuthorizationStatus)
}

func expectedCaptureStatus(a store.AttemptRow) string {
	if a.RefundID != nil && is(a.RefundStatus, paypal.StatusCompleted) {
		return paypal.StatusRefunded
	}
	return text(a.CaptureStatus)
}

func reportedFees(b *paypal.SellerReceivableBreakdown, currency string) (fee, net string) {
	if b == nil {
		return "not reported", "not reported"
	}
	fee, net = amountText(domain.Money{Currency: currency}), "not reported"
	if b.PayPalFee != nil {
		fee = payPalAmount(*b.PayPalFee)
	}
	if b.NetAmount != nil {
		net = payPalAmount(*b.NetAmount)
	}
	return fee, net
}

func amountText(m domain.Money) string { return m.String() + " " + m.Currency }

func payPalAmount(m paypal.Money) string {
	d, err := domain.ParseMoney(m.Value, m.CurrencyCode)
	if err != nil {
		return strings.TrimSpace(m.Value + " " + m.CurrencyCode)
	}
	return amountText(d)
}

func orderNotFound(err error) bool {
	pe := paypal.Definitive(err)
	return pe != nil && pe.HTTPStatus == http.StatusNotFound
}

func payPalFailure(err error) (answer, debugID string) {
	var pe *paypal.Error
	if errors.As(err, &pe) {
		what := pe.Issue
		if what == "" {
			what = pe.Name
		}
		if what == "" {
			what = "HTTP " + strconv.Itoa(pe.HTTPStatus)
		}
		return "error " + what, pe.DebugID
	}
	return "no answer", ""
}

func text(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func is(s *string, want string) bool { return s != nil && *s == want }
