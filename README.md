# CommitPool

**Find the date that fills the class.** The class happens, or nobody pays.

A life drawing teacher needs 3 people at $45 to cover $120 for the room and the model. She asks in the group chat and gets a dozen "maybe"s, no date, and no idea whether the room will be paid for.

CommitPool turns that chat into one link:
1. People say when they can come in their own words.
2. Claude reads every answer against every date.
3. CommitPool picks the date that works.
4. Everyone reserves with a PayPal hold that is charged only if enough people reserve.

If too few reserve by the deadline, every hold is released.

**[Open the demo](https://3-209-60-155.sslip.io)** and press **Try it as Iris**. You become Iris Calder, who teaches life drawing in Brooklyn, and watch six answers arrive and get read. It all runs on the PayPal Sandbox: no real money moves.

![The landing page: the promise next to a group chat being read](docs/images/landing.png)

## How it works

1. **Iris describes her class in a sentence.** Claude drafts the form, and the session cost tells her how many people she needs. Nothing is created until she checks it.
2. **She shares one link in the group chat.** People join with a name, no account, and answer like they'd text a friend.
3. **Claude reads every answer against every date.** A condition like "only if we finish before six" keeps its time. A vague answer gets a question, not a guess. CommitPool counts who can come and recommends the date.
4. **Everyone reserves with PayPal.** Each person approves a hold. When enough have reserved, the holds are captured and the class is on. Otherwise every hold is voided and any capture refunded.

![Who can come: each answer read by Claude, per date, with the words it came from](docs/images/organizer-board.png)

| The invite, opened from the group chat | Claude drafts the class, Iris checks it |
| --- | --- |
| <img src="docs/images/invite-phone.png" alt="The invite page on a phone" width="320"> | <img src="docs/images/create-draft.png" alt="The drafted form, with the session cost suggesting 3 people" width="420"> |

## PayPal holds the money until the class is real

- **Reserving is a hold, not a charge.** Orders v2 with `intent: AUTHORIZE`. Payments v2 then captures, voids or refunds.
- **"It's on" counts completed captures only.** An authorized hold is a promise, and a declined capture is not a seat.
- **Every money call is idempotent.** Each capture, void and refund has its own key, saved before the request is sent. A timeout is never retried blindly: the order is read back from PayPal and the local state is reconciled to it.
- **Webhooks and reconcile.** Webhooks are verified with PayPal before they are stored. A periodic reconcile reads back anything pending.
- **Fees, and a check against PayPal.** Each capture keeps PayPal's fee and net from its `seller_receivable_breakdown`, so the organizer sees what was collected, what PayPal kept and the net. **Verify with PayPal** reads every seat's order, hold, charge and refund back from PayPal and compares them with CommitPool's records, without changing anything.
- **Plans can change.** A participant can withdraw until they are charged, and the organizer can cancel until the class starts.
- **The organizer is paid when the class starts.** With PayPal Payouts, CommitPool sends the organizer's PayPal email what people paid, less PayPal's fees: one batch per class, under a `sender_batch_id` PayPal deduplicates, tracked until PayPal reports it paid. It waits for the start because until then a cancel refunds everyone.

The state machines, keys, deadlines and the operator runbook are in [docs/payments.md](docs/payments.md).

## Claude reads, CommitPool decides

Claude Sonnet 4.5 on Amazon Bedrock does the reading and the writing. The code makes every decision that counts.

| Claude | CommitPool |
| --- | --- |
| Reads each answer into a verdict per date, as strict JSON | Decides who can come and recommends the date |
| Drafts the class from a description | Validates every field; the organizer creates it |
| Explains the recommendation and drafts nudges | Picks who to nudge; the organizer sends them, or not |
| Never touches money | Captures, voids, refunds and pays out by its own rules |

Messages reach Claude as data, never as instructions, and the server re-validates everything it returns.

<img src="docs/images/claude-suggests.png" alt="The recommended date, a nudge Claude drafted for Grace, and Claude's explanation" width="560">

## Run it from an AI assistant

CommitPool is also a [Model Context Protocol](https://modelcontextprotocol.io) server:

```sh
claude mcp add --transport http commitpool https://3-209-60-155.sslip.io/mcp
```

What each side's assistant can do:
- **Organizer:** create a class, ask who can come and why, open reservations after the organizer approves the exact terms on their own page, and check the payments against PayPal without changing anything.
- **Participant:** join, pass on when they can come, and get their PayPal link.

No tool moves money or approves anything: only the person's own approval at PayPal places a hold, and the MCP tools alone cannot open reservations, which needs the organizer's approval on their own page. Anyone holding the owner link can still open reservations from the organizer page, and a script could forge a browser's headers; [docs/mcp.md](docs/mcp.md) spells out these limits.

## Architecture

```
AI assistant ── MCP (/mcp) ──┐
browser ── React SPA ────────┴──► Go API ──► PostgreSQL
                                    │  ▲
                                    ▼  │ verified webhooks
                       payment worker ◄──── PayPal Sandbox (Orders v2, Payments v2)
                                    │
                                    └──► Claude on Amazon Bedrock
```

- **One Go binary** serves the API, the embedded SPA and the MCP server, and runs the payment worker. Migrations apply on startup.
- **Jobs and an outbox** so a retry is safe and an email never blocks a money movement.
- **Hosting.** The demo runs on one EC2 VM with Caddy ([deploy/ec2](deploy/ec2/README.md)).

## Run it locally

```sh
cp .env.example .env    # your PayPal Sandbox app keys
make db-up              # PostgreSQL on localhost:55432
make web-build
make run                # http://localhost:8080
make seed               # Iris's class, with the answers already sent
```

Set `AWS_REGION` to read with Claude on Bedrock, or `COMMITPOOL_AI_PROVIDER=fake` to run without it.

Tests:

```sh
make test && make check
pnpm -C web test && pnpm -C web lint
bash scripts/e2e.sh
```

[docs/running.md](docs/running.md) has the rest:
- configuration and demo mode;
- Bedrock setup;
- Docker;
- deployment;
- what each test suite covers.

## Limitations

- PayPal Sandbox only, prices in USD.
- Reservations stay open for at most 72 hours, the time PayPal honours an authorization; reauthorization is not built.
- No accounts: access is by private links for the organizer, the invite and each participant.
- Claude drafts the nudges, and the organizer sends them; CommitPool does not.
- Once the organizer is paid out, a later refund, reversal or dispute on a payment it counted does not reduce the payout; the platform absorbs it.

## License

MIT. See [LICENSE](LICENSE).
