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

- **Mood:** warm, editorial and calm. A neutral off-white canvas, neutral ink,
  and teal only for what a person can act on: links, buttons, focus and the
  current top-bar item. The one data accent is a Metric's sparkline, stroked in
  teal (see § Deviations 6). No neon, and no tint on the canvas, so Health's
  fills stand out from the page (see § Deviations 7). Cards sit on the canvas
  in plain white (see § Accepted audit findings).
- **Density:** generous. Cards use 24px padding, sections sit 20–32px apart,
  and everything aligns to a 4px grid.
- **Depth:** cards lie flat on the canvas, set off by their surface and a
  1px border. Only menus and popovers float, on `--shadow-pop`, so an open
  one reads as nearer than anything under it (see § Deviations 8).

## Tokens

```css
:root {
  /* Brand */
  --color-primary:        #14B8A6; /* Teal 500: active pills, accents */
  --color-primary-hover:  #0D9488; /* Teal 600 */
  --color-primary-strong: #0F766E; /* Teal 700: filled buttons, links. See Deviations */
  --color-primary-light:  #CCFBF1; /* Teal 100: selected rows, current choices */
  --color-primary-subtle: #F0FDFA; /* Teal 50: faint tints */

  /* Canvas & surfaces */
  --color-canvas:        #F6F6F3; /* neutral off-white body background. See Deviations */
  --color-surface:       #FFFFFF; /* cards, raised containers */
  --color-surface-hover: #FCFCFA; /* See Deviations */
  --color-surface-alt:   #F2F2EE; /* secondary panels, table headers. See Deviations */

  /* Ink */
  --color-ink:         #18211F; /* headings, primary labels. See Deviations */
  --color-ink-2:       #3E4946; /* body text. See Deviations */
  --color-ink-muted:   #66706D; /* placeholders, timestamps. See Deviations */
  --color-ink-inverse: #FFFFFF; /* text on --color-primary-strong */

  /* Lines */
  --color-border:        #E1E1DC; /* cards, dividers. See Deviations */
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

  /* Stale and Path to Green overdue: chip and banner. See Components */
  --stale-bg:   #E9EAEC; /* chip fill */
  --stale-ink:  #3B4048; /* chip text */
  --stale-edge: #888F97; /* dashed edge of chip and banner. See Deviations */

  /* Goal Health. See Deviations */
  --health-g-bg: #DDEFE3; --health-g-ink: #17593A;
  --health-y-bg: #FAEBC4; --health-y-ink: #6B4800;
  --health-r-bg: #F9DEDB; --health-r-ink: #9A231B;

  /* Type */
  --font-display: "Plus Jakarta Sans", system-ui, sans-serif;
  --font-body:    "Inter", system-ui, sans-serif;
  --font-mono:    "JetBrains Mono", ui-monospace, monospace;

  /* Shape */
  --radius-sm: 4px;    /* badges, Health strip cells */
  --radius-md: 8px;    /* buttons, inputs, selects */
  --radius-lg: 12px;   /* cards, dialogs */
  --radius-full: 9999px; /* pills, avatars, counts */

  /* Elevation (teal-tinted): menus and popovers only. Cards are flat.
   * See Deviations */
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

  /* (chosen) Stale: the light chip's ink becomes the fill, and the UX
   * Review's grey the edge */
  --stale-bg:   #3B4048;
  --stale-ink:  #E8E8E8;
  --stale-edge: #8C939C;

  /* (chosen) Health hues on 20%-alpha fills with light text */
  --health-g-bg: rgba(34,160,95,.20); --health-g-ink: #8EDDAF;
  --health-y-bg: rgba(234,179,8,.20); --health-y-ink: #F5D06B;
  --health-r-bg: rgba(239,68,68,.20); --health-r-ink: #F8A39B;

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
- **Toast (`.toast`):** the one shared notice for an action that can be
  undone. It says what was done, in `--color-ink` on `--color-surface`, with a
  secondary **Undo** button beside it, inside a 1px `--color-border-strong`
  edge, `--radius-lg` and `--shadow-pop`. It is fixed 24px above the bottom of
  the window, centred, over the page content (`z-index` 30), and the page under
  it gains bottom padding so nothing ends up hidden behind it. At 600px and
  below it spans the window less 16px each side and its message wraps.
  - **Once, with no script.** The server renders it only on the page shown
    straight after the action. The post that made the change sets the
    `gt_undo` cookie, scoped to that page's path, and the page clears it as it
    reads it, so a reload or a later visit shows nothing. There is no timer:
    it stays until the person leaves.
  - **Undo is a plain form post** to the action's own reversal, checked again
    on the server, so a forged or stale Undo is refused with a message. The
    action records a random one-time token, which the `gt_undo` cookie carries
    to the toast and the toast posts as a hidden `undo` field.
  - **An Undo lasts 15 minutes and works once.** It is refused with 403 when
    its token is missing, or is for another action or person; with 422,
    saying the Undo is no longer available, when its token is more than 15
    minutes old or was used before. Posting the token uses it up, even when the
    Undo is then refused. It is also refused, with a message saying why, when
    something has happened since: the same link was requested, accepted,
    removed or rejected again; the Goal has had a later Handoff; or the retired
    value is no longer Retired under the name it was retired with. Anything
    done before tokens existed has none, so it can't be undone. A Retired
    value's own Restore button on the Dimensions page is not an Undo: it takes
    no token and works at any time.
  - **A refused Undo shows a page, not bare text.** It keeps its status and
    renders a "Can't undo" page inside the normal chrome, with one plain
    sentence saying why (no internal prefixes or IDs) and a **Back** link to
    the page the Undo was offered on: the Goal page for a link removal (Home
    when it names none), Home or the pending page for a rejection, and
    Dimensions for a retired value. The page is `undoRefusedPage` in
    `toast.templ`.
  - **Announced politely:** an `<aside role="status">`, so a screen reader
    reads it without moving focus.
  - **Where:** removing a link (Goal page), retiring a Dimension value
    (Dimensions page), and rejecting a link request or a Handoff (Home, or the
    request's pending page, whichever the Reject came from). The templ component is `toastNotice` in `layout.templ`;
    a new Undo reuses it rather than making its own notice.
- **Not found page:** like a refused Undo, a missing page shows a page, not
  bare text. Any address that names nothing (an unknown URL, or a Goal,
  Report, publication or other thing that doesn't exist or has been removed)
  answers 404 with a "Not found" page inside the normal chrome: one plain
  sentence, "There's nothing here. It may have been removed, or the link may
  be wrong.", and a way back: **Back to Home** when signed in, **Sign in**
  when not. The page is `notFoundPage` in `notfound.templ`, written by
  `s.notFound`; a handler that 404s calls it rather than `http.NotFound`.
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
  `--radius-lg`, 24px padding, and no shadow in either theme. On hover a
  clickable card's border becomes `--color-border-strong` over `--dur-base`,
  and nothing else changes: it doesn't move. That is darker in light and
  lighter in dark, so either way the edge stands further out from the card.
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
  - **Header actions (`.gp-actions`):** top right, for whoever may check in
    (the Owner or a Delegate), a primary **Check in** link to the Goal's
    Check-in page and a quiet **No change** (`.btn.quiet`). Beside them sits
    the action menu. Anyone else sees only the menu.
  - **Action menu (`.gp-more`):** a secondary button reading "⋯", named "More
    actions" for assistive technology. It is a `<details>`, so it opens without
    script, and its panel (`.gp-menu`) is a card anchored right under the
    button. It lists actions, never forms: each item is a plain link, and each
    person sees only the actions they may take. When there would be none, the
    menu is left out.
- **Forms that open in place (`.gp-open`):** a Goal page action's form opens
  in the part of the page it concerns, never in a modal, side panel or the
  menu. A menu item or sidebar link leads to `/goals/{id}?open=<form>`, which
  is the Goal page with that one form open and its first control focused. The
  form sits under a `.label` heading with a quiet **Cancel** link back to the
  plain page. A refused submit comes back as the same page with the same form
  open, the reason in an alert at its top and what was typed still in it. It
  is all links and plain form posts, so it works without script.
  - **Where each opens:** Top-level opens in the head, under the metadata line.
    Hand off, Reassign, Mark owner departed and Mark returned open in People,
    under the Owner. Delegates and Contributors forms open in their groups.
    Link to a parent Goal opens under Contributes to, Add a child Goal under
    Contributed to by, and the Dimension and Field forms in their blocks.
  - **Sidebar links (`.gp-group-head`):** a sidebar group whose form opens in
    place has a small link beside its heading, shown only to those who may use
    it and hidden while its form is open: **Manage** for Delegates and for
    Contributors, **Edit** for Dimensions and for Fields, **+ Add** for
    Contributes to. Each link's accessible name says what it acts on ("Edit
    Fields"). The sidebar blocks stay separate canvas blocks under rules.
- **Status cells (`.gp-cells`):** the Goal page's Latest status card opens with
  a row of cells, each a `.label` over its value: "Owner's Health" (a Health
  badge), "Rolled-up Health" (a Health badge, with the Stale-children count
  beside it) and "Back to Green by" (a date). A cell shows only when it has
  something to say: no Rolled-up cell without Active children and no Back to
  Green cell without a Path to Green. The cells wrap, 32px apart. The status
  text, the Path to Green and the "why this differs" explanation follow below.
  A Goal with no Health shows its message in place of the cells.
- **History timeline (`goalHistory` in `timeline.templ`):** the Goal page's
  History is one canvas block, open on load with no outer disclosure. It is
  a single list, newest first, of every Check-in, So What revision, ownership
  change (a Handoff with its outcome, or an Admin Reassign) and change to a
  Dimension value or Field.
  - **Weeks:** entries are grouped by week, Monday to Sunday in the org's
    timezone. Each week is headed by a `.label` "Week of 28 Sep", with the
    year added when it isn't this year. Each entry's time is written in that
    timezone too ("Thu 8 Jan 15:04").
  - **The rule and markers (`.tl-list`, `.tl-mark`):** the week's entries hang
    off a 1px `--color-border` rule, each with a 12px marker on the rule.
    A Check-in's marker is its Health's dot, in the Health's ink and shape:
    ● Green, ▲ Yellow, ■ Red. It reads without colour, and its badge names the
    Health beside it. Every other kind has a neutral `--color-ink-muted`
    marker with its own shape: a hollow ring for So What, a hollow diamond for
    an ownership change, a bar for a value change, and a hollow square for a
    Check-in with no Health, such as one that put the Goal On Hold. Every
    entry also opens with its kind as a bold text label (Check-in, So What,
    Handoff, Reassign, Value), so the marker is never the only signal.
  - **A Check-in entry** shows its Health badge and status, then any
    Lifecycle change, Path to Green, and the Date Slips it recorded (the
    delivery date or a Milestone, ~~old~~ new, and the reason). Last comes who
    wrote it, by Name, "for" the Owner when a Delegate wrote it. Its Metric
    readings and its explanation of a Health that differs from the Rolled-up
    Health sit in a small "Readings and explanation" disclosure on the entry.
  - **Health strip (`healthStrip`, `.hs`):** under the History heading, above
    the filter chips, the Goal's Health over its last 11 Check-in periods, one
    cell for each, oldest on the left and the current period on the right. A
    period is the Goal's cadence long (a week by default), counted in days of
    the org's timezone as Stale is, and the current one ends on the Sunday
    that closes this week, so a weekly Goal's cells are the weeks History is
    grouped by. How a period ended decides its cell: the last Check-in made in
    it. If that Check-in left the Goal Active, the cell takes its Health: the
    Health's `--health-*-bg` fill with its dot in the Health's ink and shape
    (● Green, ▲ Yellow, ■ Red), so it reads without colour. A finished period
    the Goal was Active with no Check-in is an empty cell in a 1px dashed
    `--stale-edge` border, the edge Stale uses, so a skipped week shows at a
    glance. Only a finished period can be missed: one whose last day is today
    or later (the one in progress, and any wholly ahead when the cadence is
    shorter than a week) that the Goal is Active in with no Check-in yet is
    "not yet due", an empty cell in a 1px solid `--color-border-strong` border,
    the edge of an input, a slot waiting to be filled. A period before the Goal
    became Active, or one it spent or ended On Hold, Done or Cancelled, owed no
    Check-in and is blank, with no edge. Cells are 24px tall, share the row 4px
    apart up to 480px wide, and take `--radius-sm`. Each cell's text equivalent
    is visually hidden inside it and repeated as its tooltip: "Week of 28 Sep:
    Yellow", "Week of 21 Sep: no Check-in", "Week of 5 Oct: not yet due",
    "Week of 7 Sep: On Hold", or "not yet Active". A Goal on another cadence
    names a period by its first and last days ("14 Sep – 27 Sep"). A muted line
    under the strip sums it up, counting the periods not yet due apart from the
    missed ones, and naming them only when there are any: "Last 11 weeks: 7
    Green, 1 Yellow, 2 with no Check-in, 1 not yet due." The strip shows for any
    Goal that has been Active, and it changes nothing: not Stale, not Health.
  - **Filter chips (`.tl-chips`):** a row of `--radius-full` chips above the
    list, wrapping on narrow screens: All, Check-ins, Date Slips, So What,
    Ownership, Values. Each carries its count. Date Slips counts and lists the
    Check-ins that carry one. A chip is a plain link to
    `/goals/{id}?history=<filter>#history`, so the page reloads with the
    filter in the address and the History block in view. The current chip is
    `aria-current="page"`, with a `--color-primary-light` fill and a
    `--color-primary` edge, and the rest sit on `--color-surface`. Chips are
    32px tall, and 44px on a coarse pointer.
  - **Paging:** the latest 20 entries show. When there are more, a quiet
    **Show earlier** link (`?shown=40`) reloads the page with 20 more under
    the same filter. Chips and paging are links, so History works without
    script.
- **Home's Needs you list (`homePage` in `home.templ`):** one card in the main
  column holding everything waiting on the person, in two subgroups, each a
  `.label` heading over ruled rows (`.hm-list`):
  - **Check-ins due:** the Goals the person owes a Check-in on, most overdue
    first. Each row keeps its Health badge, its Stale mark, a secondary
    **No change** and a primary **Check in**. A Goal they are a Delegate on
    carries a "for [Owner]" tag, and appears only here.
  - **Requests:** link requests and Handoffs waiting on the person, oldest
    first. **Reject** is a quiet text button (`.btn.quiet`) and **Accept** a
    secondary, outlined one (`.btn`). Reject sends at once and returns to
    Home, which offers Undo in the toast. Where accepting needs choices, as a
    Handoff of a Goal with Delegates does, the outlined button reads
    **Review** and leads to the Pending handoffs page, where those choices
    are made.

  A subgroup with nothing in it is left out, and with both empty the card
  shows one muted "You're all caught up." line. The summary under the page
  title and the top bar's Home count both count every row. Goals the person is
  a Delegate on aren't listed again elsewhere: the sidebar has one link to the
  Delegate page with their number, shown only above zero.
- **Health distribution bar (`.hm-bar`):** in Home's "Your goals", a 12px
  `--radius-full` bar of the person's own Active Goals by Health, one equal
  unit per Goal, Green then Yellow then Red. Units fill with the Health's
  `--health-*-ink`, which keeps 3:1 on the card in both themes, and a 4px gap
  of card shows where one Health's run gives way to the next. It counts Goals
  and never aggregates Metrics (ADR 0003). Goals with no Health yet aren't
  drawn, and with none to draw the bar is left out. The Health count badges
  beneath it are its labels, so it reads without colour, and its `aria-label`
  ("2 Green, 0 Yellow, 1 Red") says the same to a screen reader.
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
- **Tags (`.tag`):** `--radius-full`, outlined with no fill: a 1px solid
  `--color-border-strong` edge and `--color-ink` text. A retired Dimension
  value's tag takes a dashed edge instead, struck through and muted.
- **Stale chip (`.badge.st`):** marks a Stale Goal wherever its Health shows
  (the Goal list, Home, a Report), with its age ("Stale · 9 days" on the Goal
  list). It is a warning about freshness, never a tag, so it can't share a
  tag's look: a `--stale-bg` grey fill, `--stale-ink` text and a 1px dashed
  `--stale-edge` border, at a Health badge's `--radius-sm`. The Goal list's
  "Path to Green overdue" chip takes the same style.
- **Stale banner (`.alert.st`):** at the top of a Stale Goal's page, a
  `--color-surface` banner in a 1px dashed `--stale-edge` border, with
  `--color-ink` text: **Stale**, then how long it has gone without a Check-in
  against its cadence. For whoever may check in (the Owner or a Delegate) it
  ends, at the right, with a **Check in now** link to the Goal's Check-in
  page. Anyone else sees no link. The "Path to Green overdue" banner beside it
  takes the same style with no link, because CONTEXT.md surfaces both the same
  way.

  The ratios are in § Deviations 9.
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
   is the selected-row fill):

   | Theme | Ink | Canvas | Surface | Surface hover | Surface alt | Primary light |
   | --- | --- | --- | --- | --- | --- | --- |
   | Light | `--color-ink` `#18211F` | 15.20 | 16.46 | 16.02 | 14.66 | 14.60 |
   | Light | `--color-ink-2` `#3E4946` | 8.63 | 9.35 | 9.10 | 8.33 | 8.30 |
   | Light | `--color-ink-muted` `#66706D` | 4.73 | 5.12 | 4.98 | 4.56 | 4.54 |
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
   Health badges therefore keep their own hues:
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
   rings focus in `--color-focus: #0D9488` instead (3.46:1 on the canvas,
   3.74:1 on white, 3.34:1 on `--color-surface-alt`). On the dark-teal top
   bar that drops to 2.53:1, so nav items ring in `--nav-focus: #2DD4BF`
   (5.09:1). Dark mode keeps `#2DD4BF` everywhere.

   The source also rings a focused input in a 25%-alpha halo of the primary,
   which reaches only 1.36:1 on white. Inputs take the solid ring instead, and
   the halo token is dropped.

6. **Input borders and other non-text marks.** The source's input border
   (`#D5D2BD`) reaches 1.52:1 on white, and the dark preview's scale step 2
   (`#0A5C53`) reaches 1.87:1 on the dark surface. WCAG asks 3:1 of the edge
   that identifies a control. `--color-border-strong` is `#7B908C` in light
   mode (3.38:1 on white, 3.12:1 on the canvas, 3.01:1 on
   `--color-surface-alt`) and `#2F8479` in dark mode
   (3.29:1 on the surface). Two more marks follow the same rule:
   - A Metric's sparkline, the one data accent allowed teal (§ System), strokes
     in `--color-primary-strong`, not `--color-primary`, which reaches only
     2.49:1 on white.
   - On the Check-in form, an unselected Health choice is an outline (surface
     fill, `--color-border-strong` edge, Health ink) and the selected one is
     the filled Health badge with an edge in its ink. Nothing is dimmed.

7. **Neutral canvas.** The source's light canvas is a pale yellow
   (`#FEFCE8`), and its panel, hover and rule tokens carry the same ivory
   tint. Beside it, Yellow Health's fill (`#FAEBC4`) lost its hue, and white
   cards looked cut out of the page (UX Review). The light canvas is a neutral
   off-white instead, and the tokens beside it lose their tint while keeping
   their order: white cards, then a hovered row, the canvas, panels and table
   heads, and rules, each a step darker. The dark theme is unchanged.

   | Token | Source | Now |
   | --- | --- | --- |
   | `--color-canvas` | `#FEFCE8` | `#F6F6F3` |
   | `--color-surface-hover` | `#FDFCF7` | `#FCFCFA` |
   | `--color-surface-alt` | `#F7F6E8` | `#F2F2EE` |
   | `--color-border` | `#E8E6D5` | `#E1E1DC` |

   Each light-theme text colour reaches 4.5:1, and each control edge 3:1, on
   the canvas and every surface beside it:

   | Mark | Canvas | Surface | Surface hover | Surface alt |
   | --- | --- | --- | --- | --- |
   | `--color-ink` `#18211F` | 15.20 | 16.46 | 16.02 | 14.66 |
   | `--color-ink-2` `#3E4946` | 8.63 | 9.35 | 9.10 | 8.33 |
   | `--color-ink-muted` `#66706D` | 4.73 | 5.12 | 4.98 | 4.56 |
   | Links, `--color-primary-strong` `#0F766E` | 5.05 | 5.47 | 5.33 | 4.88 |
   | `--color-danger-ink` `#9A231B` | 7.37 | 7.98 | 7.77 | 7.11 |
   | Input edge, `--color-border-strong` `#7B908C` | 3.12 | 3.38 | 3.29 | 3.01 |
   | Focus ring, `--color-focus` `#0D9488` | 3.46 | 3.74 | 3.65 | 3.34 |

   `--color-surface-alt` is as dark as it can go: one step darker and the input
   edge on it falls under 3:1.
8. **Flat cards.** The source lifts every card off the canvas on a large
   teal-tinted shadow (`--shadow-card`, deepening to `--shadow-card-hover` on
   hover), so nothing read as nearer than anything else (UX Review). Cards are
   flat instead: a 1px `--color-border` edge and no shadow, in both themes. In
   dark, the card's `--color-surface` against the darker `--color-canvas`
   sets it apart. A clickable card's hover changes its border colour alone
   (§ Components). Only menus and popovers keep the source's `--shadow-pop`:
   the Theme menu, the Goal page's More menu, the Goal list's New goal form
   and the toast. The two card shadow tokens are gone.
9. **Stale, apart from tags.** Stale chips and banners, tags and nav counts
   all shared the pale teal `--color-primary-light` fill, so "Stale" read like
   a label such as "Platform" (UX Review). Stale is now a grey chip in a dashed
   edge, its banners a surface in the same edge, and tags an outline with no
   fill (§ Components). The UX Review gave the light edge as `#8C939C`, which
   reaches only 2.87:1 on the canvas the banners sit on. The edge is one step
   darker in the same hue, `#888F97`, which reaches 3:1 there.

   Chip and banner text reaches 4.5:1 on its fill, and the dashed edge 3:1 on
   the surfaces around it. A chip sits on a card, plain or as a hovered row. A
   banner sits on the canvas with a surface inside.

   | Theme | Mark | On | Ratio |
   | --- | --- | --- | --- |
   | Light | Chip text `#3B4048` | chip fill `#E9EAEC` | 8.67 |
   | Light | Chip edge `#888F97` | surface `#FFFFFF` / surface hover `#FCFCFA` | 3.27 / 3.18 |
   | Light | Banner text `--color-ink` `#18211F` | surface `#FFFFFF` | 16.46 |
   | Light | Banner link `--color-primary-strong` `#0F766E` | surface `#FFFFFF` | 5.47 |
   | Light | Banner edge `#888F97` | canvas `#F6F6F3` / surface `#FFFFFF` | 3.02 / 3.27 |
   | Light | Tag text `--color-ink` `#18211F` | canvas / surface / surface hover | 15.20 / 16.46 / 16.02 |
   | Light | Tag edge `--color-border-strong` `#7B908C` | canvas / surface | 3.12 / 3.38 |
   | Dark | Chip text `#E8E8E8` | chip fill `#3B4048` | 8.51 |
   | Dark | Chip edge `#8C939C` | surface `#052E29` / surface hover `#063530` | 4.74 / 4.34 |
   | Dark | Banner text `--color-ink` `#E8E8E8` | surface `#052E29` | 11.99 |
   | Dark | Banner link `--color-primary-strong` `#14B8A6` | surface `#052E29` | 5.90 |
   | Dark | Banner edge `#8C939C` | canvas `#020E0C` / surface `#052E29` | 6.32 / 4.74 |
   | Dark | Tag text `--color-ink` `#E8E8E8` | canvas / surface / surface hover | 16.01 / 11.99 / 10.99 |
   | Dark | Tag edge `--color-border-strong` `#2F8479` | canvas / surface | 4.39 / 3.29 |

## Accepted audit findings

Places where the app follows the source on purpose, though a design audit has
flagged them. They aren't deviations, and later audits shouldn't raise them
again.

1. **White card surface.** The 2026-10-01 audit called the light theme's
   `--color-surface` (`#FFFFFF`) flat beside the canvas, then ivory. It stays
   white.
   The light-theme contrast ratios in § Deviations are quoted against it, and
   two of them have little room to lose: the input border at 3.38:1 and the
   focus ring at 3.74:1, against WCAG's 3:1. Tinting the surface would mean
   recomputing every one.
2. **Hex and rgba tokens.** The same audit asked for tokens in OKLCH. They
   stay in hex and rgba, the source's notation, so they can be checked against
   it line by line. Converting them would change nothing a reader sees.
