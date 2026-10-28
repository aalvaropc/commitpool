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
| `SEAT_RESERVED` | yes | The participant enrolls (capacity permitting), or a released seat is taken again. | `AUTHORIZED`, `RELEASED`, `CAPTURE_FAILED` (authorization denied), `NEEDS_ATTENTION` |
| `AUTHORIZED` | yes | PayPal created the authorization. | `CAPTURING`, `VOIDING`, `VOIDED`, `PAID`, `CAPTURE_PENDING`, `CAPTURE_FAILED`, `NEEDS_ATTENTION` |
| `CAPTURING` | yes | The capture job is about to call PayPal. | `PAID`, `CAPTURE_PENDING`, `CAPTURE_FAILED`, `NEEDS_ATTENTION` |
| `CAPTURE_PENDING` | yes | PayPal reported the capture `PENDING`. | `PAID`, `CAPTURE_FAILED`, `NEEDS_ATTENTION` |
| `PAID` | yes | The capture `COMPLETED`. | `REFUNDING`, `REFUND_PENDING`, `REFUNDED`, `NEEDS_ATTENTION` |
| `VOIDING` | no | The void job is about to call PayPal for the enrollment's own hold. | `VOIDED`, `NEEDS_ATTENTION` |
| `REFUNDING` | no | The refund job is about to call PayPal. | `REFUNDED`, `REFUND_PENDING`, `REFUND_FAILED`, `NEEDS_ATTENTION` |
| `REFUND_PENDING` | no | PayPal reported the refund `PENDING`. | `REFUNDED`, `REFUND_FAILED`, `NEEDS_ATTENTION` |
| `VOIDED` | no | The hold was released. | Terminal. |
| `REFUNDED` | no | The refund `COMPLETED`. | Terminal. |
| `REFUND_FAILED` | no | The refund `FAILED` or was `CANCELLED`. | Terminal; needs an operator. |
| `CAPTURE_FAILED` | no | PayPal declined or refused the capture, or its deadline passed. The hold is voided. | Terminal, or `NEEDS_ATTENTION` when PayPal refuses the void. |
| `RELEASED` | no | The seat expired before the authorization. | `SEAT_RESERVED` when the buyer comes back to pay while payments are open and a seat is free. |
| `NEEDS_ATTENTION` | no | An operator must act (see below). | Terminal. |

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
| `succeeded` | Done. | Final. |

Automatic paths revive only a job that failed without a PayPal answer (a
skip), never an action PayPal refused. The return page revives an authorize
PayPal refused as `ORDER_NOT_APPROVED`, because the buyer is back with the
order approved.

A job whose retries are used up flags its enrollment `NEEDS_ATTENTION`,
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
| `reconcile:{attempt}:periodic:{minute}` | Periodic read-back (see Periodic work). |
| `close:{proposal}` | Close payments at `closes_at`. |
| `release_seats:{workshop}:{minute}` | Release the expired seats. |

Minutes are UTC (`yyyy-mm-ddThh:mm`), so a second enqueue within the same
minute is a no-op.

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
unknown. It becomes `NEEDS_ATTENTION` as soon as an enrollment is
`REFUND_FAILED` or `NEEDS_ATTENTION`, which also reopens a `SETTLED`
workshop.

The workshop is flagged `NEEDS_ATTENTION` when:

- an enrollment is `REFUND_FAILED` or `NEEDS_ATTENTION` in a resolving or
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
  confirmed;
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
- every two minutes enqueues `reconcile:{attempt}:periodic:{minute}` for the
  latest attempt of every `CAPTURE_PENDING` or `REFUND_PENDING` enrollment and
  for every attempt with a job unknown for more than five minutes, unless its
  fifth reconcile generation exists, and heals every `FUNDING` or `RESOLVING`
  workshop.

On shutdown the worker claims nothing new and lets the job it is running
finish for up to ten seconds; past that the PayPal call is cancelled and its
result is reconciled like any other ambiguous one.
