# CommitPool from an AI assistant (MCP)

CommitPool serves a [Model Context Protocol](https://modelcontextprotocol.io)
server at `/mcp`, so an organizer or a participant can run a workshop from
their own AI assistant: create it, answer for a participant, see who can come,
open payments and get the PayPal link.

The assistant prepares; the person decides. No tool moves money. A hold is
placed only when the person approves it on PayPal's own page, and captures,
voids and refunds stay with the payment worker, under the same rules as the
web app. The MCP tools alone cannot open reservations: that needs the
organizer's approval on their own CommitPool page. Anyone holding the owner
link can still open reservations from the organizer page, and a script could
forge a browser's headers; [the safety notes](#safety-by-design) say exactly
what is and is not guaranteed.

## Connect

The server speaks streamable HTTP, statelessly, with plain JSON answers. With
Claude Code:

```sh
claude mcp add --transport http commitpool https://3-209-60-155.sslip.io/mcp
# or, against a local server:
claude mcp add --transport http commitpool http://localhost:8080/mcp
```

Any MCP client that supports streamable HTTP connects the same way. There is
no login: as in the web app, the links are the access. An owner link controls
a workshop and a participant link is one person's seat, so the assistant
passes them as tool arguments and the server never logs them.

## Tools

For the organizer:

| Tool | What it does |
| --- | --- |
| `create_workshop` | Creates a paid workshop with one or more candidate dates, the organizer's name and the place. An optional session cost (room, model, materials) stays private to the organizer. An optional `payout_email` is the organizer's PayPal email: when the class starts, CommitPool pays it what participants paid, less PayPal's fees ([Payouts](payments.md#payouts)). The tool says to set it only to an address the organizer gave. Returns the owner link (private) and the share link (for participants). |
| `get_status` | From the owner link: the recommended date and why, who can come, who committed or paid, how many paid seats cover the session cost before fees, what closing payments now would do (`if_closed_now`: that nothing is held before reservations open, the outcome while they are open, how a cancelled class's money is going; left out once a confirmed class has closed its reservations), how the money of a cancelled or flagged class is being resolved (`resolution`: `RESOLVING`, `SETTLED` or `NEEDS_ATTENTION`, left out otherwise), where the organizer's payout stands (`payout`: its state, receiver, amount, whether the amount is still an estimate, the PayPal batch and transaction once sent, and a `summary` sentence), and who CommitPool suggests nudging, each with a message Claude drafted for the organizer to send. Read-only; nothing is sent. |
| `preview_open_payments` | Prepares opening payments on a date: returns the exact terms that lock (date, price, minimum, capacity, payment window and the `payout_email` the money is paid out to), an `approval_id`, its `state` and an `approve_url` for the organizer. The organizer has thirty minutes to approve; once they do, the assistant has thirty more to confirm. The workshop does not change. |
| `confirm_open_payments` | Opens payments with that `approval_id`, only once the organizer has tapped Approve on the `approve_url` page. Refused while they have not, after the approval expired or was used, and when any term changed since the preview. |
| `preview_extend_reservations` | From the owner link, while a class is short of its minimum: whether the reservation deadline can move once and to when, the reason, and an `approve_url` for the organizer, or the refusal code. Read-only: only the organizer extends, on their own page. |
| `verify_with_paypal` | From the owner link: compares each seat's PayPal order, hold, charge and refund with PayPal's live records (status, amount and the recorded fee and net) and returns what CommitPool expects, what PayPal reports, whether they match and PayPal's debug id on an error. Read-only: nothing changes at PayPal or in CommitPool. One check per workshop every 30 seconds, at most 12 seats each. |

For a participant:

| Tool | What it does |
| --- | --- |
| `get_workshop` | From the share link: terms, candidate dates, whether payments are open and how many committed. Read-only. |
| `join_workshop` | Joins for a person, with an optional email for news about the seat. Returns that person's private participant link. |
| `submit_availability` | Sends what the person says about when they can come, in their own words. Returns how CommitPool read it per date, for the person to confirm. |
| `get_my_status` | The person's seat, the payment terms once payments are open, and the consent text they must read before committing. Read-only. |
| `get_checkout_link` | Once the person has read and accepted the consent text, returns their PayPal approval link. |

There is deliberately no tool to capture, void, refund, pay out, cancel a
workshop, withdraw a seat or approve anything. Those stay in the web app, in front of
the person they affect.

## Opening reservations: the organizer approves

Opening reservations locks the terms every participant then agrees to, so the
organizer approves them the way each participant approves their own hold at
PayPal: the assistant prepares, the person taps.

1. The assistant calls `preview_open_payments` with the owner link and a
   date. CommitPool checks that the date can open now, records an approval
   request for those exact terms and returns them with an `approve_url`
   (`{base}/owner/{owner token}/approve/{approval id}`) and the instruction
   "Show these terms to the organizer and ask them to open this link and tap
   Approve; then call confirm_open_payments."
2. The organizer opens the link on their own device. The page shows the
   workshop, "Your assistant wants to open reservations on these terms:", the
   date, price, length, how long reservations stay open, the minimum, the
   spots and the PayPal email the money is paid out to when the class starts
   ("No PayPal email given" when there is none), and that nothing is charged
   until enough people reserve, with **Approve** and **Not now**.
3. After **Approve** the page says "Approved. Your assistant can now open
   reservations." The assistant calls `confirm_open_payments` with the
   `approval_id`; CommitPool opens reservations on exactly those terms and
   marks the approval used. Before the tap it answers "The organizer has not
   approved yet — ask them to open the approve link".

An approval belongs to one workshop and one set of terms (date, price,
currency, minimum, capacity, payment window and, when there is one, the
payout email, hashed when it is requested) and opens reservations once. The
payout email is set when the workshop is created and never changes, and the
hash names it, so an assistant cannot send the organizer's money elsewhere
without the organizer seeing that address on the approve page. The hash only
names a payout email when the workshop has one, so an approval requested
before payouts existed (whose workshop has none) still verifies. The organizer has thirty minutes to approve it;
approving starts a fresh thirty minutes for the assistant to confirm, and the
approve page shows that deadline. A second preview of the same terms returns
the same approval while it has at least ten minutes left (with `state`
`approved` if the organizer already tapped, and a `next` that says to
confirm), and a new one otherwise. Confirming checks the terms against the
workshop row it locks, in the same transaction that opens reservations. The organizer page's own **Open
reservations** button is unchanged: the organizer is already the one tapping.

| Endpoint | Who calls it |
| --- | --- |
| `POST /api/v1/owner/{token}/approvals` `{date_id}` | `preview_open_payments`: records the request, or returns the open one for the same terms. |
| `GET /api/v1/owner/{token}/approvals/{id}` | The approve page: terms and state (`pending`, `approved`, `used`, `expired`, `changed` or `closed`). While pending it sets the approval cookie. |
| `POST /api/v1/owner/{token}/approvals/{id}/approve` | The approve page's **Approve** button only (see below). |
| `POST /api/v1/owner/{token}/approvals/{id}/open` | `confirm_open_payments`: opens reservations and uses the approval, in one transaction. |

## Extending the deadline: the organizer confirms

When a class is short near its deadline, CommitPool computes one safe
extension (see [Deadline extension](payments.md#deadline-extension)) and the
organizer decides. No tool and no approvals table is involved beyond a link:

1. The assistant calls `preview_extend_reservations` with the owner link. It
   returns `available`, the current `deadline`, `extend_to`, the `reason` and
   an `approve_url` (`{base}/owner/{owner token}?extend={unix seconds}`), with
   the instruction to show the organizer the new deadline, ask them to open the
   link on their own device and confirm, and never to open it or confirm it
   itself. When nothing can move it returns the `refusal` code and its reason.
2. The organizer page opens the confirmation dialog for exactly that option
   (once only; everyone with an email is told; holds stay; anyone can still
   leave before charging starts). A link whose option is no longer on offer
   shows "This extension link is out of date" with the reason instead.
3. **Extend to {date}** posts `POST /api/v1/owner/{token}/extend` `{from, to}`.
   That endpoint refuses the in-process calls the tools make (`403
   approval_required`), so the MCP tools alone cannot move a deadline; it
   refuses a request without the browser headers that pass Go's cross-origin
   protection (`Sec-Fetch-Site: same-origin`, or a matching `Origin`) with `403
   extension_needs_page` ("Only the organizer can extend the deadline, from
   their own page in a browser."), so an assistant posting with a general HTTP
   tool is refused too; and it refuses an option that changed since the page
   showed it (`409 extension_changed`). The same honest limit as for approving
   applies: a program that forges those headers, or an assistant driving a
   real browser with the owner link, could still press the button.

The same organizer page offers the extension without an assistant: Claude's
`rescue` suggestion carries the **Extend to {date}** button and the message
for the group once the deadline moves.

## Safety by design

- **A human approves every hold.** `get_checkout_link` returns a link; only
  the person's approval at PayPal creates the authorization.
- **Consent is explicit.** `get_checkout_link` requires the consent version
  the person read (`get_my_status` gives it) and `human_accepted_terms: true`.
  Without it the call is refused with a message telling the assistant to show
  the text and ask.
- **The organizer approves opening payments on their own page.**
  `confirm_open_payments` works only with an approval that belongs to that
  workshop, was approved, has not expired or been used, and whose terms hash
  still matches the workshop's terms; anything else is refused with what is
  missing. No MCP tool can approve: the approve endpoint refuses the
  in-process calls the tools make, and the plain open endpoint
  (`POST /api/v1/owner/{token}/proposal`, the organizer page's button) refuses
  them too, so the MCP tools alone cannot open payments.
- **Approve has to come from the browser page.** Opening the approve page sets
  an `HttpOnly`, `SameSite=Strict` cookie (`Secure` over HTTPS) scoped to that
  approval's path, whose value is an HMAC of the workshop id and the approval
  id keyed by `COMMITPOOL_MCP_SECRET` (or a random key per process, so set it
  when running more than one instance). The approve request needs that cookie
  and browser headers that pass Go's cross-origin protection:
  `Sec-Fetch-Site: same-origin`, or, from a browser without it, an `Origin`
  that matches the host. A request with neither header is refused, so another
  site cannot make the organizer's browser approve, and an assistant posting
  to the endpoint with its HTTP tool gets `approval_needs_page` ("Only the
  organizer can approve, on the approve page in their own browser."). If the
  page's cookie is gone it refreshes once and asks for another tap, then says
  the browser could not approve rather than looping. **The honest limit:** these headers are what a
  browser sends; a program that fetches the page's API and forges them, or an
  assistant driving a real browser with the owner link, could still tap
  Approve. The owner link is also the organizer's key to their own page, so
  whoever holds it, an assistant with a general HTTP tool included, can press
  that page's **Open reservations** button through the API. What CommitPool
  guarantees is that the MCP tools alone cannot open payments and that the
  approval happens on the organizer's own page, behind the owner link, which
  the assistant is told never to open.
- **One set of rules.** Every tool calls the same JSON API the web app uses,
  in process, so validation, limits, capability checks and the payment rules
  are shared. Requests to `/mcp` are rate-limited per client address like any
  other write; the in-process calls behind them are not counted twice, and
  JSON-RPC batches are refused, so one request is one tool call.
- **Every answer says what to do next.** Each result carries a `next` field
  worded for the assistant, such as "show this reading to the person and ask
  them to confirm it".
- **Logs carry the tool name, outcome and duration**, never arguments, links
  or answers.

## A real session

This session was recorded before the organizer approval step: today
`preview_open_payments` also returns an `approve_url`, and
`confirm_open_payments` takes the `approval_id` and waits for the organizer's
tap instead of taking a confirmation token.

Recorded on 5 November 2026 with a small MCP client built on the Go SDK,
against the hosted demo, with Claude on Amazon Bedrock reading the
availability and a US$1 price. Links and tokens are redacted.

```text
> create_workshop          "MCP transcript check", Sat 14 and Sun 15 Nov, 10:00–12:00 America/New_York
  owner_link, share_link   "Give the owner link only to the organizer … Send the share link to participants."
> join_workshop            Ana
> submit_availability      "The first date works if we finish before 6pm; the second one I'm away."
  Sat 14 Nov  conditional  end before 18:00   evidence: "works if we finish before 6pm"
  Sun 15 Nov  unavailable                     evidence: "I'm away"
  next: "Show this reading to the person and ask them to confirm it."
> get_status               recommended: Sat 14 Nov; 1 compatible, minimum 1, capacity 3
  if_closed_now: "Reservations are not open yet: nothing is held or charged."
> preview_open_payments    Sat 14 Nov, 1.00 USD, minimum 1, capacity 3, window 1440 min, token valid 10 min
  next: "Read these terms to the organizer … Call confirm_open_payments with the token only after they say yes."
> confirm_open_payments    status FUNDING, payments close Fri 6 Nov 14:44 UTC
> get_my_status            seat "Not committed", consent_version consent-v2, consent text
> get_checkout_link        human_accepted_terms false
  refused: "the person must read the consent text (get_my_status) and accept it before a checkout link is created"
> get_checkout_link        human_accepted_terms true
  approve_url https://www.sandbox.paypal.com/checkoutnow?token=…   seat "Seat reserved"
  next: "Only their approval at PayPal places a hold of 1.00 USD; nothing is charged unless the group reaches its minimum."
```
