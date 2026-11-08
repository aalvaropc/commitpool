# CommitPool from an AI assistant (MCP)

CommitPool serves a [Model Context Protocol](https://modelcontextprotocol.io)
server at `/mcp`, so an organizer or a participant can run a workshop from
their own AI assistant: create it, answer for a participant, see who can come,
open payments and get the PayPal link.

The assistant prepares; the person decides. No tool moves money. A hold is
placed only when the person approves it on PayPal's own page, reservations
open only after the organizer taps Approve on their own CommitPool page, and
captures, voids and refunds stay with the payment worker, under the same rules
as the web app.

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
| `create_workshop` | Creates a paid workshop with one or more candidate dates, the organizer's name and the place. An optional studio cost stays private to the organizer. Returns the owner link (private) and the share link (for participants). |
| `get_status` | From the owner link: the recommended date and why, who can come, who committed or paid, how many paid seats cover the studio cost before fees, what closing payments now would do, and who CommitPool suggests nudging, each with a message Claude drafted for the organizer to send. Read-only; nothing is sent. |
| `preview_open_payments` | Prepares opening payments on a date: returns the exact terms that lock (date, price, minimum, capacity and payment window), an `approval_id` and an `approve_url` for the organizer, valid for thirty minutes. The workshop does not change. |
| `confirm_open_payments` | Opens payments with that `approval_id`, only once the organizer has tapped Approve on the `approve_url` page. Refused while they have not, after the approval expired or was used, and when any term changed since the preview. |

For a participant:

| Tool | What it does |
| --- | --- |
| `get_workshop` | From the share link: terms, candidate dates, whether payments are open and how many committed. Read-only. |
| `join_workshop` | Joins for a person, with an optional email for news about the seat. Returns that person's private participant link. |
| `submit_availability` | Sends what the person says about when they can come, in their own words. Returns how CommitPool read it per date, for the person to confirm. |
| `get_my_status` | The person's seat, the payment terms once payments are open, and the consent text they must read before committing. Read-only. |
| `get_checkout_link` | Once the person has read and accepted the consent text, returns their PayPal approval link. |

There is deliberately no tool to capture, void, refund, cancel a workshop,
withdraw a seat or approve anything. Those stay in the web app, in front of
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
   date, price, length, how long reservations stay open, the minimum and the
   spots, and that nothing is charged until enough people reserve, with
   **Approve** and **Not now**.
3. After **Approve** the page says "Approved. Your assistant can now open
   reservations." The assistant calls `confirm_open_payments` with the
   `approval_id`; CommitPool opens reservations on exactly those terms and
   marks the approval used. Before the tap it answers "The organizer has not
   approved yet — ask them to open the approve link".

An approval belongs to one workshop and one set of terms (date, price,
currency, minimum, capacity and payment window, hashed when it is requested),
lasts thirty minutes and opens reservations once. A second preview of the same
terms returns the same open approval. The organizer page's own **Open
reservations** button is unchanged: the organizer is already the one tapping.

| Endpoint | Who calls it |
| --- | --- |
| `POST /api/v1/owner/{token}/approvals` `{date_id}` | `preview_open_payments`: records the request, or returns the open one for the same terms. |
| `GET /api/v1/owner/{token}/approvals/{id}` | The approve page: terms and state (`pending`, `approved`, `used`, `expired`, `changed` or `closed`). While pending it sets the approval cookie. |
| `POST /api/v1/owner/{token}/approvals/{id}/approve` | The approve page's **Approve** button only (see below). |
| `POST /api/v1/owner/{token}/approvals/{id}/open` | `confirm_open_payments`: opens reservations and uses the approval, in one transaction. |

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
  approval's path, whose value is an HMAC of the approval id keyed by
  `COMMITPOOL_MCP_SECRET` (or a random key per process). The approve request
  needs that cookie, `Sec-Fetch-Site: same-origin` and an `Origin` of this
  site, so another site cannot make the organizer's browser approve, and an
  assistant posting to the endpoint with its HTTP tool is refused with
  `approval_needs_page`. **The honest limit:** these headers are what a
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
