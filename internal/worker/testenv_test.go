package worker_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/clock"
	"github.com/aalvaropc/commitpool/internal/config"
	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/paypal"
	"github.com/aalvaropc/commitpool/internal/store"
	"github.com/aalvaropc/commitpool/internal/worker"
)

var t0 = time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)

const appBaseURL = "https://commitpool.example"

// env is a runner over a fresh test database and a fake PayPal, with a fake
// clock at t0 and a FUNDING workshop whose payment window closes 24 hours
// later (minimum 3, capacity 6, US$10).
type env struct {
	t        *testing.T
	ctx      context.Context
	st       *store.Store
	fake     *paypal.FakeServer
	api      paypal.API
	clock    *clock.Fake
	runner   *worker.Runner
	logs     *logBuffer
	workshop store.WorkshopRow
	proposal store.ProposalRow
}

type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newEnv(t *testing.T, edit ...func(*paypal.Config)) *env {
	t.Helper()
	ctx := context.Background()
	st := store.TestStore(t)
	fake := paypal.NewFakeServer()
	t.Cleanup(fake.Close)

	ppCfg := paypal.Config{
		BaseURL:      fake.URL,
		ClientID:     "fake-client",
		ClientSecret: "fake-secret",
		WebhookID:    "fake-webhook",
		SandboxMocks: true,
	}
	for _, e := range edit {
		e(&ppCfg)
	}
	api, err := paypal.New(ppCfg)
	if err != nil {
		t.Fatalf("paypal.New: %v", err)
	}

	clk := &clock.Fake{}
	clk.Set(t0)
	logs := &logBuffer{}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("worker log:\n%s", logs)
		}
	})
	r := worker.New(worker.Deps{
		Store:  st,
		PayPal: api,
		Clock:  clk,
		Cfg: config.Config{
			BaseURL:            appBaseURL,
			PayPalBaseURL:      fake.URL,
			PayPalClientID:     "fake-client",
			PayPalClientSecret: "fake-secret",
			SandboxMocks:       true,
			SeatTTL:            30 * time.Minute,
		},
		Log: slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})

	e := &env{t: t, ctx: ctx, st: st, fake: fake, api: api, clock: clk, runner: r, logs: logs}
	e.openWorkshop()
	return e
}

// withClientTimeout makes the PayPal client give up after d, so a request the
// fake holds with FailNext(op, 0, "") ends in a timeout.
func withClientTimeout(d time.Duration) func(*paypal.Config) {
	return func(c *paypal.Config) { c.HTTPClient = &http.Client{Timeout: d} }
}

func (e *env) openWorkshop() {
	e.t.Helper()
	e.workshop, e.proposal = e.fundingWorkshop()
}

// fundingWorkshop creates a workshop like the env's own, with payments open
// from t0 for 24 hours and the workshop starting 48 hours after t0.
func (e *env) fundingWorkshop() (store.WorkshopRow, store.ProposalRow) {
	e.t.Helper()
	starts := t0.Add(48 * time.Hour)
	w, _, _, err := e.st.CreateWorkshop(e.ctx, store.CreateWorkshopParams{
		Title:            "Go for operators",
		Description:      "Two hours of hands-on Go.",
		DurationMin:      180,
		Timezone:         "America/New_York",
		Minimum:          3,
		Capacity:         6,
		PriceMinor:       1000,
		Currency:         "USD",
		PaymentWindowMin: 1440,
		Dates:            []struct{ StartsAt, EndsAt time.Time }{{StartsAt: starts, EndsAt: starts.Add(3 * time.Hour)}},
	})
	if err != nil {
		e.t.Fatalf("CreateWorkshop: %v", err)
	}
	dates, err := e.st.CandidateDates(e.ctx, w.ID)
	if err != nil || len(dates) != 1 {
		e.t.Fatalf("CandidateDates = %d dates, %v", len(dates), err)
	}
	p, err := e.st.OpenProposal(e.ctx, store.ProposalRow{
		WorkshopID:   w.ID,
		ChosenDateID: dates[0].ID,
		PriceMinor:   1000,
		Currency:     "USD",
		Minimum:      3,
		Capacity:     6,
		OpensAt:      t0,
		ClosesAt:     t0.Add(24 * time.Hour),
		StartsAt:     starts,
	})
	if err != nil {
		e.t.Fatalf("OpenProposal: %v", err)
	}
	return w, p
}

// enroll reserves a seat for a new participant and opens its first payment
// attempt for the proposal's price.
func (e *env) enroll(name string) (store.EnrollmentRow, store.AttemptRow) {
	e.t.Helper()
	en, err := e.reserve(e.workshop, e.proposal, name)
	if err != nil {
		e.t.Fatalf("reserve(%s): %v", name, err)
	}
	a, err := e.st.CreateAttempt(e.ctx, en.ID, e.proposal.PriceMinor, e.proposal.Currency)
	if err != nil {
		e.t.Fatalf("CreateAttempt: %v", err)
	}
	return en, a
}

// reserve creates a participant of the workshop and reserves a seat for it
// now, with a 30-minute seat window. An error means no enrollment was created.
func (e *env) reserve(w store.WorkshopRow, p store.ProposalRow, name string) (store.EnrollmentRow, error) {
	e.t.Helper()
	pt, _, err := e.st.CreateParticipant(e.ctx, w.ID, name)
	if err != nil {
		e.t.Fatalf("CreateParticipant(%s): %v", name, err)
	}
	en, created, err := e.st.ReserveSeatAndCreateEnrollment(e.ctx, store.ReserveParams{
		WorkshopID:     w.ID,
		ParticipantID:  pt.ID,
		ProposalID:     p.ID,
		ConsentVersion: "consent-v1",
		Now:            e.clock.Now(),
		SeatTTL:        30 * time.Minute,
	})
	if err == nil && !created {
		e.t.Fatalf("ReserveSeatAndCreateEnrollment(%s) returned an existing enrollment, want a new one", name)
	}
	return en, err
}

// enqueue adds the attempt's job of this kind, due now, under its contract key.
func (e *env) enqueue(kind string, a store.AttemptRow) string {
	e.t.Helper()
	key := kind + ":" + a.ID
	e.enqueueKey(kind, key, a, e.clock.Now())
	return key
}

func (e *env) enqueueKey(kind, key string, a store.AttemptRow, runAt time.Time) {
	e.t.Helper()
	if err := e.runner.Enqueue(e.ctx, kind, key, &e.workshop.ID, &a.ID, nil, runAt); err != nil {
		e.t.Fatalf("Enqueue(%s): %v", key, err)
	}
}

func (e *env) runKey(key string) {
	e.t.Helper()
	if err := e.runner.RunKey(e.ctx, key); err != nil {
		e.t.Fatalf("RunKey(%s): %v", key, err)
	}
}

func (e *env) runOnce() {
	e.t.Helper()
	ran, err := e.runner.RunOnce(e.ctx)
	if err != nil || !ran {
		e.t.Fatalf("RunOnce = %v, %v; want a job run without error", ran, err)
	}
}

func (e *env) wantIdle() {
	e.t.Helper()
	if ran, err := e.runner.RunOnce(e.ctx); err != nil || ran {
		e.t.Fatalf("RunOnce = %v, %v; want no job left to run", ran, err)
	}
}

// runUntilIdle runs due jobs one at a time until none is left, without
// moving the clock.
func (e *env) runUntilIdle() {
	e.t.Helper()
	for range 200 {
		ran, err := e.runner.RunOnce(e.ctx)
		if err != nil {
			e.t.Fatalf("RunOnce: %v", err)
		}
		if !ran {
			return
		}
	}
	e.t.Fatalf("jobs still due after 200 runs")
}

func (e *env) advance(d time.Duration) { e.clock.Set(e.clock.Now().Add(d)) }

// closeKey is the key of the env proposal's close job.
func (e *env) closeKey() string { return "close:" + e.proposal.ID }

// enqueueClose adds the env proposal's close job, due when payments close,
// and reports whether it was created; opening the proposal already scheduled
// it, so this is false unless the test removed it.
func (e *env) enqueueClose() bool {
	e.t.Helper()
	created, err := e.runner.EnqueueOrRevive(e.ctx, "close", e.closeKey(), &e.workshop.ID, nil, nil, e.proposal.ClosesAt)
	if err != nil {
		e.t.Fatalf("EnqueueOrRevive(%s): %v", e.closeKey(), err)
	}
	return created
}

func (e *env) wantWorkshop(status domain.WorkshopStatus, resolution domain.Resolution) {
	e.t.Helper()
	w, err := e.st.WorkshopByID(e.ctx, e.workshop.ID)
	if err != nil {
		e.t.Fatalf("WorkshopByID: %v", err)
	}
	if w.Status != status || w.Resolution != resolution {
		e.t.Fatalf("workshop = %s/%s, want %s/%s", w.Status, w.Resolution, status, resolution)
	}
}

func (e *env) wantNoJob(key string) {
	e.t.Helper()
	j, err := e.st.JobByKey(e.ctx, key)
	if err != nil {
		e.t.Fatalf("JobByKey(%s): %v", key, err)
	}
	if j != nil {
		e.t.Fatalf("job %s exists (%s), want none", key, j.Status)
	}
}

// jobKeys lists the keys of every job of the attempt, oldest first.
func (e *env) jobKeys(a store.AttemptRow) []string {
	e.t.Helper()
	jobs, err := e.st.JobsByAttempt(e.ctx, a.ID)
	if err != nil {
		e.t.Fatalf("JobsByAttempt: %v", err)
	}
	keys := make([]string, len(jobs))
	for i, j := range jobs {
		keys[i] = j.IdempotencyKey
	}
	return keys
}

// requestsWithID counts the requests of op the fake received under this
// PayPal-Request-Id.
func (e *env) requestsWithID(op, requestID string) int {
	n := 0
	for _, r := range e.fake.Requests(op) {
		if r.RequestID == requestID {
			n++
		}
	}
	return n
}

// setMockCode stores a PayPal-Mock-Response code on the attempt, which the
// capture and refund handlers send because the env enables sandbox mocks.
func (e *env) setMockCode(a store.AttemptRow, code string) {
	e.t.Helper()
	if _, err := e.st.UpdateAttemptFunc(e.ctx, a.ID, func(row *store.AttemptRow) error {
		row.MockCode = &code
		return nil
	}); err != nil {
		e.t.Fatalf("UpdateAttemptFunc(mock code): %v", err)
	}
}

// capturePending drives the authorized attempt's capture to CAPTURE_PENDING:
// the capture request times out while the fake still captures, the fake then
// reports the capture PENDING, and the reconcile the timeout scheduled reads
// that back. It needs an env built withClientTimeout. It returns the capture id.
func (e *env) capturePending(en store.EnrollmentRow, a store.AttemptRow) string {
	e.t.Helper()
	e.fake.FailNext("capture", 0, "")
	e.runKey(e.enqueue("capture", a))
	e.fake.WaitHung()
	captures := e.order(*a.OrderID).PurchaseUnits[0].Payments.Captures
	if len(captures) != 1 {
		e.t.Fatalf("captures on the order = %d, want 1", len(captures))
	}
	e.fake.SetCaptureStatus(captures[0].ID, "PENDING", "PENDING_REVIEW")
	reconcileKey := "reconcile:" + a.ID + ":1"
	e.clock.Set(e.job(reconcileKey).RunAt)
	e.runKey(reconcileKey)
	e.wantStatus(en.ID, domain.EnrollmentCapturePending)
	return captures[0].ID
}

// refundPending drives the paid attempt's refund job, already enqueued, to
// REFUND_PENDING the same way capturePending does for a capture. It returns
// the refund id.
func (e *env) refundPending(en store.EnrollmentRow, a store.AttemptRow) string {
	e.t.Helper()
	e.fake.FailNext("refund", 0, "")
	e.runKey("refund:" + a.ID)
	e.fake.WaitHung()
	refunds := e.order(*a.OrderID).PurchaseUnits[0].Payments.Refunds
	if len(refunds) != 1 {
		e.t.Fatalf("refunds on the order = %d, want 1", len(refunds))
	}
	e.fake.SetRefundStatus(refunds[0].ID, "PENDING")
	reconcileKey := "reconcile:" + a.ID + ":1"
	e.clock.Set(e.job(reconcileKey).RunAt)
	e.runKey(reconcileKey)
	e.wantStatus(en.ID, domain.EnrollmentRefundPending)
	return refunds[0].ID
}

// periodicKey is the key RunPeriodic gives the attempt's reconcile at now.
func (e *env) periodicKey(a store.AttemptRow) string {
	return "reconcile:" + a.ID + ":periodic:" + e.clock.Now().UTC().Format("2006-01-02T15:04")
}

// reconcileNow enqueues a periodic reconcile of the attempt due now and
// returns its key.
func (e *env) reconcileNow(a store.AttemptRow) string {
	e.t.Helper()
	key := e.periodicKey(a)
	e.enqueueKey("reconcile", key, a, e.clock.Now())
	return key
}

func (e *env) attempt(id string) store.AttemptRow {
	e.t.Helper()
	a, err := e.st.AttemptByID(e.ctx, id)
	if err != nil || a == nil {
		e.t.Fatalf("AttemptByID(%s) = %+v, %v", id, a, err)
	}
	return *a
}

func (e *env) job(key string) store.Job {
	e.t.Helper()
	j, err := e.st.JobByKey(e.ctx, key)
	if err != nil || j == nil {
		e.t.Fatalf("JobByKey(%s) = %+v, %v; want a job", key, j, err)
	}
	return *j
}

func (e *env) wantJob(key, status string) store.Job {
	e.t.Helper()
	j := e.job(key)
	if j.Status != status {
		e.t.Fatalf("job %s status = %s (last_error %v), want %s", key, j.Status, deref(j.LastError), status)
	}
	return j
}

func (e *env) wantStatus(enrollmentID string, want domain.EnrollmentStatus) {
	e.t.Helper()
	en, err := e.st.EnrollmentByID(e.ctx, enrollmentID)
	if err != nil {
		e.t.Fatalf("EnrollmentByID: %v", err)
	}
	if en.Status != want {
		e.t.Fatalf("enrollment status = %s, want %s", en.Status, want)
	}
}

func (e *env) transition(enrollmentID string, from, to domain.EnrollmentStatus) {
	e.t.Helper()
	ok, err := e.st.TransitionEnrollment(e.ctx, enrollmentID, []domain.EnrollmentStatus{from}, to)
	if err != nil || !ok {
		e.t.Fatalf("TransitionEnrollment(%s -> %s) = %v, %v", from, to, ok, err)
	}
}

// order reads the order straight from the fake, as PayPal sees it.
func (e *env) order(id string) paypal.Order {
	e.t.Helper()
	o, err := e.api.GetOrder(e.ctx, id)
	if err != nil {
		e.t.Fatalf("GetOrder(%s): %v", id, err)
	}
	return o
}

// withOrder enrolls a participant and runs create_order for its attempt.
func (e *env) withOrder(name string) (store.EnrollmentRow, store.AttemptRow) {
	e.t.Helper()
	en, a := e.enroll(name)
	e.runKey(e.enqueue("create_order", a))
	a = e.attempt(a.ID)
	if a.OrderID == nil {
		e.t.Fatalf("create_order left no order id")
	}
	return en, a
}

// authorized enrolls a participant whose order is approved and authorized.
func (e *env) authorized(name string) (store.EnrollmentRow, store.AttemptRow) {
	e.t.Helper()
	en, a := e.withOrder(name)
	return en, e.authorize(en, a)
}

// authorize plays the buyer approving the attempt's order and runs its
// authorize job.
func (e *env) authorize(en store.EnrollmentRow, a store.AttemptRow) store.AttemptRow {
	e.t.Helper()
	e.fake.Approve(*a.OrderID)
	e.runKey(e.enqueue("authorize", a))
	e.wantStatus(en.ID, domain.EnrollmentAuthorized)
	return e.attempt(a.ID)
}

// paid enrolls a participant whose authorization has been captured.
func (e *env) paid(name string) (store.EnrollmentRow, store.AttemptRow) {
	e.t.Helper()
	en, a := e.authorized(name)
	e.runKey(e.enqueue("capture", a))
	e.wantStatus(en.ID, domain.EnrollmentPaid)
	return en, e.attempt(a.ID)
}

// assertLogsClean checks the worker never logged a credential, a token or
// a buyer approval link.
func (e *env) assertLogsClean() {
	e.t.Helper()
	logs := e.logs.String()
	if logs == "" {
		e.t.Fatalf("worker logged nothing; the check would be vacuous")
	}
	for _, secret := range []string{"fake-secret", "fake-token", "Bearer", "approve?order=", "token="} {
		if strings.Contains(logs, secret) {
			e.t.Errorf("worker log contains %q:\n%s", secret, logs)
		}
	}
}

func deref[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

func wantString(t *testing.T, name string, got *string, want string) {
	t.Helper()
	if got == nil || *got != want {
		t.Errorf("%s = %v, want %q", name, deref(got), want)
	}
}
