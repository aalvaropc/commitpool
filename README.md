# CommitPool

CommitPool helps an instructor run a paid workshop without guessing demand.
The instructor proposes a few candidate dates, participants describe their
availability in plain language, and once a date is fixed each participant
authorizes a PayPal payment. The workshop is confirmed only when the minimum
number of completed payments is reached. If it is not reached by the deadline,
the workshop is cancelled and every charge already made is refunded.

The app talks to the **PayPal Sandbox** only. No real money moves.

## Stack

- Go service (`cmd/commitpool`) with PostgreSQL via `pgx`
- React + TypeScript single-page app built with Vite (`web/`), embedded into the
  Go binary with `go:embed`
- SQL migrations (`migrations/`) embedded and applied on startup

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

### Configuration

All configuration comes from environment variables; `.env.example` lists every
one of them with its default. Secrets are only ever read from the environment.

| Variable | Notes |
| --- | --- |
| `COMMITPOOL_BASE_URL` | Required. Public URL of the deployment, no trailing slash. |
| `DATABASE_URL` | Required. For the local database: `postgres://commitpool:commitpool@localhost:55432/commitpool?sslmode=disable` |
| `PAYPAL_CLIENT_ID`, `PAYPAL_CLIENT_SECRET` | Required unless `E2E_FAKE_PAYPAL=1`. PayPal Sandbox app credentials. |
| `COMMITPOOL_AI_PROVIDER` | `bedrock` (default) or `fake`, the deterministic keyword interpreter for tests and demos. |
| `AWS_REGION` | Required unless `COMMITPOOL_AI_PROVIDER=fake`. |
| `COMMITPOOL_BEDROCK_MODEL`, `COMMITPOOL_BEDROCK_FALLBACK_MODEL` | Bedrock model IDs; default `anthropic.claude-opus-5-5` and `anthropic.claude-opus-4-8`. `none` as the fallback model turns the refusal fallback off. |
| `COMMITPOOL_SANDBOX_MOCKS` | `1` is rejected unless `PAYPAL_BASE_URL` is a sandbox URL. |

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
owner's one-paragraph explanation of that recommendation (at most 80 words,
cached in memory per input). If the model declines a request, the SDK's
client-side refusal fallback retries it once on the fallback model.

Model IDs on Bedrock carry the `anthropic.` prefix (`anthropic.claude-opus-5-5`).
If Bedrock answers 400 or 404 for a model, check its ID in the Bedrock console's
model catalog for your region and set `COMMITPOOL_BEDROCK_MODEL` (and
`COMMITPOOL_BEDROCK_FALLBACK_MODEL`) to it. To use Bedrock:

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

## Tests

```sh
make db-up
make test                  # go test ./... against the commitpool_test database
go vet ./...
pnpm -C web typecheck
pnpm -C web build
```

Database tests read `TEST_DATABASE_URL` and fail when it is missing. Set
`COMMITPOOL_SKIP_DB_TESTS=1` to skip them explicitly.

No test calls Bedrock unless `COMMITPOOL_LIVE_AI=1` is set; the rest use the
fake interpreter or a local stand-in for the Bedrock endpoint.

## License

MIT. See [LICENSE](LICENSE).
