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

- **Mood:** warm, editorial and calm. Ivory canvas, neutral ink, and teal only
  for what a person can act on: links, buttons, focus and the current top-bar
  item. No neon, and no cold white on the canvas: the warmth lives
  there. Cards sit on it in plain white (see § Accepted audit findings).
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
  --color-ink:         #18211F; /* headings, primary labels. See Deviations */
  --color-ink-2:       #3E4946; /* body text. See Deviations */
  --color-ink-muted:   #66706D; /* placeholders, timestamps. See Deviations */
  --color-ink-inverse: #FFFFFF; /* text on --color-primary-strong */

  /* Lines */
  --color-border:        #E8E6D5; /* cards, dividers */
  --color-border-strong: #7B908C; /* inputs. See Deviations */
  --color-focus:         #0D9488; /* Teal 600. See Deviations */

  /* Feedback */
  --color-success: #2DD4BF;
  --color-warning: #EAB308;
  --color-error:   #EF4444;
  --color-danger-ink: #9A231B; /* destructive text (.btn.danger, form errors). See Deviations */

  /* Top bar */
  --nav-bg:    #134E4A; /* Teal 900, its own value now --color-ink is neutral */
  --nav-ink:   #FFFFFF;
  --nav-ink-2: rgba(255,255,255,.78); /* resting nav items */
  --nav-hover: rgba(255,255,255,.10); /* translucent-white hover */
  --nav-focus: #2DD4BF; /* focus ring on the top bar. See Deviations */
  --nav-count-bg:  #E3E6E5; /* neutral grey count pill. See Deviations */
  --nav-count-ink: #18211F;
  --nav-needs-bg:  #18211F; /* Home's count, both themes: a dark pill ringed in --nav-ink */

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

  /* Primary button hover fill. Light darkens to Teal 800: white text on it is
   * 7.58:1, where --color-primary-hover under white text is only 3.74:1 */
  --btn-primary-hover: #115E59;

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
  --color-ink-2:       #C2C2C2; /* (chosen) neutral grey. See Deviations */
  --color-ink-muted:   #9E9E9E; /* (chosen) neutral grey. See Deviations */
  --color-ink-inverse: #042F2E; /* (chosen) Teal 950 on teal */

  --color-border:        #0A3F39; /* (chosen) */
  --color-border-strong: #2F8479; /* (chosen) 3.3:1 on the surface */
  --color-focus:         #2DD4BF;

  --color-success: #2DD4BF;
  --color-warning: #EAB308;
  --color-error:   #F87171; /* (chosen) Red 400 reads better on dark */
  --color-danger-ink: #F87171; /* (chosen) same as --color-error */

  --nav-bg:    var(--color-surface-alt);
  --nav-ink:   var(--color-ink);
  --nav-ink-2: var(--color-ink-2);
  --nav-hover: rgba(255,255,255,.06); /* (chosen) */
  --nav-focus: var(--color-focus);
  --nav-count-bg:  #626766; /* (chosen) neutral grey count pill */
  --nav-count-ink: #FFFFFF;

  /* (chosen) Health hues on 20%-alpha fills with light text */
  --health-g-bg: rgba(34,160,95,.20); --health-g-ink: #8EDDAF;
  --health-y-bg: rgba(234,179,8,.20); --health-y-ink: #F5D06B;
  --health-r-bg: rgba(239,68,68,.20); --health-r-ink: #F8A39B;

  --shadow-card:       0 10px 25px -5px rgba(0,0,0,.45), 0 8px 10px -6px rgba(0,0,0,.30);
  --shadow-card-hover: 0 20px 30px -10px rgba(0,0,0,.55), 0 10px 15px -5px rgba(0,0,0,.35);
  --shadow-pop:        0 25px 50px -12px rgba(0,0,0,.65);

  /* Dark hover goes lighter: --color-ink-inverse on it is 7.77:1 */
  --btn-primary-hover: var(--color-primary-hover);
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

- **Top bar (`.nav`):** `--nav-bg`, which is deep teal `#134E4A` in light
  mode and `--color-surface-alt` in dark mode, with `--nav-ink` text. The light
  value is the top bar's own, not `--color-ink`, so the bar stays teal while the
  ink is neutral. Resting items use `--nav-ink-2`; hover is the
  translucent-white `--nav-hover`.
  The current item (`.on`) gets a `--color-primary` underline or pill.
  Above 900px it is one 56px row. At 900px and below it takes two rows and
  never scrolls sideways. The brand, the person and Sign out are on the first
  row and the nav items (`.navitems`) are on the second, each label on one line.
  The person shows by Name alone, and a long Name truncates with an ellipsis.
  "Signed in as" and "(Admin)" are hidden visually with `font-size:0`, so screen
  readers still read them. At 360px and below the nav items' padding tightens
  so an Admin's four items fit at 320px with two-digit counts, and so do the
  gaps and padding beside Sign out, so the Theme menu fits beside it.
- **Theme menu (`.theme`):** a quiet "Theme" disclosure in the top bar beside
  Sign out, on every page including sign-in. Opened, it lists three choices in
  words: **System** (the default: follow the operating system), **Light** and
  **Dark**. The current choice is marked with a `--color-primary-light` fill
  and a check, and `aria-current="true"`. It is a `<details>` and each choice is
  a plain form post, so it works without script. Its panel
  (`.theme-menu`) is a card that floats under the button, anchored right and
  no wider than the screen, so it never widens the top bar.
  The choice is kept per browser in the `gt_theme` cookie, not on the Account,
  so it holds before sign-in and after sign-out. It lasts a year and each
  choice renews it. Choosing System clears it. The server pins
  `data-theme="light"` or `data-theme="dark"` on `<html>` from the cookie, so
  the page has the right theme on first paint with no script. With no cookie,
  or a value it doesn't know, it pins nothing and the page follows the system.
  A Report's Print view and emails ignore the choice.
- **Primary button (`.btn.primary`):**
  - Resting: `--color-primary-strong` fill, `--color-ink-inverse` text,
    `--radius-md`, 14px, weight 600.
  - Hover: the fill changes to `--btn-primary-hover`, and nothing else does:
    no lift, no shadow. The border follows the fill, so the button still
    reads as one surface.
    - Light: the fill darkens to Teal 800 (`#115E59`), and white text on it is
      7.58:1. Light `--color-primary-hover` is only 3.74:1 under white text,
      so it can't be the fill.
    - Dark: the fill lightens to `--color-primary-hover` (`#2DD4BF`), because
      dark themes show elevation by lightness. `--color-ink-inverse` text on
      it is 7.77:1.
  - Active: `scale(0.98)`.
- **Secondary button (`.btn`):** `--color-surface` fill, 1px `--color-border`,
  `--color-ink` text. On hover, the fill becomes `--color-surface-hover` and
  the border becomes `--color-primary`.
- **Quiet button (`.btn.quiet`):** no fill, `--color-primary-strong` text. On
  hover it takes a `--color-primary-subtle` fill.
- **Card (`.card`):** `--color-surface`, 1px `--color-border`,
  `--radius-lg`, 24px padding, `--shadow-card`. On hover a clickable card's
  shadow deepens to `--shadow-card-hover` over `--dur-base`, and nothing else
  changes: it doesn't move.
  A card's own heading is an `h3` (card title); `h2` is for page sections
  that sit outside cards.
  A card is for something a person acts on or reads as a unit of status,
  such as the Goal page's Check-in, each Metric and Milestones. Reference
  blocks are not cards. On the Goal page these are Highlights, History and
  the sidebar's blocks.
- **Canvas block (`.ruled`):** a reference block sits on the canvas with no
  surface, border or shadow. A 1px `--color-border` rule sits above it with
  20px of padding below the rule. The first block in a column has no rule,
  because nothing sits above it. A canvas block in the main column takes an
  `h2`. **Sidebar exception:** a canvas block in the sidebar keeps the `h3`
  size, so the narrow column reads as one quiet list rather than a stack of
  sections. At 900px and below, the sidebar stacks under the main column, and
  its first block takes the rule too. The Goal's So What is part of the page
  head (below). It sits on the canvas with no rule.
- **Goal page head (`.gp-head`):** read top to bottom, it is the title (H1),
  then the So What, then one metadata line. The warning banners (Ownerless,
  Stale, Path to Green overdue, Incomplete) stay above it, and the actions sit
  top right.
  - **So What lead (`.gp-lead`):** larger body text, 18 / 1.55 in
    `--color-ink`, at most 720px wide. No "So What" heading shows; a visually
    hidden one (`.sr-only`) labels it for assistive technology, and a small
    "What is a So What?" help button under it opens the definition in place.
  - **Metadata line (`.gp-meta`):** the Health badge leads, then plain
    `--color-ink-muted` text separated by `·`: Lifecycle, Dated or Ongoing,
    Owner (the usual person control), the delivery date with any slipped dates
    struck through when Dated, the Check-in cadence, and Top-level when it
    applies. Only Health is a badge. The line wraps on narrow screens.
    Dimension values and Fields stay in the sidebar, never the head.
- **Status cells (`.gp-cells`):** the Goal page's Latest status card opens with
  a row of cells, each a `.label` over its value: "Owner's Health" (a Health
  badge), "Rolled-up Health" (a Health badge, with the Stale-children count
  beside it) and "Back to Green by" (a date). A cell shows only when it has
  something to say: no Rolled-up cell without Active children and no Back to
  Green cell without a Path to Green. The cells wrap, 32px apart. The status
  text, the Path to Green and the "why this differs" explanation follow below.
  A Goal with no Health shows its message in place of the cells.
- **Inputs:** `--color-surface`, 1px `--color-border-strong`, `--radius-md`,
  12px × 16px padding. On focus, the border becomes `--color-focus` and the
  field takes the same ring as everything else, `2px solid var(--color-focus)`,
  offset 1px. The ring shows on mouse focus too and never transitions. See
  Deviations.
- **Sizes and touch targets:** buttons and text, email, date, number and search
  inputs and selects are 40px tall, and `.btn.sm` is 32px. On a coarse pointer
  (`@media (pointer:coarse)`) all of them get a 44px `min-height`, so every
  touch target is at least 44px and a button beside an input still shares its
  height. A small button keeps its smaller type and padding. Textareas,
  checkboxes and radios are left alone.
- **Tables:** headers on `--color-surface-alt` with `.label` type. Rows are
  divided by `--color-border`. Selected rows use `--color-primary-light`.
- **Tags:** `--radius-full`, with a `--color-primary-light` fill and
  `--color-ink` text.
- **Nav counts (`.count`):** a `--radius-full` pill after a top-bar item's
  label, shown only above zero. Two styles, so the one that needs the person
  stands out:
  - **Home's count (`.count.needs-you`)** counts what needs the person, so it is
    a dark pill: `--nav-needs-bg` (`#18211F`) with `--nav-ink` numbers, ringed
    1px in `--nav-ink` so it shows against the top bar.
  - **Every other count (`.count`)**, such as Risks, is a neutral grey pill:
    `--nav-count-bg` with `--nav-count-ink` numbers.

  The ratios are in § Deviations 2.
- **Health badges (`.g` / `.y` / `.r`):** `--health-*-bg` fill with
  `--health-*-ink` text; see Deviations. They keep their dot
  shapes (circle, triangle, square) so color is never the only signal.

## Motion

- Transitions run 150–200ms on `--ease-out`. Every click and hover gets
  immediate feedback.
- Nothing loops. Under `prefers-reduced-motion`, drop the transforms and keep
  opacity changes of 150ms or less.

## Print variant

The Report Print view (`publicationPrintPage` in `internal/web/reports.templ`)
is a document for paper and PDF, so it doesn't use the screen system. It is
black and white, set in a serif, and declares its own tokens in its own style
block. It doesn't load `app.css`, and it ignores the Theme menu's choice: it
stays black on white whatever is chosen.

```css
:root {
  --print-font:  'Source Serif 4', Georgia, serif; /* weights 400 and 600 */
  --print-ink:   #000; /* text and heavy rules; true black prints cleanest */
  --print-ink-2: #444; /* bylines, meta lines, small-caps labels */
  --print-rule:  #999; /* hairlines between Goals and table rows */
}
```

| Role | Size / line-height | Weight |
| --- | --- | --- |
| Report name (`h1`) | 24pt / 1.15 | 600 |
| Section (`h2`) | 13pt, under a 1.5pt ink rule | 600 |
| Goal title (`h3`) | 13pt / 1.25 | 600 |
| Body | 11pt / 1.45 | 400 |
| Meta (`.small`) and tables | 9.5pt and 10pt | 400 |
| Labels and table heads | small caps, 0.05em tracking | 400 and 600 |

- Sizes are in points and spacing in rem, because the page is measured in
  paper units. The 4px grid doesn't apply.
- Page margins are 18mm top and bottom and 16mm at the sides.
- A Goal's Health sits to the right of its title. Health shows its shape (■ Red,
  ▲ Yellow, ● Green) and its name, never colour.
- A Goal block and a table row never split across pages.
- Load the font with this URL:
  `https://fonts.googleapis.com/css2?family=Source+Serif+4:ital,wght@0,400;0,600;1,400&display=swap`

## Deviations from the source

1. **Filled-button and link colour.** The source puts white text on `#14B8A6`,
   which has a contrast ratio of 2.49:1 and fails WCAG AA. Light mode adds
   `--color-primary-strong: #0F766E` for text-bearing fills and links, which
   reaches 5.47:1. `#14B8A6` stays in use for borders and accents. In
   dark mode, buttons keep `#14B8A6` with `#042F2E` text, which reaches 5.81:1.
2. **Neutral ink.** The source's ink is teal: `#134E4A`, `#2D6A66` and a muted
   `#688E8B`, which reaches only 3.6:1 on white. With text, links and buttons
   all teal, nothing separated what a person can act on from what they only
   read (UX Review, mocks 1b and 1d). Text is neutral in both themes instead,
   and teal is kept for links, buttons, focus and the current top-bar item.
   The top bar keeps its teal through its own `--nav-bg`.

   Each ink's contrast on every surface text sits on (`--color-primary-light`
   is the selected-row and tag fill):

   | Theme | Ink | Canvas | Surface | Surface hover | Surface alt | Primary light |
   | --- | --- | --- | --- | --- | --- | --- |
   | Light | `--color-ink` `#18211F` | 15.91 | 16.46 | 16.02 | 15.13 | 14.60 |
   | Light | `--color-ink-2` `#3E4946` | 9.04 | 9.35 | 9.10 | 8.59 | 8.30 |
   | Light | `--color-ink-muted` `#66706D` | 4.95 | 5.12 | 4.98 | 4.70 | 4.54 |
   | Dark | `--color-ink` `#E8E8E8` | 16.01 | 11.99 | 10.99 | 15.19 | 10.99 |
   | Dark | `--color-ink-2` `#C2C2C2` | 11.01 | 8.25 | 7.56 | 10.45 | 7.56 |
   | Dark | `--color-ink-muted` `#9E9E9E` | 7.32 | 5.48 | 5.03 | 6.95 | 5.03 |

   The nav counts' numbers reach 4.5:1 on their pills, and each pill's edge
   reaches 3:1 against the top bar so the shape shows:

   | Theme | Count | Number on pill | Edge on top bar |
   | --- | --- | --- | --- |
   | Light | Home, `#FFFFFF` on `#18211F` | 16.46 | 9.48 (`#FFFFFF` ring on `#134E4A`) |
   | Light | Other, `#18211F` on `#E3E6E5` | 13.10 | 7.54 (fill on `#134E4A`) |
   | Dark | Home, `#E8E8E8` on `#18211F` | 13.43 | 15.19 (`#E8E8E8` ring on `#031613`) |
   | Dark | Other, `#FFFFFF` on `#626766` | 5.75 | 3.24 (fill on `#031613`) |
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

   The source also rings a focused input in a 25%-alpha halo of the primary,
   which reaches only 1.36:1 on white. Inputs take the solid ring instead, and
   the halo token is dropped.

6. **Input borders and other non-text marks.** The source's input border
   (`#D5D2BD`) reaches 1.52:1 on white, and the dark preview's scale step 2
   (`#0A5C53`) reaches 1.87:1 on the dark surface. WCAG asks 3:1 of the edge
   that identifies a control. `--color-border-strong` is `#7B908C` in light
   mode (3.38:1 on white, 3.27:1 on the canvas) and `#2F8479` in dark mode
   (3.29:1 on the surface). Two more marks follow the same rule:
   - A Metric's sparkline strokes in `--color-primary-strong`, not
     `--color-primary`, which reaches only 2.49:1 on white.
   - On the Check-in form, an unselected Health choice is an outline (surface
     fill, `--color-border-strong` edge, Health ink) and the selected one is
     the filled Health badge with an edge in its ink. Nothing is dimmed.

## Accepted audit findings

Places where the app follows the source on purpose, though a design audit has
flagged them. They aren't deviations, and later audits shouldn't raise them
again.

1. **White card surface.** The 2026-10-01 audit called the light theme's
   `--color-surface` (`#FFFFFF`) flat beside the ivory canvas. It stays white.
   The light-theme contrast ratios in § Deviations are quoted against it, and
   two of them have little room to lose: the input border at 3.38:1 and the
   focus ring at 3.74:1, against WCAG's 3:1. Tinting the surface would mean
   recomputing every one.
2. **Hex and rgba tokens.** The same audit asked for tokens in OKLCH. They
   stay in hex and rgba, the source's notation, so they can be checked against
   it line by line. Converting them would change nothing a reader sees.
