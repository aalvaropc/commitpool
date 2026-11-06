# Design

CommitPool's interface is **Bento**: modular tiles of different sizes on a soft
lavender-grey canvas, one confident violet for the brand and the people who
reserved, one bright orange for the single action that moves a person forward.
It should feel like a real, warm event a friend sent you, never like a
payments dashboard. The product truth behind it is in [PRODUCT.md](PRODUCT.md).

The direction came from the UI UX Pro Max catalog (Bento Box Grid style, the
event-booking palette, the Outfit and Work Sans pairing) and was chosen over
three other directions on 6 November 2026.

## Tokens

Defined once in `web/src/index.css` (`@theme`); components use the Tailwind
utilities they generate (`bg-brand`, `text-ink-soft`, `rounded-tile`).

| Role | Token | Value | Use |
|---|---|---|---|
| Canvas | `canvas` | `#f3f2f7` | Page background |
| Surface | `surface` / `surface-muted` | `#ffffff` / `#f8f7fb` | Tiles, inputs; quiet wells |
| Ink | `ink` / `ink-soft` / `ink-faint` | `#18122b` / `#5b5470` / `#6b6480` | Text; secondary text; placeholders |
| Lines | `line` / `line-strong` | `#e4e0ef` / `#8f88a6` | Dividers; control borders (3:1 on white) |
| Brand | `brand` / `brand-deep` / `brand-wash` | `#5b21b6` / `#4c1d95` / `#ede7fb` | Identity, primary buttons, reserved spots |
| Go | `go` / `go-deep` / `go-wash` | `#ea580c` / `#c2410c` / `#ffedd5` | The one forward action per screen |
| Paid | `paid` / `paid-wash` | `#15803d` / `#dcfce7` | Paid, can come |
| Maybe | `maybe` / `maybe-wash` | `#b45309` / `#fef3c7` | Can come with a condition |
| No | `no` / `no-wash` | `#9f1239` / `#ffe4e6` | Can't come, cancelled |
| Attention | `attention` / `attention-wash` | `#c2410c` / `#ffedd5` | Something needs the organizer |
| Avatars | `avatar-{amber,sky,pink,mint,lilac,peach}` and `-ink` | pastel / dark pairs | Initials, picked by a hash of the name |

Every text pair clears WCAG AA: ink on canvas 16:1, ink-soft on white 7.1:1,
white on brand 9:1, each tone on its wash at least 4.5:1. White on `go` is
3.6:1, so only the large `go` button (19px bold, where AA asks for 3:1) uses
it; smaller orange buttons fill with `go-deep` (5.2:1).

Radii: `tile` 22px, `control` 14px, pills fully round. Shadows: `tile` (a
1px contact shadow plus a soft 28px drop), `lift` for dialogs, `go` for the
orange action. Easing: `--ease-out` (exponential ease-out).

The old token names (`paper`, `sheet`, `signal`, `hold`, `cancel`) remain as
aliases so screens not yet redesigned keep working; new code uses the names
above.

## Type

- **Outfit** (variable, self-hosted, OFL) for headings, numbers and anything
  the eye should land on first. Bold to extra-bold, tracking -0.02em.
- **Work Sans** (variable, self-hosted, OFL) for everything people read.
  Base 16px, line height 1.55.
- Numbers that line up (prices, counts, times) use `tabular-nums`.

## Components

All in `web/src/components/ui/`, exported from its `index.ts`.

| Component | What it is for |
|---|---|
| `Tile`, `TileLabel` | The bento unit. `surface`, `muted` or `brand` (the hero tile). |
| `Button`, `ButtonLink` | `primary` (violet), `go` (orange; darker below the large size), `secondary`, `quiet`, `danger`, `demo`. 44px minimum height. While busy an ordinary button keeps focus (`aria-disabled`); a submit button is disabled so Enter cannot send twice. |
| `Pill`, `Tag`, `DemoTag` | Short status labels with a dot; text always present. |
| `VerdictChip` | One person's answer for one date: icon plus words (Can come, Only if it ends by 6 PM, Can't, Not sure). |
| `Avatar`, `AvatarStack` | Initials in a pastel chosen by name; the stack names everyone for screen readers. |
| `Meter` | Progress to "it's on": paid (solid green) then reserved (violet hatch) against an ink line at the number needed, with the state and the split in words ("2 more and it's on", "1 paid · 2 reserved · 3 needed"). |
| `Cover` | Deterministic rings drawn from a workshop's id, decorating the brand tile; always round whatever the tile's shape. |
| `ConfirmDialog` | Native `<dialog>` for decisions with consequences (cancel, give up a spot, open reservations). Cannot be dismissed while its action runs, announces the busy state, returns focus on close. |
| `SegmentedControl` | Native radios styled as segments, the chosen one filled violet, for correcting a reading. |
| `Field`, `inputClass`, `describedBy` | Labelled inputs with hint and error wired to `aria-describedby`. |
| `Notice` | Inline status or error, toned. |
| `Icon` | Authored 24px stroke icons, 1.75 stroke, one family. |
| `Logo`, `LogoMark` | Three rising bars crossed by the orange line: a group filling up to the number it needs. |

## Rules

- **Money words follow PayPal, never lead it.** A label says charged, released
  or refunded only once that happened (see the glossary in PRODUCT.md).
- **One orange action per screen.** Everything else is violet, secondary or quiet.
- **Colour never carries meaning alone.** Chips and meters always have words.
- **No eyebrow labels above headings, no gradient text, no glass, no hard
  offset shadows, no emoji as icons.**
- **Phone first.** Every screen is designed at 390px and must not scroll
  sideways; tap targets are at least 44px.
- **Browser surfaces are themed**: focus ring (3px violet), selection, caret,
  accent colour for native controls, scrollbar.
- **Motion is one authored moment** (the meter filling, the "it's on" state),
  eased out, and disabled under `prefers-reduced-motion`.
- **Accessibility is tested**: `web/e2e/accessibility.spec.ts` runs axe
  (WCAG 2.2 AA) on every page a person reaches and fails on any serious or
  critical problem.
