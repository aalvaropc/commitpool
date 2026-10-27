package store_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/store"
)

const missingUUID = "00000000-0000-4000-8000-000000000000"

func assertTime(t *testing.T, name string, got, want time.Time) {
	t.Helper()
	if !got.Truncate(time.Microsecond).Equal(want.Truncate(time.Microsecond)) {
		t.Errorf("%s = %s, want %s", name, got.UTC().Format(time.RFC3339Nano), want.UTC().Format(time.RFC3339Nano))
	}
}

func enqueue(t *testing.T, s *store.Store, kind, key string, runAt time.Time) {
	t.Helper()
	created, err := s.EnqueueJob(context.Background(), kind, key, nil, nil, nil, runAt)
	if err != nil || !created {
		t.Fatalf("EnqueueJob(%s) = %v, %v; want true, nil", key, created, err)
	}
}

func jobByKey(t *testing.T, s *store.Store, key string) store.Job {
	t.Helper()
	j, err := s.JobByKey(context.Background(), key)
	if err != nil || j == nil {
		t.Fatalf("JobByKey(%s) = %+v, %v; want a job", key, j, err)
	}
	return *j
}

func claim(t *testing.T, s *store.Store, worker string, now time.Time) store.Job {
	t.Helper()
	j, err := s.ClaimJob(context.Background(), worker, now)
	if err != nil || j == nil {
		t.Fatalf("ClaimJob(%s) = %+v, %v; want a job", worker, j, err)
	}
	return *j
}

func jobLock(t *testing.T, s *store.Store, id string) (lockedAt *time.Time, lockedBy *string) {
	t.Helper()
	err := s.Pool.QueryRow(context.Background(), `select locked_at, locked_by from jobs where id = $1`, id).Scan(&lockedAt, &lockedBy)
	if err != nil {
		t.Fatalf("read lock of job %s: %v", id, err)
	}
	return lockedAt, lockedBy
}

func assertUnlocked(t *testing.T, s *store.Store, id string) {
	t.Helper()
	at, by := jobLock(t, s, id)
	if at != nil {
		t.Errorf("job %s locked_at = %s, want cleared", id, at.UTC().Format(time.RFC3339))
	}
	if by != nil {
		t.Errorf("job %s locked_by = %q, want cleared", id, *by)
	}
}

func TestEnqueueJob_StoresRowWithDefaults(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	e := newEnrollment(t, s)
	a, err := s.CreateAttempt(ctx, e.ID, 1000, "USD")
	if err != nil {
		t.Fatalf("CreateAttempt: %v", err)
	}
	runAt := baseTime.Add(time.Hour)
	key := "create_order:" + a.ID

	created, err := s.EnqueueJob(ctx, "create_order", key, &e.WorkshopID, &a.ID, map[string]any{"amount_minor": 1000}, runAt)
	if err != nil || !created {
		t.Fatalf("EnqueueJob = %v, %v; want true, nil", created, err)
	}

	j := jobByKey(t, s, key)
	if j.ID == "" || j.Kind != "create_order" || j.IdempotencyKey != key {
		t.Errorf("identity = %q %q %q, want id set, create_order, %s", j.ID, j.Kind, j.IdempotencyKey, key)
	}
	if j.WorkshopID == nil || *j.WorkshopID != e.WorkshopID || j.AttemptID == nil || *j.AttemptID != a.ID {
		t.Errorf("workshop/attempt = %v/%v, want %s/%s", j.WorkshopID, j.AttemptID, e.WorkshopID, a.ID)
	}
	assertJSONEqual(t, "payload", j.Payload, []byte(`{"amount_minor":1000}`))
	if j.Status != "pending" || j.Attempts != 0 {
		t.Errorf("status/attempts = %s/%d, want pending/0", j.Status, j.Attempts)
	}
	assertTime(t, "run_at", j.RunAt, runAt)
	if j.HTTPStatus != nil || j.ProviderIssue != nil || j.DebugID != nil || j.LastError != nil {
		t.Errorf("failure columns = %v %v %v %v, want all nil", j.HTTPStatus, j.ProviderIssue, j.DebugID, j.LastError)
	}
}

func TestEnqueueJob_NilPayloadStoresEmptyObject(t *testing.T) {
	s := store.TestStore(t)
	enqueue(t, s, "close", "close:p1", baseTime)

	j := jobByKey(t, s, "close:p1")
	assertJSONEqual(t, "payload", j.Payload, []byte(`{}`))
	if j.WorkshopID != nil || j.AttemptID != nil {
		t.Errorf("workshop/attempt = %v/%v, want nil/nil", j.WorkshopID, j.AttemptID)
	}
}

func TestEnqueue_SameKeyIsNoop(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	key := "authorize:attempt-1"

	created, err := s.EnqueueJob(ctx, "authorize", key, nil, nil, map[string]string{"order_id": "first"}, baseTime)
	if err != nil || !created {
		t.Fatalf("first EnqueueJob = %v, %v; want true, nil", created, err)
	}
	claim(t, s, "w1", baseTime)

	created, err = s.EnqueueJob(ctx, "authorize", key, nil, nil, map[string]string{"order_id": "second"}, baseTime.Add(time.Hour))
	if err != nil {
		t.Fatalf("second EnqueueJob: %v", err)
	}
	if created {
		t.Errorf("second EnqueueJob created = true, want false")
	}

	if n := count(t, s, `select count(*) from jobs where idempotency_key = $1`, key); n != 1 {
		t.Errorf("rows with key = %d, want 1", n)
	}
	j := jobByKey(t, s, key)
	assertJSONEqual(t, "payload", j.Payload, []byte(`{"order_id":"first"}`))
	if j.Status != "running" || j.Attempts != 1 {
		t.Errorf("status/attempts = %s/%d, want running/1 (untouched)", j.Status, j.Attempts)
	}
	assertTime(t, "run_at", j.RunAt, baseTime)
}

func TestEnqueueJob_ConcurrentSameKeyCreatesOne(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	const callers = 8

	var wg sync.WaitGroup
	var mu sync.Mutex
	createdCount := 0
	start := make(chan struct{})
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			created, err := s.EnqueueJob(ctx, "reconcile", "reconcile:a1:1", nil, nil, nil, baseTime)
			if err != nil {
				t.Errorf("EnqueueJob: %v", err)
				return
			}
			if created {
				mu.Lock()
				createdCount++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()

	if createdCount != 1 {
		t.Errorf("callers that created the job = %d, want 1", createdCount)
	}
	if n := count(t, s, `select count(*) from jobs`); n != 1 {
		t.Errorf("rows = %d, want 1", n)
	}
}

func TestClaimJob_ClaimsPendingAndReturnsPostUpdateState(t *testing.T) {
	s := store.TestStore(t)
	enqueue(t, s, "capture", "capture:a1", baseTime)

	now := baseTime.Add(time.Minute)
	j := claim(t, s, "worker-1", now)

	if j.Kind != "capture" || j.IdempotencyKey != "capture:a1" {
		t.Errorf("claimed %s %s, want capture capture:a1", j.Kind, j.IdempotencyKey)
	}
	if j.Status != "running" || j.Attempts != 1 {
		t.Errorf("returned status/attempts = %s/%d, want running/1", j.Status, j.Attempts)
	}
	stored := jobByKey(t, s, "capture:a1")
	if stored.Status != "running" || stored.Attempts != 1 {
		t.Errorf("stored status/attempts = %s/%d, want running/1", stored.Status, stored.Attempts)
	}
	at, by := jobLock(t, s, j.ID)
	if at == nil || by == nil {
		t.Fatalf("lock = (%v, %v), want set", at, by)
	}
	assertTime(t, "locked_at", *at, now)
	if *by != "worker-1" {
		t.Errorf("locked_by = %q, want worker-1", *by)
	}
}

func TestClaimJob_ReturnsNilWhenNothingClaimable(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)

	if j, err := s.ClaimJob(ctx, "w1", baseTime); err != nil || j != nil {
		t.Fatalf("ClaimJob on empty queue = %+v, %v; want nil, nil", j, err)
	}

	enqueue(t, s, "capture", "capture:a1", baseTime)
	claim(t, s, "w1", baseTime)
	if j, err := s.ClaimJob(ctx, "w2", baseTime); err != nil || j != nil {
		t.Fatalf("ClaimJob with only a running job = %+v, %v; want nil, nil", j, err)
	}
}

func TestClaimJob_RespectsRunAt(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	runAt := baseTime.Add(10 * time.Minute)
	enqueue(t, s, "close", "close:p1", runAt)

	if j, err := s.ClaimJob(ctx, "w1", runAt.Add(-time.Second)); err != nil || j != nil {
		t.Fatalf("ClaimJob before run_at = %+v, %v; want nil, nil", j, err)
	}
	if got := jobByKey(t, s, "close:p1"); got.Status != "pending" || got.Attempts != 0 {
		t.Errorf("job after early claim = %s/%d, want pending/0", got.Status, got.Attempts)
	}

	j := claim(t, s, "w1", runAt)
	if j.IdempotencyKey != "close:p1" || j.Status != "running" {
		t.Errorf("claim at run_at = %s %s, want close:p1 running", j.IdempotencyKey, j.Status)
	}
}

func TestClaimJob_OldestRunAtFirst(t *testing.T) {
	s := store.TestStore(t)
	enqueue(t, s, "capture", "capture:late", baseTime.Add(2*time.Minute))
	enqueue(t, s, "capture", "capture:early", baseTime)
	enqueue(t, s, "capture", "capture:middle", baseTime.Add(time.Minute))

	var got []string
	for range 3 {
		got = append(got, claim(t, s, "w1", baseTime.Add(time.Hour)).IdempotencyKey)
	}
	want := []string{"capture:early", "capture:middle", "capture:late"}
	if !slices.Equal(got, want) {
		t.Errorf("claim order = %v, want %v", got, want)
	}
}

func TestClaimJob_TwoWorkersNeverRunSameJob(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	const total = 40

	want := make(map[string]bool, total)
	for i := range total {
		key := fmt.Sprintf("capture:attempt-%02d", i)
		enqueue(t, s, "capture", key, baseTime)
		want[jobByKey(t, s, key).ID] = true
	}

	workers := []string{"worker-a", "worker-b"}
	claimed := make([][]store.Job, len(workers))
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i, name := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for {
				j, err := s.ClaimJob(ctx, name, baseTime)
				if err != nil {
					t.Errorf("%s: ClaimJob: %v", name, err)
					return
				}
				if j == nil {
					return
				}
				claimed[i] = append(claimed[i], *j)
			}
		}()
	}
	close(start)
	wg.Wait()

	seen := make(map[string]bool, total)
	for i, jobs := range claimed {
		for _, j := range jobs {
			if seen[j.ID] {
				t.Errorf("job %s was claimed more than once", j.ID)
			}
			seen[j.ID] = true
			if j.Status != "running" || j.Attempts != 1 {
				t.Errorf("job %s status/attempts = %s/%d, want running/1", j.ID, j.Status, j.Attempts)
			}
		}
		if n := count(t, s, `select count(*) from jobs where locked_by = $1`, workers[i]); n != len(jobs) {
			t.Errorf("%s: rows locked by it = %d, claimed %d", workers[i], n, len(jobs))
		}
	}
	if len(seen) != total {
		t.Errorf("distinct jobs claimed = %d, want %d", len(seen), total)
	}
	for id := range want {
		if !seen[id] {
			t.Errorf("job %s was never claimed", id)
		}
	}
	if n := count(t, s, `select count(*) from jobs where status = 'running' and attempts = 1`); n != total {
		t.Errorf("running jobs with attempts = 1 in the database = %d, want %d", n, total)
	}
	t.Logf("claims per worker: %s=%d %s=%d", workers[0], len(claimed[0]), workers[1], len(claimed[1]))
}

func TestCompleteJob_SucceedsAndClearsLock(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	enqueue(t, s, "authorize", "authorize:a1", baseTime)
	j := claim(t, s, "w1", baseTime)

	if err := s.CompleteJob(ctx, j.ID, ptr(201)); err != nil {
		t.Fatalf("CompleteJob: %v", err)
	}

	got := jobByKey(t, s, "authorize:a1")
	if got.Status != "succeeded" {
		t.Errorf("status = %s, want succeeded", got.Status)
	}
	if got.HTTPStatus == nil || *got.HTTPStatus != 201 {
		t.Errorf("http_status = %v, want 201", got.HTTPStatus)
	}
	assertUnlocked(t, s, j.ID)
	if next, err := s.ClaimJob(ctx, "w2", baseTime.Add(time.Hour)); err != nil || next != nil {
		t.Errorf("ClaimJob after completion = %+v, %v; want nil, nil", next, err)
	}
}

func TestRetryJob_BackoffThenNeedsAttention(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	enqueue(t, s, "capture", "capture:a1", baseTime)

	backoff := []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute}
	now := baseTime
	for attempt := 1; attempt <= 4; attempt++ {
		j := claim(t, s, "w1", now)
		if j.Attempts != attempt {
			t.Fatalf("claim #%d: attempts = %d, want %d", attempt, j.Attempts, attempt)
		}
		needsAttention, err := s.RetryJob(ctx, j.ID, now, "boom", ptr(503), nil, nil)
		if err != nil || needsAttention {
			t.Fatalf("RetryJob after attempt %d = %v, %v; want false, nil", attempt, needsAttention, err)
		}

		got := jobByKey(t, s, "capture:a1")
		want := now.Add(backoff[attempt-1])
		if got.Status != "pending" {
			t.Errorf("after attempt %d: status = %s, want pending", attempt, got.Status)
		}
		assertTime(t, fmt.Sprintf("run_at after attempt %d", attempt), got.RunAt, want)
		assertUnlocked(t, s, j.ID)

		if early, err := s.ClaimJob(ctx, "w2", want.Add(-time.Second)); err != nil || early != nil {
			t.Fatalf("ClaimJob one second before the backoff ends = %+v, %v; want nil, nil", early, err)
		}
		now = want
	}

	j := claim(t, s, "w1", now)
	if j.Attempts != 5 {
		t.Fatalf("claim #5: attempts = %d, want 5", j.Attempts)
	}
	needsAttention, err := s.RetryJob(ctx, j.ID, now, "boom", ptr(503), nil, nil)
	if err != nil || !needsAttention {
		t.Fatalf("RetryJob after attempt 5 = %v, %v; want true, nil", needsAttention, err)
	}
	got := jobByKey(t, s, "capture:a1")
	if got.Status != "needs_attention" || got.Attempts != 5 {
		t.Errorf("status/attempts = %s/%d, want needs_attention/5", got.Status, got.Attempts)
	}
	assertUnlocked(t, s, j.ID)
	if next, err := s.ClaimJob(ctx, "w2", now.Add(24*time.Hour)); err != nil || next != nil {
		t.Errorf("ClaimJob of a needs_attention job = %+v, %v; want nil, nil", next, err)
	}
}

func TestRetryJob_StoresFailureDetailsAndReplacesThemOnNextRetry(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	enqueue(t, s, "capture", "capture:a1", baseTime)

	j := claim(t, s, "w1", baseTime)
	if _, err := s.RetryJob(ctx, j.ID, baseTime, "authorization expired", ptr(422), ptr("AUTHORIZATION_EXPIRED"), ptr("dbg-123")); err != nil {
		t.Fatalf("RetryJob: %v", err)
	}
	got := jobByKey(t, s, "capture:a1")
	if got.LastError == nil || *got.LastError != "authorization expired" {
		t.Errorf("last_error = %v, want authorization expired", got.LastError)
	}
	if got.HTTPStatus == nil || *got.HTTPStatus != 422 {
		t.Errorf("http_status = %v, want 422", got.HTTPStatus)
	}
	if got.ProviderIssue == nil || *got.ProviderIssue != "AUTHORIZATION_EXPIRED" {
		t.Errorf("provider_issue = %v, want AUTHORIZATION_EXPIRED", got.ProviderIssue)
	}
	if got.DebugID == nil || *got.DebugID != "dbg-123" {
		t.Errorf("debug_id = %v, want dbg-123", got.DebugID)
	}

	j = claim(t, s, "w1", got.RunAt)
	if _, err := s.RetryJob(ctx, j.ID, got.RunAt, "network error", nil, nil, nil); err != nil {
		t.Fatalf("second RetryJob: %v", err)
	}
	got = jobByKey(t, s, "capture:a1")
	if got.LastError == nil || *got.LastError != "network error" {
		t.Errorf("last_error = %v, want network error", got.LastError)
	}
	if got.HTTPStatus != nil || got.ProviderIssue != nil || got.DebugID != nil {
		t.Errorf("http/issue/debug = %v %v %v, want all cleared by the latest failure", got.HTTPStatus, got.ProviderIssue, got.DebugID)
	}
}

func TestMarkJobUnknown_ParksJobAndClearsLock(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	enqueue(t, s, "refund", "refund:a1", baseTime)
	j := claim(t, s, "w1", baseTime)

	if err := s.MarkJobUnknown(ctx, j.ID, "request timed out", nil); err != nil {
		t.Fatalf("MarkJobUnknown: %v", err)
	}

	got := jobByKey(t, s, "refund:a1")
	if got.Status != "unknown" || got.Attempts != 1 {
		t.Errorf("status/attempts = %s/%d, want unknown/1", got.Status, got.Attempts)
	}
	if got.LastError == nil || *got.LastError != "request timed out" || got.HTTPStatus != nil {
		t.Errorf("last_error/http_status = %v/%v, want request timed out/nil", got.LastError, got.HTTPStatus)
	}
	assertUnlocked(t, s, j.ID)
	if next, err := s.ClaimJob(ctx, "w2", baseTime.Add(time.Hour)); err != nil || next != nil {
		t.Errorf("ClaimJob of an unknown job = %+v, %v; want nil, nil", next, err)
	}

	if err := s.MarkJobUnknown(ctx, j.ID, "gateway timeout", ptr(504)); err != nil {
		t.Fatalf("MarkJobUnknown with status: %v", err)
	}
	got = jobByKey(t, s, "refund:a1")
	if got.HTTPStatus == nil || *got.HTTPStatus != 504 {
		t.Errorf("http_status = %v, want 504", got.HTTPStatus)
	}
}

func TestJobTransitions_UnknownJobIsNotFound(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)

	for _, id := range []string{missingUUID, "not-a-uuid"} {
		if err := s.CompleteJob(ctx, id, nil); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("CompleteJob(%s) = %v, want ErrNotFound", id, err)
		}
		if _, err := s.RetryJob(ctx, id, baseTime, "boom", nil, nil, nil); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("RetryJob(%s) = %v, want ErrNotFound", id, err)
		}
		if err := s.MarkJobUnknown(ctx, id, "timeout", nil); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("MarkJobUnknown(%s) = %v, want ErrNotFound", id, err)
		}
	}
}

func TestReenqueueJob_KeepsKeyAndResetsRunAt(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	enqueue(t, s, "capture", "capture:a1", baseTime)
	j := claim(t, s, "w1", baseTime)
	if err := s.MarkJobUnknown(ctx, j.ID, "request timed out", nil); err != nil {
		t.Fatalf("MarkJobUnknown: %v", err)
	}

	now := baseTime.Add(30 * time.Second)
	if err := s.ReenqueueJob(ctx, "capture:a1", now); err != nil {
		t.Fatalf("ReenqueueJob: %v", err)
	}

	got := jobByKey(t, s, "capture:a1")
	if got.ID != j.ID || got.IdempotencyKey != "capture:a1" {
		t.Errorf("id/key = %s/%s, want the same row %s/capture:a1", got.ID, got.IdempotencyKey, j.ID)
	}
	if got.Status != "pending" {
		t.Errorf("status = %s, want pending", got.Status)
	}
	assertTime(t, "run_at", got.RunAt, now)
	if got.Attempts != 1 {
		t.Errorf("attempts = %d, want 1 (not reset)", got.Attempts)
	}
	assertUnlocked(t, s, j.ID)
	if n := count(t, s, `select count(*) from jobs`); n != 1 {
		t.Errorf("rows = %d, want 1", n)
	}

	again := claim(t, s, "w2", now)
	if again.ID != j.ID || again.Attempts != 2 {
		t.Errorf("reclaimed job = %s attempts %d, want %s attempts 2", again.ID, again.Attempts, j.ID)
	}
}

func TestReenqueueJob_ValidFromFailedAndNeedsAttention(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	enqueue(t, s, "void", "void:a1", baseTime.Add(time.Hour))
	enqueue(t, s, "refund", "refund:a1", baseTime.Add(time.Hour))
	if _, err := s.Pool.Exec(ctx, `update jobs set status = 'failed' where idempotency_key = 'void:a1'`); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	if _, err := s.Pool.Exec(ctx, `update jobs set status = 'needs_attention', attempts = 5 where idempotency_key = 'refund:a1'`); err != nil {
		t.Fatalf("set needs_attention: %v", err)
	}

	for _, key := range []string{"void:a1", "refund:a1"} {
		if err := s.ReenqueueJob(ctx, key, baseTime); err != nil {
			t.Fatalf("ReenqueueJob(%s): %v", key, err)
		}
		got := jobByKey(t, s, key)
		if got.Status != "pending" {
			t.Errorf("%s status = %s, want pending", key, got.Status)
		}
		assertTime(t, key+" run_at", got.RunAt, baseTime)
	}
}

func TestReenqueueJob_LeavesOtherStatusesAlone(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	enqueue(t, s, "authorize", "authorize:done", baseTime)
	enqueue(t, s, "authorize", "authorize:busy", baseTime)
	enqueue(t, s, "authorize", "authorize:waiting", baseTime.Add(time.Hour))
	done := claim(t, s, "w1", baseTime)
	if done.IdempotencyKey != "authorize:done" {
		t.Fatalf("first claim = %s, want authorize:done", done.IdempotencyKey)
	}
	if err := s.CompleteJob(ctx, done.ID, ptr(201)); err != nil {
		t.Fatalf("CompleteJob: %v", err)
	}
	busy := claim(t, s, "w1", baseTime)
	if busy.IdempotencyKey != "authorize:busy" {
		t.Fatalf("second claim = %s, want authorize:busy", busy.IdempotencyKey)
	}

	later := baseTime.Add(2 * time.Hour)
	for _, key := range []string{"authorize:done", "authorize:busy", "authorize:waiting"} {
		if err := s.ReenqueueJob(ctx, key, later); err != nil {
			t.Fatalf("ReenqueueJob(%s): %v", key, err)
		}
	}

	if got := jobByKey(t, s, "authorize:done"); got.Status != "succeeded" {
		t.Errorf("succeeded job status = %s, want succeeded", got.Status)
	}
	if got := jobByKey(t, s, "authorize:busy"); got.Status != "running" || got.ID != busy.ID {
		t.Errorf("running job status = %s, want running", got.Status)
	}
	if at, by := jobLock(t, s, busy.ID); at == nil || by == nil {
		t.Errorf("running job lock = (%v, %v), want kept", at, by)
	}
	got := jobByKey(t, s, "authorize:waiting")
	if got.Status != "pending" {
		t.Errorf("pending job status = %s, want pending", got.Status)
	}
	assertTime(t, "pending job run_at", got.RunAt, baseTime.Add(time.Hour))
}

func TestReenqueueJob_MissingKeyIsNotFound(t *testing.T) {
	s := store.TestStore(t)
	if err := s.ReenqueueJob(context.Background(), "capture:missing", baseTime); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("ReenqueueJob(missing) = %v, want ErrNotFound", err)
	}
}

func TestJobByKey_MissingIsNil(t *testing.T) {
	s := store.TestStore(t)
	if j, err := s.JobByKey(context.Background(), "capture:missing"); err != nil || j != nil {
		t.Errorf("JobByKey(missing) = %+v, %v; want nil, nil", j, err)
	}
}

func TestJobsByAttempt_OrderedByCreation(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	e := newEnrollment(t, s)
	a1, err := s.CreateAttempt(ctx, e.ID, 1000, "USD")
	if err != nil {
		t.Fatalf("CreateAttempt #1: %v", err)
	}
	a2, err := s.CreateAttempt(ctx, e.ID, 1000, "USD")
	if err != nil {
		t.Fatalf("CreateAttempt #2: %v", err)
	}
	for _, kind := range []string{"create_order", "authorize", "capture"} {
		if _, err := s.EnqueueJob(ctx, kind, kind+":"+a1.ID, nil, &a1.ID, nil, baseTime); err != nil {
			t.Fatalf("EnqueueJob(%s): %v", kind, err)
		}
	}
	if _, err := s.EnqueueJob(ctx, "create_order", "create_order:"+a2.ID, nil, &a2.ID, nil, baseTime); err != nil {
		t.Fatalf("EnqueueJob for attempt 2: %v", err)
	}
	enqueue(t, s, "close", "close:p1", baseTime)

	jobs, err := s.JobsByAttempt(ctx, a1.ID)
	if err != nil {
		t.Fatalf("JobsByAttempt: %v", err)
	}
	var kinds []string
	for _, j := range jobs {
		kinds = append(kinds, j.Kind)
	}
	if want := []string{"create_order", "authorize", "capture"}; !slices.Equal(kinds, want) {
		t.Errorf("kinds = %v, want %v", kinds, want)
	}

	none, err := s.JobsByAttempt(ctx, missingUUID)
	if err != nil || none == nil || len(none) != 0 {
		t.Errorf("JobsByAttempt(unknown) = %v, %v; want an empty non-nil slice", none, err)
	}
}

func TestRequeueStale_RecoversAbandonedRunningJobs(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	enqueue(t, s, "capture", "capture:abandoned", baseTime)
	enqueue(t, s, "capture", "capture:done", baseTime)
	enqueue(t, s, "capture", "capture:fresh", baseTime)
	enqueue(t, s, "capture", "capture:pending", baseTime.Add(24*time.Hour))

	abandoned := claim(t, s, "crashed-worker", baseTime)
	done := claim(t, s, "w1", baseTime)
	if err := s.CompleteJob(ctx, done.ID, ptr(201)); err != nil {
		t.Fatalf("CompleteJob: %v", err)
	}
	fresh := claim(t, s, "w2", baseTime.Add(9*time.Minute))
	if abandoned.IdempotencyKey != "capture:abandoned" || done.IdempotencyKey != "capture:done" || fresh.IdempotencyKey != "capture:fresh" {
		t.Fatalf("claims = %s, %s, %s; want abandoned, done, fresh in creation order",
			abandoned.IdempotencyKey, done.IdempotencyKey, fresh.IdempotencyKey)
	}

	now := baseTime.Add(10*time.Minute + time.Second)
	n, err := s.RequeueStale(ctx, now, 10*time.Minute)
	if err != nil {
		t.Fatalf("RequeueStale: %v", err)
	}
	if n != 1 {
		t.Fatalf("RequeueStale count = %d, want 1", n)
	}

	got := jobByKey(t, s, abandoned.IdempotencyKey)
	if got.Status != "pending" || got.Attempts != 1 {
		t.Errorf("abandoned job status/attempts = %s/%d, want pending/1", got.Status, got.Attempts)
	}
	assertTime(t, "abandoned run_at", got.RunAt, now)
	assertUnlocked(t, s, abandoned.ID)

	if got := jobByKey(t, s, fresh.IdempotencyKey); got.Status != "running" {
		t.Errorf("recently claimed job status = %s, want running", got.Status)
	}
	if at, by := jobLock(t, s, fresh.ID); at == nil || by == nil {
		t.Errorf("recently claimed job lock = (%v, %v), want kept", at, by)
	}
	if got := jobByKey(t, s, "capture:done"); got.Status != "succeeded" {
		t.Errorf("succeeded job status = %s, want succeeded", got.Status)
	}
	if got := jobByKey(t, s, "capture:pending"); got.Status != "pending" {
		t.Errorf("pending job status = %s, want pending", got.Status)
	}

	again := claim(t, s, "w3", now)
	if again.ID != abandoned.ID || again.Attempts != 2 {
		t.Errorf("reclaimed job = %s attempts %d, want %s attempts 2", again.ID, again.Attempts, abandoned.ID)
	}

	if n, err := s.RequeueStale(ctx, now, 10*time.Minute); err != nil || n != 0 {
		t.Errorf("second RequeueStale = %d, %v; want 0, nil", n, err)
	}
}
