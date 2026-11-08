# Design

CommitPool's interface is **kiln chat**: a warm charcoal canvas, bone-coloured
ink, one ember brand colour and four glazes for states. The thesis is that
**the interface is the group chat**. People answer in their own words, as they
would in the chat where the invite was shared, and Claude is one more member
of that chat: it reads each message, replies when a message is unclear and
drafts the nudges the organizer may send. CommitPool, not Claude, counts who
can come, picks the date and moves money. The product truth behind it is in
[PRODUCT.md](PRODUCT.md).

## Tokens

Defined once in `web/src/index.css` (`@theme`); components use the Tailwind
utilities they generate (`bg-brand`, `text-ink-soft`, `rounded-tile`). The page
is dark only (`color-scheme: dark`).

| Role | Token | Value | Use |
|---|---|---|---|
| Canvas | `canvas` | `#14100d` | Page background |
| Surface | `surface` / `surface-muted` | `#1d1713` / `#2a221c` | Tiles; wells, inputs, other people's bubbles |
| Ink | `ink` / `ink-soft` / `ink-faint` | `#f4ece0` / `#c2b5a5` / `#a39686` | Text; secondary text; placeholders, interest bars |
| White | `white` | `#fbf6ee` | Hover of the `inverse` button only |
| Lines | `line` / `line-strong` | `#3a2f27` / `#7d6d60` | Dividers; input borders |
| Brand (ember) | `brand` / `brand-deep` / `brand-wash` | `#ff6a3d` / `#ff8660` / `#3a1d12` | Logo, primary buttons, your own bubble, "It's on"; hover; CommitPool's bubble |
| On brand | `on-brand` | `#1a0d07` | Every text or icon on ember |
| Paid glaze | `paid` / `paid-wash` | `#7be0ae` / `#14271e` | Paid, can come |
| Hold glaze | `hold` / `hold-wash` | `#8aa4ff` / `#181f3d` | Reserved (a PayPal hold), reservations open |
| Maybe glaze | `maybe` / `maybe-wash` | `#f4bd4f` / `#2b2210` | Can come with a condition |
| No glaze | `no` / `no-wash` | `#f08096` / `#351a21` | Can't come, cancelled, field errors |
| Attention | `attention` / `attention-wash` | `#f4bd4f` / `#2b2210` | Something needs the organizer |
| Avatars | `avatar-{amber,sky,pink,mint,lilac,peach}` and `-ink` | light / dark pairs | Initials, picked by a hash of the name |

`web/src/components/ui/tone.ts` maps the five status tones (`neutral`, `hold`,
`paid`, `cancel`, `attention`) to text, wash and fill classes; workshop status
maps to them in `web/src/lib/status.ts` (collecting neutral, funding hold,
confirmed paid, cancelled cancel).

Contrast (WCAG): ink on canvas 16.2:1, ink-soft on surface-muted 7.8:1,
ink-faint on surface-muted 5.4:1, on-brand on ember 6.7:1, each glaze on its
wash at least 6.2:1, ember on canvas 6.7:1, line-strong on surface-muted 3.2:1.

Radii: `tile` 1.5rem, `control` 1rem; buttons, pills, chips and segments are
fully round; chat bubbles are `rounded-3xl` with one tucked corner. Nothing
uses a shadow: depth comes from the surface steps. Easing: `--ease-out`
(`cubic-bezier(0.16, 1, 0.3, 1)`).

## Type

- **Bricolage Grotesque** (SIL OFL 1.1) is the only family, self-hosted as one
  variable Latin woff2 (`web/src/assets/fonts`, weights 200–800, widths
  75–100%) with `font-optical-sizing: auto`.
- Body 1.0625rem, line height 1.5. `h1`–`h3` are 700, line height 1.02,
  tracking -0.025em, `text-wrap: balance`.
- Scale: `text-display` `clamp(2.75rem, 9.5vw, 6rem)` / 0.92, `text-head`
  `clamp(2rem, 5vw, 3.5rem)` / 1, `text-lead`
  `clamp(1.25rem, 1.1rem + 0.6vw, 1.5rem)` / 1.3.
- **`shout`**: 800 weight, 75% width, tracking -0.02em, uppercase. Used for the
  landing headlines (`text-display`, `text-head`), the event title on the brand
  tile and the "It's on" moment. `display`, `wide` and `semiwide` are defined
  for tighter display tracking.
- Prices, counts and times use `tabular-nums`.

## Components

All in `web/src/components/ui/`, exported from its `index.ts`.

| Component | What it is for |
|---|---|
| `Tile`, `TileLabel` | The unit of every screen: `surface`, `muted`, or `brand` (the event hero; focus rings turn on-brand inside it). |
| `Button`, `ButtonLink` | `primary` (ember, on-brand text), `secondary`, `quiet`, `danger`, `demo`, `inverse`. `sm`/`md` are at least 44px tall, `lg` 56px. A busy button keeps focus with `aria-disabled`; a busy submit is disabled so Enter cannot send twice. |
| `Pill`, `DemoTag` | Short status with a dot, in a tone's wash; the text is always there. |
| `VerdictChip` | One person's answer for one date: icon plus words, paid glaze (can come), maybe (conditional), no (can't), muted (not sure). |
| `Avatar`, `AvatarStack` | Initials in a colour chosen by name; the stack is one labelled image for screen readers. |
| `Meter` | Progress to "it's on": paid glaze then hold glaze on a `surface-muted` track, a 3px ink line at the number needed, a caption ("2 more and it's on") and an sr-only summary. The `interest` variant shows who can come in `ink-faint`. |
| `Stat` | Label, big number, note. |
| `Notice` | Inline status or error in a tone's wash. |
| `ConfirmDialog` | Native `<dialog>` for decisions with consequences; cannot be dismissed while its action runs and announces the busy state. |
| `SegmentedControl` | Native radios as segments; the chosen one is ember with on-brand text. |
| `Field`, `inputClass`, `describedBy` | Labelled inputs with hint and error wired to `aria-describedby`; invalid fields turn to the no glaze. |
| `Icon` | One family of authored 24px stroke icons (1.75 stroke). |
| `Logo`, `LogoMark` | The bowl: an ember half-disc with a canvas rim line and a bone lid line, beside the 800-weight wordmark. |

Product compositions built from them:

- **Decision card** (`web/src/components/Decision.tsx`), the first thing an
  organizer sees: a live status line with its tone icon, the next step as a
  heading, the chosen date and reservation deadline, the counts (`Meter` plus a
  key of paid, reserved and needed) and at most one action (open reservations
  for the recommended day, or the invite link to share).
- **StatusBanner** does the same job for a participant; **EventHero** is the
  brand tile with the `shout` title, host avatar and place.
- **Claude's read** and **Claude suggests** (`ClaudeRead.tsx`,
  `ClaudeSuggests.tsx`) say in their own copy that CommitPool decides and
  Claude only words it; suggested nudges are drafts the organizer chooses to
  send.
- **Chat bubbles** (`web/src/components/home/ChatBubbles.tsx`, the landing
  demo): other people's messages in `surface-muted` with an avatar and a
  tucked bottom-left corner; your own in ember with on-brand text, tucked
  bottom-right; typing dots; Claude's reading as `VerdictChip`s labelled with
  who read it ("Read by Claude", "Prepared reading", "Simple rules read this
  one"); CommitPool's counts in a `brand-wash` bubble; Claude's clarifying
  question in a muted bubble; centred `Pill` notices; and the ember "It's on"
  card in `shout`, marked "Simulated here: no money moved."

## Motion

- `rise` (420ms, `--ease-out`, fade up 10px from 98% scale) brings in chat
  rows, notices and feed items; `blink` pulses the typing dots.
- The `Meter` fill animates its width over 700ms; buttons ease their
  background in 200ms and press down 1px; loading skeletons use
  `animate-pulse`.
- Under `prefers-reduced-motion: reduce` a global rule cuts every animation and
  transition to 0.01ms, runs each once and turns off smooth scrolling. The chat
  demo also shortens its scripted pauses to at most 20ms and scrolls without
  smoothing.

## Accessibility

- **Never white on ember.** White on `#ff6a3d` is 2.9:1; every label, icon and
  selection on ember uses `on-brand` (6.7:1).
- **44px targets**: buttons and segments are at least `min-h-11`, inputs
  `min-h-12`.
- **Focus**: a 3px ember `:focus-visible` outline with a 3px offset (on-brand
  inside the brand tile); a skip link to `#main`; after a step completes,
  focus moves to the heading that describes the new state (`tabIndex={-1}`),
  and a failed form focuses its first invalid field; confirmations are native
  modal `<dialog>`s.
- **Announced changes**: status lines are `role="status"` with
  `aria-live="polite"`; the chat demo is a `role="log"`.
- **Colour never carries meaning alone**: chips, pills and meters always have
  words.
- Browser surfaces are themed: selection, caret, `accent-color` and the
  scrollbar.
- Layouts are written phone first: unprefixed classes are the phone layout,
  `sm:` and `lg:` add columns.
- `web/e2e/accessibility.spec.ts` runs axe (WCAG 2.2 AA) with reduced motion on
  every page a person reaches and fails on any serious or critical problem.

## Link previews

- **Landing**: `/` carries `web/public/og-default.png` (1200×630): the bowl and
  wordmark, "The class happens, or nobody pays." in `shout` with the second
  line in ember, verdict chips and an ember "It's on".
- **Invites**: `/w/{token}` points `og:image` at `GET /api/v1/w/{token}/og.png`,
  drawn in Go by `internal/ogimage` in the same family: canvas, bowl and
  wordmark, a status pill (neutral "Finding the date" and "Reservations
  closed", hold "Reservations open", ember "It's on", no "Cancelled"), the
  title in `shout` (120px down to 60px, up to three lines, then an ellipsis),
  "with {host} · {place}", and three tiles: the dates or the chosen date, the
  price in ember with "a spot", and a meter with "{n} needed · {m} spots",
  "{n} reserved · {m} needed" or "{n} paid · {m} spots". It never shows
  participant names.
- The image fonts are static Bricolage instances made with fontTools from the
  google/fonts variable font (ExtraBold at 75% width for the title, price and
  "It's on"; SemiBold and Medium at 100% for text; an ExtraBold wordmark
  subset), Latin and Latin Extended-A, with the OFL next to them. Characters
  Bricolage lacks fall back to the Go fonts; anything neither draws is left
  out.
- `og:image` carries `?v=` (12 hex characters of a SHA-256 over
  `ogimage.Revision` and the card). The versioned URL is cached for a year as
  immutable, the bare URL for five minutes. Bump `ogimage.Revision` whenever
  the drawing changes so chats fetch the new card.
