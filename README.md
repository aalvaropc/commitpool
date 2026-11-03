# CommitPool

CommitPool helps an instructor run a paid workshop without guessing demand.
The instructor proposes a few candidate dates, participants describe their
availability in plain language, and once a date is fixed each participant
authorizes a PayPal payment. The workshop is confirmed only when the minimum
number of completed payments is reached. If it is not reached by the deadline,
the workshop is cancelled, every hold is released and every charge already made
is refunded.

The app talks to the **PayPal Sandbox** only. No real money moves.

## How it works

1. **Create the workshop.** Title, duration, time zone, minimum and maximum
   seats, price, payment window, and two or more candidate dates. The
   organizer gets a private link and an invite link.
2. **Share the invite link.** Each participant joins with a name and gets a
   link of their own; no accounts, no passwords.
3. **Say when you can come, in your own words.** "Saturday works if we finish
   before six" is read by Claude into one verdict per candidate date:
   available, unavailable, conditional (with the condition) or unknown. The
   participant sees the reading and can correct it. A vague message is
   flagged as needing clarification instead of being guessed.
4. **The server recommends a date.** Compatibility per date is computed from
   the verdicts and conditions, in the workshop's time zone, by code; the
   model only writes a short explanation of that recommendation. The
   organizer opens payments on a date, which locks date, price, minimum and
   capacity.
5. **Participants commit.** Each one reads the consent text, takes a seat
   (held for a few minutes while they go to PayPal) and authorizes a hold for
   the price. Nothing is charged yet.
6. **Minimum reached, money moves.** When the holds plus completed payments
   reach the minimum, the holds are captured. With the minimum of *completed*
   payments the workshop is confirmed; later joiners are charged one by one
   until it is full or the window closes. If the window closes short of the
   minimum, the workshop is cancelled: open holds are voided and every
   capture is refunded, and everyone can watch each refund land.

Three rules hold the whole thing together:

- **Confirmation counts completed payments only.** An authorized hold is a
  promise, a declined capture is not a seat.
- **Every money operation is idempotent.** Each capture, void and refund has
  its own key, persisted before the request is sent, and is retried with the
  same key. A timeout or an ambiguous answer is never retried blindly: the
  order is read back from PayPal and the local state is reconciled to it.
- **States are visible and separate.** Interested, hold authorized, charging,
  paid, refund in progress, refund completed, needs attention: participants
  see their own seat, the organizer sees everyone's.

## Architecture

```
browser ── React SPA (embedded) ──► Go HTTP API ──► PostgreSQL
                                        │  ▲
                 jobs: create_order,    ▼  │ webhooks (signature verified)
                 authorize, capture,  worker ◄──── PayPal Sandbox
                 void, refund, close,   │          Orders v2 · Payments v2
                 reconcile, heal        ▼
                                   Claude on Amazon Bedrock
                                   (availability → verdicts, explanation)
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
- **Deployment** on AWS with Terraform: Fargate, RDS PostgreSQL, an ALB
  behind CloudFront, Secrets Manager. See
  [deploy/aws/README.md](deploy/aws/README.md).

## Demo walkthrough

Hosted demo: _to be published with the submission_.

The video follows this script against a local or hosted instance:

1. `make seed` creates the demo workshop through the API: a street
   photography walk on the next free weekend, minimum 3 of 6 seats at US$10,
   and six participants whose availability is already sent. Four can come on
   the Saturday, three on the Sunday, and one message is too vague to read,
   so the organizer page shows a clarification request.
2. On the organizer page, open payments on the recommended date.
3. Three participants, in their own browser contexts, accept the consent and
   authorize with Personal Sandbox accounts. The third authorization starts
   the capture round; with three completed payments the headline turns to
   **Workshop confirmed** and each seat reads **Seat paid**.
4. For the second run, with demo controls on, **Fail next charge** on one
   seat makes PayPal answer `AUTHORIZATION_EXPIRED` to that capture, and
   **Close payments now** ends the window early: below the minimum the
   workshop cancels, the open hold is voided and the completed captures are
   refunded, each refund tracked to `Refund completed`.

Everything in the demo is labelled: the Sandbox, the prepared data, the
accelerated clock and the induced failure. The demo controls exist only when
`COMMITPOOL_DEMO_MODE=1` and `COMMITPOOL_SANDBOX_MOCKS=1` are set, and the
mock header is refused outside the Sandbox.

## Stack

- Go service (`cmd/commitpool`) with PostgreSQL via `pgx`
- React + TypeScript single-page app built with Vite (`web/`), embedded into the
  Go binary with `go:embed`
- SQL migrations (`migrations/`) embedded and applied on startup
- Anthropic Go SDK against the Bedrock Mantle endpoint
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
curl -s localhost:8080/healthz   # {"ok":true}
```

For frontend work, run `pnpm -C web dev`; Vite proxies `/api`, `/paypal` and
`/healthz` to the Go service on port 8080.

### Seed a demo workshop

With the service running, `make seed` creates the demo through the HTTP API: a
street photography walk on the next free weekend, minimum 3 of 6 seats at
US$10, and six participants whose availability messages are already sent (one
of them deliberately vague, so the organizer sees a clarification request). It
prints the organizer link, the invite link and one link per participant, and
needs no credentials. `go run ./cmd/seed -h` lists the flags: another base URL
(`-base`), price, time zone, payment window, and `-open-payments` to open
payments on the recommended date right away.

### Configuration

All configuration comes from environment variables; `.env.example` lists every
one of them with its default. Secrets are only ever read from the environment.

| Variable | Notes |
| --- | --- |
| `PORT` | HTTP port the service listens on (default `8080`). With compose, `COMMITPOOL_PORT` picks the host port. |
| `COMMITPOOL_BASE_URL` | Required. Public URL of the deployment, no trailing slash; PayPal sends buyers back to it. |
| `DATABASE_URL` | Required. For the local database: `postgres://commitpool:commitpool@localhost:55432/commitpool?sslmode=disable` |
| `PAYPAL_CLIENT_ID`, `PAYPAL_CLIENT_SECRET` | Required unless `E2E_FAKE_PAYPAL=1`. PayPal Sandbox app credentials. |
| `PAYPAL_WEBHOOK_ID` | Id of the Sandbox webhook pointing at `/paypal/webhook` (`scripts/register-webhook.sh` creates it). Without it the webhook endpoint answers 503 and the periodic reconcile does the work alone. |
| `COMMITPOOL_AI_PROVIDER` | `bedrock` (default) or `fake`, the deterministic keyword interpreter for tests and demos. |
| `AWS_REGION` | Required unless `COMMITPOOL_AI_PROVIDER=fake`. |
| `COMMITPOOL_BEDROCK_MODEL`, `COMMITPOOL_BEDROCK_FALLBACK_MODEL` | Bedrock model IDs; default `anthropic.claude-opus-5-5` and `anthropic.claude-opus-4-8`. `none` as the fallback model turns the refusal fallback off. |
| `COMMITPOOL_BEDROCK_EFFORT` | Effort for every Bedrock call: `low` (default), `medium`, `high`, `xhigh`, `max`, or `none` to send no effort. |
| `COMMITPOOL_SEAT_TTL_MIN` | Minutes a reserved seat is held while the buyer approves at PayPal (default `30`). |
| `COMMITPOOL_DEMO_MODE` | `1` enables the organizer's **Close payments now** control. |
| `COMMITPOOL_SANDBOX_MOCKS` | `1` enables **Fail next charge** and the `PayPal-Mock-Response` header; rejected unless `PAYPAL_BASE_URL` is a sandbox URL. |
| `E2E_FAKE_PAYPAL` | `1` runs an in-process fake PayPal, for the end-to-end tests. |

### Availability with Claude on Amazon Bedrock

Participants describe their availability in plain language. With
`COMMITPOOL_AI_PROVIDER=bedrock`, Claude on Amazon Bedrock reads each message
into one verdict per candidate date, through the Bedrock Mantle endpoint
(`https://bedrock-mantle.{region}.api.aws/anthropic`, the Messages API) of the
Anthropic Go SDK. The answer is strict structured output: a JSON schema with
every object closed and the candidate dates as an enum. The server validates
it again before keeping it, so a reading with an extra field or an unknown
date is refused. The message is data, never instructions, and the schema has
no field for the price, the currency, the minimum or the capacity: those come
from the workshop alone. The server, not the model, decides which date is
compatible for whom and which date to recommend; the model only writes the
owner's one-paragraph explanation of that recommendation (at most 80 words).
The owner view never waits for it: the explanation is written once per input
in the background and kept in memory, and until it is ready the view answers
with `explanation_pending: true`. If the model declines a request, the SDK's
client-side refusal fallback retries it once on the fallback model.

Model IDs on Bedrock carry the `anthropic.` prefix (`anthropic.claude-opus-5-5`).
If Bedrock answers 400 or 404 for that ID, first try the bare form,
`COMMITPOOL_BEDROCK_MODEL=claude-opus-5-5` (and
`COMMITPOOL_BEDROCK_FALLBACK_MODEL=claude-opus-4-8`): the SDK's own Mantle
tests use bare IDs. If that fails too, copy the ID from the Bedrock console's
model catalog for your region. To use Bedrock:

1. Run `aws configure` (or set `AWS_PROFILE`, or `AWS_ACCESS_KEY_ID` and
   `AWS_SECRET_ACCESS_KEY`) and pick a region where the model is offered, then
   set `AWS_REGION` to it. A Bedrock API key in `AWS_BEARER_TOKEN_BEDROCK`
   works too. Credentials are checked at startup, so a missing one stops the
   service right away.
2. In the Amazon Bedrock console, enable access to the model (and to the
   fallback model) in that region.
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
`.env` says for a run on the host. The host port is `COMMITPOOL_PORT` (default
8080); `COMMITPOOL_BASE_URL` in `.env` must name that same port, because it
is the URL PayPal sends buyers back to. Inside the container only environment
variables are read: there is no `~/.aws`, so Bedrock credentials must be in
`.env` (`AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY`, or
`AWS_BEARER_TOKEN_BEDROCK`).

```sh
docker compose -f deploy/compose.yaml --profile app up -d --build --wait
curl -s localhost:8080/healthz   # {"ok":true}
```

Migrations run at startup, as with `make run`. Stop everything with:

```sh
docker compose -f deploy/compose.yaml --profile app down
```

Without `--profile app`, compose starts only PostgreSQL, which is what
`make db-up` and the end-to-end script use.

## Deploy to AWS

`deploy/aws/` holds a Terraform configuration for one environment: a VPC
with two public subnets, the service on ECS Fargate, PostgreSQL on RDS in
private subnets, an Application Load Balancer that only CloudFront can reach,
CloudFront for HTTPS on its default certificate, Secrets Manager for the
database URL and the PayPal credentials, ECR for the image, and a task role
limited to Bedrock. The runbook in [deploy/aws/README.md](deploy/aws/README.md)
goes from an empty account to a registered Sandbox webhook, with the
approximate monthly cost.

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
bash scripts/e2e.sh        # three Playwright journeys against a fresh server with the fake PayPal
```

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
- The payment window defaults to 24 hours. PayPal honours an authorization
  for a limited period; a window longer than 72 hours has not been exercised
  in the Sandbox and should be before it is offered.
- Access is by capability links (organizer, invite, participant); there are
  no user accounts.
- Sandbox only. The deployment has no WAF, rate limiting or autoscaling.

## License

MIT. See [LICENSE](LICENSE).
