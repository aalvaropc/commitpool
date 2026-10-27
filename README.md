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
| `AWS_REGION` | Required unless `COMMITPOOL_AI_PROVIDER=fake`. |
| `COMMITPOOL_SANDBOX_MOCKS` | `1` is rejected unless `PAYPAL_BASE_URL` is a sandbox URL. |

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

## License

MIT. See [LICENSE](LICENSE).
