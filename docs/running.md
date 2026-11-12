# Running CommitPool

How to run, configure, test and deploy CommitPool. For what it is and how it
works, start with the [README](../README.md).

## Run it locally

Requirements: Go 1.26, Node 22+ with pnpm 10, Docker with Compose.

```sh
cp .env.example .env     # then fill in the values you need
make db-up               # PostgreSQL 16 on localhost:55432
make web-build           # builds the SPA into web/dist
make run                 # loads .env and starts the service on :8080
curl -s localhost:8080/healthz   # {"ok":true,...} with queue counts and worker heartbeats
```

For frontend work, run `pnpm -C web dev`. Vite proxies `/api`, `/paypal`,
`/mcp` and `/healthz` to the Go service on port 8080.

### Seed Iris's class

With the service running, `make seed` creates Iris's demo workshop through the
HTTP API on the next free weekend: 3 needed of 6 spots at US$45. Its six
participants have already sent their availability messages. Saturday has 2 of
the 3 needed: Grace's message leaves open whether she can make a morning, so
Claude asks her about the class hours, and her answer is what lets Saturday open.

`make seed` prints the organizer link, the invite link and one link per
participant, and needs no credentials. `go run ./cmd/seed -h` lists the flags:
- `-base` for another base URL;
- the price, time zone and payment window;
- `-open-payments` to send Grace's answer to Claude's question and open payments on the recommended date right away.

### Demo mode

With `COMMITPOOL_DEMO_MODE=1`, the landing page offers **Try it as Iris**:
- `GET /api/v1/demo` says demo workshops are on, and carries `video_url`, `devpost_url` and `judge_login` when they are configured (see below).
- `POST /api/v1/demo/workshops` creates a workshop for Iris with nobody in it yet. It answers with the organizer link, the invite link and an empty `participants` list.
- The organizer view of a demo workshop carries `demo_chat`: Iris's group chat as a WhatsApp export, with her six people's messages word for word. The board offers it in **Paste your group chat**; **Read the chat** then **Add 6 people** imports them like any pasted chat, and Claude reads them in the background. A demo workshop takes one import of at most 8 people, and only while nobody is in it.
- The organizer view of a demo workshop lists the participant links of the people imported from its chat as `act_as`, so **Act as** works from any device that has the organizer link. Only those people are listed, and only on a demo workshop; the plain links are kept for them alone (`participants.demo_link_token`). The MCP `get_status` never carries them.
- In every workshop, demo or not, people the organizer adds from a pasted group chat also keep their plain page link (`participants.chat_link_token`), and only those rows can (`source = 'chat'`). It lets the organizer copy each person's link from the board (`participant_url` in the organizer view) and lets **Read again** resubmit what they said in the chat. Only the organizer view carries it: the participant and invite views, the logs and the MCP `get_status` never do. Removing that person deletes the link with their row.

Limits on starting demos:
- One address can start five demos, then one every 20 minutes. This applies only while the rate limit is on.
- At most 30 demos start in any hour, and at most 6 are being set up at once.
- A demo starts only when the Bedrock caps leave room for about 10 more calls. Otherwise it answers 429 `demo_resting` with `Retry-After` and says when to try again.

Both endpoints answer 404 outside demo mode. The demo also adds the
organizer's **Close reservations now** control; once the class is on and
reservations are closed, **Start the class now**, which moves the demo clock
to the class start and pays the organizer out right away (see
[Payouts](#payouts)); and, while a class is funding, a **Demo clock** with
**Skip to the last 2 hours** (`POST /api/v1/owner/{token}/demo/skip-to-last-hours`):
while its deadline has never been extended, it moves `closes_at` to two hours
from now (only ever earlier) and brings the `close:{proposal}` job forward
with it, in one transaction guarded by `extended_at is null`
(`store.SkipToLastHours`), so a short class reaches the window in which Claude suggests
rescuing it with one deadline extension. It answers 404 outside demo mode
too.

With `COMMITPOOL_SANDBOX_MOCKS=1` as well, **Fail next charge** makes PayPal
answer `AUTHORIZATION_EXPIRED` to one capture through the
`PayPal-Mock-Response` header. The server refuses that header outside the
Sandbox.

## Pasting a group chat

On the organizer page, **Paste your group chat** reads a chat the organizer
pastes (or uploads as WhatsApp's **Export chat**, `.txt`, without media) and adds
the people in it to the class, each with what they wrote. It works while the
class is still finding its date (`collecting`). Three endpoints, all under the
organizer link and answering JSON:

| Endpoint | What it does |
| --- | --- |
| `POST /api/v1/owner/{token}/chat/preview` | Body `{"text": "..."}`. Parses the chat and returns `people` (`name`, `excerpt`, `messages`, `host`, `joined`) and how many lines were `skipped`. Writes nothing and calls no model. |
| `POST /api/v1/owner/{token}/chat/import` | Body `{"text": "...", "names": ["..."]}`. Adds the chosen people, skipping the organizer (a sender whose name is theirs or starts with their first name, as in "Iris 🎨" or "Iris C.") and anyone already in the class, and answers 202 with `imported` (`name`, `participant_url`) and `skipped` (`name`, `reason`: `host`, `already_here` or `not_in_chat`). Claude reads each person's text in the background; the board shows "Claude is reading..." until each reading arrives. |
| `POST /api/v1/owner/{token}/chat/retry` | Reads again the people added from the chat who have no reading yet, and answers 202 with `{"reading": n}`. It is what **Read again** calls after a person shows "Couldn't read yet". |

`DELETE /api/v1/owner/{token}/participants/{id}` removes someone added from the
chat who has not reserved, while the class is still finding its date.

Pasting the chat again reads only what is new. A person already added whose messages in the new paste go beyond what was stored is read again with just the new messages, as a follow-up to Claude's question; the preview marks them with `new_messages`, and the import answers them under `updated` next to `imported`. An edited chat, or a different export that does not continue the stored one, is left alone, and someone still being read is skipped until the reading lands. A demo workshop allows one such re-paste after its import (422 `demo_update_used` afterwards), and its owner view carries `demo_update`: Iris's chat with Grace's reply, offered while her question is open.

Limits:
- A chat is at most 20,000 characters (413 `chat_too_long`) and at most 30 people (422 `chat_too_many_people`). An empty paste is 422 `chat_empty`, and a paste with no `Name: message` lines is 422 `chat_no_lines`. Nothing is created in any of these cases.
- Each person's text sent to Claude is at most 2,000 characters, keeping the newest messages. Messages that span several lines, WhatsApp's direction marks and names with accents or emoji are handled; names are sanitized and always shown as text.
- A demo workshop takes one import of at most 8 people, and only while nobody is in it (422 `demo_import_used`, 422 `demo_too_many_people`).
- A real workshop takes at most three imports (or retries) per 10 minutes, counted in memory (429 `import_limit` with `Retry-After`).
- Before anything is created, an import checks that the Bedrock caps leave room for one reading per person; otherwise it answers 429 `ai_capped` with `Retry-After`. If a reading still fails later, the person shows as "Couldn't read yet" and **Read again** finishes them.
- At most two readings of pasted chats run at once on the server.

Claude only reads availability from a message: a message cannot change the price, the minimum or the dates, and the server counts a person only once their reading is clear. People added this way keep a plain page link the organizer can copy (see [Demo mode](#demo-mode) for how it is stored).

## Configuration

All configuration comes from environment variables, and secrets are only ever
read from the environment. `.env.example` lists every variable with its
default. `commitpool check-config` loads them as the server would, prints
`config: ok` and exits 0, or prints why the server would refuse to start and
exits 1; the EC2 deploy runs it before it replaces the running app.

| Variable | Notes |
| --- | --- |
| `PORT` | HTTP port the service listens on (default `8080`). With compose, `COMMITPOOL_PORT` set in the shell picks the host port. |
| `COMMITPOOL_BASE_URL` | Required. Public URL of the deployment, no trailing slash; PayPal sends buyers back to it. |
| `DATABASE_URL` | Required. For the local database: `postgres://commitpool:commitpool@localhost:55432/commitpool?sslmode=disable` |
| `PAYPAL_BASE_URL` | Defaults to the Sandbox, `https://api-m.sandbox.paypal.com`, and must be exactly that unless `COMMITPOOL_ALLOW_LIVE_PAYPAL=1`. |
| `COMMITPOOL_ALLOW_LIVE_PAYPAL` | `1` accepts a `PAYPAL_BASE_URL` other than the Sandbox, such as the live `https://api-m.paypal.com`; it must still be https. Default `0`. Refused next to `COMMITPOOL_DEMO_MODE=1` or `COMMITPOOL_SANDBOX_MOCKS=1`, which are Sandbox-only. |
| `PAYPAL_CLIENT_ID`, `PAYPAL_CLIENT_SECRET` | Required unless `E2E_FAKE_PAYPAL=1`. PayPal Sandbox app credentials. |
| `PAYPAL_WEBHOOK_ID` | Id of the Sandbox webhook pointing at `/paypal/webhook` (`scripts/register-webhook.sh` creates it). Without it the webhook endpoint answers 503 and the periodic reconcile does the work alone. |
| `COMMITPOOL_AI_PROVIDER` | `bedrock` (default) or `fake`, a deterministic keyword interpreter for tests that also plays back the scripted readings of Iris's demo cast, including Grace's answer when it follows her first message. |
| `AWS_REGION` | Required unless `COMMITPOOL_AI_PROVIDER=fake`. |
| `COMMITPOOL_BEDROCK_MODEL`, `COMMITPOOL_BEDROCK_FALLBACK_MODEL` | Bedrock model or inference profile IDs. The model defaults to Claude Sonnet 4.5 through the US inference profile, `us.anthropic.claude-sonnet-4-5-20250929-v1:0`. The fallback defaults to none, which turns the refusal fallback off. |
| `COMMITPOOL_AI_HOURLY_CAP`, `COMMITPOOL_AI_DAILY_CAP` | Most Bedrock calls in any sliding hour (default `200`) and in any sliding 24 hours (default `1500`). They count model calls as the app makes them; the SDK may retry a failed call up to twice, and that is still one call. Readings of availability may use the whole cap; explanations, nudges and drafts only three quarters of it, so they never starve readings. Past its share, a reading answers 429 `ai_capped` with `Retry-After` and a message that says when to try again, and the others fall back to plain text or an empty draft until the wait is over. Every reading calls Claude; an explanation, nudge or draft identical to one of the last 512 reuses that answer and is not counted. Setting only the daily cap lowers the hourly default to it. The fake provider has no cap. |
| `COMMITPOOL_BEDROCK_EFFORT` | Effort for every Bedrock call: `none` (default; Sonnet 4.5 takes no effort), or `low`, `medium`, `high`, `xhigh` or `max` for a model that takes one. |
| `COMMITPOOL_SEAT_TTL_MIN` | Minutes a reserved seat is held while the buyer approves at PayPal (default `30`). |
| `COMMITPOOL_DEMO_MODE` | `1` enables **Try it as Iris** and **Close reservations now**. |
| `COMMITPOOL_VIDEO_URL` | Optional. A YouTube link to the demo video (`https://youtu.be/…` or `https://www.youtube.com/watch?v=…`). On a demo server, `GET /api/v1/demo` returns it as `video_url`, a privacy-enhanced `youtube-nocookie.com` embed, and the landing's **For judges** section plays it. Any other link is refused at startup. |
| `COMMITPOOL_DEVPOST_URL` | Optional. An `https://` link to the project's Devpost page. On a demo server, `GET /api/v1/demo` returns it as `devpost_url` and the landing's **For judges** section links to it; without it the link is hidden. Any other value is refused at startup. |
| `COMMITPOOL_JUDGE_BUYER_EMAIL`, `COMMITPOOL_JUDGE_BUYER_PASSWORD` | Optional, set both or neither. A PayPal **Sandbox** Personal account for judges. On a demo server, `GET /api/v1/demo` returns them as `judge_login` and the landing shows them with copy buttons; without them, the landing points to the testing instructions instead. Never put a live PayPal login here: the page is public. |
| `COMMITPOOL_DEMO_PACE_MS` | Milliseconds a reading of a pasted chat waits, times the attempt, before trying again when Claude is busy and gave no `Retry-After` (default `2000`; the end-to-end script uses `50`). |
| `COMMITPOOL_SANDBOX_MOCKS` | `1` enables **Fail next charge** and the `PayPal-Mock-Response` header. Refused unless `PAYPAL_BASE_URL` is exactly the Sandbox URL. |
| `COMMITPOOL_PAYOUTS` | `1` pays each confirmed class's organizer, when the class starts, what participants paid less PayPal's fees, with PayPal Payouts. Default `0`: payout jobs wait and nothing is sent. See [Payouts](#payouts). |
| `COMMITPOOL_DEMO_PAYOUT_EMAIL` | The PayPal email demo workshops are paid out to, one plain address (a Sandbox Personal account). Default none: demo workshops have no payout. Logged only as set or not. |
| `COMMITPOOL_MAILER`, `COMMITPOOL_MAIL_FROM` | `log` (default) logs notifications with a masked address and does not send them. `ses` sends them with Amazon SES v2 and the default AWS credentials, from `COMMITPOOL_MAIL_FROM`. That must be a verified sender identity, optionally with a name: `CommitPool <hello@example.com>`. |
| `COMMITPOOL_RATE_LIMIT` | How many writes (POST, PUT) one client address may send a minute, in bursts of half that. `off` turns it off. Default `120`. Reads are never limited. |
| `COMMITPOOL_MCP_SECRET` | Signs the organizer approval cookie and the landing chat's questions. Without it a random key per process is used, so an approve page opened before a restart must be reloaded before Approve works. Required when more than one instance serves the site. |
| `E2E_FAKE_PAYPAL` | `1` runs an in-process fake PayPal for the end-to-end tests. Only a binary built with `-tags e2e` carries it (`scripts/e2e.sh` builds one). It is refused next to real PayPal credentials or with a `COMMITPOOL_BASE_URL` that is not localhost. |

## Sandbox accounts

Buyers approve checkouts with Personal Sandbox accounts. Create them in the
PayPal Developer Dashboard under Sandbox, Accounts, and log in with one on the
PayPal approval page.
- One Personal account may approve several orders; the app does not check that buyers are distinct.
- The Business Sandbox account that owns the REST app (`PAYPAL_CLIENT_ID` and `PAYPAL_CLIENT_SECRET`) receives the payments.

No real money moves.

## Payouts

With `COMMITPOOL_PAYOUTS=1`, a confirmed class's organizer is paid at the
class start, to the PayPal email they gave when creating the workshop
([Payments: Payouts](payments.md#payouts)). On the Sandbox this needs:

- **Payouts on the REST app.** In the Developer Dashboard, under Apps &
  Credentials, open the app behind `PAYPAL_CLIENT_ID` and turn on Payouts
  among its features. Without it PayPal answers the payout with a 403; the
  job retries it four times and then waits, parked, for an operator to
  revive it once Payouts is on.
- **A receiver.** A Personal Sandbox account whose email is the workshop's
  payout email; for the demo, set it as `COMMITPOOL_DEMO_PAYOUT_EMAIL`
  (`/commitpool/demo_payout_email` on EC2). An email with no Sandbox account
  leaves the payout `unclaimed`.
- **A Business balance.** The payout is drawn from the Business account's
  PayPal balance, where the captures land; PayPal may charge the sender a
  payout fee on top. A short balance is refused (`INSUFFICIENT_FUNDS`) and
  the payout is `failed`; the runbook says how to send it again.
- **Payout webhooks (optional).** `scripts/register-webhook.sh` subscribes to
  `PAYMENT.PAYOUTSBATCH.*` and `PAYMENT.PAYOUTS-ITEM.*`, and adds them to an
  existing webhook. Without them the hourly checks still find the outcome.

`scripts/e2e.sh` turns payouts on against the fake PayPal, which pays every
payout at once.

## Claude on Amazon Bedrock

CommitPool calls Claude Sonnet 4.5 through the Bedrock runtime with the
Anthropic Go SDK: `InvokeModel` with a Messages API body, signed with SigV4.

What each call must meet:
- **Readings and drafts** use strict JSON schemas, with every object closed and the candidate dates as an enum. The server validates every answer again before keeping it.
- **User text** reaches the model as a JSON string labelled as data, never as instructions.
- **Explanations** are cut at 60 words, and **nudges** refuse links and contact details. Both fall back to plain words when the model fails, or when an explanation names a date other than the recommended one.
- **The service logs**, per call, the model, latency and token counts. It never logs the prompt, the message or the answer.

Claude Sonnet 4.5 is served on Bedrock only through inference profiles:
`us.anthropic.claude-sonnet-4-5-20250929-v1:0` (US regions) or
`global.anthropic.claude-sonnet-4-5-20250929-v1:0`. The bare model id is
refused for on-demand calls.

To use Bedrock:

1. **Credentials and region.** Run `aws configure`, or set `AWS_PROFILE`, or set `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY`. Pick a region where the model is offered and set `AWS_REGION` to it. A Bedrock API key in `AWS_BEARER_TOKEN_BEDROCK` works too.
   - Credentials are checked at startup, so a missing one stops the service right away.
   - The Go SDK does not read a session from `aws login`. Export it for the shell first: `eval "$(aws configure export-credentials --format env)"`.
2. **Model access.** In the Amazon Bedrock console, enable access to the model in that region, and to the fallback model if you set one. For Anthropic models, submit the use-case form and accept the model agreement once per account; until then Bedrock answers 403.
3. **Check it end to end.** This smoke test calls Bedrock for real:

   ```sh
   COMMITPOOL_LIVE_AI=1 go test ./internal/ai/ -run Live -skip LiveEval -count=1 -v
   ```

## Run with Docker

The image is a multi-stage build:
1. Node builds the SPA.
2. Go compiles a static binary that embeds it.
3. The result runs on `gcr.io/distroless/static-debian12:nonroot` as a non-root user.

```sh
docker build -f deploy/Dockerfile -t commitpool:dev .
docker compose -f deploy/compose.yaml --profile app up -d --build --wait
docker compose -f deploy/compose.yaml --profile app down
```

The `app` profile runs the image next to PostgreSQL, fed by the `.env` file at
the repository root. Compose overrides `DATABASE_URL` and `PORT` for the
container.

To publish another host port, set `COMMITPOOL_PORT` in the shell that runs
compose; compose reads it from the shell, not from `.env`.
`COMMITPOOL_BASE_URL` must name that port.

The container has no `~/.aws`, so Bedrock credentials must be in `.env`.
Without `--profile app`, compose starts only PostgreSQL.

## Deploy to AWS

The hosted demo runs on one EC2 VM, at about US$17 a month while it is on:
- Docker runs PostgreSQL, the app and Caddy for HTTPS.
- The VM's role reaches Bedrock.
- The secrets live in SSM Parameter Store.

[deploy/ec2/README.md](../deploy/ec2/README.md) goes from an empty account to
a registered Sandbox webhook, and `scripts/deploy-ec2.sh` ships each new
commit.

`deploy/aws/` holds a production-shaped alternative that the demo does not
use. It runs ECS Fargate, RDS in private subnets and an ALB reachable only
from CloudFront, at about US$63 a month
([deploy/aws/README.md](../deploy/aws/README.md)).

## Tests

```sh
make db-up
make test                  # go test -race ./... against the commitpool_test database
make check                 # gofmt, go vet, staticcheck, govulncheck, no test code in the binaries, no code comments
pnpm -C web typecheck      # the SPA, the Playwright config and the e2e specs
pnpm -C web test           # vitest
pnpm -C web lint           # no code comments in the web sources
bash scripts/e2e.sh        # Playwright journeys and axe checks against a fresh server with the fake PayPal
```

**Go suites.** They cover:
- the pure rules;
- the store against a real PostgreSQL;
- the worker and the HTTP API against an in-process fake PayPal (timeouts, 409s, pending states, mock codes, duplicate webhooks);
- the demo seed against the real API.

**Database tests.** They read `TEST_DATABASE_URL` and refuse a database whose name does not end in `_test`. Set `COMMITPOOL_SKIP_DB_TESTS=1` to skip them.

**Fuzz targets.** Two of them cover money parsing and the cleaning of the model's reading:
- `go test ./internal/domain -fuzz FuzzParseMoney`
- `go test ./internal/httpapi -fuzz FuzzCleanInterpretation`

**Bedrock.** No test calls Bedrock unless `COMMITPOOL_LIVE_AI=1` is set.

**Reading eval.** [docs/eval.md](eval.md) compares Claude's readings with the keyword reader on the hand-written messages in `internal/ai/testdata/eval/readings.json`. In CI, `go test ./internal/ai` checks the corpus and scores the keyword reader without calling Bedrock. To measure Claude, export Bedrock credentials as above and run `COMMITPOOL_LIVE_AI=1 COMMITPOOL_EVAL_REPORT=$PWD/docs/eval.md go test ./internal/ai -run LiveEval -count=1 -timeout 20m -v`. It makes one Bedrock call per message, four at a time, retries throttled calls until each message has an answer, and rewrites the report. Claude's answers vary a little from run to run, so a rerun can move a miss from one message to another. CI fails when the report no longer matches the corpus, so a change to the corpus needs a new run. It fails only if a prompt injection worked or a message got no answer from Bedrock; accuracy is reported, not gated.

**No code comments.** The code carries none; names, tests and the docs say why.
- `go test ./internal/codestyle` fails on a comment in Go, SQL, shell, Terraform, YAML, CSS or HTML.
- `pnpm -C web lint` fails on one in the web sources.

Compiler and linter directives are allowed. The scripts are described in
[scripts/README.md](../scripts/README.md).

## Limits

- **Size.** A workshop takes at most 200 participants, and a participant at most 20 availability messages.
- **Model calls.** At most 9 run at once: 4 for readings and explanations, 2 for drafts, 2 for the landing chat and 1 for nudges. On Bedrock, `COMMITPOOL_AI_HOURLY_CAP` and `COMMITPOOL_AI_DAILY_CAP` bound how many are made, with a quarter kept for readings; past them, a message answers 429 `ai_capped` with `Retry-After`. The 503 `ai_busy` answer is different: every slot was taken for 10 seconds.
- **Drafts.** A draft answers 200 with an empty draft when Claude fails or every drafting slot is taken. Past the per-visitor or hourly quota it answers 429 with `Retry-After`.
- **Landing chat.** `POST /api/v1/sample-reading` reads the visitor's answer (1 to 160 characters) against next weekend's demo dates and stores nothing. It returns the verdicts, what counts and Claude's question, never the model's other text. An answer to that question sends the earlier answer back with the question and its `question_sig`; questions without a valid signature (keyed by `COMMITPOOL_MCP_SECRET`, or per process) are dropped. One address can send three, then one every 20 seconds and ten an hour while the rate limit is on; at most 60 are read in any hour and 150 in any day. It never waits for a slot (429 `sample_busy`), and it stops (429 `ai_capped`) once its own reading would leave an AI window with less than a tenth of that window's cap (at least 20 calls, at most half the cap) for participants; the hour and the day each keep their own reserve, so it never takes a reading a participant needs. Claude gets 12 seconds; a failure answers 503. The page then reads the answer with simple rules in the browser and says so.
- **Refunds.** A paid seat cannot refund itself. Only a cancellation refunds it, so the minimum the organizer relies on cannot drop after the fact.
- **Emails.** They are delivered at least once: after a crash between sending and recording, one may arrive twice. While the SES account is in its sandbox, only verified addresses receive mail.
- **Hardening.** There is no WAF or autoscaling.
