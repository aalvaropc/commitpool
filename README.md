# CommitPool

**The class happens, or nobody pays.**

CommitPool is Doodle meets Kickstarter, for small paid classes. A pottery
teacher, a yoga instructor or a baker shares one link in the group chat.
People say when they can come in their own words, Claude reads every answer
against every date, CommitPool picks the date that works, and everyone
reserves with PayPal. Nobody is charged unless enough people reserve for the
class to run. If they don't, every hold is released and any charge refunded.

Try it in one click: **[Try it as Maya](https://3-209-60-155.sslip.io)**.
You become Maya Okafor, who teaches wheel-throwing in Greenpoint, Brooklyn,
and you watch six people's answers arrive. Everything runs on the **PayPal
Sandbox**; no real money moves.

![The organizer's board: who can come on each date, read by Claude](docs/images/organizer-board.png)

## The story

1. **Maya describes her class.** "Two hours, $45 a person, the studio costs me
   $120 and I have 6 wheels, Saturday or Sunday next week, 10 to 12." Claude
   drafts the form; Maya checks every field. The studio cost suggests how many
   people she needs (3 × $45 covers $120). Nothing is created until she says so.
2. **She shares the link.** The invite unfurls in the chat with the class, the
   dates and the price. People join with a name, no account.
3. **Everyone answers like they'd text a friend.** "Either day is fine, but on
   Saturday only if we finish before six." Claude reads each message into a
   verdict per date, with the words it came from; anyone can correct theirs.
   A vague answer gets a question, not a guess.
4. **The date rises to the top.** CommitPool counts who can come, in the
   class's time zone, and recommends the date; Claude puts the reason into
   words and drafts the nudges Maya can copy to whoever still needs to answer
   or reserve. Maya opens reservations on the date with one tap.
5. **People reserve with PayPal.** Each person approves a hold at PayPal.
   Nothing is charged. When enough have reserved, the holds are charged and
   the class is **on**: everyone gets the calendar invite. If the deadline
   passes short, the class is cancelled, holds are released and charges are
   refunded, and everyone can watch their refund land.

## Screens

| The invite, as it opens from the chat | Claude drafts the class from a sentence |
| --- | --- |
| ![The invite page on a phone](docs/images/invite-phone.png) | ![The create form drafted by Claude](docs/images/create-draft.png) |
| **The landing page** | **"It's on", with the calendar invite** |
| ![The landing page](docs/images/landing.png) | ![The confirmation email](docs/images/email-its-on.png) |

## Fair for both sides

- **The organizer** knows the room is covered before committing to it, stops
  chasing a chat full of maybes, and keeps one private link (also by email).
- **Everyone who comes** says when they can in plain words, reserves knowing
  they pay only if it happens, and can give up their spot until they are
  charged.

## Claude reads, CommitPool decides, PayPal moves the money

| | Claude on Amazon Bedrock | CommitPool (code) | People |
| --- | --- | --- | --- |
| Availability | Reads each message into verdicts per date, flags vague ones | Decides who can come and recommends the date | Correct how their message was read |
| Creating a class | Drafts the form from a description | Validates and normalizes every field | The organizer checks and creates |
| Explaining | Puts the recommendation and the nudges into words | Picks who to nudge and why | The organizer sends the messages, or not |
| Money | Never | Charges, releases and refunds through PayPal by its rules | Each person approves their own hold at PayPal |

Claude's output is always structured or checked: strict JSON schemas for
readings and drafts, the server re-validates everything, an explanation or
nudge that names another date falls back to plain words, and nothing it writes
can open reservations or move money.

## Rules that hold the money

- **Confirmation counts completed payments only.** An authorized hold is a
  promise; a declined capture is not a seat.
- **Every money operation is idempotent.** Each capture, void and refund has
  its own key, persisted before the request is sent, and is retried with the
  same key. A timeout or an ambiguous answer is never retried blindly: the
  order is read back from PayPal and the local state is reconciled to it.
- **States are visible and separate.** At checkout, spot reserved, charging,
  paid, releasing, released, refund on its way, refunded, needs a check:
  participants see their own spot, the organizer sees everyone's.
- **Plans can change.** The organizer can cancel until the class starts, and a
  participant can withdraw until they are charged. When PayPal does not settle
  something on its own, the seat is flagged for the organizer with the
  runbook's actions instead of being dropped.

## Use it from an AI assistant (MCP)

CommitPool is also a [Model Context Protocol](https://modelcontextprotocol.io)
server at `/mcp`. From Claude Code:

```sh
claude mcp add --transport http commitpool https://3-209-60-155.sslip.io/mcp
```

An organizer's assistant can create the workshop, read who can come and why
a date is recommended, see who CommitPool suggests nudging with a message
Claude drafted (nothing is sent), and open reservations after a preview whose
terms the organizer approves (a token bound to those exact terms). A participant's
assistant can join, pass on their availability in their own words and hand
them the PayPal link once they accept the consent text. No tool moves money:
only the person's approval at PayPal places a hold, and the payment worker
does the rest. Tools, safeguards and a real session are in
[docs/mcp.md](docs/mcp.md).

## Architecture

```
AI assistant ── MCP (/mcp) ──┐
browser ── React SPA (embedded) ──► Go HTTP API ──► PostgreSQL
                                        │  ▲
                 jobs: create_order,    ▼  │ webhooks (signature verified)
                 authorize, capture,  worker ◄──── PayPal Sandbox
                 void, refund, close,   │          Orders v2 · Payments v2
                 reconcile, heal,       ▼
                 notify            Claude on Amazon Bedrock
                                   (availability → verdicts, explanation)
                                   Amazon SES (optional notifications)
```

- **One binary.** `cmd/commitpool` serves the JSON API and the SPA
  (`web/`, built with Vite and embedded with `go:embed`) and runs the payment
  worker in the same process. SQL migrations (`migrations/`) are embedded and
  applied on startup.
- **PayPal.** Orders v2 with `intent: AUTHORIZE`; Payments v2 to capture
  (`final_capture`), void and refund; webhooks for the seven order and payment
  events, verified with PayPal before they are stored; a periodic reconcile
  that reads back anything pending or unknown. Negative Sandbox tests use the
  `PayPal-Mock-Response` header behind a flag. The state machines, the keys,
  the capture deadline, the close and the operator runbook are in
  [docs/payments.md](docs/payments.md).
- **Claude on Amazon Bedrock** reads availability messages with strict
  structured output (see [below](#availability-with-claude-on-amazon-bedrock)).
- **MCP server** (`internal/mcpserver`) at `/mcp`, in the same binary; each
  tool calls the JSON API in process ([docs/mcp.md](docs/mcp.md)).
- **Notifications** go through an outbox table and a `notify` job, so an
  email never blocks or undoes a money movement. `internal/notice` renders
  each kind as HTML and text in the CommitPool identity; "It's on" carries
  the calendar invite. Amazon SES sends them, or the log stands in. A message
  that is no longer true when its turn comes is skipped, and one carrying a
  private link is cleared from the database once sent.
- **Link previews and calendars.** Invite links unfurl with a 1200×630 card
  rendered in Go (`internal/ogimage`) and Open Graph tags; `/p/`, `/owner/`
  and `/w/` pages are `noindex`. `internal/ics` writes the calendar invite
  (`/api/v1/w/{token}/calendar.ics`, and a link-free one per participant).
- **Deployment** on AWS with Terraform: the demo on one EC2 VM with Caddy
  ([deploy/ec2/README.md](deploy/ec2/README.md)); a Fargate, RDS and
  CloudFront configuration for production in
  [deploy/aws/README.md](deploy/aws/README.md).

## Demo walkthrough

Hosted demo: <https://3-209-60-155.sslip.io> (PayPal Sandbox; MCP at
`/mcp`).

The video follows this script against a local or hosted instance:

1. **Try it as Maya** on the landing page (or `make seed`) creates Maya
   Okafor's "Wheel-throwing for beginners" at her studio in Greenpoint,
   Brooklyn: US$45 a spot, a US$120 studio, 3 needed of 6 spots, Saturday or
   Sunday 10 AM–12 PM New York time. Six participants answer one by one and
   the grid fills in: four can come on the Saturday, three on the Sunday, and
   Grace's "ideally in the afternoon" is too vague to place, so **Claude
   suggests** asking her which hours work, with a message ready to copy.
2. **Create a workshop** shows the other way in: Maya's description becomes a
   drafted form she checks, with the studio cost suggesting 3 people.
3. On the organizer page, open reservations on the recommended date. The
   invite link now unfurls with the class card in any chat.
4. Three participants, in their own browser contexts, accept the consent and
   reserve with Personal Sandbox accounts; each one with an email hears
   "spot reserved, not charged yet". The third reservation starts the capture
   round; with three completed payments the headline turns to **It's on**,
   each seat reads **Paid**, and the confirmation email carries the calendar
   invite.
5. For the second run, with demo controls on, **Fail next charge** on one
   seat makes PayPal answer `AUTHORIZATION_EXPIRED` to that capture, and
   **Close reservations now** ends the window early: below the minimum the
   workshop cancels, the open hold is voided and the completed captures are
   refunded, each refund tracked to **Refunded**.

Everything in the demo is labelled: the Sandbox, the prepared data, the
accelerated clock and the induced failure. The demo controls exist only when
`COMMITPOOL_DEMO_MODE=1` and `COMMITPOOL_SANDBOX_MOCKS=1` are set, and the
mock header is refused outside the Sandbox.

On a demo server, `GET /api/v1/demo` says demo workshops are on and
`POST /api/v1/demo/workshops` creates one for Maya: it answers with the
organizer link, the invite link and one link per participant, then sends the
six answers in the background, one every `COMMITPOOL_DEMO_PACE_MS`. One
address can start two demos and then one every 20 minutes (only while the
rate limit is on); at most 30 start in any hour and 6 fill in at once. Both
endpoints answer 404 outside demo mode.

## Stack

- Go service (`cmd/commitpool`) with PostgreSQL via `pgx`
- React + TypeScript single-page app built with Vite (`web/`), embedded into the
  Go binary with `go:embed`
- SQL migrations (`migrations/`) embedded and applied on startup
- Claude Sonnet 4.5 on Amazon Bedrock through the Anthropic Go SDK
- Terraform for AWS; Playwright for the end-to-end journeys

## Run it locally

Requirements: Go 1.26, Node 22+ with pnpm 10, Docker with Compose.

```sh
cp .env.example .env     # then fill in the values you need
make db-up               # PostgreSQL 16 on localhost:55432
make web-build           # builds the SPA into web/dist
make run                 # loads .env and starts the service on :8080
```

Check that it is up:

```sh
curl -s localhost:8080/healthz   # {"ok":true,...} with queue counts and worker heartbeats
```

For frontend work, run `pnpm -C web dev`; Vite proxies `/api`, `/paypal` and
`/healthz` to the Go service on port 8080.

### Seed a demo workshop

With the service running, `make seed` creates Maya's demo workshop through the
HTTP API on the next free weekend: 3 needed of 6 spots at US$45, and six
participants whose availability messages are already sent (one of them
deliberately vague, so the organizer sees a clarification request). Against a
server without demo mode, the description says the data is prepared. It
prints the organizer link, the invite link and one link per participant, and
needs no credentials. `go run ./cmd/seed -h` lists the flags: another base URL
(`-base`), price, time zone, payment window, and `-open-payments` to open
payments on the recommended date right away.

### Configuration

All configuration comes from environment variables; `.env.example` lists every
one of them with its default. Secrets are only ever read from the environment.

| Variable | Notes |
| --- | --- |
| `PORT` | HTTP port the service listens on (default `8080`). With compose, `COMMITPOOL_PORT` set in the shell picks the host port. |
| `COMMITPOOL_BASE_URL` | Required. Public URL of the deployment, no trailing slash; PayPal sends buyers back to it. |
| `DATABASE_URL` | Required. For the local database: `postgres://commitpool:commitpool@localhost:55432/commitpool?sslmode=disable` |
| `PAYPAL_CLIENT_ID`, `PAYPAL_CLIENT_SECRET` | Required unless `E2E_FAKE_PAYPAL=1`. PayPal Sandbox app credentials. |
| `PAYPAL_WEBHOOK_ID` | Id of the Sandbox webhook pointing at `/paypal/webhook` (`scripts/register-webhook.sh` creates it). Without it the webhook endpoint answers 503 and the periodic reconcile does the work alone. |
| `COMMITPOOL_AI_PROVIDER` | `bedrock` (default) or `fake`, the deterministic keyword interpreter for tests and demos. |
| `AWS_REGION` | Required unless `COMMITPOOL_AI_PROVIDER=fake`. |
| `COMMITPOOL_BEDROCK_MODEL`, `COMMITPOOL_BEDROCK_FALLBACK_MODEL` | Bedrock model or inference profile IDs. The model defaults to Claude Sonnet 4.5 through the US inference profile, `us.anthropic.claude-sonnet-4-5-20250929-v1:0`; the fallback model defaults to none, which turns the refusal fallback off. |
| `COMMITPOOL_BEDROCK_EFFORT` | Effort for every Bedrock call: `none` (default; Sonnet 4.5 takes no effort), or `low`, `medium`, `high`, `xhigh`, `max` for a model that takes one. |
| `COMMITPOOL_SEAT_TTL_MIN` | Minutes a reserved seat is held while the buyer approves at PayPal (default `30`). |
| `COMMITPOOL_DEMO_MODE` | `1` enables "Try it as Maya" (the demo workshop endpoints) and the organizer's **Close reservations now** control. |
| `COMMITPOOL_DEMO_PACE_MS` | Milliseconds between the answers of a demo workshop (default `2000`; the end-to-end script uses `50`). |
| `COMMITPOOL_SANDBOX_MOCKS` | `1` enables **Fail next charge** and the `PayPal-Mock-Response` header; rejected unless `PAYPAL_BASE_URL` is a sandbox URL. |
| `COMMITPOOL_MAILER`, `COMMITPOOL_MAIL_FROM` | How notifications go out: `log` (default: logged with a masked address, not sent) or `ses` (Amazon SES v2 with the default AWS credentials, from `COMMITPOOL_MAIL_FROM`, a verified sender identity, optionally with a name: `CommitPool <hello@example.com>`). |
| `COMMITPOOL_RATE_LIMIT` | Writes (POST, PUT) a minute one client address may send, in bursts of half that; `off` turns it off (the end-to-end script does). Default `120`. Reads are never limited. |
| `COMMITPOOL_MCP_SECRET` | Signs the MCP server's confirmation tokens; without it a random key per process is used, so a preview made before a restart must be made again. |
| `E2E_FAKE_PAYPAL` | `1` runs an in-process fake PayPal, for the end-to-end tests. Only a binary built with `-tags e2e` carries it (`scripts/e2e.sh` builds one); refused next to real PayPal credentials or with a `COMMITPOOL_BASE_URL` that is not localhost. |

### Claude on Amazon Bedrock

Claude does four things in CommitPool, and decides none of them:

| Use | What Claude returns | What the server does with it |
| --- | --- | --- |
| Read availability | A verdict per candidate date, strict JSON schema | Validates it, decides who can come and recommends the date |
| Draft a class | A closed-schema draft from the organizer's description | Clips, bounds and re-validates every field; the organizer edits and creates |
| Explain the recommendation | At most 60 words | Falls back to plain words if it names another date |
| Suggest nudges | A short message per person CommitPool chose | Refuses links, contact details and long answers; falls back to a plain message |

Every user-written text reaches Claude as a JSON string labelled as data,
never instructions. Drafts and nudges never block a page: drafts answer 200
with an empty draft when Claude is busy or fails, and explanations and nudges
are written in the background and cached.

Participants describe their availability in plain language. With
`COMMITPOOL_AI_PROVIDER=bedrock`, Claude on Amazon Bedrock reads each message
into one verdict per candidate date. The model is Claude Sonnet 4.5, called
through the Bedrock runtime (`InvokeModel` with a Messages API body, signed
with SigV4) by the Anthropic Go SDK. The answer is strict structured output: a JSON schema with
every object closed and the candidate dates as an enum. The server validates
it again before keeping it, so a reading with an extra field or an unknown
date is refused. The message is data, never instructions, and the schema has
no field for the price, the currency, the minimum or the capacity: those come
from the workshop alone. The server, not the model, decides which date is
compatible for whom and which date to recommend; the model only writes the
owner's one-paragraph explanation of that recommendation (at most 60 words).
The owner view never waits for it: the explanation is written once per input
in the background and kept in memory, and until it is ready the view answers
with `explanation_pending: true`. With a fallback model configured, the
SDK's client-side refusal fallback retries a declined request once on it.

Claude Sonnet 4.5 is served on Bedrock only through inference profiles: use
`us.anthropic.claude-sonnet-4-5-20250929-v1:0` (US regions) or
`global.anthropic.claude-sonnet-4-5-20250929-v1:0`; the bare
`anthropic.claude-sonnet-4-5-20250929-v1:0` is refused for on-demand calls.
Bedrock lists Sonnet 4.5 as a legacy model, available until April 2027. To
use Bedrock:

1. Run `aws configure` (or set `AWS_PROFILE`, or `AWS_ACCESS_KEY_ID` and
   `AWS_SECRET_ACCESS_KEY`) and pick a region where the model is offered, then
   set `AWS_REGION` to it. A Bedrock API key in `AWS_BEARER_TOKEN_BEDROCK`
   works too. Credentials are checked at startup, so a missing one stops the
   service right away. A session from `aws login` is not read by the Go SDK
   the service uses; export it for the shell first with
   `eval "$(aws configure export-credentials --format env)"`.
2. In the Amazon Bedrock console, enable access to the model (and to the
   fallback model) in that region. For Anthropic models this means
   submitting the use-case form and accepting the model agreement once per
   account; until then Bedrock answers 403.
3. Check it end to end with the live smoke test, which calls Bedrock for real:

   ```sh
   COMMITPOOL_LIVE_AI=1 go test ./internal/ai/ -run Live -count=1 -v
   ```

The service logs, per call, the model, latency and token counts, never the
prompt, the message or the answer.

## Run with Docker

The image is a multi-stage build: Node builds the SPA, Go compiles a static
binary that embeds it, and the result runs on
`gcr.io/distroless/static-debian12:nonroot` as a non-root user. Build it from
the repository root:

```sh
docker build -f deploy/Dockerfile -t commitpool:dev .
```

The compose file has an `app` profile that builds the same image and runs it
next to the PostgreSQL service. The `.env` file at the repository root feeds
the container, so create it first (`cp .env.example .env`) and fill in the
values you need. Compose always overrides `DATABASE_URL` with
`postgres://commitpool:commitpool@postgres:5432/commitpool?sslmode=disable`
and `PORT` with `8080`, so the container reaches the compose database whatever
`.env` says for a run on the host. The host port defaults to 8080; to publish
another one, set `COMMITPOOL_PORT` in the shell that runs compose
(`COMMITPOOL_PORT=9090 docker compose …`), because compose reads it from the
shell, not from `.env`. `COMMITPOOL_BASE_URL` in `.env` must name the
published port, because it is the URL PayPal sends buyers back to. Inside
the container only environment
variables are read: there is no `~/.aws`, so Bedrock credentials must be in
`.env` (`AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY`, or
`AWS_BEARER_TOKEN_BEDROCK`).

```sh
docker compose -f deploy/compose.yaml --profile app up -d --build --wait
curl -s localhost:8080/healthz   # {"ok":true,...} with queue counts and worker heartbeats
```

Migrations run at startup, as with `make run`. Stop everything with:

```sh
docker compose -f deploy/compose.yaml --profile app down
```

Without `--profile app`, compose starts only PostgreSQL, which is what
`make db-up` and the end-to-end script use.

## Deploy to AWS

The hosted demo runs on one EC2 VM: Docker runs PostgreSQL, the app and
Caddy for HTTPS, the VM's role reaches Bedrock, and the secrets live in SSM
Parameter Store; it costs about US$17 a month while it is on. The runbook in
[deploy/ec2/README.md](deploy/ec2/README.md) goes from an empty account to a
registered Sandbox webhook, and `scripts/deploy-ec2.sh` ships each new
commit.

`deploy/aws/` holds a production-shaped alternative, not used for the demo:
ECS Fargate, RDS PostgreSQL in private subnets, an Application Load Balancer
that only CloudFront can reach, Secrets Manager and a task role limited to
Bedrock, at about US$63 a month
([deploy/aws/README.md](deploy/aws/README.md)).

## Sandbox test accounts

Buyers approve checkouts with Personal Sandbox accounts. Create them in the
PayPal Developer Dashboard under Sandbox, Accounts, and log in with one on the
PayPal approval page. One Personal account may approve several orders: the app
does not check that buyers are distinct. The Business Sandbox account that owns
the REST app (the `PAYPAL_CLIENT_ID` and `PAYPAL_CLIENT_SECRET` in `.env`)
receives the Sandbox payments. No real money moves.

## Tests

```sh
make db-up
make test                  # go test ./... against the commitpool_test database
go vet ./...
pnpm -C web typecheck      # the SPA, the Playwright config and the e2e specs
pnpm -C web test           # vitest
pnpm -C web build
make check                 # gofmt, go vet, staticcheck, govulncheck, no test code in the binaries, no code comments
pnpm -C web lint           # no code comments in the web sources
bash scripts/e2e.sh        # the Playwright journeys (and axe checks) against a fresh server with the fake PayPal
```

The code carries no comments: names, tests and the docs say why.
`go test ./internal/codestyle` fails on a comment in Go, SQL, shell,
Terraform, YAML, CSS or HTML, and `pnpm -C web lint` on one in the web
sources. Compiler and linter directives are allowed. The payment rules live in
[docs/payments.md](docs/payments.md) and the scripts in
[scripts/README.md](scripts/README.md).

`make test` and CI run the Go tests with the race detector. Two fuzz targets
cover money parsing and the cleaning of the model's reading
(`go test ./internal/domain -fuzz FuzzParseMoney`,
`go test ./internal/httpapi -fuzz FuzzCleanInterpretation`).

The Go suites cover the pure rules, the store against a real PostgreSQL, the
worker and the HTTP API against an in-process fake PayPal server (timeouts,
409s, pending states, mock codes, duplicate webhooks), and the demo seed
against the real API. Database tests read `TEST_DATABASE_URL`, refuse a
database whose name does not end in `_test`, and fail when the variable is
missing; set `COMMITPOOL_SKIP_DB_TESTS=1` to skip them explicitly.

No test calls Bedrock unless `COMMITPOOL_LIVE_AI=1` is set; the rest use the
fake interpreter or a local stand-in for the Bedrock endpoint.

## Limitations

- Prices are in USD only.
- The payment window defaults to 24 hours and is capped at 72 hours, the
  period PayPal honours an authorization for; longer windows would need
  reauthorization, which is not built.
- Access is by capability links (organizer, invite, participant); there are
  no user accounts.
- Sandbox only. Writes are rate-limited per client address, a workshop takes
  at most 200 participants, a participant at most 20 availability messages,
  and at most 4 model calls run at once; there is no WAF or autoscaling.
- A paid seat cannot refund itself: only a cancellation refunds it, so the
  minimum the organizer relies on cannot drop after the fact.
- Emails are delivered at least once: after a crash between sending and
  recording, one may arrive twice. While the SES account is in its sandbox,
  only verified addresses receive mail.
- Claude's nudges are copied and sent by the organizer; CommitPool does not
  send them itself.
- Without `COMMITPOOL_MCP_SECRET`, an MCP preview token dies with a restart
  and has to be made again.

## License

MIT. See [LICENSE](LICENSE).
