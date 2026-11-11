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
| Paper | `paper` / `paper-deep` / `paper-rule` | `#f4ece0` / `#ebe1d2` / `#d6c8b4` | The landing's sketchbook pages; their hairlines |
| Charcoal | `charcoal` | `#14100d` | Text, rules and strokes on paper |
| Graphite | `graphite` / `graphite-soft` | `#5b4a3d` / `#6b5a4c` | Secondary and tertiary text on paper; the measuring ruler |
| Ember ink | `ember-ink` | `#a83c16` | Ember as text or a small mark on paper (the FAQ plus) |
| Composer | `composer` | `#241d18` | The landing chat's footer and the browser frame behind a screenshot |

`web/src/components/ui/tone.ts` maps the five status tones (`neutral`, `hold`,
`paid`, `cancel`, `attention`) to text, wash and fill classes; workshop status
maps to them in `web/src/lib/status.ts` (collecting neutral, funding hold,
confirmed paid, cancelled cancel).

Contrast (WCAG): ink on canvas 16.2:1, ink-soft on surface-muted 7.8:1,
ink-faint on surface-muted 5.4:1, on-brand on ember 6.7:1, each glaze on its
wash at least 6.2:1, ember on canvas 6.7:1, line-strong on surface-muted 3.2:1.
On paper: charcoal 16.2:1, graphite 7.2:1, graphite-soft 5.6:1, ember-ink
5.4:1. Ember itself is 2.4:1 on paper, so on paper it is only ever a stroke or
a fill behind on-brand text, never text.

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
  `clamp(1.25rem, 1.1rem + 0.6vw, 1.5rem)` / 1.3. The landing adds `text-hero`
  `clamp(3.25rem, 13.6vw, 6rem)` / 0.9 for the h1, `text-section`
  `clamp(2.25rem, 6.4vw, 4rem)` / 0.95 for section headings, `text-intro`
  `clamp(1.125rem, 1rem + 0.4vw, 1.3125rem)` / 1.4 beside them and
  `text-landing-lead` `clamp(1.1875rem, 1.05rem + 0.6vw, 1.5rem)` / 1.32.
- **`shout`**: 800 weight, 75% width, tracking -0.02em, uppercase. Used for the
  landing headlines (`text-hero`, `text-section`), the event title on the brand
  tile and the "It's on" moment; with `normal-case` it sets amounts and the
  "9 of 9 records match PayPal" line. `display` (-0.03em) and `wide` (-0.02em) set
  the display family with tighter tracking; `semiwide` sets only the family.
- Prices, counts and times use `tabular-nums`.

## Components

All in `web/src/components/ui/`, exported from its `index.ts`.

| Component | What it is for |
|---|---|
| `Tile`, `TileLabel` | The unit of every screen: `surface`, `muted`, or `brand` (the event hero; focus rings turn on-brand inside it). |
| `Button`, `ButtonLink` | `primary` (ember, on-brand text), `secondary`, `quiet`, `danger`, `demo`, `inverse`, and for ember backgrounds `ink` (on-brand fill, ink text) and `outline` (2px on-brand border). `sm`/`md` are at least 44px tall, `lg` 56px. A busy button keeps focus with `aria-disabled`; a busy submit is disabled so Enter cannot send twice. |
| `Pill`, `DemoTag` | Short status with a dot, in a tone's wash; the text is always there. |
| `VerdictChip` | One person's answer for one date: icon plus words, paid glaze (can come), maybe (conditional), no (can't), muted (not sure). `size="sm"` (13px) inside chat bubbles. |
| `Avatar`, `AvatarStack` | Initials in a colour chosen by name; the stack is one labelled image for screen readers. |
| `Meter` | Progress to "it's on": paid glaze then hold glaze on a `surface-muted` track, a 3px ink line at the number needed, a caption ("2 more and it's on") and an sr-only summary. The `interest` variant shows who can come in `ink-faint`. |
| `Stat` | Label, big number, note. |
| `Notice` | Inline status or error in a tone's wash. |
| `ConfirmDialog` | Native `<dialog>` for decisions with consequences; cannot be dismissed while its action runs and announces the busy state. |
| `SegmentedControl` | Native radios as segments; the chosen one is ember with on-brand text. |
| `Field`, `inputClass`, `describedBy` | Labelled inputs with hint and error wired to `aria-describedby`; invalid fields turn to the no glaze. |
| `Icon` | One family of authored 24px stroke icons (1.75 stroke). |
| `Logo`, `LogoMark` | The bowl: an ember half-disc with a canvas rim line and a bone lid line, beside the 800-weight wordmark; `size` and `mark` scale it (the landing header uses 22px type and a 30px bowl). |

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

## The landing: kiln and sketchbook

The landing (`web/src/pages/Home.tsx`, sections in `web/src/components/home/`)
alternates dark **kiln** sections with bone **paper** pages, as if a
sketchbook lay open on the studio floor. Order: hero, sourced figures, See it
work (paper), Claude finds the date, The money comes last (paper), PayPal for
real, the MCP assistant (paper), For judges, the FAQ (paper), the ember
closing band and the footer.

- **Layout.** `wrap` holds a 80rem column inside a `--gutter` of
  `clamp(1rem, 4.2vw, 4rem)`; desktop compositions sit on 12 columns with a
  2rem gap, phone layouts stack. `section-pad` gives every section
  `clamp(4rem, 9vw, 7.5rem)` above and below.
- **Paper pages** (`PaperSection`) are `paper grain`, `rounded-sheet`
  (1.75rem) and inset `clamp(0.5rem, 1.4vw, 1.25rem)` from the viewport edge.
  `grain` lays a multiplied fractal-noise SVG at half opacity behind the
  content. `paper` turns text charcoal, focus rings charcoal and selection
  charcoal on bone. Anything dark placed on paper (the chat, a screenshot
  frame, the terminal, the receipt) carries `kiln`, which restores ink text,
  ember focus rings and ember selection inside it.
- **The hero sheet** is a paper page that bleeds off the right edge on
  desktop (square right corners) and runs full width on a phone. It carries
  the class's page head (title with an ember underline, place, price, cost),
  the seated figure, and the live chat, which overhangs the sheet's left edge
  by 2.25rem and sits above the figure (`z-[2]`).
- **Screenshots** of the real demo live in `web/src/assets/shots/` as WebP
  (at most 1600px wide and 200 KB) and sit in CSS frames: a browser frame
  with a lock and a label, and a phone with a status bar and island. They are
  lazy, carry width and height, and say in their alt text what the screen
  shows. `web/scripts/capture.mjs <base-url>` walks the demo with Playwright
  and rewrites them, along with the README images in `docs/images/`.
- **The demo video** (For judges, only when the server sends `video_url`)
  shows its YouTube poster with a charcoal "Watch the demo" pill and loads
  the youtube-nocookie player only when pressed, then moves focus to it.
- **Section heads** are a `shout` `text-section` h2 with an intro paragraph
  beside it (columns 8 to 12) or under it when stacked. No eyebrows or
  kickers above them.

### Drawing rules

The drawings are authored inline SVG in `web/src/components/home/Drawings.tsx`:
the seated figure, the page-head underline, the hand-drawn arrow, the six
easels and the charcoal stick.

- Charcoal gesture lines with round caps and joins, 1 to 3.4px, some at 40 to
  80% opacity for construction strokes; graphite for the measuring ruler and
  for empty easels (dashed); one ember line per drawing, the gesture that
  matters (the figure's spine, the stroke the charcoal stick is making), and
  ember canvases with an on-brand mark for reserved easels.
- A drawing that carries meaning is `role="img"` with a label that says what
  it shows; a decorative one (arrows, underline) is `aria-hidden`.
- Drawings are never filled shapes or icons; icons stay in `Icon`. No
  gradients, no shadows.
- Arrows point from a caption to the screenshot it explains and show only on
  desktop.

## Motion

- `rise` (420ms, `--ease-out`, fade up 10px from 98% scale) brings in chat
  rows, notices and feed items; `blink` pulses the typing dots.
- The `Meter` fill animates its width over 700ms; buttons ease their
  background in 200ms and press down 1px; loading skeletons use
  `animate-pulse`.
- The landing's one authored moment is drawing: the hero figure's strokes
  draw themselves in sequence (`sketch-line`, `pathLength` 1, 900ms each,
  38ms apart, the ember spine last) and the reserved easels' canvases rise in
  as they scroll into view (`easel-canvas`, a scroll-driven `view()`
  timeline where supported, static otherwise). Both live inside
  `prefers-reduced-motion: no-preference`; without it the drawings are simply
  there.
- Under `prefers-reduced-motion: reduce` a global rule cuts every animation and
  transition to 0.01ms, runs each once and turns off smooth scrolling. The chat
  demo also shortens its scripted pauses to at most 20ms and scrolls without
  smoothing.

## Accessibility

- **Never white on ember.** White on `#ff6a3d` is 2.9:1; every label, icon and
  selection on ember uses `on-brand` (6.7:1).
- **44px targets**: buttons and segments are at least `min-h-11`, inputs
  `min-h-12`; on the landing every link and button in the header and main,
  including the source links, nav links and copy buttons, is at least 44 by
  44px, which `web/e2e/accessibility.spec.ts` checks at 390 and 1440px.
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
  wordmark, "Find the date that fills the class." in `shout` with the second
  line in ember, the promise line under a rule, and a paper sheet bleeding
  off the right edge with the class title, its ember underline, the seated
  figure, a "Sat · Can come" chip and an ember "It's on".
  `node web/scripts/og-default.mjs` renders it from
  `web/scripts/og-default.html` with Playwright and the self-hosted font.
- **Invites**: `/w/{token}` points `og:image` at `GET /api/v1/w/{token}/og.png`,
  drawn in Go by `internal/ogimage` in the same family: canvas, bowl and
  wordmark, a status pill (neutral "Finding the date" and "Reservations
  closed", hold "Reservations open", ember "It's on", no "Cancelled"), the
  title in `shout` (120px down to 60px, up to three lines, then an ellipsis;
  "A class on CommitPool" if nothing in it can be drawn), "with {host} ·
  {place}" (left out when the host cannot be drawn), and three tiles: the
  dates or the chosen date, the price in ember with "a spot", and a meter with
  "{n} needed · {m} spots", "{n} reserved · {m} needed" or "{n} paid · {m}
  spots". The dates tile is never narrower than 456px and only breaks between
  dates; the caption steps down to 24px and the price down to 34px before an
  absurdly long price is cut. It never shows participant names.
- The image fonts are static Bricolage instances made with fontTools from the
  google/fonts variable font (ExtraBold at 75% width for the title, price and
  "It's on"; SemiBold and Medium at 100% for text; an ExtraBold wordmark
  subset) covering Latin-1, Latin Extended-A, Romanian comma letters,
  Vietnamese, № and ≈ ≠ ≤ ≥, with the OFL next to them. Text is NFC-normalised
  and drawn one character at a time: each character Bricolage lacks comes from
  the Go fonts, and a character neither has becomes a space.
- `og:image` carries `?v=` (12 hex characters of a SHA-256 over
  `ogimage.Revision` and the card). The versioned URL is cached for a year as
  immutable, the bare URL for five minutes. Bump `ogimage.Revision` whenever
  the drawing changes so chats fetch the new card; a golden test of the sample
  cards' pixels fails until you do.
