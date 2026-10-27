package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Job is one row of the payment-action queue. IdempotencyKey is also the
// PayPal-Request-Id, so every retry of the same logical action reuses it.
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

// jobBackoff is how long RetryJob waits before the next run, indexed by the
// number of attempts already made minus one. A job whose attempts reach
// maxJobAttempts is parked as needs_attention instead.
var jobBackoff = [...]time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute}

const maxJobAttempts = len(jobBackoff) + 1

// EnqueueJob adds a pending job that runs at runAt, with payload marshalled as
// JSON (nil is stored as {}). The idempotency key is unique: when a job with
// the same key already exists nothing is written, the stored row keeps its
// payload, status and run_at, and created is false.
func (s *Store) EnqueueJob(ctx context.Context, kind, key string, workshopID, attemptID *string, payload any, runAt time.Time) (created bool, err error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return false, wrap("marshal job payload", err)
	}
	if string(body) == "null" {
		body = []byte("{}")
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

// ClaimJob takes the pending job with the oldest run_at that is due at now
// and marks it running for workerID, counting one more attempt. Rows another
// worker is claiming at the same moment are skipped, so no job is handed to
// two workers. It returns nil, nil when nothing is claimable.
func (s *Store) ClaimJob(ctx context.Context, workerID string, now time.Time) (*Job, error) {
	var claimed *Job
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var id string
		err := tx.QueryRow(ctx,
			`select id from jobs
			 where status = 'pending' and run_at <= $1
			 order by run_at, created_at
			 limit 1
			 for update skip locked`, now).Scan(&id)
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

// CompleteJob marks the job succeeded, stores the provider's HTTP status and
// releases the lock. It returns ErrNotFound when the job does not exist.
func (s *Store) CompleteJob(ctx context.Context, id string, httpStatus *int) error {
	return s.finishJob(ctx, "complete job", id,
		`update jobs set status = 'succeeded', http_status = $2,
			locked_at = null, locked_by = null, updated_at = now()
		 where id = $1`, httpStatus)
}

// RetryJob records a failed run and decides what happens next from the number
// of attempts the job has already made: it goes back to pending at now plus
// 5s, 30s, 2m or 10m, and after the fifth attempt it is parked as
// needs_attention (needsAttention is true), keeping its run_at, and is no
// longer claimed. The failure columns are overwritten with what the call
// gives, and the lock is released.
//
// It returns ErrNotFound when the job does not exist.
func (s *Store) RetryJob(ctx context.Context, id string, now time.Time, lastError string, httpStatus *int, issue, debugID *string) (needsAttention bool, err error) {
	err = pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var attempts int
		if err := tx.QueryRow(ctx, `select attempts from jobs where id = $1 for update`, id).Scan(&attempts); err != nil {
			return lookupErr("lock job", err)
		}
		status := "needs_attention"
		var runAt *time.Time
		if attempts < maxJobAttempts {
			status = "pending"
			next := now.Add(jobBackoff[max(attempts, 1)-1])
			runAt = &next
		}
		needsAttention = status == "needs_attention"
		_, err := tx.Exec(ctx,
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

// MarkJobUnknown parks a job whose outcome at the provider is ambiguous
// (timeout, 5xx, 409, 429, network error) as unknown, stores the error and
// releases the lock. A reconcile job resolves it later. It returns
// ErrNotFound when the job does not exist.
func (s *Store) MarkJobUnknown(ctx context.Context, id, lastError string, httpStatus *int) error {
	return s.finishJob(ctx, "mark job unknown", id,
		`update jobs set status = 'unknown', last_error = $2, http_status = $3,
			locked_at = null, locked_by = null, updated_at = now()
		 where id = $1`, lastError, httpStatus)
}

func (s *Store) finishJob(ctx context.Context, op, id, sql string, args ...any) error {
	tag, err := s.Pool.Exec(ctx, sql, append([]any{id}, args...)...)
	if err != nil {
		return lookupErr(op, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ReenqueueJob sends the job with this idempotency key back to pending at now,
// on the same row and with the same key, so the retry reuses the provider
// request ID. Attempts are not reset. It applies to unknown, failed and
// needs_attention jobs; a pending, running or succeeded job is left as it is.
// It returns ErrNotFound when no job has the key.
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

// JobByKey returns the job with this idempotency key, or nil when there is none.
func (s *Store) JobByKey(ctx context.Context, key string) (*Job, error) {
	return oneOrNil[Job](ctx, s.Pool, "job by key",
		`select `+jobCols+` from jobs where idempotency_key = $1`, key)
}

// JobsByAttempt returns every job of the payment attempt, oldest first.
func (s *Store) JobsByAttempt(ctx context.Context, attemptID string) ([]Job, error) {
	return many[Job](ctx, s.Pool, "jobs by attempt",
		`select `+jobCols+` from jobs where attempt_id = $1 order by created_at, id`, attemptID)
}

// RequeueStale recovers jobs a worker claimed and never finished: every
// running job locked more than olderThan before now goes back to pending, due
// at now, with the lock released. It returns how many jobs it recovered.
func (s *Store) RequeueStale(ctx context.Context, now time.Time, olderThan time.Duration) (int, error) {
	tag, err := s.Pool.Exec(ctx,
		`update jobs set status = 'pending', run_at = $1,
			locked_at = null, locked_by = null, updated_at = now()
		 where status = 'running' and locked_at < $2`, now, now.Add(-olderThan))
	if err != nil {
		return 0, wrap("requeue stale jobs", err)
	}
	return int(tag.RowsAffected()), nil
}
