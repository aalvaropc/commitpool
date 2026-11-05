package store_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/store"
	"github.com/aalvaropc/commitpool/internal/store/storetest"
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
	s := storetest.New(t)
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
	s := storetest.New(t)
	enqueue(t, s, "close", "close:p1", baseTime)

	j := jobByKey(t, s, "close:p1")
	assertJSONEqual(t, "payload", j.Payload, []byte(`{}`))
	if j.WorkshopID != nil || j.AttemptID != nil {
		t.Errorf("workshop/attempt = %v/%v, want nil/nil", j.WorkshopID, j.AttemptID)
	}
}

func TestEnqueue_SameKeyIsNoop(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
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
	s := storetest.New(t)
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
	s := storetest.New(t)
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
	s := storetest.New(t)

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
	s := storetest.New(t)
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
	s := storetest.New(t)
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
	s := storetest.New(t)
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

func TestClaimJobByKey_ClaimsOnlyThatJob(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	enqueue(t, s, "capture", "capture:older", baseTime)
	enqueue(t, s, "capture", "capture:wanted", baseTime.Add(time.Minute))

	now := baseTime.Add(time.Hour)
	j, err := s.ClaimJobByKey(ctx, "capture:wanted", "worker-1", now)
	if err != nil || j == nil {
		t.Fatalf("ClaimJobByKey = %+v, %v; want the job", j, err)
	}
	if j.IdempotencyKey != "capture:wanted" || j.Status != "running" || j.Attempts != 1 {
		t.Errorf("claimed %s %s/%d, want capture:wanted running/1", j.IdempotencyKey, j.Status, j.Attempts)
	}
	at, by := jobLock(t, s, j.ID)
	if at == nil || by == nil {
		t.Fatalf("lock = (%v, %v), want set", at, by)
	}
	assertTime(t, "locked_at", *at, now)
	if *by != "worker-1" {
		t.Errorf("locked_by = %q, want worker-1", *by)
	}
	if other := jobByKey(t, s, "capture:older"); other.Status != "pending" || other.Attempts != 0 {
		t.Errorf("older job = %s/%d, want pending/0", other.Status, other.Attempts)
	}

	if got, err := s.ClaimJobByKey(ctx, "capture:missing", "worker-1", now); err != nil || got != nil {
		t.Errorf("ClaimJobByKey(missing key) = %+v, %v; want nil, nil", got, err)
	}
}

func TestClaimJobByKey_SkipsRunningAndFuture(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	enqueue(t, s, "capture", "capture:running", baseTime)
	enqueue(t, s, "capture", "capture:future", baseTime.Add(10*time.Minute))
	enqueue(t, s, "capture", "capture:done", baseTime)
	enqueue(t, s, "capture", "capture:locked", baseTime)

	if _, err := s.ClaimJobByKey(ctx, "capture:running", "w1", baseTime); err != nil {
		t.Fatalf("first ClaimJobByKey: %v", err)
	}
	done, err := s.ClaimJobByKey(ctx, "capture:done", "w1", baseTime)
	if err != nil || done == nil {
		t.Fatalf("ClaimJobByKey(done) = %+v, %v", done, err)
	}
	if err := s.CompleteJob(ctx, done.ID, nil); err != nil {
		t.Fatalf("CompleteJob: %v", err)
	}

	for _, key := range []string{"capture:running", "capture:future", "capture:done"} {
		if got, err := s.ClaimJobByKey(ctx, key, "w2", baseTime); err != nil || got != nil {
			t.Errorf("ClaimJobByKey(%s) = %+v, %v; want nil, nil", key, got, err)
		}
	}
	if got := jobByKey(t, s, "capture:running"); got.Status != "running" || got.Attempts != 1 {
		t.Errorf("running job = %s/%d, want running/1 (not claimed twice)", got.Status, got.Attempts)
	}
	if got := jobByKey(t, s, "capture:future"); got.Status != "pending" || got.Attempts != 0 {
		t.Errorf("future job = %s/%d, want pending/0", got.Status, got.Attempts)
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `select id from jobs where idempotency_key = 'capture:locked' for update`); err != nil {
		t.Fatalf("lock row: %v", err)
	}
	if got, err := s.ClaimJobByKey(ctx, "capture:locked", "w2", baseTime); err != nil || got != nil {
		t.Errorf("ClaimJobByKey(row locked by another claimer) = %+v, %v; want nil, nil", got, err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	if got, err := s.ClaimJobByKey(ctx, "capture:future", "w2", baseTime.Add(10*time.Minute)); err != nil || got == nil {
		t.Errorf("ClaimJobByKey(future) once due = %+v, %v; want the job", got, err)
	}
}

func TestCompleteJob_SucceedsAndClearsLock(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
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

func TestCompleteJob_AcceptsUnknownJob(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	enqueue(t, s, "capture", "capture:a1", baseTime)
	j := claim(t, s, "w1", baseTime)
	if err := s.MarkJobUnknown(ctx, j.ID, "request timed out", nil); err != nil {
		t.Fatalf("MarkJobUnknown: %v", err)
	}

	if err := s.CompleteJob(ctx, j.ID, ptr(200)); err != nil {
		t.Fatalf("CompleteJob on an unknown job: %v", err)
	}

	got := jobByKey(t, s, "capture:a1")
	if got.Status != "succeeded" {
		t.Errorf("status = %s, want succeeded", got.Status)
	}
	if got.HTTPStatus == nil || *got.HTTPStatus != 200 {
		t.Errorf("http_status = %v, want 200", got.HTTPStatus)
	}
	assertUnlocked(t, s, j.ID)
}

func TestCompleteJob_RefusesJobThatIsNotRunningOrUnknown(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	enqueue(t, s, "capture", "capture:pending", baseTime.Add(time.Hour))
	enqueue(t, s, "capture", "capture:done", baseTime)
	enqueue(t, s, "capture", "capture:parked", baseTime)
	done := claim(t, s, "w1", baseTime)
	if err := s.CompleteJob(ctx, done.ID, ptr(201)); err != nil {
		t.Fatalf("CompleteJob: %v", err)
	}
	parked := claim(t, s, "w1", baseTime)
	if _, err := s.Pool.Exec(ctx, `update jobs set status = 'needs_attention', attempts = 5, locked_at = null, locked_by = null where id = $1`, parked.ID); err != nil {
		t.Fatalf("park job: %v", err)
	}

	wantStatus := map[string]string{"capture:pending": "pending", "capture:done": "succeeded", "capture:parked": "needs_attention"}
	for key, status := range wantStatus {
		j := jobByKey(t, s, key)
		if err := s.CompleteJob(ctx, j.ID, ptr(200)); !errors.Is(err, store.ErrJobNotActive) {
			t.Errorf("CompleteJob(%s) = %v, want ErrJobNotActive", key, err)
		}
		got := jobByKey(t, s, key)
		if got.Status != status {
			t.Errorf("%s status after refusal = %s, want %s", key, got.Status, status)
		}
	}
	if got := jobByKey(t, s, "capture:done"); got.HTTPStatus == nil || *got.HTTPStatus != 201 {
		t.Errorf("succeeded job http_status = %v, want 201 kept", got.HTTPStatus)
	}
}

func TestRetryJob_BackoffThenNeedsAttention(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
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
	s := storetest.New(t)
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

func TestRetryJob_RefusesSucceededJob(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	enqueue(t, s, "capture", "capture:a1", baseTime)
	j := claim(t, s, "w1", baseTime)
	if err := s.CompleteJob(ctx, j.ID, ptr(201)); err != nil {
		t.Fatalf("CompleteJob: %v", err)
	}

	needsAttention, err := s.RetryJob(ctx, j.ID, baseTime.Add(time.Minute), "late failure", ptr(503), ptr("SERVICE_UNAVAILABLE"), ptr("dbg-late"))
	if !errors.Is(err, store.ErrJobNotActive) || needsAttention {
		t.Fatalf("RetryJob on a succeeded job = %v, %v; want false, ErrJobNotActive", needsAttention, err)
	}

	got := jobByKey(t, s, "capture:a1")
	if got.Status != "succeeded" || got.Attempts != 1 {
		t.Errorf("status/attempts = %s/%d, want succeeded/1", got.Status, got.Attempts)
	}
	if got.LastError != nil || got.ProviderIssue != nil || got.DebugID != nil {
		t.Errorf("last_error/issue/debug = %v %v %v, want the late failure not stored", got.LastError, got.ProviderIssue, got.DebugID)
	}
	if got.HTTPStatus == nil || *got.HTTPStatus != 201 {
		t.Errorf("http_status = %v, want 201 kept", got.HTTPStatus)
	}
	assertTime(t, "run_at", got.RunAt, baseTime)
	assertUnlocked(t, s, j.ID)
}

func TestRetryJob_RefusesJobThatIsNotRunning(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	enqueue(t, s, "capture", "capture:pending", baseTime.Add(time.Hour))
	enqueue(t, s, "capture", "capture:unknown", baseTime)
	unknown := claim(t, s, "w1", baseTime)
	if err := s.MarkJobUnknown(ctx, unknown.ID, "request timed out", nil); err != nil {
		t.Fatalf("MarkJobUnknown: %v", err)
	}

	for key, status := range map[string]string{"capture:pending": "pending", "capture:unknown": "unknown"} {
		j := jobByKey(t, s, key)
		if _, err := s.RetryJob(ctx, j.ID, baseTime, "boom", nil, nil, nil); !errors.Is(err, store.ErrJobNotActive) {
			t.Errorf("RetryJob(%s) = %v, want ErrJobNotActive", key, err)
		}
		if got := jobByKey(t, s, key); got.Status != status {
			t.Errorf("%s status after refusal = %s, want %s", key, got.Status, status)
		}
	}
}

func TestMarkJobUnknown_ParksJobAndClearsLock(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	enqueue(t, s, "refund", "refund:a1", baseTime)
	j := claim(t, s, "w1", baseTime)
	if _, err := s.RetryJob(ctx, j.ID, baseTime, "declined", ptr(422), ptr("INSTRUMENT_DECLINED"), ptr("dbg-1")); err != nil {
		t.Fatalf("RetryJob: %v", err)
	}
	retryAt := baseTime.Add(5 * time.Second)
	j = claim(t, s, "w1", retryAt)

	if err := s.MarkJobUnknown(ctx, j.ID, "request timed out", nil); err != nil {
		t.Fatalf("MarkJobUnknown: %v", err)
	}

	got := jobByKey(t, s, "refund:a1")
	if got.Status != "unknown" || got.Attempts != 2 {
		t.Errorf("status/attempts = %s/%d, want unknown/2", got.Status, got.Attempts)
	}
	if got.LastError == nil || *got.LastError != "request timed out" {
		t.Errorf("last_error = %v, want request timed out", got.LastError)
	}
	if got.HTTPStatus != nil || got.ProviderIssue != nil || got.DebugID != nil {
		t.Errorf("http/issue/debug = %v %v %v, want all cleared so the row does not mix two failures", got.HTTPStatus, got.ProviderIssue, got.DebugID)
	}
	assertUnlocked(t, s, j.ID)
	if next, err := s.ClaimJob(ctx, "w2", retryAt.Add(time.Hour)); err != nil || next != nil {
		t.Errorf("ClaimJob of an unknown job = %+v, %v; want nil, nil", next, err)
	}

	if err := s.MarkJobUnknown(ctx, j.ID, "again", ptr(504)); !errors.Is(err, store.ErrJobNotActive) {
		t.Errorf("MarkJobUnknown on an unknown job = %v, want ErrJobNotActive", err)
	}
	if got := jobByKey(t, s, "refund:a1"); got.LastError == nil || *got.LastError != "request timed out" || got.HTTPStatus != nil {
		t.Errorf("refused call changed the row: last_error/http_status = %v/%v", got.LastError, got.HTTPStatus)
	}
}

func TestMarkJobUnknown_StoresHTTPStatus(t *testing.T) {
	s := storetest.New(t)
	enqueue(t, s, "refund", "refund:a1", baseTime)
	j := claim(t, s, "w1", baseTime)

	if err := s.MarkJobUnknown(context.Background(), j.ID, "gateway timeout", ptr(504)); err != nil {
		t.Fatalf("MarkJobUnknown: %v", err)
	}
	got := jobByKey(t, s, "refund:a1")
	if got.HTTPStatus == nil || *got.HTTPStatus != 504 {
		t.Errorf("http_status = %v, want 504", got.HTTPStatus)
	}
}

func TestMarkJobUnknown_RefusesNonRunningJob(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	enqueue(t, s, "refund", "refund:pending", baseTime.Add(time.Hour))
	enqueue(t, s, "refund", "refund:done", baseTime)
	done := claim(t, s, "w1", baseTime)
	if err := s.CompleteJob(ctx, done.ID, ptr(201)); err != nil {
		t.Fatalf("CompleteJob: %v", err)
	}
	pending := jobByKey(t, s, "refund:pending")

	for _, j := range []store.Job{pending, done} {
		if err := s.MarkJobUnknown(ctx, j.ID, "request timed out", ptr(504)); !errors.Is(err, store.ErrJobNotActive) {
			t.Errorf("MarkJobUnknown(%s) = %v, want ErrJobNotActive", j.IdempotencyKey, err)
		}
	}
	if got := jobByKey(t, s, "refund:pending"); got.Status != "pending" || got.LastError != nil {
		t.Errorf("pending job after refusal = %s/%v, want pending/nil", got.Status, got.LastError)
	}
	got := jobByKey(t, s, "refund:done")
	if got.Status != "succeeded" || got.LastError != nil {
		t.Errorf("succeeded job after refusal = %s/%v, want succeeded/nil", got.Status, got.LastError)
	}
	if got.HTTPStatus == nil || *got.HTTPStatus != 201 {
		t.Errorf("succeeded job http_status = %v, want 201 kept", got.HTTPStatus)
	}
}

func TestFailJob_MarksFailedAndIsReenqueueable(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	enqueue(t, s, "authorize", "authorize:a1", baseTime)
	j := claim(t, s, "w1", baseTime)

	if err := s.FailJob(ctx, j.ID, "paypal: HTTP 422 (ORDER_NOT_APPROVED)", ptr(422), ptr("ORDER_NOT_APPROVED"), ptr("dbg-1")); err != nil {
		t.Fatalf("FailJob: %v", err)
	}

	got := jobByKey(t, s, "authorize:a1")
	if got.Status != "failed" || got.Attempts != 1 {
		t.Errorf("status/attempts = %s/%d, want failed/1", got.Status, got.Attempts)
	}
	if got.LastError == nil || *got.LastError != "paypal: HTTP 422 (ORDER_NOT_APPROVED)" ||
		got.HTTPStatus == nil || *got.HTTPStatus != 422 ||
		got.ProviderIssue == nil || *got.ProviderIssue != "ORDER_NOT_APPROVED" ||
		got.DebugID == nil || *got.DebugID != "dbg-1" {
		t.Errorf("failure columns = %v %v %v %v, want the refusal", got.LastError, got.HTTPStatus, got.ProviderIssue, got.DebugID)
	}
	assertUnlocked(t, s, j.ID)

	if err := s.FailJob(ctx, j.ID, "again", nil, nil, nil); !errors.Is(err, store.ErrJobNotActive) {
		t.Errorf("FailJob on a failed job = %v, want ErrJobNotActive", err)
	}
	if err := s.FailJob(ctx, missingUUID, "missing", nil, nil, nil); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("FailJob on a missing job = %v, want ErrNotFound", err)
	}

	if err := s.ReenqueueJob(ctx, "authorize:a1", baseTime.Add(time.Minute)); err != nil {
		t.Fatalf("ReenqueueJob: %v", err)
	}
	revived := jobByKey(t, s, "authorize:a1")
	if revived.ID != j.ID || revived.Status != "pending" || revived.Attempts != 1 {
		t.Errorf("revived job = %s %s/%d, want the same row %s pending with attempts kept at 1", revived.ID, revived.Status, revived.Attempts, j.ID)
	}
	assertTime(t, "revived run_at", revived.RunAt, baseTime.Add(time.Minute))
	again := claim(t, s, "w2", baseTime.Add(time.Minute))
	if again.IdempotencyKey != "authorize:a1" || again.Attempts != 2 {
		t.Errorf("claimed %s with attempts %d, want authorize:a1 with attempts 2", again.IdempotencyKey, again.Attempts)
	}
}

func TestJobTransitions_UnknownJobIsNotFound(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)

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

func TestReviveJob_PolicyAndSingleStatement(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	failed := func(key string, httpStatus *int, issue *string) {
		t.Helper()
		enqueue(t, s, "void", key, baseTime)
		j, err := s.ClaimJobByKey(ctx, key, "w1", baseTime)
		if err != nil || j == nil {
			t.Fatalf("ClaimJobByKey(%s) = %+v, %v", key, j, err)
		}
		if err := s.FailJob(ctx, j.ID, "refused", httpStatus, issue, nil); err != nil {
			t.Fatalf("FailJob(%s): %v", key, err)
		}
	}
	failed("void:skipped", nil, nil)
	failed("void:refused", ptr(422), ptr("PREVIOUSLY_CAPTURED"))
	failed("void:refused-without-issue", ptr(400), nil)
	enqueue(t, s, "void", "void:exhausted", baseTime)
	if _, err := s.Pool.Exec(ctx, `update jobs set status = 'needs_attention', attempts = 5 where idempotency_key = 'void:exhausted'`); err != nil {
		t.Fatal(err)
	}
	enqueue(t, s, "void", "void:pending", baseTime)
	enqueue(t, s, "void", "void:running", baseTime)
	if j, err := s.ClaimJobByKey(ctx, "void:running", "w1", baseTime); err != nil || j == nil {
		t.Fatalf("claim void:running = %+v, %v", j, err)
	}
	enqueue(t, s, "void", "void:unknown", baseTime)
	if j, err := s.ClaimJobByKey(ctx, "void:unknown", "w1", baseTime); err != nil || j == nil || s.MarkJobUnknown(ctx, j.ID, "timeout", nil) != nil {
		t.Fatalf("park void:unknown = %+v, %v", j, err)
	}
	enqueue(t, s, "void", "void:succeeded", baseTime)
	if j, err := s.ClaimJobByKey(ctx, "void:succeeded", "w1", baseTime); err != nil || j == nil || s.CompleteJob(ctx, j.ID, nil) != nil {
		t.Fatalf("complete void:succeeded = %+v, %v", j, err)
	}

	now := baseTime.Add(time.Hour)
	revive := func(key string, anyFailure, want bool) {
		t.Helper()
		before := jobByKey(t, s, key)
		got, err := s.ReviveJob(ctx, key, now, anyFailure)
		if err != nil || got != want {
			t.Fatalf("ReviveJob(%s, anyFailure=%v) = %v, %v; want %v, nil", key, anyFailure, got, err, want)
		}
		after := jobByKey(t, s, key)
		if !want {
			if after.Status != before.Status {
				t.Errorf("%s status = %s, want it left %s", key, after.Status, before.Status)
			}
			return
		}
		if after.Status != "pending" || after.ID != before.ID || after.Attempts != before.Attempts {
			t.Errorf("revived %s = %s %s/%d, want the same row %s pending with attempts kept at %d",
				key, after.ID, after.Status, after.Attempts, before.ID, before.Attempts)
		}
		assertTime(t, key+" run_at", after.RunAt, now)
		assertUnlocked(t, s, after.ID)
	}

	revive("void:skipped", false, true)
	for _, key := range []string{"void:refused", "void:refused-without-issue", "void:exhausted", "void:pending", "void:running", "void:unknown", "void:succeeded"} {
		revive(key, false, false)
	}
	for _, key := range []string{"void:refused", "void:refused-without-issue", "void:exhausted"} {
		revive(key, true, true)
	}
	for _, key := range []string{"void:running", "void:unknown", "void:succeeded"} {
		revive(key, true, false)
	}
	if got, err := s.ReviveJob(ctx, "void:no-such-job", now, true); err != nil || got {
		t.Errorf("ReviveJob(missing key) = %v, %v; want false, nil", got, err)
	}
}

func TestReenqueueJob_KeepsKeyAndResetsRunAt(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
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
	s := storetest.New(t)
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
	s := storetest.New(t)
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
	s := storetest.New(t)
	if err := s.ReenqueueJob(context.Background(), "capture:missing", baseTime); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("ReenqueueJob(missing) = %v, want ErrNotFound", err)
	}
}

func TestJobByKey_MissingIsNil(t *testing.T) {
	s := storetest.New(t)
	if j, err := s.JobByKey(context.Background(), "capture:missing"); err != nil || j != nil {
		t.Errorf("JobByKey(missing) = %+v, %v; want nil, nil", j, err)
	}
}

func TestUpdateJobPayload_ReplacesPayload(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	created, err := s.EnqueueJob(ctx, "create_order", "create_order:a1", nil, nil, map[string]string{"note": "first"}, baseTime)
	if err != nil || !created {
		t.Fatalf("EnqueueJob = %v, %v; want true, nil", created, err)
	}
	j := claim(t, s, "w1", baseTime)

	err = s.UpdateJobPayload(ctx, j.ID, map[string]string{"approve_url": "https://pay.example/approve?order=O1", "order_id": "O1"})
	if err != nil {
		t.Fatalf("UpdateJobPayload: %v", err)
	}

	got := jobByKey(t, s, "create_order:a1")
	assertJSONEqual(t, "payload", got.Payload, []byte(`{"approve_url":"https://pay.example/approve?order=O1","order_id":"O1"}`))
	if got.Status != "running" || got.Attempts != 1 {
		t.Errorf("status/attempts = %s/%d, want running/1 (unchanged)", got.Status, got.Attempts)
	}

	if err := s.UpdateJobPayload(ctx, missingUUID, map[string]string{"order_id": "O2"}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("UpdateJobPayload(missing job) = %v, want ErrNotFound", err)
	}
}

func TestJobsByAttempt_OrderedByCreation(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
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
	s := storetest.New(t)
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

func TestRequeueStale_ParksExhaustedJobsAsNeedsAttention(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	enqueue(t, s, "capture", "capture:hangs", baseTime)
	enqueue(t, s, "capture", "capture:below-cap", baseTime)
	enqueue(t, s, "capture", "capture:fresh", baseTime)
	hangs := claim(t, s, "crashed-worker", baseTime)
	below := claim(t, s, "crashed-worker", baseTime)
	if hangs.IdempotencyKey != "capture:hangs" || below.IdempotencyKey != "capture:below-cap" {
		t.Fatalf("claims = %s, %s; want hangs, below-cap in creation order", hangs.IdempotencyKey, below.IdempotencyKey)
	}
	freshAt := baseTime.Add(9 * time.Minute)
	fresh := claim(t, s, "w2", freshAt)
	if _, err := s.Pool.Exec(ctx, `update jobs set attempts = 5 where id = $1`, hangs.ID); err != nil {
		t.Fatalf("set attempts: %v", err)
	}
	if _, err := s.Pool.Exec(ctx, `update jobs set attempts = 4 where id = $1`, below.ID); err != nil {
		t.Fatalf("set attempts: %v", err)
	}

	now := baseTime.Add(10*time.Minute + time.Second)
	n, err := s.RequeueStale(ctx, now, 10*time.Minute)
	if err != nil {
		t.Fatalf("RequeueStale: %v", err)
	}
	if n != 2 {
		t.Fatalf("RequeueStale count = %d, want 2 (one parked, one requeued)", n)
	}

	got := jobByKey(t, s, "capture:hangs")
	if got.Status != "needs_attention" || got.Attempts != 5 {
		t.Errorf("exhausted job status/attempts = %s/%d, want needs_attention/5", got.Status, got.Attempts)
	}
	if want := "abandoned by its worker after the last attempt"; got.LastError == nil || *got.LastError != want {
		t.Errorf("exhausted job last_error = %v, want %q", got.LastError, want)
	}
	assertUnlocked(t, s, hangs.ID)

	got = jobByKey(t, s, "capture:below-cap")
	if got.Status != "pending" || got.Attempts != 4 {
		t.Errorf("job below the cap status/attempts = %s/%d, want pending/4", got.Status, got.Attempts)
	}
	assertTime(t, "below-cap run_at", got.RunAt, now)
	assertUnlocked(t, s, below.ID)

	if got := jobByKey(t, s, "capture:fresh"); got.Status != "running" || got.ID != fresh.ID {
		t.Errorf("fresh job status = %s, want running", got.Status)
	}

	next := claim(t, s, "w3", now)
	if next.ID != below.ID {
		t.Errorf("claimed %s, want only the below-cap job %s", next.IdempotencyKey, below.IdempotencyKey)
	}
	if again, err := s.ClaimJob(ctx, "w3", now.Add(24*time.Hour)); err != nil || again != nil {
		t.Errorf("ClaimJob with the exhausted job left = %+v, %v; want nil, nil", again, err)
	}
}

func TestAdvanceJob_OnlyBringsAPendingJobForward(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	now := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	later := now.Add(24 * time.Hour)

	enqueue(t, s, "close", "close:later", later)
	if moved, err := s.AdvanceJob(ctx, "close:later", now); err != nil || !moved {
		t.Fatalf("AdvanceJob(pending, later) = %v, %v; want true", moved, err)
	}
	assertTime(t, "run_at after AdvanceJob", jobByKey(t, s, "close:later").RunAt, now)
	if moved, err := s.AdvanceJob(ctx, "close:later", later); err != nil || moved {
		t.Errorf("AdvanceJob to a later time = %v, %v; want false: it never postpones", moved, err)
	}
	assertTime(t, "run_at after a later AdvanceJob", jobByKey(t, s, "close:later").RunAt, now)

	enqueue(t, s, "close", "close:running", later)
	claimed, err := s.ClaimJobByKey(ctx, "close:running", "w1", later)
	if err != nil || claimed == nil {
		t.Fatalf("ClaimJobByKey = %+v, %v", claimed, err)
	}
	if moved, err := s.AdvanceJob(ctx, "close:running", now); err != nil || moved {
		t.Errorf("AdvanceJob(running) = %v, %v; want false", moved, err)
	}
	if got := jobByKey(t, s, "close:running"); got.ID != claimed.ID || got.Status != "running" {
		t.Errorf("running job after AdvanceJob = %+v", got)
	}
	if moved, err := s.AdvanceJob(ctx, "close:missing", now); err != nil || moved {
		t.Errorf("AdvanceJob(missing key) = %v, %v; want false, nil", moved, err)
	}
}

func TestPurgeSucceededJobs_DropsOldReadsAndSeatReleasesOnly(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	en := newEnrollment(t, s)
	a, err := s.CreateAttempt(ctx, en.ID, 1000, "USD")
	if err != nil {
		t.Fatal(err)
	}
	now := baseTime.Add(60 * 24 * time.Hour)
	add := func(kind, key string, attempt bool, status string, age time.Duration) {
		t.Helper()
		var attemptID *string
		if attempt {
			attemptID = &a.ID
		}
		if _, err := s.EnqueueJob(ctx, kind, key, &en.WorkshopID, attemptID, nil, baseTime); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Pool.Exec(ctx, `update jobs set status = $2, updated_at = $3 where idempotency_key = $1`,
			key, status, now.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}
	old, recent := 31*24*time.Hour, 29*24*time.Hour
	purged := []string{
		"release_seats:" + en.WorkshopID + ":2026-11-02T09:00",
		"reconcile:" + a.ID + ":periodic:2026-11-02T09",
		"reconcile:" + a.ID + ":periodic:2026-11-02T09:04",
		"reconcile:" + a.ID + ":webhook:WH-1",
		"reconcile:" + a.ID + ":return:2026-11-02T09:04",
		"reconcile:" + a.ID + ":close",
	}
	for _, key := range purged {
		kind := "reconcile"
		if strings.HasPrefix(key, "release_seats:") {
			kind = "release_seats"
		}
		add(kind, key, kind == "reconcile", store.JobSucceeded, old)
	}
	kept := map[string]bool{}
	keep := func(kind, key string, attempt bool, status string, age time.Duration) {
		add(kind, key, attempt, status, age)
		kept[key] = true
	}
	// Payment actions, the close and numbered reconcile generations are what
	// the worker decides from, so they stay whatever their age.
	for _, kind := range []string{"create_order", "authorize", "capture", "void", "refund"} {
		keep(kind, kind+":"+a.ID, true, store.JobSucceeded, old)
	}
	keep("close", "close:"+en.ProposalID, false, store.JobSucceeded, old)
	keep("reconcile", "reconcile:"+a.ID+":1", true, store.JobSucceeded, old)
	keep("reconcile", "reconcile:"+a.ID+":5", true, store.JobSucceeded, old)
	keep("release_seats", "release_seats:"+en.WorkshopID+":2026-11-02T09:01", false, store.JobSucceeded, recent)
	keep("release_seats", "release_seats:"+en.WorkshopID+":2026-11-02T09:02", false, store.JobFailed, old)
	keep("reconcile", "reconcile:"+a.ID+":periodic:2026-11-02T10", true, store.JobNeedsAttention, old)

	n, err := s.PurgeSucceededJobs(ctx, now.Add(-30*24*time.Hour))
	if err != nil || n != len(purged) {
		t.Fatalf("PurgeSucceededJobs = %d, %v; want %d, nil", n, err, len(purged))
	}
	for _, key := range purged {
		if j, err := s.JobByKey(ctx, key); err != nil || j != nil {
			t.Errorf("job %s = %+v, %v; want purged", key, j, err)
		}
	}
	for key := range kept {
		jobByKey(t, s, key)
	}
	if n, err := s.PurgeSucceededJobs(ctx, now.Add(-30*24*time.Hour)); err != nil || n != 0 {
		t.Errorf("second PurgeSucceededJobs = %d, %v; want 0, nil", n, err)
	}
}

func TestRetryJob_KindsThatReturnMoneyOrKeepHouseNeverPark(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	backoff := []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute, time.Hour, time.Hour}
	for _, kind := range []string{"void", "refund", "reconcile", "close", "release_seats"} {
		key := kind + ":forever"
		enqueue(t, s, kind, key, baseTime)
		now := baseTime
		for attempt := 1; attempt <= len(backoff); attempt++ {
			j := claim(t, s, "w1", now)
			if j.IdempotencyKey != key {
				t.Fatalf("%s: claimed %s, want %s", kind, j.IdempotencyKey, key)
			}
			escalate, err := s.RetryJob(ctx, j.ID, now, "rate limited", ptr(429), nil, nil)
			if err != nil {
				t.Fatalf("%s: RetryJob after attempt %d: %v", kind, attempt, err)
			}
			if escalate != (attempt == 5) {
				t.Errorf("%s: escalate after attempt %d = %v, want it only at the fifth", kind, attempt, escalate)
			}
			got := jobByKey(t, s, key)
			if got.Status != "pending" {
				t.Fatalf("%s: status after attempt %d = %s, want pending: it never parks", kind, attempt, got.Status)
			}
			assertTime(t, fmt.Sprintf("%s run_at after attempt %d", kind, attempt), got.RunAt, now.Add(backoff[attempt-1]))
			now = got.RunAt
		}
	}
}

func TestRequeueStale_NeverParksKindsThatRetryForever(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	enqueue(t, s, "close", "close:hangs", baseTime)
	if _, err := s.Pool.Exec(ctx,
		`update jobs set status = 'running', attempts = 9, locked_at = $1, locked_by = 'crashed-worker' where idempotency_key = 'close:hangs'`,
		baseTime); err != nil {
		t.Fatal(err)
	}

	if n, err := s.RequeueStale(ctx, baseTime.Add(time.Hour), 5*time.Minute); err != nil || n != 1 {
		t.Fatalf("RequeueStale = %d, %v; want the one job", n, err)
	}

	if got := jobByKey(t, s, "close:hangs"); got.Status != "pending" {
		t.Errorf("close job after RequeueStale = %s, want pending: a close never parks", got.Status)
	}
}
