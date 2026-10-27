package worker_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/paypal"
	"github.com/aalvaropc/commitpool/internal/store"
)

func TestRunKey_RunsThatJobNowAndReturnsNotFoundForUnknownKey(t *testing.T) {
	e := newEnv(t)
	_, first := e.enroll("Ana")
	_, second := e.enroll("Ben")
	firstKey := e.enqueue("create_order", first)
	secondKey := e.enqueue("create_order", second)

	e.runKey(secondKey)

	e.wantJob(secondKey, "succeeded")
	if j := e.wantJob(firstKey, "pending"); j.Attempts != 0 {
		t.Errorf("other job attempts = %d, want 0 (never claimed)", j.Attempts)
	}
	reqs := e.fake.Requests("create_order")
	if len(reqs) != 1 || reqs[0].RequestID != secondKey {
		t.Fatalf("create_order requests = %+v, want one with PayPal-Request-Id %s", reqs, secondKey)
	}

	e.runKey(secondKey)
	if n := len(e.fake.Requests("create_order")); n != 1 {
		t.Errorf("RunKey on a finished job sent %d requests, want none", n-1)
	}

	_, later := e.enroll("Cleo")
	laterKey := "create_order:" + later.ID
	e.enqueueKey("create_order", laterKey, later, e.clock.Now().Add(time.Hour))
	e.runKey(laterKey)
	if j := e.wantJob(laterKey, "pending"); j.Attempts != 0 {
		t.Errorf("job not yet due attempts = %d, want 0", j.Attempts)
	}

	if err := e.runner.RunKey(e.ctx, "create_order:no-such-attempt"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("RunKey(unknown key) = %v, want store.ErrNotFound", err)
	}
}

func TestRunOnce_DispatchesRegisteredKindsAndRetriesUnknownKinds(t *testing.T) {
	e := newEnv(t)
	var ran []string
	e.runner.Register("release_seats", func(ctx context.Context, job *store.Job) (*int, error) {
		ran = append(ran, job.IdempotencyKey)
		return nil, nil
	})
	now := e.clock.Now()
	if err := e.runner.Enqueue(e.ctx, "release_seats", "release_seats:w1:2026-11-02T09:00", &e.workshop.ID, nil, nil, now); err != nil {
		t.Fatal(err)
	}
	if err := e.runner.Enqueue(e.ctx, "mystery", "mystery:1", nil, nil, nil, now); err != nil {
		t.Fatal(err)
	}

	e.runOnce()
	e.runOnce()

	if len(ran) != 1 || ran[0] != "release_seats:w1:2026-11-02T09:00" {
		t.Errorf("registered handler ran for %v, want the release_seats job once", ran)
	}
	e.wantJob("release_seats:w1:2026-11-02T09:00", "succeeded")
	j := e.wantJob("mystery:1", "pending")
	if j.Attempts != 1 || j.LastError == nil || *j.LastError != "unknown kind" {
		t.Errorf("unknown kind job attempts/last_error = %d/%v, want 1/\"unknown kind\"", j.Attempts, deref(j.LastError))
	}
	if !j.RunAt.Equal(now.Add(5 * time.Second)) {
		t.Errorf("unknown kind job run_at = %s, want the first backoff %s", j.RunAt, now.Add(5*time.Second))
	}
	e.wantIdle()
}

func TestRun_TakesBackStaleJobsRunsDueOnesAndStopsOnCancel(t *testing.T) {
	e := newEnv(t)
	ran := make(chan string, 4)
	e.runner.Register("release_seats", func(ctx context.Context, job *store.Job) (*int, error) {
		ran <- job.IdempotencyKey
		return nil, nil
	})
	staleAt := e.clock.Now().Add(-10 * time.Minute)
	if err := e.runner.Enqueue(e.ctx, "release_seats", "release_seats:stale", &e.workshop.ID, nil, nil, staleAt); err != nil {
		t.Fatal(err)
	}
	if j, err := e.st.ClaimJob(e.ctx, "crashed-worker", staleAt); err != nil || j == nil {
		t.Fatalf("ClaimJob(crashed-worker) = %+v, %v", j, err)
	}
	if err := e.runner.Enqueue(e.ctx, "release_seats", "release_seats:due", &e.workshop.ID, nil, nil, e.clock.Now()); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(e.ctx)
	done := make(chan error, 1)
	go func() { done <- e.runner.Run(ctx) }()

	got := map[string]bool{}
	for len(got) < 2 {
		select {
		case key := <-ran:
			got[key] = true
		case <-time.After(5 * time.Second):
			cancel()
			t.Fatalf("Run ran %v within 5s, want the stale and the due job", got)
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run returned %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	e.wantJob("release_seats:stale", "succeeded")
	e.wantJob("release_seats:due", "succeeded")
}

func TestRetry_FiveFailuresMarkNeedsAttention(t *testing.T) {
	e := newEnv(t)
	en, a := e.authorized("Ana")
	key := e.enqueue("capture", a)
	backoff := []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute}

	for i := range 5 {
		e.fake.FailNext("capture", 429, "RATE_LIMIT_REACHED")
		e.runOnce()
		j := e.job(key)
		if j.Attempts != i+1 || j.HTTPStatus == nil || *j.HTTPStatus != 429 {
			t.Fatalf("after failure %d: attempts/http_status = %d/%v, want %d/429", i+1, j.Attempts, deref(j.HTTPStatus), i+1)
		}
		wantString(t, "provider_issue", j.ProviderIssue, "RATE_LIMIT_REACHED")
		if i == 4 {
			break
		}
		if j.Status != "pending" || !j.RunAt.Equal(e.clock.Now().Add(backoff[i])) {
			t.Fatalf("after failure %d: status/run_at = %s/%s, want pending/%s", i+1, j.Status, j.RunAt, e.clock.Now().Add(backoff[i]))
		}
		e.wantIdle()
		e.advance(backoff[i])
	}

	e.wantJob(key, "needs_attention")
	e.wantStatus(en.ID, domain.EnrollmentNeedsAttention)
	reqs := e.fake.Requests("capture")
	if len(reqs) != 5 {
		t.Fatalf("capture requests = %d, want 5", len(reqs))
	}
	for i, r := range reqs {
		if r.RequestID != key {
			t.Errorf("capture request %d PayPal-Request-Id = %q, want %q", i+1, r.RequestID, key)
		}
	}
	if e.fake.Captures() != 0 {
		t.Errorf("captures = %d, want 0", e.fake.Captures())
	}
	if j, err := e.st.JobByKey(e.ctx, "reconcile:"+a.ID+":1"); err != nil || j != nil {
		t.Errorf("reconcile job after 429s = %+v, %v; want none (a 429 is not ambiguous)", j, err)
	}
	e.advance(time.Hour)
	e.wantIdle()
}

func TestReconcile_ReenqueuesOriginalKeyWhenResourceMissing(t *testing.T) {
	e := newEnv(t)
	en, a := e.authorized("Ana")
	e.fake.FailNext("capture", 503, "")
	key := e.enqueue("capture", a)

	e.runOnce()

	original := e.wantJob(key, "unknown")
	if original.HTTPStatus == nil || *original.HTTPStatus != 503 {
		t.Errorf("unknown job http_status = %v, want 503", deref(original.HTTPStatus))
	}
	e.wantStatus(en.ID, domain.EnrollmentCapturing)
	reconcileKey := "reconcile:" + a.ID + ":1"
	r := e.wantJob(reconcileKey, "pending")
	if !r.RunAt.Equal(e.clock.Now().Add(30 * time.Second)) {
		t.Errorf("reconcile run_at = %s, want now+30s", r.RunAt)
	}

	e.advance(31 * time.Second)
	e.runKey(reconcileKey)

	e.wantJob(reconcileKey, "succeeded")
	again := e.wantJob(key, "pending")
	if again.ID != original.ID || again.IdempotencyKey != key || !again.RunAt.Equal(e.clock.Now()) {
		t.Errorf("re-enqueued job = %s %s run_at %s, want the same row %s with key %s due now",
			again.ID, again.IdempotencyKey, again.RunAt, original.ID, key)
	}

	e.runOnce()

	e.wantStatus(en.ID, domain.EnrollmentPaid)
	e.wantJob(key, "succeeded")
	reqs := e.fake.Requests("capture")
	if len(reqs) != 2 || reqs[0].RequestID != key || reqs[1].RequestID != key {
		t.Fatalf("capture requests = %+v, want two, both with PayPal-Request-Id %s", reqs, key)
	}
	if e.fake.Captures() != 1 {
		t.Errorf("captures = %d, want 1", e.fake.Captures())
	}
	e.wantIdle()
}

func TestReconcile_DiscoversCaptureListedOnOrder(t *testing.T) {
	e := newEnv(t)
	en, a := e.authorized("Ana")
	captured, err := e.api.CaptureAuthorization(e.ctx, paypal.CaptureParams{AuthorizationID: *a.AuthorizationID, RequestID: "capture:" + a.ID})
	if err != nil {
		t.Fatalf("capture at PayPal: %v", err)
	}
	if a.CaptureID != nil {
		t.Fatalf("attempt already knows capture %s", *a.CaptureID)
	}

	key := "reconcile:" + a.ID + ":1"
	e.enqueueKey("reconcile", key, a, e.clock.Now())
	e.runKey(key)

	got := e.attempt(a.ID)
	wantString(t, "capture_id", got.CaptureID, captured.ID)
	wantString(t, "capture_status", got.CaptureStatus, "COMPLETED")
	wantString(t, "authorization_status", got.AuthorizationStatus, "CAPTURED")
	e.wantStatus(en.ID, domain.EnrollmentPaid)
	e.wantJob(key, "succeeded")
	if n := len(e.fake.Requests("capture")); n != 1 {
		t.Errorf("capture requests = %d, want only the one PayPal already had", n)
	}
}

func TestApply_NeverRegressesTerminalStatus(t *testing.T) {
	e := newEnv(t)
	cases := []struct {
		name     string
		status   domain.EnrollmentStatus
		apply    func(attemptID string) error
		recorded func(a store.AttemptRow) *string
		want     string
	}{
		{
			name:   "completed capture on a refunded enrollment",
			status: domain.EnrollmentRefunded,
			apply: func(id string) error {
				return e.runner.ApplyCapture(e.ctx, id, paypal.Capture{ID: "CAPTURE1", Status: "COMPLETED"})
			},
			recorded: func(a store.AttemptRow) *string { return a.CaptureStatus },
			want:     "COMPLETED",
		},
		{
			name:   "pending refund on a refunded enrollment",
			status: domain.EnrollmentRefunded,
			apply: func(id string) error {
				return e.runner.ApplyRefund(e.ctx, id, paypal.Refund{ID: "REFUND1", Status: "PENDING"})
			},
			recorded: func(a store.AttemptRow) *string { return a.RefundStatus },
			want:     "PENDING",
		},
		{
			name:   "created authorization on a voided enrollment",
			status: domain.EnrollmentVoided,
			apply: func(id string) error {
				return e.runner.ApplyAuthorization(e.ctx, id, paypal.Authorization{ID: "AUTH1", Status: "CREATED"})
			},
			recorded: func(a store.AttemptRow) *string { return a.AuthorizationStatus },
			want:     "CREATED",
		},
		{
			name:   "pending capture on a failed capture",
			status: domain.EnrollmentCaptureFailed,
			apply: func(id string) error {
				return e.runner.ApplyCapture(e.ctx, id, paypal.Capture{ID: "CAPTURE2", Status: "PENDING"})
			},
			recorded: func(a store.AttemptRow) *string { return a.CaptureStatus },
			want:     "PENDING",
		},
		{
			name:   "created authorization on a paid enrollment",
			status: domain.EnrollmentPaid,
			apply: func(id string) error {
				return e.runner.ApplyAuthorization(e.ctx, id, paypal.Authorization{ID: "AUTH2", Status: "CREATED"})
			},
			recorded: func(a store.AttemptRow) *string { return a.AuthorizationStatus },
			want:     "CREATED",
		},
	}
	for i, tc := range cases {
		en, a := e.enroll("Participant " + string(rune('A'+i)))
		e.transition(en.ID, domain.EnrollmentSeatReserved, tc.status)

		if err := tc.apply(a.ID); err != nil {
			t.Fatalf("%s: apply: %v", tc.name, err)
		}

		got, err := e.st.EnrollmentByID(e.ctx, en.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != tc.status {
			t.Errorf("%s: enrollment = %s, want it left at %s", tc.name, got.Status, tc.status)
		}
		if rec := tc.recorded(e.attempt(a.ID)); rec == nil || *rec != tc.want {
			t.Errorf("%s: attempt recorded %v, want PayPal's %q", tc.name, deref(rec), tc.want)
		}
	}
}

func TestApply_DifferentResourceIDMarksNeedsAttention(t *testing.T) {
	e := newEnv(t)
	en, a := e.paid("Ana")
	known := *a.CaptureID

	if err := e.runner.ApplyCapture(e.ctx, a.ID, paypal.Capture{ID: "SECONDCAPTURE", Status: "COMPLETED"}); err != nil {
		t.Fatalf("ApplyCapture: %v", err)
	}

	wantString(t, "capture_id", e.attempt(a.ID).CaptureID, known)
	e.wantStatus(en.ID, domain.EnrollmentNeedsAttention)
}
