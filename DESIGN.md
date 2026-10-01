# Design — Goal Tracker

The design system for Goal Tracker's web UI, with a light and a dark theme.
Pages follow this file. If you need to change the system, edit this file
deliberately; don't override it on one page.

## Provenance

- **Source:** "d3kn Warm Teal" by D3KN-prog,
  <https://designmd.ai/D3KN-prog/d3kn-warm-teal-2>, downloaded 2026-09-30.
- **Light theme:** the published DESIGN.md, taken verbatim except where
  § Deviations says otherwise.
- **Dark theme:** the source doesn't publish dark tokens. Its dark preview
  derives them in the browser from the primary teal:
  - a 12-step HSL tonal scale of `#14B8A6`
  - canvas `#020E0C`
  - tiles `rgba(6,53,48,.2)`
  - text `#E8E8E8`
  - card preview `#052E29`

  The dark tokens below start from those values. Any role the preview leaves
  out is marked *(chosen)*.
- The source's Arabic faces (Tajawal, Cairo) are dropped because this app is
  English-only.

## System

- **Mood:** warm, editorial and calm. Ivory canvas, deep-teal ink, vivid teal
  for interaction. No cold whites or neon.
- **Density:** generous. Cards use 24px padding, sections sit 20–32px apart,
  and everything aligns to a 4px grid.
- **Depth:** layered, teal-tinted shadows lift cards off the canvas.

## Tokens

```css
:root {
  /* Brand */
  --color-primary:        #14B8A6; /* Teal 500: active pills, accents */
  --color-primary-hover:  #0D9488; /* Teal 600 */
  --color-primary-strong: #0F766E; /* Teal 700: filled buttons, links. See Deviations */
  --color-primary-light:  #CCFBF1; /* Teal 100: badge fills, selected rows */
  --color-primary-subtle: #F0FDFA; /* Teal 50: faint tints */

  /* Canvas & surfaces */
  --color-canvas:        #FEFCE8; /* warm ivory body background */
  --color-surface:       #FFFFFF; /* cards, raised containers */
  --color-surface-hover: #FDFCF7;
  --color-surface-alt:   #F7F6E8; /* secondary panels, table headers */

  /* Ink */
  --color-ink:         #134E4A; /* Teal 900: headings, primary labels */
  --color-ink-2:       #2D6A66; /* body text */
  --color-ink-muted:   #527673; /* placeholders, timestamps. See Deviations */
  --color-ink-inverse: #FFFFFF; /* text on --color-primary-strong */

  /* Lines */
  --color-border:        #E8E6D5; /* cards, dividers */
  --color-border-strong: #D5D2BD; /* inputs */
  --color-focus:         #0D9488; /* Teal 600. See Deviations */
  --color-focus-halo:    rgba(13,148,136,.25); /* input focus outline */

  /* Feedback */
  --color-success: #2DD4BF;
  --color-warning: #EAB308;
  --color-error:   #EF4444;
  --color-danger-ink: #9A231B; /* destructive text (.btn.danger, form errors). See Deviations */

  /* Top bar */
  --nav-bg:    var(--color-ink);
  --nav-ink:   #FFFFFF;
  --nav-ink-2: rgba(255,255,255,.78); /* resting nav items */
  --nav-hover: rgba(255,255,255,.10); /* translucent-white hover */
  --nav-focus: #2DD4BF; /* focus ring on the top bar. See Deviations */

  /* Goal Health. See Deviations */
  --health-g-bg: #DDEFE3; --health-g-ink: #17593A;
  --health-y-bg: #FAEBC4; --health-y-ink: #6B4800;
  --health-r-bg: #F9DEDB; --health-r-ink: #9A231B;

  /* Type */
  --font-display: "Plus Jakarta Sans", system-ui, sans-serif;
  --font-body:    "Inter", system-ui, sans-serif;
  --font-mono:    "JetBrains Mono", ui-monospace, monospace;

  /* Shape */
  --radius-sm: 4px;    /* tags, code */
  --radius-md: 8px;    /* buttons, inputs, selects */
  --radius-lg: 12px;   /* cards, dialogs */
  --radius-full: 9999px; /* pills, avatars, counts */

  /* Elevation (teal-tinted) */
  --shadow-card:       0 10px 25px -5px rgba(19,78,74,.09), 0 8px 10px -6px rgba(19,78,74,.04);
  --shadow-card-hover: 0 20px 30px -10px rgba(19,78,74,.17), 0 10px 15px -5px rgba(19,78,74,.08);
  --shadow-pop:        0 25px 50px -12px rgba(19,78,74,.25);
  --shadow-glow:       0 4px 14px 0 rgba(20,184,166,.38); /* primary button hover */

  /* Motion */
  --ease-out: cubic-bezier(0.16, 1, 0.3, 1);
  --dur-fast: 150ms;
  --dur-base: 200ms;
}

/* Dark: follows the OS unless the page pins a theme. */
@media (prefers-color-scheme: dark) {
  :root:not([data-theme="light"]) { /* same block as [data-theme="dark"] below */ }
}
:root[data-theme="dark"] {
  --color-primary:        #14B8A6; /* unchanged */
  --color-primary-hover:  #2DD4BF; /* dark hovers go lighter */
  --color-primary-strong: #14B8A6; /* filled buttons: teal with dark text */
  --color-primary-light:  #063530; /* scale step 1 */
  --color-primary-subtle: #031613; /* the preview's tile colour */

  --color-canvas:        #020E0C; /* scale step 0 (preview canvas) */
  --color-surface:       #052E29; /* preview card */
  --color-surface-hover: #063530; /* scale step 1 */
  --color-surface-alt:   #031613; /* preview tile */

  --color-ink:         #E8E8E8; /* preview text */
  --color-ink-2:       #B8CFCC; /* (chosen) */
  --color-ink-muted:   #8FAFAB; /* (chosen) */
  --color-ink-inverse: #042F2E; /* (chosen) Teal 950 on teal */

  --color-border:        #0A3F39; /* (chosen) */
  --color-border-strong: #0A5C53; /* scale step 2 */
  --color-focus:         #2DD4BF;
  --color-focus-halo:    rgba(45,212,191,.30); /* (chosen) */

  --color-success: #2DD4BF;
  --color-warning: #EAB308;
  --color-error:   #F87171; /* (chosen) Red 400 reads better on dark */
  --color-danger-ink: #F87171; /* (chosen) same as --color-error */

  --nav-bg:    var(--color-surface-alt);
  --nav-ink:   var(--color-ink);
  --nav-ink-2: var(--color-ink-2);
  --nav-hover: rgba(255,255,255,.06); /* (chosen) */
  --nav-focus: var(--color-focus);

  /* (chosen) Health hues on 20%-alpha fills with light text */
  --health-g-bg: rgba(34,160,95,.20); --health-g-ink: #8EDDAF;
  --health-y-bg: rgba(234,179,8,.20); --health-y-ink: #F5D06B;
  --health-r-bg: rgba(239,68,68,.20); --health-r-ink: #F8A39B;

  --shadow-card:       0 10px 25px -5px rgba(0,0,0,.45), 0 8px 10px -6px rgba(0,0,0,.30);
  --shadow-card-hover: 0 20px 30px -10px rgba(0,0,0,.55), 0 10px 15px -5px rgba(0,0,0,.35);
  --shadow-pop:        0 25px 50px -12px rgba(0,0,0,.65);
  --shadow-glow:       0 4px 14px 0 rgba(20,184,166,.30);
}
```

In the stylesheet, the empty dark `@media` block above repeats the full
`[data-theme="dark"]` declarations.

## Type scale

| Role | Face | Size / line-height | Weight | Tracking |
| --- | --- | --- | --- | --- |
| Display | Plus Jakarta Sans | 56 / 1.1 | 700 | -0.03em |
| H1 (page title) | Plus Jakarta Sans | 36 / 1.2 | 700 | -0.02em |
| H2 (section) | Plus Jakarta Sans | 26 / 1.3 | 600 | -0.01em |
| H3 (card title) | Plus Jakarta Sans | 19 / 1.4 | 600 | — |
| Body | Inter | 15 / 1.6 | 400 | — |
| Body small | Inter | 13 / 1.5 | 400 | — |
| Micro / badge / `.label` | Inter | 11 | 500, uppercase | — |
| Numbers (`.num`) | JetBrains Mono | inherit | 500 | — |

Load the fonts with this URL:
`https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600&family=JetBrains+Mono:wght@500&family=Plus+Jakarta+Sans:wght@600;700&display=swap`

## Components (mapped to `internal/web/static/app.css`)

- **Top bar (`.nav`):** `--nav-bg`, which is `--color-ink` in light mode and
  `--color-surface-alt` in dark mode, with `--nav-ink` text. Resting items use
  `--nav-ink-2`; hover is the translucent-white `--nav-hover`.
  The current item (`.on`) gets a `--color-primary` underline or pill.
- **Primary button (`.btn.primary`):**
  - Resting: `--color-primary-strong` fill, `--color-ink-inverse` text,
    `--radius-md`, 14px, weight 600.
  - Hover: `translateY(-1px)` and `--shadow-glow`.
  - Active: `scale(0.98)`.
- **Secondary button (`.btn`):** `--color-surface` fill, 1px `--color-border`,
  `--color-ink` text. On hover, the fill becomes `--color-surface-hover` and
  the border becomes `--color-primary`.
- **Quiet button (`.btn.quiet`):** no fill, `--color-primary-strong` text. On
  hover it takes a `--color-primary-subtle` fill.
- **Card (`.card`):** `--color-surface`, 1px `--color-border`,
  `--radius-lg`, 24px padding, `--shadow-card`. Clickable cards lift on hover
  with `--shadow-card-hover` and `translateY(-2px)`, over `--dur-base`.
- **Inputs:** `--color-surface`, 1px `--color-border-strong`, `--radius-md`,
  12px × 16px padding. On focus, the border becomes `--color-focus` with an
  outline of `2px solid var(--color-focus-halo)`.
- **Tables:** headers on `--color-surface-alt` with `.label` type. Rows are
  divided by `--color-border`. Selected rows use `--color-primary-light`.
- **Tags and counts:** `--radius-full`, with a `--color-primary-light` fill
  and `--color-ink` text.
- **Health badges (`.g` / `.y` / `.r`):** `--health-*-bg` fill with
  `--health-*-ink` text; see Deviations. They keep their dot
  shapes (circle, triangle, square) so color is never the only signal.

## Motion

- Transitions run 150–200ms on `--ease-out`. Every click and hover gets
  immediate feedback.
- Nothing loops. Under `prefers-reduced-motion`, drop the transforms and keep
  opacity changes of 150ms or less.

## Deviations from the source

1. **Filled-button and link colour.** The source puts white text on `#14B8A6`,
   which has a contrast ratio of 2.49:1 and fails WCAG AA. Light mode adds
   `--color-primary-strong: #0F766E` for text-bearing fills and links, which
   reaches 5.47:1. `#14B8A6` stays in use for borders and accents. In
   dark mode, buttons keep `#14B8A6` with `#042F2E` text, which reaches 5.81:1.
2. **Muted ink.** The source's `#688E8B` reaches 3.6:1 on white, which is too
   low for the 12–13px labels this app uses heavily. It is darkened to
   `#527673` (5.0:1 on white, 4.84:1 on the canvas).
3. **Goal Health.** The source's success teal (`#2DD4BF`) is almost the same
   as the primary colour, so a Green Health badge would read as a button.
   Health badges therefore keep their own hues, adjusted to the warm canvas:
   - Green: `#DDEFE3` / `#17593A`
   - Yellow: `#FAEBC4` / `#6B4800`
   - Red: `#F9DEDB` / `#9A231B`

   In dark mode, the same hues sit on 20%-alpha fills with light text
   (`--health-*` in the dark block). The `--color-success`/`warning`/`error`
   tokens are for alerts and form validation, not for Health.
4. **Danger text.** `--color-error` (`#EF4444`) reaches only 3.76:1 on white,
   too low for 13–15px text. Destructive buttons and inline errors use
   `--color-danger-ink` (`#9A231B`, the Red Health ink) in light mode instead;
   `--color-error` stays for borders and icons.
5. **Focus ring.** The source rings focus in `#14B8A6`, which reaches only
   2.41:1 on the canvas, below WCAG's 3:1 for focus indicators. Light mode
   rings focus in `--color-focus: #0D9488` instead (3.62:1 on the canvas,
   3.74:1 on white, 3.44:1 on `--color-surface-alt`). On the dark-teal top
   bar that drops to 2.53:1, so nav items ring in `--nav-focus: #2DD4BF`
   (5.09:1). Dark mode keeps `#2DD4BF` everywhere.
