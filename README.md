# CommitPool

**The class happens, or nobody pays.**

A pottery teacher needs 3 people at $45 to cover a $120 studio. She asks in the group chat and gets a dozen "maybe"s, no date, and no idea whether the room will be paid for.

CommitPool turns that chat into one link:
1. People say when they can come in their own words.
2. Claude reads every answer against every date.
3. CommitPool picks the date that works.
4. Everyone reserves with a PayPal hold that is charged only if enough people reserve.

If too few reserve by the deadline, every hold is released.

**[Open the demo](https://3-209-60-155.sslip.io)** and press **Try it as Maya**. You become Maya Okafor, who teaches wheel-throwing in Brooklyn, and watch six answers arrive and get read. It all runs on the PayPal Sandbox: no real money moves.

![The landing page: the promise next to a group chat being read](docs/images/landing.png)

## How it works

1. **Maya describes her class in a sentence.** Claude drafts the form, and the studio cost tells her how many people she needs. Nothing is created until she checks it.
2. **She shares one link in the group chat.** People join with a name, no account, and answer like they'd text a friend.
3. **Claude reads every answer against every date.** A condition like "only if we finish before six" keeps its time. A vague answer gets a question, not a guess. CommitPool counts who can come and recommends the date.
4. **Everyone reserves with PayPal.** Each person approves a hold. When enough have reserved, the holds are captured and the class is on. Otherwise every hold is voided and any capture refunded.

![Who can come: each answer read by Claude, per date, with the words it came from](docs/images/organizer-board.png)

| The invite, opened from the group chat | Claude drafts the class, Maya checks it |
| --- | --- |
| <img src="docs/images/invite-phone.png" alt="The invite page on a phone" width="320"> | <img src="docs/images/create-draft.png" alt="The drafted form, with the studio cost suggesting 3 people" width="420"> |

## PayPal holds the money until the class is real

- **Reserving is a hold, not a charge.** Orders v2 with `intent: AUTHORIZE`. Payments v2 then captures, voids or refunds.
- **"It's on" counts completed captures only.** An authorized hold is a promise, and a declined capture is not a seat.
- **Every money call is idempotent.** Each capture, void and refund has its own key, saved before the request is sent. A timeout is never retried blindly: the order is read back from PayPal and the local state is reconciled to it.
- **Webhooks and reconcile.** Webhooks are verified with PayPal before they are stored. A periodic reconcile reads back anything pending.
- **Plans can change.** A participant can withdraw until they are charged, and the organizer can cancel until the class starts.

The state machines, keys, deadlines and the operator runbook are in [docs/payments.md](docs/payments.md).

## Claude reads, CommitPool decides

Claude Sonnet 4.5 on Amazon Bedrock does the reading and the writing. The code makes every decision that counts.

| Claude | CommitPool |
| --- | --- |
| Reads each answer into a verdict per date, as strict JSON | Decides who can come and recommends the date |
| Drafts the class from a description | Validates every field; the organizer creates it |
| Explains the recommendation and drafts nudges | Picks who to nudge; the organizer sends them, or not |
| Never touches money | Captures, voids and refunds by its own rules |

Messages reach Claude as data, never as instructions, and the server re-validates everything it returns.

<img src="docs/images/claude-suggests.png" alt="The recommended date, a nudge Claude drafted for Grace, and Claude's explanation" width="560">

## Run it from an AI assistant

CommitPool is also a [Model Context Protocol](https://modelcontextprotocol.io) server:

```sh
claude mcp add --transport http commitpool https://3-209-60-155.sslip.io/mcp
```

What each side's assistant can do:
- **Organizer:** create a class, ask who can come and why, and open reservations after the organizer approves the exact terms on their own page.
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
make seed               # Maya's class, with the answers already sent
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

## License

MIT. See [LICENSE](LICENSE).
