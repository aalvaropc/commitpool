package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
	_ "time/tzdata"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/clock"
	"github.com/aalvaropc/commitpool/internal/config"
	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/httpapi"
	"github.com/aalvaropc/commitpool/internal/mail"
	"github.com/aalvaropc/commitpool/internal/mcpserver"
	"github.com/aalvaropc/commitpool/internal/paypal"
	"github.com/aalvaropc/commitpool/internal/paypal/paypaltest"
	"github.com/aalvaropc/commitpool/internal/store"
	"github.com/aalvaropc/commitpool/internal/store/storetest"
	"github.com/aalvaropc/commitpool/internal/worker"
)

var (
	t0       = time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	satStart = time.Date(2026, 11, 14, 20, 0, 0, 0, time.UTC)
	sunStart = time.Date(2026, 11, 15, 15, 0, 0, 0, time.UTC)
)

const (
	appBaseURL = "https://commitpool.example"
	indexHTML  = `<!doctype html><html lang="en"><head><meta charset="UTF-8" /><meta name="description" content="The class happens, or nobody pays." /><title>CommitPool</title></head><body><div id="root"></div></body></html>`
)

type env struct {
	t          *testing.T
	ctx        context.Context
	background *sync.WaitGroup
	st         *store.Store
	fake       *paypaltest.Server
	clock      *clock.Fake
	runner     *worker.Runner
	cfg        config.Config
	h          http.Handler
	mcp        *mcp.Server
	logs       *logBuffer
	mailer     *mail.Memory
	explained  chan struct{}
}

type setup struct {
	cfg    config.Config
	ai     ai.Interpreter
	paypal func(paypal.API) paypal.API
}

type option func(*setup)

func withDemoMode() option     { return func(s *setup) { s.cfg.DemoMode = true } }
func withSandboxMocks() option { return func(s *setup) { s.cfg.SandboxMocks = true } }

const testWebhookID = "WH-ID-COMMITPOOL-TEST"

func withWebhookID() option { return func(s *setup) { s.cfg.PayPalWebhookID = testWebhookID } }
func withRateLimit(perMinute int) option {
	return func(s *setup) { s.cfg.RateLimitPerMin = perMinute }
}
func withAI(i ai.Interpreter) option {
	return func(s *setup) { s.ai = i }
}

func withPayPal(wrap func(paypal.API) paypal.API) option {
	return func(s *setup) { s.paypal = wrap }
}

type gatedPayPal struct {
	paypal.API
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newGate(t *testing.T) *gatedPayPal {
	g := &gatedPayPal{entered: make(chan struct{}, 1), release: make(chan struct{})}
	t.Cleanup(g.open)
	return g
}

func (g *gatedPayPal) wrap(api paypal.API) paypal.API {
	g.API = api
	return g
}

func (g *gatedPayPal) open() { g.once.Do(func() { close(g.release) }) }

func (g *gatedPayPal) CreateOrder(ctx context.Context, p paypal.CreateOrderParams) (paypal.Order, error) {
	select {
	case g.entered <- struct{}{}:
	default:
	}
	select {
	case <-g.release:
	case <-ctx.Done():
		return paypal.Order{}, ctx.Err()
	}
	return g.API.CreateOrder(ctx, p)
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

func newEnv(t *testing.T, opts ...option) *env {
	t.Helper()
	ctx := context.Background()
	st := storetest.New(t)
	fake := paypaltest.NewServer()
	t.Cleanup(fake.Close)

	s := setup{
		cfg: config.Config{
			BaseURL:            appBaseURL,
			PayPalBaseURL:      fake.URL,
			PayPalClientID:     "fake-client",
			PayPalClientSecret: "fake-secret",
			AIProvider:         config.AIProviderFake,
			SeatTTL:            30 * time.Minute,
			FakePayPal:         true,
		},
		ai: ai.Fake{},
	}
	for _, o := range opts {
		o(&s)
	}
	client, err := paypal.New(paypal.Config{
		BaseURL:      fake.URL,
		ClientID:     s.cfg.PayPalClientID,
		ClientSecret: s.cfg.PayPalClientSecret,
		WebhookID:    s.cfg.PayPalWebhookID,
		SandboxMocks: s.cfg.SandboxMocks,
	})
	if err != nil {
		t.Fatalf("paypal.New: %v", err)
	}
	var api paypal.API = client
	if s.paypal != nil {
		api = s.paypal(api)
	}

	explained := make(chan struct{}, 64)
	httpapi.SetAfterExplanation(t, func() {
		select {
		case explained <- struct{}{}:
		default:
		}
	})

	clk := &clock.Fake{}
	clk.Set(t0)
	logs := &logBuffer{}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("server log:\n%s", logs)
		}
	})
	log := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	mailer := &mail.Memory{}
	runner := worker.New(worker.Deps{Store: st, PayPal: api, Clock: clk, Cfg: s.cfg, Log: log, Mailer: mailer})
	assistant := mcpserver.New(mcpserver.Config{Secret: []byte("test-confirmation-secret"), Clock: clk, Log: log})
	background := &sync.WaitGroup{}
	t.Cleanup(background.Wait)
	h := httpapi.New(httpapi.Deps{
		Background: background,
		MCP:        assistant,
		Store:      st,
		Worker:     runner,
		PayPal:     api,
		AI:         s.ai,
		Clock:      clk,
		Cfg:        s.cfg,
		Log:        log,
		Dist: fstest.MapFS{
			"index.html":    {Data: []byte(indexHTML)},
			"assets/app.js": {Data: []byte("console.log('commitpool')")},
		},
	})
	return &env{t: t, ctx: ctx, background: background, st: st, fake: fake, clock: clk, runner: runner, cfg: s.cfg, h: h, mcp: assistant.MCP(), mailer: mailer, logs: logs, explained: explained}
}

func (e *env) request(method, path string, body any) *httptest.ResponseRecorder {
	e.t.Helper()
	var reader io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		reader = strings.NewReader(b)
	case []byte:
		reader = bytes.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			e.t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequestWithContext(e.ctx, method, path, reader)
	if reader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func (e *env) call(method, path string, body any, want int, out any) {
	e.t.Helper()
	rec := e.request(method, path, body)
	if rec.Code != want {
		e.t.Fatalf("%s %s = %d %s, want %d", method, path, rec.Code, rec.Body.String(), want)
	}
	if out != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			e.t.Fatalf("%s %s: decode %q: %v", method, path, rec.Body.String(), err)
		}
	}
}

func (e *env) wantError(method, path string, body any, status int, code string) errorJSON {
	e.t.Helper()
	rec := e.request(method, path, body)
	if rec.Code != status {
		e.t.Fatalf("%s %s = %d %s, want %d %s", method, path, rec.Code, rec.Body.String(), status, code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		e.t.Errorf("%s %s Content-Type = %q, want JSON", method, path, ct)
	}
	var got errorJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		e.t.Fatalf("%s %s: decode error %q: %v", method, path, rec.Body.String(), err)
	}
	if got.Error != code || got.Message == "" {
		e.t.Fatalf("%s %s error = %+v, want code %q with a message", method, path, got, code)
	}
	return got
}

func workshopRequest() map[string]any {
	return map[string]any{
		"title":        "Go for operators",
		"description":  "Three hours of hands-on Go.",
		"duration_min": 180,
		"timezone":     "America/Lima",
		"minimum":      2,
		"capacity":     3,
		"price":        "10.00",
		"currency":     "USD",
		"dates": []map[string]string{
			{"starts_at": satStart.Format(time.RFC3339), "ends_at": satStart.Add(3 * time.Hour).Format(time.RFC3339)},
			{"starts_at": sunStart.Format(time.RFC3339), "ends_at": sunStart.Add(3 * time.Hour).Format(time.RFC3339)},
		},
	}
}

type workshopHandle struct {
	created    createdJSON
	ownerToken string
	shareToken string
	ownerPath  string
	sharePath  string
	satID      string
	sunID      string
}

func (e *env) createWorkshop(edit ...func(map[string]any)) workshopHandle {
	e.t.Helper()
	body := workshopRequest()
	for _, ed := range edit {
		ed(body)
	}
	var h workshopHandle
	e.call(http.MethodPost, "/api/v1/workshops", body, http.StatusCreated, &h.created)
	h.ownerToken = strings.TrimPrefix(h.created.OwnerURL, appBaseURL+"/owner/")
	h.shareToken = strings.TrimPrefix(h.created.ShareURL, appBaseURL+"/w/")
	h.ownerPath = "/api/v1/owner/" + h.ownerToken
	h.sharePath = "/api/v1/w/" + h.shareToken
	view := e.owner(h)
	for _, d := range view.Dates {
		switch {
		case d.StartsAt.Equal(satStart):
			h.satID = d.ID
		case d.StartsAt.Equal(sunStart):
			h.sunID = d.ID
		}
	}
	return h
}

func (e *env) owner(h workshopHandle) ownerViewJSON {
	e.t.Helper()
	var v ownerViewJSON
	e.call(http.MethodGet, h.ownerPath, nil, http.StatusOK, &v)
	return v
}

func (e *env) ownerRequest(h workshopHandle) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, httptest.NewRequestWithContext(e.ctx, http.MethodGet, h.ownerPath, nil))
	return rec
}

func (e *env) settledOwner(h workshopHandle) ownerViewJSON {
	e.t.Helper()
	for {
		v := e.owner(h)
		pending := v.Recommendation.ExplanationPending
		if pending == nil {
			e.t.Fatalf("recommendation has no explanation_pending: %+v", v.Recommendation)
		}
		if !*pending {
			return v
		}
		select {
		case <-e.explained:
		case <-time.After(10 * time.Second):
			e.t.Fatalf("the explanation is still pending after 10s")
		}
	}
}

type participantHandle struct {
	token string
	path  string
}

func (e *env) join(h workshopHandle, name string) participantHandle {
	e.t.Helper()
	var out struct {
		ParticipantURL string `json:"participant_url"`
	}
	e.call(http.MethodPost, h.sharePath+"/participants", map[string]string{"display_name": name}, http.StatusCreated, &out)
	token := strings.TrimPrefix(out.ParticipantURL, appBaseURL+"/p/")
	return participantHandle{token: token, path: "/api/v1/p/" + token}
}

func (e *env) say(p participantHandle, message string) availabilityJSON {
	e.t.Helper()
	var out struct {
		Availability availabilityJSON `json:"availability"`
	}
	e.call(http.MethodPost, p.path+"/availability", map[string]string{"message": message}, http.StatusCreated, &out)
	return out.Availability
}

func (e *env) joinAndSay(h workshopHandle, name, message string) participantHandle {
	e.t.Helper()
	p := e.join(h, name)
	e.say(p, message)
	return p
}

func (e *env) openProposal(h workshopHandle, dateID string) proposalJSON {
	e.t.Helper()
	var out struct {
		Proposal proposalJSON `json:"proposal"`
	}
	e.call(http.MethodPost, h.ownerPath+"/proposal", map[string]string{"date_id": dateID}, http.StatusCreated, &out)
	return out.Proposal
}

func enrollBody(proposalID string) map[string]string {
	return map[string]string{"consent_version": httpapi.ConsentVersion, "proposal_id": proposalID}
}

func (e *env) enroll(p participantHandle, proposalID string, want int) enrollJSON {
	e.t.Helper()
	var out enrollJSON
	e.call(http.MethodPost, p.path+"/enrollment", enrollBody(proposalID), want, &out)
	return out
}

func (e *env) latestAttempt(enrollmentID string) store.AttemptRow {
	e.t.Helper()
	a, err := e.st.LatestAttempt(e.ctx, enrollmentID)
	if err != nil || a == nil {
		e.t.Fatalf("LatestAttempt(%s) = %+v, %v", enrollmentID, a, err)
	}
	return *a
}

func (e *env) runPayment(kind, workshopID string, a store.AttemptRow) {
	e.t.Helper()
	key := kind + ":" + a.ID
	if err := e.runner.Enqueue(e.ctx, kind, key, &workshopID, &a.ID, nil, e.clock.Now()); err != nil {
		e.t.Fatalf("Enqueue(%s): %v", key, err)
	}
	if err := e.runner.RunKey(e.ctx, key); err != nil {
		e.t.Fatalf("RunKey(%s): %v", key, err)
	}
}

func (e *env) authorize(workshopID, enrollmentID string) store.AttemptRow {
	e.t.Helper()
	a := e.latestAttempt(enrollmentID)
	if a.OrderID == nil {
		e.t.Fatalf("attempt %s has no order", a.ID)
	}
	e.fake.Approve(*a.OrderID)
	e.runPayment("authorize", workshopID, a)
	e.wantStatus(enrollmentID, domain.EnrollmentAuthorized)
	return e.latestAttempt(enrollmentID)
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

func (e *env) job(key string) store.Job {
	e.t.Helper()
	j, err := e.st.JobByKey(e.ctx, key)
	if err != nil || j == nil {
		e.t.Fatalf("JobByKey(%s) = %+v, %v; want a job", key, j, err)
	}
	return *j
}

type errorJSON struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Reason  string `json:"reason"`
}

type moneyJSON struct {
	Minor    int64  `json:"minor"`
	Currency string `json:"currency"`
	Value    string `json:"value"`
}

type workshopJSON struct {
	ID               string     `json:"id"`
	Title            string     `json:"title"`
	Description      string     `json:"description"`
	DurationMin      int        `json:"duration_min"`
	Timezone         string     `json:"timezone"`
	Minimum          int        `json:"minimum"`
	Capacity         int        `json:"capacity"`
	Price            moneyJSON  `json:"price"`
	Currency         string     `json:"currency"`
	PaymentWindowMin int        `json:"payment_window_min"`
	Status           string     `json:"status"`
	Resolution       string     `json:"resolution"`
	HostName         string     `json:"host_name"`
	Location         string     `json:"location"`
	VenueCost        *moneyJSON `json:"venue_cost"`
	BreakEvenSeats   int        `json:"break_even_seats"`
}

type createdJSON struct {
	Workshop workshopJSON `json:"workshop"`
	OwnerURL string       `json:"owner_url"`
	ShareURL string       `json:"share_url"`
}

type dateJSON struct {
	ID       string    `json:"id"`
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
	Label    string    `json:"label"`
}

type proposalJSON struct {
	ID           string    `json:"id"`
	Version      int       `json:"version"`
	DateID       string    `json:"date_id"`
	Price        moneyJSON `json:"price"`
	Minimum      int       `json:"minimum"`
	Capacity     int       `json:"capacity"`
	OpensAt      time.Time `json:"opens_at"`
	ClosesAt     time.Time `json:"closes_at"`
	StartsAt     time.Time `json:"starts_at"`
	PaymentsOpen bool      `json:"payments_open"`
	CalendarPath string    `json:"calendar_path"`
}

type tallyJSON struct {
	Interested int  `json:"interested"`
	Reserved   *int `json:"reserved"`
	Authorized int  `json:"authorized"`
	Capturing  *int `json:"capturing"`
	Pending    *int `json:"pending"`
	Paid       int  `json:"paid"`
	Failed     *int `json:"failed"`
	SeatsTaken *int `json:"seats_taken"`
	Minimum    int  `json:"minimum"`
	Capacity   int  `json:"capacity"`
}

type paymentJSON struct {
	AttemptID           string  `json:"attempt_id"`
	AttemptNo           int     `json:"attempt_no"`
	OrderID             *string `json:"order_id"`
	AuthorizationID     *string `json:"authorization_id"`
	CaptureID           *string `json:"capture_id"`
	RefundID            *string `json:"refund_id"`
	OrderStatus         *string `json:"order_status"`
	AuthorizationStatus *string `json:"authorization_status"`
	CaptureStatus       *string `json:"capture_status"`
	CaptureIssue        *string `json:"capture_issue"`
	RefundStatus        *string `json:"refund_status"`
}

type enrollmentJSON struct {
	ID                  string       `json:"id"`
	ParticipantID       string       `json:"participant_id"`
	DisplayName         string       `json:"display_name"`
	Status              string       `json:"status"`
	StatusLabel         string       `json:"status_label"`
	SeatExpiresAt       time.Time    `json:"seat_expires_at"`
	ApprovalCancelledAt *time.Time   `json:"approval_cancelled_at"`
	Payment             *paymentJSON `json:"payment"`
}

type availabilityJSON struct {
	ID                 string                 `json:"id"`
	ParticipantID      string                 `json:"participant_id"`
	DisplayName        string                 `json:"display_name"`
	Message            string                 `json:"message"`
	Interpretation     domain.Interpretation  `json:"interpretation"`
	Corrected          *domain.Interpretation `json:"corrected"`
	NeedsClarification bool                   `json:"needs_clarification"`
}

type recommendationJSON struct {
	DateID       string `json:"date_id"`
	MeetsMinimum bool   `json:"meets_minimum"`
	PerDate      []struct {
		DateID     string            `json:"date_id"`
		Compatible []string          `json:"compatible"`
		Blockers   map[string]string `json:"blockers"`
	} `json:"per_date"`
	Explanation        *string `json:"explanation"`
	ExplanationPending *bool   `json:"explanation_pending"`
}

type ownerViewJSON struct {
	ShareURL     string        `json:"share_url"`
	Workshop     workshopJSON  `json:"workshop"`
	Dates        []dateJSON    `json:"dates"`
	Proposal     *proposalJSON `json:"proposal"`
	Tally        tallyJSON     `json:"tally"`
	Participants []struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"participants"`
	Availabilities []availabilityJSON `json:"availabilities"`
	Recommendation recommendationJSON `json:"recommendation"`
	Enrollments    []enrollmentJSON   `json:"enrollments"`
	Demo           struct {
		DemoMode     bool `json:"demo_mode"`
		SandboxMocks bool `json:"sandbox_mocks"`
	} `json:"demo"`
}

type participantViewJSON struct {
	Participant struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"participant"`
	Workshop     workshopJSON      `json:"workshop"`
	Dates        []dateJSON        `json:"dates"`
	Proposal     *proposalJSON     `json:"proposal"`
	Availability *availabilityJSON `json:"availability"`
	Enrollment   *enrollmentJSON   `json:"enrollment"`
	Tally        tallyJSON         `json:"tally"`
	Consent      struct {
		Version string `json:"version"`
		Text    string `json:"text"`
	} `json:"consent"`
}

type enrollJSON struct {
	EnrollmentID string `json:"enrollment_id"`
	ApproveURL   string `json:"approve_url"`
	Status       string `json:"status"`
	StatusLabel  string `json:"status_label"`
}
