# CommitPool from an AI assistant (MCP)

CommitPool serves a [Model Context Protocol](https://modelcontextprotocol.io)
server at `/mcp`, so an organizer or a participant can run a workshop from
their own AI assistant: create it, answer for a participant, see who can come,
open payments and get the PayPal link.

The assistant prepares; the person decides. No tool moves money. A hold is
placed only when the person approves it on PayPal's own page, and captures,
voids and refunds stay with the payment worker, under the same rules as the
web app.

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
| `create_workshop` | Creates a paid workshop with two or more candidate dates. Returns the owner link (private) and the share link (for participants). |
| `get_status` | From the owner link: the recommended date and why, who can come, who committed or paid, and what closing payments now would do. Read-only. |
| `preview_open_payments` | Shows the exact terms that opening payments on a date locks: date, price, minimum, capacity and payment window. Changes nothing. Returns a confirmation token valid for ten minutes. |
| `confirm_open_payments` | Opens payments with that token, after the organizer has seen the terms and said yes. Refused when the token expired or any term changed since the preview. |

For a participant:

| Tool | What it does |
| --- | --- |
| `get_workshop` | From the share link: terms, candidate dates, whether payments are open and how many committed. Read-only. |
| `join_workshop` | Joins for a person, with an optional email for news about the seat. Returns that person's private participant link. |
| `submit_availability` | Sends what the person says about when they can come, in their own words. Returns how CommitPool read it per date, for the person to confirm. |
| `get_my_status` | The person's seat, the payment terms once payments are open, and the consent text they must read before committing. Read-only. |
| `get_checkout_link` | Once the person has read and accepted the consent text, returns their PayPal approval link. |

There is deliberately no tool to capture, void, refund, cancel a workshop or
withdraw a seat. Those stay in the web app, in front of the person they
affect.

## Safety by design

- **A human approves every hold.** `get_checkout_link` returns a link; only
  the person's approval at PayPal creates the authorization.
- **Consent is explicit.** `get_checkout_link` requires the consent version
  the person read (`get_my_status` gives it) and `human_accepted_terms: true`.
  Without it the call is refused with a message telling the assistant to show
  the text and ask.
- **Opening payments is two-step.** The confirmation token is an HMAC over the
  date, price, currency, minimum, capacity, payment window and expiry, signed
  with `COMMITPOOL_MCP_SECRET` (or a random key per process). The assistant
  cannot open payments on terms the organizer has not seen.
- **One set of rules.** Every tool calls the same JSON API the web app uses,
  in process, so validation, limits, capability checks and the payment rules
  are shared. Requests to `/mcp` are rate-limited per client address like any
  other write; the in-process calls behind them are not counted twice.
- **Every answer says what to do next.** Each result carries a `next` field
  worded for the assistant, such as "show this reading to the person and ask
  them to confirm it".
- **Logs carry the tool name, outcome and duration**, never arguments, links
  or answers.

## A real session

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
  if_closed_now: "Payments are not open yet: nothing is held or charged."
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
