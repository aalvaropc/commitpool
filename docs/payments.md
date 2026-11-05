# Payments

How CommitPool takes money for a workshop through PayPal, and the rules the
worker follows. The code is the reference: `internal/domain` holds the pure
rules, `internal/worker` applies them, `internal/store` holds the state.

A participant reserves a seat, approves a PayPal order and the worker
authorizes it: the buyer's money is held, not taken. Holds are captured once
enough of them exist to reach the workshop's minimum. The workshop is
confirmed when the minimum number of completed payments is reached; if it is
not reached when the payment window closes, the workshop is cancelled, holds
are voided and completed payments refunded.

Each checkout is a payment attempt with at most one PayPal order, one
authorization, one capture (in full, `final_capture: true`) and one refund.
A new attempt can only be opened while the enrollment is `SEAT_RESERVED` and
its latest attempt holds no authorization, so a hold, its capture, its void
and its refund always belong to the attempt the enrollment follows.

## Workshop

| Status | Meaning | Next |
|---|---|---|
| `COLLECTING` | Participants give their availability. | `FUNDING` when the owner opens a proposal. |
| `FUNDING` | Payments are open until the proposal's `closes_at`. | `CONFIRMED` once completed payments reach the minimum; `CANCELLED` at close below it. |
| `CONFIRMED` | The workshop runs. Payments stay open until `closes_at`, so late joiners are captured one by one. | Final. |
| `CANCELLED` | The workshop does not run. | Final. |

The resolution tracks the money side, separately from the status:

| Resolution | Meaning |
|---|---|
| `OPEN` | Nothing to settle: funding, or confirmed. |
| `RESOLVING` | Cancelled; voids, refunds or in-flight captures are still running. |
| `SETTLED` | Every enrollment is finished and no hold may remain. |
| `NEEDS_ATTENTION` | An operator must act. Never cleared by the worker. |

"Payments open" has one definition (`domain.PaymentsOpen`): the workshop is
`FUNDING` or `CONFIRMED` and now is before `closes_at`. Seats are reserved
and authorizations placed only while payments are open.

## Enrollment

| Status | Seat | Reached when | Next |
|---|---|---|---|
| `SEAT_RESERVED` | yes | The participant enrolls (capacity permitting), or a released seat is taken again. Also while PayPal holds the authorization `PENDING` (review): the seat is kept past its expiry and read back hourly, and the enrollment is labelled “Hold pending verification”. | `AUTHORIZED`, `RELEASED`, `CAPTURE_FAILED` (authorization denied) |
| `AUTHORIZED` | yes | PayPal created the authorization. | `CAPTURING`, `VOIDING`, `VOIDED`, `PAID`, `CAPTURE_PENDING`, `CAPTURE_FAILED` |
| `CAPTURING` | yes | The capture job is about to call PayPal. | `PAID`, `CAPTURE_PENDING`, `CAPTURE_FAILED` |
| `CAPTURE_PENDING` | yes | PayPal reported the capture `PENDING`. | `PAID`, `CAPTURE_FAILED` |
| `PAID` | yes | The capture `COMPLETED`. | `REFUNDING`, `REFUND_PENDING`, `REFUNDED` |
| `VOIDING` | no | The void job is about to call PayPal for the enrollment's own hold. | `VOIDED` |
| `REFUNDING` | no | The refund job is about to call PayPal. | `REFUNDED`, `REFUND_PENDING`, `REFUND_FAILED` |
| `REFUND_PENDING` | no | PayPal reported the refund `PENDING`. | `REFUNDED`, `REFUND_FAILED` |
| `VOIDED` | no | The hold was released. | Terminal. |
| `REFUNDED` | no | The refund `COMPLETED`. | Terminal. |
| `REFUND_FAILED` | no | The refund `FAILED`, was `CANCELLED`, or PayPal refused it (then also flagged). | Terminal; needs an operator. |
| `CAPTURE_FAILED` | no | PayPal declined or refused the capture, or its deadline passed. The hold is voided. | Terminal (flagged when PayPal refuses the void). |
| `RELEASED` | no | The seat expired before the authorization (never while the latest attempt's authorization is `PENDING`). | `SEAT_RESERVED` when the buyer comes back to pay while payments are open and a seat is free. |
| `NEEDS_ATTENTION` | no | Legacy: rows written before the operator flag (migration 0003) keep it, flagged; never written again. | Terminal. |

### The operator flag

An enrollment that needs an operator is **flagged** (`needs_attention_at`,
`attention_reason`) and keeps its status, so the status always says where the
money is. A flagged enrollment keeps its seat, stays the participant's one
active enrollment (no second hold can be placed), and is still voided,
captured or refunded by status when the workshop is healed or closed. It is
flagged when PayPal reports a second resource for one attempt, money moved on
an older attempt, a partial refund nobody asked for, a void or refund PayPal
refuses (`VOIDING` stays, `CAPTURE_FAILED` stays, `REFUNDING` becomes
`REFUND_FAILED`), a job gives up after its retries, or the last reconcile
still finds no trace of an action. The flag is never cleared by the worker.
The API shows a flagged enrollment's `status` as `NEEDS_ATTENTION` (label
“Needs attention”) and its real status in `money_status`.

A move only happens from the statuses listed for it, so applying the same
PayPal answer twice changes nothing and a finished enrollment is never moved
back. Only the latest attempt moves its enrollment. A hold PayPal reports on
an attempt the enrollment does not use (an older attempt, or a released seat)
is voided without touching the enrollment; money moved on such an attempt
flags the enrollment.

## Jobs

Every PayPal action is a row in `jobs`, written before the request leaves.

| Status | Meaning | Next |
|---|---|---|
| `pending` | Due at `run_at`. | `running` when claimed. |
| `running` | Claimed by a worker. | `succeeded`; `failed` on a refusal; `unknown` on an ambiguous result; `pending` for a retry (5 s, 30 s, 2 min, 10 min); `needs_attention` after the fifth attempt; `pending` again when its worker has not finished it after 5 minutes. |
| `unknown` | PayPal may or may not have performed the action. | `succeeded` or `pending` (same key) once a reconcile has read PayPal. |
| `failed` | Refused: by PayPal for good, by the worker (a passed deadline), or skipped because the action did not apply. | `pending` (same key) when revived. |
| `needs_attention` | Retries used up. | `pending` (same key) only when revived by hand. |
| `succeeded` | Done. | Final. A succeeded seat release or read-only reconcile is deleted after 30 days (see Periodic work). |

Automatic paths revive only a job that failed without a PayPal answer (a
skip), never an action PayPal refused. The one exception is an authorize
PayPal refused as `ORDER_NOT_APPROVED`: the return page and a reconcile that
finds the order `APPROVED` both revive it, because the order is approved
now.

A job whose retries are used up flags its enrollment (the status stays),
unless the enrollment is paid or finished; a `close` or `release_seats` job
that gives up flags its workshop.

## Idempotency keys

The job's key is its PayPal-Request-Id, so every retry of an action reuses
it and PayPal performs the action at most once.

| Key | Action |
|---|---|
| `create_order:{attempt}` | Create the attempt's order. |
| `authorize:{attempt}` | Authorize the approved order. |
| `capture:{attempt}` | Capture the authorization. |
| `void:{attempt}` | Void the authorization. |
| `refund:{attempt}` | Refund the capture. |
| `reconcile:{attempt}:{n}` | Read the attempt back after an ambiguous result; `n` is the generation. |
| `reconcile:{attempt}:close` | Read back a capture still in flight at close. |
| `reconcile:{attempt}:webhook:{event_id}` | Read back after a verified webhook. A webhook never applies a payment state itself. |
| `reconcile:{attempt}:return:{minute}` | Read back a checkout whose order PayPal did not show on the return page. |
| `reconcile:{attempt}:periodic:{hour}` | Periodic read-back of a pending authorization, capture or refund (see Periodic work). |
| `reconcile:{attempt}:periodic:{minute}` | Periodic read-back of an attempt with a job unknown for too long. |
| `close:{proposal}` | Close payments at `closes_at`. |
| `release_seats:{workshop}:{minute}` | Release the expired seats. |

Minutes (`yyyy-mm-ddThh:mm`) and hours (`yyyy-mm-ddThh`) are UTC, so a
second enqueue within the same minute or hour is a no-op.

## Ambiguous results

A result is ambiguous when PayPal may have performed the action: a timeout or
any transport failure, a 5xx, or a 409 (`PREVIOUS_REQUEST_IN_PROGRESS`). The
job becomes `unknown` and `reconcile:{attempt}:{n}` is scheduled 30 seconds
later. The reconcile reads the order, authorization, capture and refund from
PayPal and applies them; an unknown job whose resource now exists is
completed, and one whose resource does not exist is sent again under its own
key. The fifth generation sends nothing: it leaves the job `unknown` for an
operator and flags the enrollment, or the workshop when the enrollment is
finished or paid. Only numeric generations count towards that limit.

A 429, 401 or 403 is not ambiguous (PayPal did not process the request) and
is retried with the same key. Any other 4xx is a final answer.

## Capture round

- In a `FUNDING` workshop, once authorized plus paid enrollments reach the
  minimum before `closes_at` (and paid alone does not), every `AUTHORIZED`
  enrollment is captured.
- The workshop is confirmed, by compare-and-set from `FUNDING`, as soon as
  completed payments reach the minimum.
- After a failed capture or a payment that does not confirm, the round is
  evaluated again, since authorizations may have arrived meanwhile.
- In a `CONFIRMED` workshop every new authorization is captured while
  payments are open.
- No capture starts in a cancelled workshop or once payments have closed.

## Capture deadline

A capture must happen before the earlier of the proposal's `closes_at` and
the authorization's `auth_expires_at`. A capture job that runs past it is not
sent to PayPal: the enrollment becomes `CAPTURE_FAILED` with
`capture_issue = AUTHORIZATION_EXPIRED_LOCAL` when the authorization expired
first, or `CAPTURE_DEADLINE_PASSED` otherwise, the hold is voided and the
round is evaluated again, as after a refusal from PayPal. A capture that is
resumed past its deadline first reads PayPal and keeps a capture an earlier
run already made.

An authorization that lands once payments have closed, or on a cancelled
workshop, is voided; the authorize job itself refuses to call PayPal once
payments have closed.

## Close

`close:{proposal}` runs at `closes_at` and first heals the workshop (see
below). Then:

| Workshop at close | Outcome | Voids | Refunds | Reconciles |
|---|---|---|---|---|
| `FUNDING`, completed payments ≥ minimum | `CONFIRMED` | `AUTHORIZED` whose capture has not started | none | `CAPTURING`, `CAPTURE_PENDING` |
| `FUNDING`, below the minimum | `CANCELLED`, `RESOLVING` | `AUTHORIZED` | `PAID` | `CAPTURING`, `CAPTURE_PENDING` |
| `CONFIRMED` | unchanged | `AUTHORIZED` whose capture has not started | none | `CAPTURING`, `CAPTURE_PENDING` |

Captures still in flight are never counted as paid. The status is written by
compare-and-set before anything is enqueued; when the workshop changed
meanwhile the decision is taken again. Once a cancellation is written, what
it owes is derived again from a fresh read, so a payment that completed in
between is refunded by the close itself. Every key is fixed by its attempt,
so running the close again enqueues nothing new.

A payment that completes after a cancellation is refunded. One that
completes after a confirmation keeps its seat until the workshop starts and
is refunded after that.

## Settlement

A `RESOLVING` workshop becomes `SETTLED` once every enrollment is finished
(or paid, in a confirmed workshop) and no failed capture or released seat may
still hold the buyer's money: an authorization PayPal has not shown voided,
denied or expired, or an authorize that is running or whose result is
unknown. It becomes `NEEDS_ATTENTION` as soon as an enrollment is flagged
or `REFUND_FAILED`, which also reopens a `SETTLED` workshop.

The workshop is flagged `NEEDS_ATTENTION` when:

- an enrollment is flagged or `REFUND_FAILED` in a resolving or
  settled workshop (settlement) or in a confirmed one, which is never
  settled;
- a funding workshop is confirmed while such an enrollment exists;
- PayPal refuses the void of a hold the enrollment does not use (a released
  seat or an older attempt);
- the last reconcile generation leaves a job unknown on an enrollment that is
  finished or paid;
- a `close` or `release_seats` job gives up.

A flag survives the workshop's confirmation or cancellation.

## Healing

Hooks run after each enrollment move. When one fails after the move was
written, healing redoes its follow-up from a fresh read, idempotently by key:

- a failed capture or a released seat whose authorization still holds is
  voided (a skipped void is revived; a void PayPal refused is not sent again);
- a `FUNDING` workshop whose completed payments reach the minimum is
  confirmed, and one whose authorized plus paid enrollments reach it while
  payments are open gets its capture round (under `capture:{attempt}`, so a
  round already started enqueues nothing new);
- in a cancelled workshop, a payment with no refund job is refunded and an
  authorization with no void job is voided;
- in a confirmed workshop, an authorization with neither a capture nor a void
  job is captured while payments are open and voided after, and an enrollment
  that needs an operator flags the workshop;
- a resolving or settled workshop is settled again.

Healing runs at the start of every close, at the end of every reconcile and
in the periodic work.

## Periodic work

The worker, in process:

- every minute takes back jobs claimed more than five minutes ago (parked as
  `needs_attention` once their attempts are used up) and enqueues
  `release_seats` for every workshop with a reserved seat;
- every two minutes enqueues `reconcile:{attempt}:periodic:{hour}` for the
  latest attempt of every `CAPTURE_PENDING` or `REFUND_PENDING` enrollment,
  and of every `SEAT_RESERVED` enrollment whose authorization PayPal reports
  `PENDING` (so a pending authorization, capture or refund is read back
  within two minutes of turning pending and then once an hour; the
  authorization then moves `CREATED` → `AUTHORIZED` or `DENIED` →
  `CAPTURE_FAILED`, which frees the seat), and `reconcile:{attempt}:periodic:{minute}`
  for every attempt with a job unknown for more than five minutes, unless its
  fifth reconcile generation exists; then heals every `FUNDING` workshop and
  every workshop whose money is not settled (`RESOLVING`, or cancelled and
  not `SETTLED`, `NEEDS_ATTENTION` included);
- on the same two-minute tick deletes the `succeeded` jobs last updated
  more than 30 days ago that only read or released something:
  `release_seats` and every reconcile except a numbered generation
  (`reconcile:{attempt}:{n}`). Payment actions, `close` and numbered
  generations are kept whatever their age, because the worker decides from
  their existence (healing, the next generation and the last one that leaves
  a job to an operator). The count is logged when it is not zero.

On shutdown the worker claims nothing new and lets the job it is running
finish for up to ten seconds; past that the PayPal call is cancelled and its
result is reconciled like any other ambiguous one.

## Operator runbook

Run these against the application database (`psql "$DATABASE_URL"`). Read
before you write: every statement below is a compare-and-set that changes
nothing when the row is not in the state it expects.

### Jobs waiting for an operator

A job is left for an operator when its retries are used up
(`needs_attention`) or when the last reconcile generation still could not
tell whether PayPal performed it (`unknown`).

```sql
select j.kind, j.idempotency_key, j.status, j.attempts, j.http_status,
       j.provider_issue, j.debug_id, j.last_error, j.updated_at,
       w.id as workshop_id, w.title, w.status as workshop_status, w.resolution,
       e.id as enrollment_id, e.status as enrollment_status, a.attempt_no
from jobs j
left join workshops w on w.id = j.workshop_id
left join payment_attempts a on a.id = j.attempt_id
left join enrollments e on e.id = a.enrollment_id
where j.status in ('needs_attention', 'unknown')
order by j.updated_at;
```

An `unknown` job updated in the last few minutes is still being reconciled
and needs nothing yet. `debug_id` is PayPal's id for the failed request;
quote it to PayPal support. `close` and `release_seats` jobs have no attempt, so their
enrollment columns are empty.

### Reviving a job

```sql
update jobs
set status = 'pending', run_at = now(), locked_at = null, locked_by = null, updated_at = now()
where idempotency_key = $1 and status in ('needs_attention', 'unknown', 'failed');
```

A revived job runs on the same row under the same key, and the key is its
`PayPal-Request-Id`, so PayPal replays the answer it gave that request
instead of performing the action a second time. PayPal remembers a request
id only for a limited time: for an `unknown` job that is days old, first
check at PayPal (by the attempt's order, authorization, capture or refund id,
or the `debug_id`) whether the action happened, and revive only one that did
not. The job keeps its `attempts`, so a job revived from `needs_attention`
that fails again is parked again after that one try. Never revive a `succeeded` job, and never
insert a job by hand under a new key.

### Clearing a workshop flag

A workshop flagged `NEEDS_ATTENTION` is never cleared by the worker. Before
clearing it, open the owner view (`GET /api/v1/owner/{token}`) and the
query above, and make sure what flagged it is resolved at PayPal. Then:

```sql
update workshops set resolution = $2, updated_at = now()
where id = $1 and resolution = 'NEEDS_ATTENTION';
```

with `$2`:

- `RESOLVING` for a cancelled workshop that still owes voids or refunds (or
  has captures in flight); settlement moves it to `SETTLED` by itself once
  they finish;
- `SETTLED` for a cancelled workshop only when every enrollment is terminal
  and no hold may remain;
- `OPEN` for a `CONFIRMED` workshop.

A flagged or `REFUND_FAILED` enrollment flags its workshop again
(settlement, healing, the next hook). Once its money is settled at PayPal,
record what PayPal shows on it and clear its flag first, for example
`update enrollments set status = 'REFUNDED', needs_attention_at = null, attention_reason = null, updated_at = now() where id = $1 and status = 'REFUND_FAILED';`.

### What the owner view shows and what only `jobs` shows

The owner view lists every enrollment with its status, its seat expiry and,
for its latest attempt, the PayPal order, authorization, capture and refund
ids, their statuses and `capture_issue` (the issue PayPal gave for a refused
capture, or `AUTHORIZATION_EXPIRED_LOCAL` / `CAPTURE_DEADLINE_PASSED`), plus
the workshop's status and resolution.

Only the `jobs` table shows which action is stuck and why: the job status,
`attempts`, `run_at`, `http_status`, `provider_issue` (for a capture the
owner view shows it as `capture_issue`), `debug_id` and `last_error`. Older attempts of an
enrollment, `auth_expires_at`, `capture_debug_id`, `capture_reason` and
`refund_reason` are only in `payment_attempts`.
