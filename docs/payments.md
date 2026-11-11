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

The payment window is at most 72 hours (`domain.MaxPaymentWindow`), the
period PayPal honours an authorization for: workshop creation refuses a
longer one, and opening payments refuses it too (`window_too_long`) for a
workshop created before the rule. A seat holds through its expiry instant
(`domain.SeatExpired`); a reservation first releases the workshop's lapsed
seats, so a seat no release job has freed yet never blocks a new one. A job
a request runs inline (the PayPal return, the checkout) is detached from the
request: a buyer closing the tab never cuts a PayPal call short.

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
money is. A flagged enrollment stays the participant's one active
enrollment whatever its status (migration 0005 widens the unique index), so
no second hold can be placed beside it; a flagged reserved seat is never
released and the PayPal return never authorizes a flagged enrollment. It is
still voided, captured or refunded by status when the workshop is healed or
closed, and a capture that ran out of retries is sent again once payments
have closed, which past its deadline fails it and voids the hold. It is
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
| `running` | Claimed by a worker. | `succeeded`; `failed` on a refusal; `unknown` on an ambiguous result; `pending` for a retry (5 s, 30 s, 2 min, 10 min); `needs_attention` after the fifth attempt, except the kinds that retry forever (`void`, `refund`, `reconcile`, `close`, `release_seats`: then 30 min and every hour after, flagged once at the fifth); `pending` again when its worker has not finished it after 5 minutes. |
| `unknown` | PayPal may or may not have performed the action. | `succeeded` or `pending` (same key) once a reconcile has read PayPal. |
| `failed` | Refused: by PayPal for good, by the worker (a passed deadline), or skipped because the action did not apply. | `pending` (same key) when revived. |
| `needs_attention` | Retries used up (`create_order`, `authorize`, `capture`, `payout`, `payout_check`; a close parked before this rule is revived by the periodic work). | `pending` (same key) only when revived by hand. |
| `succeeded` | Done. | Final. A succeeded seat release or read-only reconcile is deleted after 30 days (see Periodic work). |

Automatic paths revive only a job that failed without a PayPal answer (a
skip), never an action PayPal refused. The one exception is an authorize
PayPal refused as `ORDER_NOT_APPROVED`: the return page and a reconcile that
finds the order `APPROVED` both revive it, because the order is approved
now.

A job whose retries are used up flags its enrollment (the status stays),
unless the enrollment is paid or finished; a `close` or `release_seats` job
that keeps failing flags its workshop and keeps retrying; so does a void or
refund that keeps failing for an enrollment that cannot carry the flag (a
failed capture, a released seat, an older attempt). Retrying a void or a
refund forever is safe: every retry reuses the same `PayPal-Request-Id`, so
PayPal replays an action it already performed, and once it forgets the key
a void or full refund sent again meets `PREVIOUSLY_VOIDED` or
`CAPTURE_FULLY_REFUNDED`, both counted as done. An ambiguous answer is
reconciled instead of retried.

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
| `close:{proposal}` | Close payments at `closes_at`; an extension moves its `run_at` with the deadline (see [Deadline extension](#deadline-extension)). |
| `release_seats:{workshop}:{minute}` | Release the expired seats. |
| `payout:{workshop}` | Pay the organizer at the class start (see [Payouts](#payouts)). Its `sender_batch_id` is `cp-{workshop}`. |
| `reconcile:{attempt}:payout:{minute}` | Read back a paid seat whose fees the payout needs and whose capture PayPal no longer reports `COMPLETED`. |
| `payout_check:{payout}:created:{n}` | Read the payout batch back after sending; `n` from 1 to 5, at 0 s, 30 s, 2 min, 10 min and 1 h. |
| `payout_check:{payout}:periodic:{hour}` | Hourly read-back of a payout that is sent, unclaimed or unknown with a batch id. |
| `payout_check:{payout}:webhook:{event_id}` | Read-back after a verified payout webhook. |
| `payout_check:{payout}:operator:{minute}` | Read-back an operator asked for. |

Minutes (`yyyy-mm-ddThh:mm`) and hours (`yyyy-mm-ddThh`) are UTC, so a
second enqueue within the same minute or hour is a no-op.

## Ambiguous results

A result is ambiguous when PayPal may have performed the action: a timeout or
any transport failure, a 5xx, or a 409 (`PREVIOUS_REQUEST_IN_PROGRESS`). The
job becomes `unknown` and `reconcile:{attempt}:{n}` is scheduled after a gap
that grows with the generation: 30 s, 2 min, 10 min, 1 h and 6 h, so a PayPal
outage of a few minutes never uses the generations up. The reconcile reads the order, authorization, capture and refund from
PayPal and applies them; an unknown job whose resource now exists is
completed, and one whose resource does not exist is sent again under its own
key. The fifth generation sends nothing: it leaves the job `unknown` for an
operator and flags the enrollment, or the workshop when the enrollment is
finished or paid. Only numeric generations count towards that limit. While
the fifth waits its six hours, the periodic reconciles below keep reading
PayPal and sending the action again, so a PayPal that recovers in the
meantime still completes it without an operator.

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

A cancelled workshop is past every capture deadline, also when the organizer
cancels before `closes_at`: a queued capture of an `AUTHORIZED` enrollment is
skipped (the cancel voids the hold), and one already `CAPTURING` (resumed after
a retryable error, or overtaken by the cancel right after it moved) is failed
without sending it, with `capture_issue = WORKSHOP_CANCELLED`, and voided.

An authorization that lands once payments have closed, or on a cancelled
workshop, is voided; the authorize job itself refuses to call PayPal once
payments have closed.

## Fees

Every completed capture records PayPal's fee and net, in minor units, from
the capture's `seller_receivable_breakdown` (`payment_attempts.capture_fee_minor`
and `capture_net_minor`, migration 0014). `domain.CaptureFees` accepts a
breakdown only when the gross is the attempt's price, the fee and net are in
the same currency, the net is exactly gross − fee (a missing fee counts as zero
only when the net is the whole gross) and there is no `receivable_amount`, which
PayPal adds when it converted the money; anything else records nothing and is
logged.

Fees are written once, only while the attempt holds none, and get there in
three ways:

- **The capture's own answer.** When the capture call, or a reconcile reading
  the order or the capture back, reports it `COMPLETED` with an accepted
  breakdown. A `COMPLETED` capture without a breakdown records nothing and is
  logged as a warning.
- **A reconcile.** Any reconcile of the attempt (pending, unknown, webhook or
  operator) reads the capture again, so it fills the fees of a capture still
  `COMPLETED` at PayPal.
- **The periodic backfill.** Each periodic pass reads back, with `GET` capture,
  at most 20 attempts with a capture and no fees whose enrollment is `PAID` or
  `REFUNDED` (oldest update first), and records the fees from a capture PayPal
  reports `COMPLETED`, `REFUNDED` or `PARTIALLY_REFUNDED` under the same rule. It
  changes nothing else: no status, no job. An attempt PayPal does not return,
  or returns without an accepted breakdown, is logged and left for an hour (in
  memory). This is how a seat captured before migration 0014, or refunded
  since, gets its fees.

Every later update keeps them (the store writes them through `coalesce`),
including the refund path that records a capture with only its status, so a
sum of the stored net amounts is exact. The owner view shows each payment's
`fee` and `net`; a participant's view never does. The fake PayPal charges 3.49%
of the gross, rounded half up, plus 0.49.

## Payouts

Every payment lands in the PayPal Business account that owns the REST app.
When a confirmed class starts, CommitPool sends its organizer what the
participants paid, less PayPal's fees, with the PayPal Payouts API, to the
PayPal email given when the workshop was created (`workshops.payout_email`:
optional, validated like the organizer's email, never changed afterwards,
shown on the approve page and part of the approval's terms hash).

The payout waits for the class to start, not for the confirmation: until the
start the organizer can cancel, and a cancel refunds every participant, which
money already paid out could not cover.

`COMMITPOOL_PAYOUTS=1` turns sending on. It is off by default: a payout job
then waits, and the periodic sweep picks it up once payouts are on. Demo
workshops are paid out to `COMMITPOOL_DEMO_PAYOUT_EMAIL`. A workshop without a
payout email is never paid out.

### Ready to pay

`domain.PayoutBlocker` decides, and `store.BeginPayout` checks it again from
SQL, inside one transaction, under the workshop's row lock:

| Rule | Refusal |
|---|---|
| The workshop is `CONFIRMED`. | `not_confirmed` |
| Its resolution is not `NEEDS_ATTENTION`. | `needs_attention` |
| It has a payout email. | `no_payout_email` |
| Payments have closed (`closes_at`). | `payments_open` |
| The class has started (`starts_at`), unless the demo starts it early. | `not_started` |
| No enrollment is `AUTHORIZED`, `CAPTURING`, `CAPTURE_PENDING`, `VOIDING`, `REFUNDING` or `REFUND_PENDING`, and no `PAID` one has a refund under way (a refund job on its latest attempt that has not succeeded, or a refund recorded on it). | `payments_in_flight` |
| No enrollment is flagged. | `payment_flagged` |
| The latest attempt of every `PAID` enrollment has its fee and net. | `fees_missing` |
| The net is above zero. | `nothing_to_pay` |

A refusal is a typed `store.ErrPayoutNotReady{Reason}`: the job fails without
a PayPal answer (a wait) and never parks or flags anything. The periodic sweep
re-arms it with its attempts reset (`store.RearmJob`), so waiting never uses
up the retries the send needs later; a wait that no payment ends on its own
(`nothing_to_pay`, `payment_flagged`, `needs_attention`) is re-armed an hour
later instead of every two minutes.

### Freeze and lock

When the rules hold, `BeginPayout` sums the net of the latest attempt of every
`PAID` enrollment and inserts the workshop's one `payouts` row (migration
0015) with the amount (the net), the gross, the fee, the seats, the receiver
and `sender_batch_id = "cp-" + workshop id`, all frozen. In the same
transaction it stamps the latest attempt of every one of those `PAID`
enrollments with the payout (`payment_attempts.payout_id`), checks that the
stamped seats add up to exactly the frozen amounts (any difference rolls the
whole payout back), and sets `workshops.payout_locked_at`. The organizer cancel's
compare-and-set requires that lock to be empty, so a cancel and a payout
racing never both win. From the lock on the class counts as started
(`domain.EffectiveStart`, the earlier of `starts_at` and the lock): the
organizer can no longer cancel, and a capture that completes after it is
refunded, not kept unpaid.

A payment is either paid out or refunded, never both. The refund job moves a
seat to `REFUNDING` only under a key-share lock on the workshop row
(`store.StartRefund`), the lock `BeginPayout` takes for update, and refuses an
attempt the payout stamped ("paid out to the organizer"), leaving it `PAID`
and calling nothing at PayPal. A late capture on a stamped attempt keeps its
seat instead of queueing a refund, and the operator's retry refund refuses a
stamped attempt (`409 paid_out`).

### Sending

`payout:{workshop}` is enqueued when the class is confirmed (with payouts on
and a payout email), to run at `starts_at`. It is not one of the kinds that
retry forever. In order:

1. A cancelled workshop pays nothing: the job succeeds without a call.
2. A missing fee is read back first (`GET` capture, the [Fees](#fees) rule),
   but only from a capture PayPal reports `COMPLETED`: any other status
   (refunded, partially refunded, pending) enqueues
   `reconcile:{attempt}:payout:{minute}` and the payout waits with
   `payments_in_flight`.
3. The payout is frozen; a refusal waits (above).
4. A payout that already has a batch, or is no longer `created`, is only
   checked, never sent again.
5. A payout created more than 25 days ago is never sent: PayPal refuses a
   reused `sender_batch_id` for 30 days, and past that a second batch could
   go out. It becomes `unknown` with `PAYOUT_TOO_OLD_TO_SEND`.
6. `POST /v1/payments/payouts`, built from the row alone: one item,
   `recipient_type: EMAIL`, the receiver, the amount as `{value, currency}`,
   `sender_item_id` = the workshop id and the note `CommitPool payout for
   {title}`, under the row's `sender_batch_id`, with the job key as
   `PayPal-Request-Id`.

| PayPal's answer | Outcome |
|---|---|
| 201 with a batch id | `sent`, with the batch id and status; checks start. |
| A transport failure, a timeout, a 5xx, a 409, or a 201 without a batch id | Retried under the same `sender_batch_id` and `PayPal-Request-Id` (5 s, 30 s, 2 min, 10 min), then parked `needs_attention`. |
| A duplicate `sender_batch_id` that links to the batch | `sent`, with that batch id; checks start. The job succeeds. |
| A duplicate without a link to the batch | `unknown` with `DUPLICATE_SENDER_BATCH_ID` and PayPal's debug id. Never sent again; an operator looks the batch up. |
| Any other 4xx | `failed`, with PayPal's issue (or name) and debug id. |

Which answers count as a duplicate is decided in one place,
`paypal.DuplicatePayout`: a 4xx whose name or detail issue is
`SENDER_BATCH_ID_ALREADY_USED`, `DUPLICATE_SENDER_BATCH_ID`,
`DUPLICATE_REQUEST_ID` or `BATCH_ALREADY_EXISTS`, or whose message or detail
mentions `sender_batch_id` with "already" or "duplicate" (such as PayPal's
`USER_BUSINESS_ERROR` "Batch with given sender_batch_id already exists").
`(*paypal.Error).ExistingPayoutBatchID` reads the batch id from any link to
`/v1/payments/payouts/{id}`, in the error's `links` or a detail's `link` or
`links`.

### Statuses and checks

| Status | Meaning |
|---|---|
| `created` | Frozen; not known to be at PayPal yet. |
| `sent` | PayPal has the batch; the item is not final. |
| `succeeded` | The item is `SUCCESS`: the money is in the organizer's PayPal account. |
| `unclaimed` | The item is `UNCLAIMED`: no PayPal account has that email yet. PayPal returns it after 30 days (then `failed`). |
| `failed` | PayPal refused the payout, or reports the item `FAILED`, `RETURNED`, `BLOCKED`, `REFUNDED`, `REVERSED`, `DENIED` or `CANCELED`, or the batch `DENIED` or `CANCELED`. |
| `unknown` | CommitPool cannot tell whether a batch exists; it is never sent again. |

`payout_check:{payout}:{bucket}` reads `GET /v1/payments/payouts/{batch}`,
picks the item whose `sender_item_id` is the workshop and maps it with
`domain.PayoutState`; anything not listed above stays `sent`. A succeeded or
failed payout only ever moves to the other final status. It records the
batch and item statuses, the item and transaction ids and the item's error
name as the issue. The first check runs right after sending and, while the
payout is not final, schedules the next one (30 s, 2 min, 10 min, 1 h, five
in all); after that the periodic sweep checks it hourly. A read PayPal does
not answer, a 404 for a batch just created included, is retried under the
same key (5 s, 30 s, 2 min, 10 min) and then parked; it never changes the
payout. A payout that turns `succeeded` queues
`notify:owner_paid_out:{workshop}`.

Payout problems never flag the workshop `NEEDS_ATTENTION`: that flag can be
cleared without fixing the payout. `/healthz` counts them instead,
`payouts.failed` and `payouts.unknown`, and a parked payout job counts in
`jobs.parked`.

### Organizer page and demo

The owner view's `payout` block is `null` until the class is confirmed. Then
it has a `state`: `no_email`, `off` (payouts switched off), `scheduled` (with
`scheduled_at`, the start), `waiting` (the class started but a rule above
still refuses, named in `waiting`), `created` (frozen, PayPal has not
confirmed a batch yet), `parked` (frozen, and its job is parked for an
operator), `sending` (`sent`), `paid`, `unclaimed`, `failed` or `unknown`;
`locked` once the payout is frozen (the organizer page then hides Cancel); the
receiver; the amount, gross and fees
with `estimated` until the row freezes them (no amount while a fee is
unknown); and once sent the PayPal batch, item and transaction ids, their
statuses, the issue and the debug id. The MCP `get_status` returns it with a
sentence.

`POST /api/v1/owner/{token}/demo/start-class` (demo mode only; 404 otherwise)
starts the class now: it freezes the payout early, runs `payout:{workshop}`
inline and then its first check, and answers `202 {payout}`. It is refused
with `409 payout_not_ready` and the rule's `reason` above (or
`payouts_off`). The organizer page offers it as **Start the class now** once
the class is confirmed and reservations are closed, and only before any
payout.

`POST /api/v1/owner/{token}/operator/check-payout` reads the batch again
inline (`payout_check:{payout}:operator:{minute}`); `409 no_payout` before
there is one. The organizer page offers it as **Check the payout with
PayPal** while the payout is sending, unclaimed or unknown.

### Known limit: money that moves after the payout

Once a payout is sent, a later refund, reversal or dispute on a payment it
counted does not reduce it: the payout already went to the organizer, and the
platform's PayPal account absorbs the loss. CommitPool never refunds a
payment it paid out itself (see above), but PayPal can still move one, through
a dispute or a refund made in the PayPal account, and a reconcile then records
it on the seat.

### What the Sandbox probe confirms

These are the PayPal behaviours the code assumes and the Sandbox probe checks
before the hosted demo relies on them:

- `POST /v1/payments/payouts` answers 201 with `batch_header.payout_batch_id`
  and a `PENDING` (or `PROCESSING`) `batch_status`, and no items.
- PayPal honours `PayPal-Request-Id` on that call: the same id replays the
  first answer.
- A second batch with the same `sender_batch_id` is refused with a 4xx that
  the matcher above recognises, ideally with a link to the first batch. The
  exact name, status and shape are what the probe records.
- `sender_batch_id` stays unique for 30 days.
- `GET /v1/payments/payouts/{batch}` lists the item with its
  `payout_item_id`, `transaction_id`, `transaction_status` and
  `payout_item.sender_item_id`, and an `errors.name` on a failed or unclaimed
  item.
- An insufficient balance or an invalid receiver is a 4xx and creates no
  batch.
- The payout webhooks carry `payout_batch_id` (`batch_header` for batch
  events; the item's fields for item events, including `sender_batch_id`).

## Close

`close:{proposal}` runs at `closes_at` and first heals the workshop (see
below). Then:

| Workshop at close | Outcome | Voids | Refunds | Reconciles |
|---|---|---|---|---|
| `FUNDING`, completed payments ≥ minimum | `CONFIRMED` | `AUTHORIZED` whose capture has not started | none | `CAPTURING`, `CAPTURE_PENDING` |
| `FUNDING`, below the minimum | `CANCELLED`, `RESOLVING` | `AUTHORIZED` | `PAID` | `CAPTURING`, `CAPTURE_PENDING` |
| `CONFIRMED` | unchanged | `AUTHORIZED` whose capture has not started | none | `CAPTURING`, `CAPTURE_PENDING` |

A close that runs more than a minute before its proposal's `closes_at` does
nothing: it is refused as a skip (`failed`, no PayPal answer), which never
flags the workshop, and the periodic work runs it again once it is overdue.

Captures still in flight are never counted as paid. The status is written by
compare-and-set before anything is enqueued; when the workshop changed
meanwhile the decision is taken again. Once a cancellation is written, what
it owes is derived again from a fresh read, so a payment that completed in
between is refunded by the close itself. Every key is fixed by its attempt,
so running the close again enqueues nothing new.

A payment that completes after a cancellation is refunded. One that
completes after a confirmation keeps its seat until the workshop starts, or
until its payout locked the workshop if that came first
(`domain.EffectiveStart`), and is refunded after that.

## Deadline extension

When a class is short of its minimum near its deadline, the organizer can
move the deadline later, once (`domain.CanExtend`, migration 0016). The one
option on offer (`domain.ExtendedDeadline`) is

```
LatestDeadline = floor(min(opens_at + 72 h, starts_at − 24 h), 15 min)
```

and never later than 72 hours after the oldest hold that may still be live
(each payment attempt of the workshop that holds an authorization, or belongs
to a seat still taken, counts from its `created_at`, which precedes its
authorization), so every hold is still within the three days PayPal honours
it and the class keeps its day between the close and the start.

It is refused, each with its own code:

| Refusal | When |
|---|---|
| `not_funding` | The workshop is not `FUNDING`, or payments have closed. |
| `too_late_to_extend` | Less than 10 minutes are left. |
| `already_extended` | The proposal was extended once (`extended_at` is set). |
| `already_funded` | Completed payments reach the minimum. |
| `charging_started` | Any seat is `PAID`, `CAPTURING` or `CAPTURE_PENDING`, a capture job exists for a seat still taken, or the holds alone reach the minimum (charging is about to start): those people could no longer leave during the extra time. |
| `consent_predates_extension` | A seat still taken was reserved under a consent version outside `domain.ExtensionConsents` (an explicit list, `consent-v5` today), so its holder never agreed that the deadline could move. |
| `extension_too_small` | The option adds less than an hour. |

`POST /api/v1/owner/{token}/extend` `{from, to}` (the current `closes_at` and
the option the page showed) runs `store.ExtendPaymentWindow` in one
transaction: it locks the workshop, then the proposal, then (`for update`, in
id order) every seat still taken and every released one, so a hold or a charge
landing meanwhile either commits first and is seen or waits for the
extension; `REFUND_FAILED` rows, which the refund retry locks before the
workshop, stay outside. It evaluates `CanExtend` again on those rows,
requires the stored
`closes_at` to equal `from` and the option to equal `to` (`409
extension_changed` otherwise), writes `closes_at`, `extended_from` and
`extended_at` (both null or both set), and moves the pending
`close:{proposal}` job to the new deadline. That update must touch exactly one
pending row; a close that is running, finished, failed or missing rolls the
whole extension back (`409 close_in_progress`). An in-process caller (an MCP
tool) is refused with `403 approval_required`, and a request without a
browser's same-origin headers (`Sec-Fetch-Site: same-origin`, or a matching
`Origin`) with `403 extension_needs_page`: only the organizer's page extends.

Once extended:

- every seat still taken whose participant left an email is told
  (`notify:deadline_extended:{enrollment}`); holds stay as they are, and
  anyone can still withdraw until charging starts;
- the capture deadline and payments open follow the new `closes_at`, so a
  late hold is captured until then;
- seat releases (`ReleaseExpiredSeats`, `ReleaseReservedSeats`) take the
  workshop lock first, as reservations and the extension do, so a release
  and an extension never wait on each other's seats;
- shortening the window later (the demo's **Close reservations now**) keeps
  `extended_from`, and no second extension is offered;
- a lapsed seat reserved under a consent outside `domain.ExtensionConsents`
  is never taken again (`store.ReReserveSeat` refuses it, so a late PayPal
  return or reconcile does not authorize it); its holder never agreed to the
  later deadline.

The owner view carries `extension` (`available`, `from`, `to` and an
`approve_url` when on offer, or the `refusal` code, with a `reason` either
way), and the proposal in the owner, participant and invite views carries
`extended_from` and `latest_closes_at` (`domain.LatestPossibleDeadline`), a
date a later extension cannot pass. While the deadline may still move (the
workshop is `FUNDING`, at least 10 minutes are left, it was never extended and
no seat is `PAID`), it is `LatestDeadline`, the option without the cap of the
holds, which only rises as old holds leave. It stays published while a refusal
can still lift: a seat under an older consent that may be released, holds that
reach the minimum and may drop before charging, a charge (`CAPTURING`,
`CAPTURE_PENDING`) that may be declined and free its seat, or a gain under an
hour that grows once the oldest hold leaves. It is `closes_at` itself once the
deadline cannot move (already extended, a seat paid or the class funded, under
10 minutes left, payments closed, or `LatestDeadline` adds less than an hour).
While the class is funding, a paid seat leaves `PAID` only through a refund or
dispute made in the PayPal account, outside the app. The organizer's
`extension.to` and the rescue keep the hold-capped option, the one the server
accepts now. Other responses carry `closes_at`. The participant's card shows
it as "Latest possible deadline", followed by "The organizer can extend the
deadline once, up to this date." when it is later than `closes_at`, and "This
deadline will not move." (or "It was already extended once.") otherwise, and
both pages show "Deadline extended from X to Y" while the class is funding.

The suggestion `rescue` (see `domain.Suggestions`) appears for a `FUNDING`
class short of its minimum with an extension on offer and at most six hours
left (`domain.RescueWhenLeft`). The server writes its reason; Claude drafts
the message for the group once the deadline moves (the new deadline and how
many are missing), under the same rules as every suggestion, with the plain
message as the fallback and a filter that only lets it name the class's date,
the current deadline and the new one. While the rescue is offered, the
`share` suggestion is not, so no message for the group names the deadline
about to change; the per-person nudges stay. After an extension the `share`
suggestion appears whatever the time left.

## Participant withdrawal

`POST /api/v1/p/{token}/withdraw` gives the participant's seat up before they
are charged, under the enrollment's row lock (`store.Withdraw`, keeps
`withdrawn_at`):

| Status | Withdrawal |
|---|---|
| `SEAT_RESERVED` | `RELEASED` at once; refused (`hold_under_review`) while PayPal reviews the hold (`PENDING`). |
| `AUTHORIZED` | `VOIDING`, then `void:{attempt}` runs inline; a capture still queued finds the hold gone and is skipped. |
| `CAPTURING`, `CAPTURE_PENDING` | Refused, `capture_in_progress`. |
| `PAID` | Refused, `paid_no_self_refund`: the minimum counts completed payments, so a paid seat is refunded only if the workshop is cancelled, also by the organizer. |
| Flagged | Refused, `needs_attention`. |
| Anything else | `nothing_to_withdraw`. |

A withdrawn enrollment never takes a seat again (`ReReserveSeat` refuses
it, so a later PayPal return or reconcile does not bring it back); the
person can still commit again with a new enrollment while payments are
open. Its seat reads **Gave up the spot**, **Gave up the spot — releasing the hold** while
the void is under way, and **Gave up the spot — hold released** once it went
through, never "Reservation expired". The consent (`consent-v5`, see
[Consent versions](#consent-versions)) says all this before anyone
reserves.

A participant whose availability in force rules out the proposal's date
(`domain.Compatible`) is refused a checkout with `date_incompatible`; one who
never said when they can come is let through, since the consent names the
date.

## Notifications

A participant may leave an email when joining, and the organizer when
creating the workshop. Notifications go through an outbox (`notifications`,
one row per idempotency key) and a `notify` job under the same key, which
retries forever on a transient failure and marks the row `failed` on a final
refusal (an unverified recipient while SES is in its sandbox). They never
block a money move: a notification that cannot be queued is logged.

Each message is rendered once, when it is queued, by `internal/notice` into a
text and an HTML part. When its turn comes, the worker checks it is still
true and marks it `skipped` otherwise: a "spot reserved" whose hold was
already charged or released, an "It's on" for a workshop cancelled since, or a
moved deadline once the class is decided.

| Key | To | When |
|---|---|---|
| `notify:joined:{participant}` | Participant | On joining. Carries the private link, since only its hash is kept. |
| `notify:reserved:{enrollment}` | Participant | Their hold landed while reservations are open and the class is not on yet. |
| `notify:confirmed:{enrollment}` | Participant | The workshop is confirmed (by a capture round or at the close), for each paid seat, and for a seat paid after. Carries the `.ics`; if SES refuses the message, it goes again once without the attachment. |
| `notify:cancelled:{participant}` | Participant | The workshop is cancelled, at the close (not enough people by the deadline) or by the organizer (with the reason). |
| `notify:payment_failed:{enrollment}` | Participant | A charge failed and the hold is released. |
| `notify:deadline_extended:{enrollment}` | Participant | The organizer extended the deadline, for each seat still taken: both deadlines, and that the spot and hold stay. Skipped once the workshop is no longer `FUNDING` or the seat was given up. |
| `notify:refund_completed:{enrollment}` | Participant | A refund completed. |
| `notify:owner_created:{workshop}` | Organizer | On creation, queued in the same request: the only moment the organizer link exists. Carries it and the invite message. |
| `notify:owner_confirmed:{workshop}` | Organizer | The workshop is confirmed: how many paid and the total. |
| `notify:owner_cancelled:{workshop}` | Organizer | The workshop is cancelled, with what happened to the money. |
| `notify:owner_paid_out:{workshop}` | Organizer | PayPal reports the payout paid: the amount, the PayPal email it went to, what was paid and PayPal's fees. |

Every refused or skipped message drops its body and HTML. The two carrying a
private link (`joined`, `owner_created`) also drop them once sent, and give
up after five failed attempts so the link is not kept while a provider keeps
failing.

Delivery is at least once: a send that succeeded at the provider but was not
recorded is sent again.

## Organizer cancel

`POST /api/v1/owner/{token}/cancel` (optional `reason`, at most 280
characters) cancels the workshop any time before it starts, once
(`domain.CanCancel`; `already_cancelled` and `workshop_started` otherwise).
Once the organizer's payout has locked the workshop it counts as started:
the cancel is refused with `workshop_started`, whatever the time. It is one
compare-and-set on status, resolution and an empty `payout_locked_at`
(`CANCELLED`, with `cancelled_at` and `cancel_reason`): `SETTLED` at once when payments never
opened, `RESOLVING` otherwise, and still `NEEDS_ATTENTION` when the workshop
was flagged. A workshop with payments then releases its reserved seats (a
seat kept for a `PENDING` authorization stays until PayPal decides) and is
healed, which enqueues `void:{attempt}` for every hold and `refund:{attempt}`
for every payment: the keys a later `close:{proposal}` derives too, so the
close adds nothing. A capture still in flight is refunded when it lands
(`domain.LateCaptureAfterCancel`). In a confirmed workshop this refunds every
participant; nothing else gives money back once a seat is paid.

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
- a `close` or `release_seats` job fails a fifth time (it keeps retrying).

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
  `needs_attention` once their attempts are used up, unless their kind
  retries forever) and enqueues `release_seats` for every workshop with a
  seat that expired (a seat holds through its expiry instant, and one kept
  for a `PENDING` authorization is not released);
- every two minutes enqueues `reconcile:{attempt}:periodic:{hour}` for the
  latest attempt of every `CAPTURE_PENDING` or `REFUND_PENDING` enrollment,
  and of every `SEAT_RESERVED` enrollment whose authorization PayPal reports
  `PENDING` (so a pending authorization, capture or refund is read back
  within two minutes of turning pending and then once an hour; the
  authorization then moves `CREATED` → `AUTHORIZED` or `DENIED` →
  `CAPTURE_FAILED`, which frees the seat), and `reconcile:{attempt}:periodic:{minute}`
  for every attempt with a job unknown for more than five minutes, unless its
  fifth reconcile generation has run; runs again the close of every workshop
  still `FUNDING` more than five minutes past its `closes_at`, extended or not
  (enqueued, or revived when it failed, was refused as early or was parked);
  then heals every `FUNDING` workshop and
  every workshop whose money is not settled (`RESOLVING`, or cancelled and
  not `SETTLED`, `NEEDS_ATTENTION` included);
- on the same two-minute tick deletes the `succeeded` jobs last updated
  more than 30 days ago that only read or released something:
  `release_seats` and every reconcile except a numbered generation
  (`reconcile:{attempt}:{n}`). Payment actions, `close` and numbered
  generations are kept whatever their age, because the worker decides from
  their existence (healing, the next generation and the last one that leaves
  a job to an operator). The count is logged when it is not zero;
- then runs the fee backfill (see [Fees](#fees));
- last, the payout sweep (see [Payouts](#payouts)): with payouts on, every
  confirmed workshop with a payout email whose class has started and whose
  payout is missing or still `created` gets `payout:{workshop}` enqueued, or
  re-armed with its attempts reset when it waited (an hour later for a wait
  that needs an operator or has nothing to pay; never when PayPal refused it
  or it is parked); and
  every payout that is `sent`, `unclaimed` or `unknown` with a batch id gets
  `payout_check:{payout}:periodic:{hour}`. Succeeded payout checks are deleted
  after 30 days like the read-only reconciles.

On shutdown the worker claims nothing new and lets the job it is running
finish for up to ten seconds; past that the PayPal call is cancelled and its
result is reconciled like any other ambiguous one.

## Operator runbook

Three actions on the owner link replace most of the SQL below, for the
workshop's own enrollments only:

| Route | What it does |
|---|---|
| `POST /api/v1/owner/{token}/operator/reconcile` `{enrollment_id}` | Reads the enrollment's latest attempt back from PayPal inline (`reconcile:{attempt}:operator:{minute}`); safe at any time. |
| `POST /api/v1/owner/{token}/operator/retry-refund` `{enrollment_id}` | For a `REFUND_FAILED` enrollment only, and never for a payment the organizer's payout counted (`409 paid_out`): keeps the failed refund's id in `failed_refund_ids`, sends the refund again inline under `refund:{attempt}:retry:{n}`, puts the enrollment back to `PAID` with its flag cleared and reopens the workshop's settlement. A new key is safe: a failed refund returned nothing, and PayPal refuses a refund beyond what was captured. PayPal keeps listing the failed refund on the order, so a reconcile or webhook ignores any refund in `failed_refund_ids`. |
| `POST /api/v1/owner/{token}/operator/clear-flag` `{enrollment_id?}` | Clears the enrollment's flag once its money is settled: none of its jobs is unknown, running, parked or still failing after the attempt that flagged it, and PayPal keeps no hold on a seat that should hold none (failed capture, released, withdrawn or voiding); without an enrollment, clears the workshop's `NEEDS_ATTENTION` once no enrollment needs an operator and every enrollment's money is settled in the same sense (`RESOLVING` and settled again when cancelled, `OPEN` otherwise). `still_unresolved` otherwise. |

The organizer page offers them as buttons in a **Needs your check** panel,
shown only while a seat is flagged or has a refused refund, or the workshop is
flagged. The SQL below remains for what these do not cover.

One more route only reads, and the organizer page offers it on its Payments
tile as **Verify with PayPal** (distinct from the panel's **Check with PayPal**,
which writes):

| Route | What it does |
|---|---|
| `POST /api/v1/owner/{token}/paypal/verify` | Reads the order, authorization, capture and refund of each seat's latest attempt from PayPal and compares each with what the attempt implies PayPal reports: the status, the amount and, once recorded, the fee and net. Returns per seat `{enrollment_id, seat, checks: [{resource, id, local, paypal, match, debug_id}]}`, where only a check PayPal answered with an error carries PayPal's `debug_id`, and a `summary` of records, matching records, seats and skipped seats. A capture recorded `COMPLETED` or `REFUNDED` without fees shows `fee not recorded, net not recorded` against the fees PayPal reports, so the gap counts as a difference until the backfill fills it. It never runs a job or writes a row. One run per workshop every 30 seconds, counted once the seats are loaded and before PayPal is read (`429 verify_too_soon` with `Retry-After` otherwise, kept in memory), and at most 12 seats a run, in enrollment order. The MCP tool `verify_with_paypal` calls it. |

What an attempt implies PayPal reports: an order with an authorization is
`COMPLETED` and one recorded `EXPIRED` matches PayPal's not found; an
authorization whose capture is `COMPLETED`, `PENDING`, `REFUNDED` or
`PARTIALLY_REFUNDED` is `CAPTURED`; a capture with a completed refund is
`REFUNDED`; otherwise the status the attempt recorded.

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
   or (j.status = 'pending' and j.attempts >= 5)
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
capture, or `AUTHORIZATION_EXPIRED_LOCAL` / `CAPTURE_DEADLINE_PASSED` /
`WORKSHOP_CANCELLED`), the capture's PayPal `fee` and `net` once recorded, plus
the workshop's status and resolution.

Only the `jobs` table shows which action is stuck and why: the job status,
`attempts`, `run_at`, `http_status`, `provider_issue` (for a capture the
owner view shows it as `capture_issue`), `debug_id` and `last_error`. Older attempts of an
enrollment, `auth_expires_at`, `capture_debug_id`, `capture_reason` and
`refund_reason` are only in `payment_attempts`.

### Payouts

Payouts that are not done, with their job:

```sql
select p.id, w.id as workshop_id, w.title, p.status, p.receiver, p.amount_minor, p.currency,
       p.sender_batch_id, p.payout_batch_id, p.item_status, p.issue, p.debug_id, p.created_at,
       j.status as job_status, j.attempts, j.last_error
from payouts p
join workshops w on w.id = p.workshop_id
left join jobs j on j.idempotency_key = 'payout:' || w.id
where p.status <> 'succeeded'
order by p.updated_at;
```

A confirmed class that started and has no payout row is waiting: its
`payout:{workshop}` job is `failed` with a `last_error` of
`payout waits: {rule}` (the rules are in [Payouts](#ready-to-pay)). Settle what
the rule names (a payment in flight, a flag to clear); the periodic sweep runs
the job again within two minutes.

- **Parked** (`created`, job `needs_attention`): PayPal never answered
  clearly. Within 25 days of `created_at`, revive the job (see
  [Reviving a job](#reviving-a-job)). It is sent under the same
  `sender_batch_id` and `PayPal-Request-Id`, so PayPal replays the batch,
  names it as a duplicate, or creates it once.
- **Unknown** (`DUPLICATE_SENDER_BATCH_ID` or `PAYOUT_TOO_OLD_TO_SEND`): find
  the batch in the PayPal account (its payouts history, by the
  `sender_batch_id` `cp-{workshop}`, or through PayPal support with the
  `debug_id`). If it exists, record it; the next hourly check, or **Check the
  payout with PayPal**, reads it:

  ```sql
  update payouts set payout_batch_id = $2, status = 'sent', issue = null, debug_id = null, updated_at = now()
  where id = $1 and status = 'unknown' and payout_batch_id is null;
  ```

  If PayPal has none and the payout is younger than 25 days, set it back to
  `created` and revive `payout:{workshop}`. Older than that, pay the
  organizer from the PayPal account by hand and record it as
  `failed` with `issue = 'PAID_BY_HAND'`, so it is never sent.
- **Failed before sending** (no `payout_batch_id`, such as
  `INSUFFICIENT_FUNDS` or a receiver PayPal refuses): fix the cause, then,
  within 25 days, send it again under the same `sender_batch_id`:

  ```sql
  update payouts set status = 'created', issue = null, debug_id = null, completed_at = null, updated_at = now()
  where id = $1 and status = 'failed' and payout_batch_id is null;
  ```

  and revive `payout:{workshop}` (it failed with a PayPal answer, so revive
  it by hand). The receiver cannot change: it is part of the terms people
  approved.
- **Failed after sending** (`RETURNED`, `BLOCKED`, `REFUNDED`, `REVERSED`, or
  a `DENIED` batch): PayPal holds or returned the money. Quote the batch,
  item and debug ids to PayPal support, or pay the organizer by hand; never
  send a second batch.
- **Unclaimed**: no PayPal account uses that email yet. Ask the organizer to
  sign up, or add that email to their account, and accept it. PayPal returns
  an unclaimed payout after 30 days (`RETURNED`, then `failed`).

## Consent versions

Every enrollment records the version of the consent its participant
accepted (`enrollments.consent_version`), and a checkout is refused unless it
names the version in force, so nobody reserves on text they did not see. A
page left open while the version changes gets `consent_version_mismatch` and
shows the new text before the participant tries again.

`consent-v5` (current):

> Reserving your spot places a temporary hold for the price on your PayPal
> account. Nothing is charged yet. Holds are charged once enough people have
> reserved, and the workshop is on when the minimum number of payments has
> gone through. If the minimum is not reached by the deadline, the workshop is
> cancelled: every hold is released and any charge already made is refunded.
> You can leave and have your hold released until charging starts; after
> that, your payment is refunded only if the workshop is cancelled, including
> when the organizer cancels it. Refunds can take a few days, and you can
> follow yours on your page. The organizer can extend the deadline once, to
> at most three days after reservations opened. Your page always shows the
> current deadline and the latest possible one, and you can leave and release
> your hold until charging starts.

v5 adds the last two sentences. Only enrollments that accepted a version in
`domain.ExtensionConsents` (v5) allow an extension; a workshop with a seat
still taken under v4 or earlier keeps its deadline.

`consent-v4` (from 8 November 2026 until v5), kept for the enrollments that
accepted it:

> Reserving your spot places a temporary hold for the price on your PayPal
> account. Nothing is charged yet. Holds are charged once enough people have
> reserved, and the workshop is on when the minimum number of payments has
> gone through. If the minimum is not reached by the deadline, the workshop is
> cancelled: every hold is released and any charge already made is refunded.
> You can leave and have your hold released until charging starts; after
> that, your payment is refunded only if the workshop is cancelled, including
> when the organizer cancels it. Refunds can take a few days, and you can
> follow yours on your page.

`consent-v3` (from 6 November 2026 until v4), kept for the enrollments that
accepted it:

> Reserving your spot places a temporary hold for the price on your PayPal
> account. Nothing is charged yet. Holds are charged once enough people have
> reserved, and the workshop is on when the minimum number of payments has
> gone through. If the minimum is not reached by the deadline, the workshop is
> cancelled: every hold is released and any charge already made is refunded.
> You can leave and have your hold released until you are charged; after
> that, your payment is refunded only if the workshop is cancelled, including
> when the organizer cancels it. Refunds can take a few days, and you can
> follow yours on your page.

v4 changes one sentence: withdrawing is refused as soon as charging starts
(`CAPTURING`, `CAPTURE_PENDING`), not only once the charge has gone through.

`consent-v2` (5 to 6 November 2026), kept for the enrollments that accepted
it:

> You authorize a temporary hold. We will attempt to charge it once enough
> people have authorized. The workshop is confirmed when the minimum number of
> completed payments is reached. If the minimum is not reached by the
> deadline, the workshop is cancelled and we request a refund of any charge
> already made. You can withdraw and have your hold released until you are
> charged; after that, payments are refunded only if the workshop is
> cancelled, also when the organizer cancels it. Refunds can take time and you
> will be able to follow their status.

v2 and v3 say the same thing; v3 says it in the words the app now uses.

## Implementation reference

The code carries no comments. The rules it follows, beyond the sections above,
are here.

### Return from PayPal

- PayPal sends the buyer to `/paypal/return?e={enrollment}&token={order}`. The
  query only finds the checkout: the order must be the enrollment's latest
  attempt's, and PayPal must report it for this enrollment, this attempt and
  the proposal's price and currency before `authorize:{attempt}` runs.
- The buyer is redirected to `/paypal/returned` with the enrollment and the
  outcome only, never a token or a PayPal issue.
- A checkout whose hold is already recorded, or whose enrollment is past
  checkout, is answered from the stored state without asking PayPal, so a
  repeated return sends nothing twice.
- An authorize skipped earlier is revived automatically; one PayPal refused as
  `ORDER_NOT_APPROVED` is revived because the buyer is back with the order
  approved. Any other refusal, or a job running, unknown or waiting for its
  retry, is left alone.
- A seat past its expiry is taken again under the capacity rule
  (`store.ReReserveSeat`) for the configured seat TTL. When that is refused the
  enrollment is read again, since another request or a reconcile may have
  renewed the seat or placed the hold first.
- An order PayPal no longer knows expired unapproved: the attempt records it
  and the next checkout opens a new one. When PayPal does not answer,
  `reconcile:{attempt}:return:{minute}` reads the checkout back shortly and the
  buyer is told it is pending.
- `/paypal/cancel` records the cancel on a `SEAT_RESERVED` enrollment; the seat
  is kept until it expires, so the buyer can still approve the same checkout.
- The inline authorize runs on even if the buyer closes the tab, so PayPal's
  answer is recorded, not lost.

### Webhooks

- A delivery is checked for plausibility first (certificate URL on https
  `*.paypal.com`, transmission time at most five minutes ahead), then verified
  with PayPal (`verify-webhook-signature`, every transmission header and the
  body byte for byte) before anything is stored.
- An event is stored once by its id and answered `200` every time, a repeat
  included.
- The webhook never applies a payment state. It finds the attempt from the
  resource or its related capture, authorization or order, and enqueues
  `reconcile:{attempt}:webhook:{event_id}`; the reconcile reads PayPal and
  applies what it finds, so an early or repeated event cannot count a payment
  twice.
- `PAYMENT.PAYOUTSBATCH.*` and `PAYMENT.PAYOUTS-ITEM.*` events find the payout
  by `payout_batch_id`, `sender_batch_id` or `payout_item_id` (from the
  resource or its `batch_header`) and enqueue
  `payout_check:{payout}:webhook:{event_id}`. They never set a payout status;
  the only thing they write is a missing PayPal batch id, from a delivery
  PayPal has verified, which moves a `created` or `unknown` payout to `sent`;
  a payout that already holds another batch keeps it, the event is logged and
  still answered `200`.
- The log names a verified event and its attempt, never the body, and nothing
  of an unverified delivery.

### Worker

- Three loops run side by side, each recovering from a panic and recording a
  heartbeat for `/healthz`: claiming due jobs (every two seconds when idle),
  taking back jobs claimed more than five minutes ago plus enqueuing seat
  releases (every minute), and the periodic reconciles and heals (every two
  minutes).
- On shutdown no new job is claimed; a job already sent to PayPal may run on
  for a short grace period so its answer is recorded instead of becoming
  unknown, and results are written in a context that outlives the shutdown.
- A handler's error decides the next step: an ambiguous PayPal error parks the
  job as `unknown` and schedules the next reconcile generation; a recorded
  refusal fails the job and keeps its key; anything else (a 429, a store
  failure, a panic) retries it under the same key.
- `EnqueueOrRevive` revives only a skip, never an action PayPal refused;
  `Revive` is for callers that know something changed (the buyer is back with
  an approved order, an operator).
- A late result for a job that was taken back or settled elsewhere is dropped
  (`ErrJobNotActive`), never written over the newer state.
- A pending reconcile of the attempt that has not started yet resolves a new
  unknown job too, so no extra generation is enqueued.

### Job store

- Every transition is a status-guarded update: only a running job can be
  completed, retried, failed or parked as unknown (an unknown job can also be
  completed by its reconcile). A guard that matches no row reports
  `ErrNotFound` or `ErrJobNotActive`.
- Claiming skips rows another worker is locking, so no job runs twice at once.
- Enqueuing an existing key writes nothing and keeps the stored payload,
  status and `run_at`.
- `ReenqueueJob` and `ReviveJob` reuse the row and the key, keeping the
  attempt count, so a retry reuses the PayPal-Request-Id. `AdvanceJob` only
  brings a pending job forward, never later.
- An unknown job's age is measured against the database clock.
- The purge keeps payment actions, closes and numbered reconciles, because the
  worker decides from their existence.

### Payment attempts

- A new attempt needs a `SEAT_RESERVED` enrollment whose latest attempt holds
  no authorization, numbered under the enrollment's row lock. An attempt with
  a hold therefore stays the enrollment's latest, so the hold, its capture, its
  void and its refund always belong to the attempt the enrollment follows.
- A second tab or a repeated click reuses the open checkout's attempt; a new
  one is opened only when the order expired or PayPal refused to create it.
- PayPal identity columns (order, authorization, capture and refund ids, hold
  expiry) are only ever set: an update never clears them and refuses to
  replace one with a different id.
- The `Apply*` functions can run any number of times with the same answer.
  They move an enrollment only for its latest attempt and never back from a
  later or terminal status. A hold nobody will capture (an older attempt, a
  released seat, a declined capture) gets its `void:{attempt}`.
- Every capture is final (`final_capture: true`), so a capture answer of
  `COMPLETED` also records a `CREATED` authorization as `CAPTURED`, as PayPal
  reports it, without waiting for a webhook or a reconcile to read the
  authorization back. A later read that still says `CREATED` or `PENDING`
  does not move a `CAPTURED` authorization back.

### Seats

- Seats are reserved under the workshop's row lock, so concurrent requests
  never exceed capacity; a lapsed seat no release job has freed yet does not
  count.
- A participant's active enrollment is any non-terminal one, or a flagged one
  whatever its status, so a seat whose money state is uncertain never gets a
  second hold beside it.
- A seat kept while PayPal reviews an authorization (`PENDING`) is never
  released by the seat tick.

### Recommendation explanation

- The recommended date always comes from `domain.Recommend`; Claude only
  phrases it.
- The owner view never waits for the model: on a cache miss it answers with
  no text and `explanation_pending`, and one background call writes the text,
  however many views ask meanwhile.
- The cache key is the workshop plus exactly the facts the model is told, so
  a change the model would not see does not ask for a new text.
- A failed call or a panic falls back to the plain explanation, which is
  served for a minute before the model is asked again. A text that names
  another candidate date is replaced by the plain explanation for good, until
  the facts change.
