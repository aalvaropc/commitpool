package store

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	KindCreateOrder  = "create_order"
	KindAuthorize    = "authorize"
	KindCapture      = "capture"
	KindVoid         = "void"
	KindRefund       = "refund"
	KindReconcile    = "reconcile"
	KindClose        = "close"
	KindReleaseSeats = "release_seats"
)

const (
	JobPending        = "pending"
	JobRunning        = "running"
	JobSucceeded      = "succeeded"
	JobFailed         = "failed"
	JobUnknown        = "unknown"
	JobNeedsAttention = "needs_attention"
)

func (j Job) StillFailing() bool { return j.Status == JobPending && j.Attempts >= maxJobAttempts }

func JobKey(kind, subject string) string { return kind + ":" + subject }

func Minute(t time.Time) string { return t.UTC().Format("2006-01-02T15:04") }

type Job struct {
	ID, Kind, IdempotencyKey          string
	WorkshopID, AttemptID             *string
	Payload                           json.RawMessage
	Status                            string
	Attempts                          int
	RunAt                             time.Time
	HTTPStatus                        *int
	ProviderIssue, DebugID, LastError *string
}

const jobCols = `id, kind, idempotency_key, workshop_id, attempt_id, payload, status, attempts, run_at,
	http_status, provider_issue, debug_id, last_error`

var jobBackoff = [...]time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute}

const maxJobAttempts = len(jobBackoff) + 1

var foreverKinds = []string{KindVoid, KindRefund, KindReconcile, KindClose, KindReleaseSeats, KindNotify}

func RetriesForever(kind string) bool { return slices.Contains(foreverKinds, kind) }

func retryDelay(kind string, attempts int) (delay time.Duration, park bool) {
	switch {
	case attempts < maxJobAttempts:
		return jobBackoff[max(attempts, 1)-1], false
	case !RetriesForever(kind):
		return 0, true
	case attempts == maxJobAttempts:
		return 30 * time.Minute, false
	default:
		return time.Hour, false
	}
}

func (s *Store) EnqueueJob(ctx context.Context, kind, key string, workshopID, attemptID *string, payload any, runAt time.Time) (created bool, err error) {
	body, err := jobPayload(payload)
	if err != nil {
		return false, err
	}
	tag, err := s.Pool.Exec(ctx,
		`insert into jobs (kind, idempotency_key, workshop_id, attempt_id, payload, run_at)
		 values ($1, $2, $3, $4, $5, $6)
		 on conflict (idempotency_key) do nothing`,
		kind, key, workshopID, attemptID, body, runAt)
	if err != nil {
		return false, wrap("enqueue job", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Store) ClaimJob(ctx context.Context, workerID string, now time.Time) (*Job, error) {
	return s.claim(ctx, workerID, now,
		`select id from jobs
		 where status = 'pending' and run_at <= $1
		 order by run_at, created_at
		 limit 1
		 for update skip locked`, now)
}

func (s *Store) ClaimJobByKey(ctx context.Context, key, workerID string, now time.Time) (*Job, error) {
	return s.claim(ctx, workerID, now,
		`select id from jobs
		 where idempotency_key = $2 and status = 'pending' and run_at <= $1
		 for update skip locked`, now, key)
}

func (s *Store) claim(ctx context.Context, workerID string, now time.Time, selectSQL string, args ...any) (*Job, error) {
	var claimed *Job
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var id string
		err := tx.QueryRow(ctx, selectSQL, args...).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return wrap("select claimable job", err)
		}
		job, err := one[Job](ctx, tx,
			`update jobs set status = 'running', attempts = attempts + 1,
				locked_at = $2, locked_by = $3, updated_at = now()
			 where id = $1
			 returning `+jobCols, id, now, workerID)
		if err != nil {
			return wrap("claim job", err)
		}
		claimed = &job
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

func (s *Store) CompleteJob(ctx context.Context, id string, httpStatus *int) error {
	return s.finishJob(ctx, "complete job", id,
		`update jobs set status = 'succeeded', http_status = $2,
			locked_at = null, locked_by = null, updated_at = now()
		 where id = $1 and status in ('running', 'unknown')`, httpStatus)
}

func (s *Store) RetryJob(ctx context.Context, id string, now time.Time, lastError string, httpStatus *int, issue, debugID *string) (needsAttention bool, err error) {
	err = pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var attempts int
		var kind string
		err := tx.QueryRow(ctx, `select attempts, kind from jobs where id = $1 and status = $2 for update`, id, JobRunning).Scan(&attempts, &kind)
		if errors.Is(err, pgx.ErrNoRows) {
			return jobMissError(ctx, tx, "retry job", id)
		}
		if err != nil {
			return lookupErr("lock job", err)
		}
		status := JobNeedsAttention
		var runAt *time.Time
		delay, park := retryDelay(kind, attempts)
		if !park {
			status = JobPending
			next := now.Add(delay)
			runAt = &next
		}
		needsAttention = park || attempts == maxJobAttempts
		_, err = tx.Exec(ctx,
			`update jobs set status = $2, run_at = coalesce($3, run_at), last_error = $4, http_status = $5,
				provider_issue = $6, debug_id = $7,
				locked_at = null, locked_by = null, updated_at = now()
			 where id = $1`,
			id, status, runAt, lastError, httpStatus, issue, debugID)
		if err != nil {
			return wrap("retry job", err)
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return needsAttention, nil
}

func (s *Store) MarkJobUnknown(ctx context.Context, id, lastError string, httpStatus *int) error {
	return s.finishJob(ctx, "mark job unknown", id,
		`update jobs set status = 'unknown', last_error = $2, http_status = $3,
			provider_issue = null, debug_id = null,
			locked_at = null, locked_by = null, updated_at = now()
		 where id = $1 and status = 'running'`, lastError, httpStatus)
}

func (s *Store) FailJob(ctx context.Context, id, lastError string, httpStatus *int, issue, debugID *string) error {
	return s.finishJob(ctx, "fail job", id,
		`update jobs set status = 'failed', last_error = $2, http_status = $3,
			provider_issue = $4, debug_id = $5,
			locked_at = null, locked_by = null, updated_at = now()
		 where id = $1 and status = 'running'`, lastError, httpStatus, issue, debugID)
}

func (s *Store) finishJob(ctx context.Context, op, id, sql string, args ...any) error {
	tag, err := s.Pool.Exec(ctx, sql, append([]any{id}, args...)...)
	if err != nil {
		return lookupErr(op, err)
	}
	if tag.RowsAffected() == 0 {
		return jobMissError(ctx, s.Pool, op, id)
	}
	return nil
}

type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func jobMissError(ctx context.Context, q rowQuerier, op, id string) error {
	var exists bool
	if err := q.QueryRow(ctx, `select exists (select 1 from jobs where id = $1)`, id).Scan(&exists); err != nil {
		return lookupErr(op, err)
	}
	if !exists {
		return ErrNotFound
	}
	return ErrJobNotActive
}

func (s *Store) ReenqueueJob(ctx context.Context, key string, now time.Time) error {
	tag, err := s.Pool.Exec(ctx,
		`update jobs set status = 'pending', run_at = $2,
			locked_at = null, locked_by = null, updated_at = now()
		 where idempotency_key = $1 and status in ('unknown', 'failed', 'needs_attention')`, key, now)
	if err != nil {
		return wrap("reenqueue job", err)
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	var exists bool
	if err := s.Pool.QueryRow(ctx, `select exists (select 1 from jobs where idempotency_key = $1)`, key).Scan(&exists); err != nil {
		return wrap("reenqueue job", err)
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdateJobPayload(ctx context.Context, id string, payload any) error {
	body, err := jobPayload(payload)
	if err != nil {
		return err
	}
	tag, err := s.Pool.Exec(ctx, `update jobs set payload = $2, updated_at = now() where id = $1`, id, body)
	if err != nil {
		return lookupErr("update job payload", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func jobPayload(payload any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, wrap("marshal job payload", err)
	}
	if string(body) == "null" {
		return []byte("{}"), nil
	}
	return body, nil
}

func (s *Store) ReviveJob(ctx context.Context, key string, now time.Time, anyFailure bool) (bool, error) {
	tag, err := s.Pool.Exec(ctx,
		`update jobs set status = 'pending', run_at = $2,
			locked_at = null, locked_by = null, updated_at = now()
		 where idempotency_key = $1
		   and ((status = 'failed' and ($3 or (provider_issue is null and http_status is null)))
		        or ($3 and status = 'needs_attention'))`, key, now, anyFailure)
	if err != nil {
		return false, wrap("revive job", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Store) AdvanceJob(ctx context.Context, key string, runAt time.Time) (bool, error) {
	tag, err := s.Pool.Exec(ctx,
		`update jobs set run_at = $2, updated_at = now()
		 where idempotency_key = $1 and status = 'pending' and run_at > $2`, key, runAt)
	if err != nil {
		return false, wrap("advance job", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Store) JobByKey(ctx context.Context, key string) (*Job, error) {
	return oneOrNil[Job](ctx, s.Pool, "job by key",
		`select `+jobCols+` from jobs where idempotency_key = $1`, key)
}

func (s *Store) JobsByAttempt(ctx context.Context, attemptID string) ([]Job, error) {
	return many[Job](ctx, s.Pool, "jobs by attempt",
		`select `+jobCols+` from jobs where attempt_id = $1 order by created_at, id`, attemptID)
}

func (s *Store) JobsByAttempts(ctx context.Context, attemptIDs []string) ([]Job, error) {
	if len(attemptIDs) == 0 {
		return nil, nil
	}
	return many[Job](ctx, s.Pool, "jobs by attempts",
		`select `+jobCols+` from jobs where attempt_id = any($1::uuid[]) order by created_at, id`, attemptIDs)
}

func (s *Store) UnknownJobs(ctx context.Context, olderThan time.Duration) ([]Job, error) {
	return many[Job](ctx, s.Pool, "unknown jobs",
		`select `+jobCols+` from jobs
		 where status = $1 and updated_at < now() - make_interval(secs => $2)
		 order by updated_at, id`, JobUnknown, olderThan.Seconds())
}

const abandonedReason = "abandoned by its worker after the last attempt"

func (s *Store) RequeueStale(ctx context.Context, now time.Time, olderThan time.Duration) (int, error) {
	tag, err := s.Pool.Exec(ctx,
		`update jobs set
			status = case when attempts >= $3 and not kind = any($8) then $5 else $6 end,
			run_at = case when attempts >= $3 and not kind = any($8) then run_at else $1 end,
			last_error = case when attempts >= $3 and not kind = any($8) then $4 else last_error end,
			locked_at = null, locked_by = null, updated_at = now()
		 where status = $7 and locked_at < $2`,
		now, now.Add(-olderThan), maxJobAttempts, abandonedReason, JobNeedsAttention, JobPending, JobRunning, foreverKinds)
	if err != nil {
		return 0, wrap("requeue stale jobs", err)
	}
	return int(tag.RowsAffected()), nil
}

func (s *Store) PurgeSucceededJobs(ctx context.Context, before time.Time) (int, error) {
	tag, err := s.Pool.Exec(ctx,
		`delete from jobs
		 where status = $1 and updated_at < $2
		   and (kind = $3 or kind = $5 or (kind = $4 and idempotency_key !~ ('^' || $4 || ':' || attempt_id::text || ':[0-9]+$')))`,
		JobSucceeded, before, KindReleaseSeats, KindReconcile, KindPayoutCheck)
	if err != nil {
		return 0, wrap("purge succeeded jobs", err)
	}
	return int(tag.RowsAffected()), nil
}

type QueueHealth struct {
	Unknown, Parked, StuckPending, WorkshopsFlagged int
	PayoutsFailed, PayoutsUnknown                   int
}

func (s *Store) Health(ctx context.Context) (QueueHealth, error) {
	var h QueueHealth
	err := s.Pool.QueryRow(ctx,
		`select
		   (select count(*) from jobs where status = 'unknown'),
		   (select count(*) from jobs where status = 'needs_attention'),
		   (select count(*) from jobs where status = 'pending' and attempts >= $1),
		   (select count(*) from workshops where resolution = 'NEEDS_ATTENTION'),
		   (select count(*) from payouts where status = 'failed'),
		   (select count(*) from payouts where status = 'unknown')`, maxJobAttempts).
		Scan(&h.Unknown, &h.Parked, &h.StuckPending, &h.WorkshopsFlagged, &h.PayoutsFailed, &h.PayoutsUnknown)
	if err != nil {
		return QueueHealth{}, wrap("queue health", err)
	}
	return h, nil
}

func (s *Store) Ping(ctx context.Context) error { return s.Pool.Ping(ctx) }
