# Design — Goal Tracker

The design system for Goal Tracker's web UI, with a light and a dark theme.
Pages follow this file. If you need to change the system, edit this file
deliberately; don't override it on one page.

**The system is moving to the flat frame** (§ Direction, adopted 2026-10-20).
Where § Direction and a later section disagree, § Direction is the target. The
frame itself has landed: § Tokens, § Type scale and the Sidebar, Page header,
Theme menu and Nav counts components describe it as built. The other sections
describe the app as built, and each is rewritten to match as its page moves
over.

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

## Direction: the flat frame

Adopted 2026-10-20 from the maintainer's mockups of Home, the Goal list and the
Goal page, which were modelled on Linear. The aim is to get rid of the "everything in a card"
look: a grey canvas under white bordered boxes. A page becomes one surface, and
type, space and hairlines carry the hierarchy.

### Principles

- **One surface.** The page is a single plain background, white in light. No
  section sits in a box. Sections are a heading over rows separated by 1px
  hairlines, with space between sections doing the grouping.
- **Surfaces are for what floats.** A raised surface (fill, border, shadow) is
  kept for menus, popovers, dialogs, the toast and the Check-in page's sticky
  submit bar. A raised surface then means "this is on top". `.card` is
  retired as a page container. A form is a plain section, and its fields have
  their own edges.
- **Type leads.** Each page has one confident title and real section headings.
  The `.label` caps style is retired for headings.
- **Health is the colour.** Red, Yellow and Green, as their shape and pill, are
  the page's strongest colour. Teal stays the accent for what a person acts
  on: primary buttons, links, focus, and the logo mark.
- **Tight, aligned density.** It is built for long lists: 40–44px rows, 13–14px
  text, a 4px grid, and numbers in tabular figures so columns line up.

### The frame

- **Sidebar (replaces the top bar).**
  - **Size and surface:** a 240px column on the left of every signed-in page,
    in `--color-sidebar` with a 1px `--color-border` right edge.
  - **Order, top to bottom:**
    1. The app's name, with a small teal mark. It links to Home.
    2. A **Search** field. It submits a GET to the Goal list's search
       (`/goals?q=`). It shows no keyboard-shortcut hint: the app has no
       shortcut.
    3. The nav: **Home · Goals · Follow-ups · Reports**, and **Admin** for an
       Admin. Each has a 16px stroke icon and its label, and the count sits at
       the right edge in muted tabular figures. Home's count keeps its emphasis
       (§ Nav counts) as a filled pill.
    4. **My org.** For someone with people under them, their direct reports by
       Name, each linking to that person's org on Goals. It isn't shown to
       anyone without reports.
    5. At the foot, the signed-in person's Name with their initials. It opens a
       menu holding **Theme** (System, Light, Dark) and **Sign out**.
  - **Nav items:** 32px tall with a 6px radius, `--color-ink-2` at rest. Hover
    and the current item take `--color-sidebar-hover` and `--color-sidebar-on`
    fills with `--color-ink` text. The current item is marked
    `aria-current="page"`, and the rule that marks Goals current on a Goal's
    pages stays.
- **Page header.**
  - **Layout:** a 48px strip across the top of the content, with a 1px
    `--color-border` under it. It holds the page's title, or a breadcrumb on a
    page under another ("Goals › <Goal title>"), with a muted count where the
    page has one ("24 in My org").
  - **Actions:** the page's actions sit at its right, with the primary one as
    a filled button (New goal, Check in). It isn't sticky.
- **Content.** Pages are fluid within the content column:
  - a list fills it edge to edge, with 28px side padding;
  - a reading page centres a column up to 880px;
  - the Goal page uses a document column with a 300px properties rail beside it
    (§ Page shapes).
- **Phone (900px and below).** The sidebar collapses into a 48px top strip:
  - the app's name on the left, then **Menu**, a `<details>` disclosure that
    opens the nav, counts, My org and the person's menu as a sheet;
  - Home's count also shows on the strip, so it isn't hidden in the menu.

  Nothing scrolls sideways. A wide list scrolls inside its own container.

### Tokens

These replace the matching tokens in § Tokens when the frame lands. Light:

```css
--color-canvas:        #FFFFFF; /* the page */
--color-sidebar:       #F7F7F8;
--color-sidebar-hover: #EEEEF0;
--color-sidebar-on:    #E8E8EB; /* the current nav item */
--color-row-hover:     #F6F6F7; /* a list row under the pointer */
--color-group:         #FAFAFB; /* a list's group header row */
--color-border:        #EDEDEF; /* hairlines */
--color-border-strong: #8A8D93; /* inputs and chip edges: 3:1 on every surface above */
--color-ink:           #1B1D22;
--color-ink-2:         #33363C;
--color-ink-muted:     #5F636B;
--color-chip-bg:       #F1F1F3; --color-chip-ink: #4A4D54; /* signals and tags */
--health-r-bg: #FDECEC; --health-r-ink: #A61B1B; --health-r-mark: #C62828;
--health-y-bg: #FFF3D1; --health-y-ink: #7A4F00; --health-y-mark: #B27B00;
--health-g-bg: #E5F3EA; --health-g-ink: #1C6435; --health-g-mark: #2E7D4F;
--radius-md: 6px;      /* buttons, inputs, chips, nav items */
--radius-lg: 10px;     /* menus, dialogs, the toast */
```

Dark (chosen):

```css
--color-canvas:        #0F1012;
--color-sidebar:       #151618;
--color-sidebar-hover: #1C1D20;
--color-sidebar-on:    #222327;
--color-row-hover:     #1A1B1E;
--color-group:         #17181B;
--color-border:        #26282C;
--color-border-strong: #6B6F77;
--color-ink:           #EDEEF0;
--color-ink-2:         #C9CBD0;
--color-ink-muted:     #9A9EA6;
--health-r-mark: #F07171; --health-y-mark: #E0B23A; --health-g-mark: #4CC38A;
/* Health pill fills and inks keep the dark 20%-alpha fills in § Tokens */
```

These stay as they are:
- the teal `--color-primary-*` tokens, `--color-focus` and `--color-danger-ink`;
- the Check-in overdue tokens;
- `--shadow-pop`, now used by every floating surface.

The `--nav-*` tokens go with the top bar. Measured on canvas, sidebar, row hover
and group header, every text ink is at least 4.5:1, and every input edge,
focus ring and Health mark at least 3:1, in both themes. The lowest pairs are:

- light muted ink: 5.58;
- light input edge: 3.08;
- the Yellow mark: 3.40;
- dark input edge: 3.42.

They landed with the frame: § Tokens holds them, with the roles § Direction
leaves out marked *(chosen)*, and § Deviations 10 their ratios, which
`scripts/contrast.py` checks.

### Type

- **One face: Inter**, for headings and text alike. Plus Jakarta Sans and
  JetBrains Mono are retired.
- **Sizes and weights:**
  - page title: 28/1.2, 600, -0.02em;
  - section heading: 13/1.4, 600 (an `h2`);
  - row title: 14, 500;
  - body: 14/1.5;
  - meta: 12.5–13 in `--color-ink-muted`;
  - small label: 12, 500, sentence case, never uppercase.
- **Numbers:** dates, counts and Metric values use
  `font-variant-numeric: tabular-nums`, not a monospace face.
- **Exception:** the Print variant keeps its own serif.

### Components in the frame

- **List rows.** These replace tables-in-cards for the Goal list, Follow-ups,
  the Reports list and the like.
  - **Rows:** 40–44px tall, full-bleed, separated by `--color-border`, with
    `--color-row-hover` under the pointer.
  - **Columns** sit on a CSS grid: a Health mark, the title (500 weight, an
    ellipsis when long), signal chips, then the Owner with initials, then the
    tabular dates.
  - **Column labels:** a muted 12px row at the top, not a filled table head.
  - **Groups:** a `--color-group` header row with the group's shape, name and
    count, for example "Red 4".
  - **Phone:** at phone width a row stacks: title first, meta under it.
- **Health.**
  - **In a list:** the 11px shape alone in `--health-*-mark`, with an
    `aria-label` naming the Health.
  - **Elsewhere:** a pill on `--health-*-bg` with `--health-*-ink`, holding the
    shape and the word. Examples are "Red · 9 wks", Home's rows and the Goal
    page's properties.
  - **Shapes stay:** square for Red, triangle for Yellow, circle for Green, so
    colour is never the only signal.
- **Signal chips.**
  - **What they cover:** Check-in overdue, Path to Green overdue, Milestone
    overdue, Schedule conflict, the back-to-Green date, and the like.
  - **Look:** a 22px pill on `--color-chip-bg` with `--color-chip-ink`. Check-in
    overdue keeps its dashed edge, so it still reads as a warning, not a tag.
- **Filter bar.**
  - **Filters:** a row of 28px chip buttons with a 1px `--color-border-strong`
    edge: Scope, Health, Lifecycle, Incomplete, "+ Filter" for More filters,
    and Group by.
  - **Set filters:** a set chip reads its value ("Scope: My org") on
    `--color-primary-subtle`.
  - **Search and Clear:** search sits at the row's right, and Clear follows the
    chips when anything is set.
  - **Kept as is:** what the filters do and their no-script behaviour.
- **Buttons.**
  - **Size:** 30px tall with a 6px radius. A coarse pointer still gets 44px.
  - **Primary:** a teal fill.
  - **Secondary:** a white fill and a 1px `#DEDEE2` edge.
  - **Quiet:** text only.
- **Avatars.** A person's Name may carry a 22px circle with their initials, in
  a colour fixed for that person. The Name stays the control that shows their
  email (CONTEXT.md: Name).
- **Segmented control.** Two or three choices in one 30px track, as on the Goal
  list's List / Table switch: a `--color-chip-bg` track with a white current
  segment.

### Page shapes

- **Home:**
  - Your week as the page title, with the date and the Your goals line under
    it;
  - then Needs you, My org and Your Yellow or Red goals as sections of rows;
  - no card, Needs you included: its rows lead the page, and each row's actions
    sit at its right.
- **The Goal list:** a full-bleed grouped list under the filter bar, with
  Health groups by default.
- **The Goal page:**
  - **Document column:** the title, Why it matters as a lead paragraph, then
    Latest status, Metrics, Milestones, Highlights, Action Items, Comments and
    History as sections. The column is up to 860px.
  - **Properties rail:** at its right, behind a 1px left edge, a list of label
    and value rows. It holds Health, Lifecycle, Owner, Delegates, cadence,
    delivery, level and Dimensions, then Contributes to and Contributed to by
    as lists, each Goal with its Health shape.
  - **Phone:** the rail stacks under the column.
- **Follow-ups:**
  - its group summaries become a row of plain counts with their chips, not
    cards;
  - its flagged Goals become a list in the shape above.
- **Forms** (New goal, Check-in, Report builder): a single centred column of
  plain sections, with no outer card. The Check-in's sticky submit bar stays a
  floating surface.

### Rollout

The change lands as one frame-and-tokens change, then one change per page,
each rewriting its section below to match. Until a page moves, it keeps its
current look inside the new frame.

## System

- **Mood:** calm and dense. One white canvas (near-black in dark), neutral
  ink and hairlines, and teal only for what a person can act on: links,
  buttons, focus and the logo mark. The one data accent is a Metric's trend:
  its sparkline on the Goal page and its progress bar on the Check-in page,
  both in teal (see § Deviations 6). No neon, and no tint on the canvas, so Health's
  fills stand out from the page (see § Deviations 7). Until its page moves, a
  card sits on the canvas as a white box behind a 1px hairline.
- **Density:** generous. Cards use 24px padding, sections sit 20–32px apart,
  and everything aligns to a 4px grid.
- **Cards are earned** (superseded by § Direction: One surface, and kept
  here until each page moves): a card is for a form, a table, or a page's single
  primary task list, the one thing the page is there for a person to do now.
  Every other section is a plain section on the canvas: a heading over rows
  separated by 1px `--color-border` hairlines, with no surface, border or
  padding box. So the eye finds the page's job first, and the context around
  it reads lighter. A page's primary section may be that card and still keep
  a section-level `h2` on it, as Home's Needs you does (§ Components: Card).
  Home follows the rule (Needs you is its one card), and so does the Reports
  list (its table is its one card; Open Action Items above it is a plain
  section); other pages are brought in line one at a time.
- **Depth:** cards lie flat on the canvas, set off by their surface and a
  1px border. Only menus and popovers float, on `--shadow-pop`, so an open
  one reads as nearer than anything under it (see § Deviations 8).

## Tokens

The flat frame's tokens (§ Direction: Tokens). The roles § Direction leaves
out are marked *(chosen)*: the card surfaces pages still use until they move,
and the dark chips.

```css
:root {
  /* Brand */
  --color-primary:        #14B8A6; /* Teal 500: active pills, accents */
  --color-primary-hover:  #0D9488; /* Teal 600 */
  --color-primary-strong: #0F766E; /* Teal 700: filled buttons, links, the logo mark. See Deviations */
  --color-primary-light:  #CCFBF1; /* Teal 100: the current choice in a menu */
  --color-primary-subtle: #F0FDFA; /* Teal 50: faint tints */

  /* The frame: one white surface, and the steps off it */
  --color-canvas:        #FFFFFF; /* the page */
  --color-sidebar:       #F7F7F8;
  --color-sidebar-hover: #EEEEF0;
  --color-sidebar-on:    #E8E8EB; /* the current nav item */
  --color-row-hover:     #F6F6F7; /* a list row under the pointer */
  --color-group:         #FAFAFB; /* a list's group header row */

  /* (chosen) Cards, a hovered card, and panels and table heads, for pages
   * that haven't moved to the frame */
  --color-surface:       #FFFFFF;
  --color-surface-hover: var(--color-row-hover);
  --color-surface-alt:   var(--color-sidebar);

  /* Ink */
  --color-ink:         #1B1D22; /* headings, primary labels. See Deviations */
  --color-ink-2:       #33363C; /* body text */
  --color-ink-muted:   #5F636B; /* meta, counts, placeholders */
  --color-ink-inverse: #FFFFFF; /* text on --color-primary-strong */

  /* Lines */
  --color-border:        #EDEDEF; /* hairlines */
  --color-border-strong: #8A8D93; /* inputs and chip edges: 3:1 on every surface. See Deviations */
  --color-focus:         #0D9488; /* Teal 600. See Deviations */

  /* Feedback */
  --color-success: #2DD4BF;
  --color-warning: #EAB308;
  --color-error:   #EF4444;
  --color-danger-ink: #9A231B; /* destructive text (.btn.danger, form errors). See Deviations */

  /* Signal chips and tags */
  --color-chip-bg:  #F1F1F3;
  --color-chip-ink: #4A4D54;

  /* Check-in overdue and Path to Green overdue: chip and banner. See Components */
  --checkin-overdue-bg:   #E9EAEC; /* chip fill */
  --checkin-overdue-ink:  #3B4048; /* chip text */
  --checkin-overdue-edge: #888F97; /* dashed edge of chip and banner. See Deviations */

  /* Goal Health: a pill's fill and ink, and the shape a list shows alone.
   * See Deviations */
  --health-g-bg: #E5F3EA; --health-g-ink: #1C6435; --health-g-mark: #2E7D4F;
  --health-y-bg: #FFF3D1; --health-y-ink: #7A4F00; --health-y-mark: #B27B00;
  --health-r-bg: #FDECEC; --health-r-ink: #A61B1B; --health-r-mark: #C62828;

  /* Type */
  --font-body: "Inter", system-ui, sans-serif;

  /* Shape */
  --radius-sm: 4px;      /* badges, Health strip cells */
  --radius-md: 6px;      /* buttons, inputs, chips, nav items */
  --radius-lg: 10px;     /* menus, dialogs, the toast, cards */
  --radius-full: 9999px; /* pills, avatars, Home's count */

  /* Elevation (teal-tinted): what floats only. Cards are flat.
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

  /* (chosen) the frame, in neutral near-blacks */
  --color-canvas:        #0F1012;
  --color-sidebar:       #151618;
  --color-sidebar-hover: #1C1D20;
  --color-sidebar-on:    #222327;
  --color-row-hover:     #1A1B1E;
  --color-group:         #17181B;

  /* (chosen) a card lifts by lightness, as dark themes show elevation;
   * panels and table heads sink to the canvas */
  --color-surface:       var(--color-sidebar);
  --color-surface-hover: var(--color-sidebar-hover);
  --color-surface-alt:   var(--color-canvas);

  --color-ink:         #EDEEF0; /* (chosen) */
  --color-ink-2:       #C9CBD0; /* (chosen) */
  --color-ink-muted:   #9A9EA6; /* (chosen) */
  --color-ink-inverse: #042F2E; /* (chosen) Teal 950 on teal */

  --color-border:        #26282C; /* (chosen) */
  --color-border-strong: #6B6F77; /* (chosen) 3:1 on every surface */
  --color-focus:         #2DD4BF;

  --color-success: #2DD4BF;
  --color-warning: #EAB308;
  --color-error:   #F87171; /* (chosen) Red 400 reads better on dark */
  --color-danger-ink: #F87171; /* (chosen) same as --color-error */

  --color-chip-bg:  #222327; /* (chosen) the current nav item's fill */
  --color-chip-ink: #C9CBD0; /* (chosen) --color-ink-2 */

  /* (chosen) Check-in overdue: the light chip's ink becomes the fill, and the UX
   * Review's grey the edge */
  --checkin-overdue-bg:   #3B4048;
  --checkin-overdue-ink:  #E8E8E8;
  --checkin-overdue-edge: #8C939C;

  /* (chosen) Health hues on 20%-alpha fills with light text, and bright
   * shapes */
  --health-g-bg: rgba(34,160,95,.20); --health-g-ink: #8EDDAF; --health-g-mark: #4CC38A;
  --health-y-bg: rgba(234,179,8,.20); --health-y-ink: #F5D06B; --health-y-mark: #E0B23A;
  --health-r-bg: rgba(239,68,68,.20); --health-r-ink: #F8A39B; --health-r-mark: #F07171;

  --shadow-pop:        0 25px 50px -12px rgba(0,0,0,.65);

  /* Dark hover goes lighter: --color-ink-inverse on it is 7.77:1 */
  --btn-primary-hover: var(--color-primary-hover);
}
```

In the stylesheet, the empty dark `@media` block above repeats the full
`[data-theme="dark"]` declarations.

## Type scale

One face, Inter, for headings and text alike (§ Direction: Type).

| Role | Size / line-height | Weight | Tracking |
| --- | --- | --- | --- |
| Page title (`h1`) | 28 / 1.2 | 600 | -0.02em |
| Section heading (`h2`) | 13 / 1.4 | 600 | — |
| Card title (`h3`), until its page moves | 14 / 1.4 | 600 | — |
| Row title | 14 | 500 | — |
| Body | 14 / 1.5 | 400 | — |
| Meta, `.small` | 12.5–13 / 1.5, in `--color-ink-muted` | 400 | — |
| Small label (`.label`, `th`) | 12, sentence case, never uppercase | 500 | — |
| Numbers (`.num`) | inherit, `font-variant-numeric: tabular-nums` | 500 | — |

Dates, counts and Metric values line up in tabular figures, not a monospace
face. The app serves Inter itself, from `internal/web/static/fonts/`, as a
variable font by `@font-face` rules at the top of `app.css`. No page loads a
font from another host. The Print variant keeps its own serif.

## Dates and times

Every page writes dates and times one way, through `internal/web/dates.go`:

- **A date** reads "Nov 7", or "Nov 7, 2025" when it falls outside the
  current year, past or next.
- **A timestamp** reads "Nov 7, 15:04": 24-hour, with no zone suffix, under
  the same year rule ("Nov 7, 2025, 15:04").
- **The current year** is the company's, judged in its timezone
  (`GOAL_TRACKER_TIMEZONE`) from the app's clock.
- **Report pages always show the year**, the current one included: every
  date and time on a draft or a publication, and a publication's date
  wherever it appears, as on the Reports list ("Nov 7, 2026").
- **The company's timezone:** a timestamp, and an instant shown as a date
  (when a Check-in or reading was written), is shown in it. A calendar date
  (a delivery date, Milestone, Metric target, Back to Green, baseline or due
  date) is the day it names and is never shifted.
- **Inputs stay ISO:** an `<input type="date">` holds `2026-11-07`, as do
  query strings such as `?baseline=`; `datetime=` attributes are RFC 3339.
- **Tabular figures only in tables:** a date takes `.num` only in a table
  column, such as the Goal list's Due or the Milestones table. Elsewhere it
  is body type.

## Components (mapped to `internal/web/static/app.css`)

- **Sidebar (`.sidebar`, `sidebar` in `layout.templ`):** every signed-in page
  sits in the flat frame (`.frame`): a 240px column on the left in
  `--color-sidebar`, with a 1px `--color-border` right edge, beside the
  content column. It stays in view as the page scrolls. Top to bottom:
  1. **The app's name (`.brand`)** with its 18px `--color-primary-strong`
     mark. It links to Home.
  2. **Search (`.sb-search`)**, a GET form to the Goal list's search
     (`/goals?q=`). It shows no shortcut hint, as the app has none.
  3. **The nav (`.navitems`):** **Home · Goals · Follow-ups · Reports**, then
     **Admin** for an Admin. Each item is 32px tall with `--radius-md`, a 16px
     stroke icon and its label in `--color-ink-2`, and its count at the right
     edge (§ Nav counts). Hover takes `--color-sidebar-hover` and the current
     item (`.on`, `aria-current="page"`) `--color-sidebar-on`, both with
     `--color-ink` text. An item is current on its own page and every page
     under it, so a Goal's page, New goal and a Check-in all mark Goals.
     Home counts what needs the person and Follow-ups counts the Goals it
     flags in the person's default scope (see Scope), as many as a bare
     `/follow-ups` lists for them; Goals and Reports carry no count.
  4. **My org (`.sb-org`):** for someone with people directly under them by
     Manager, a muted "My org" heading over each of those people by Name,
     sorted by Name, each linking to their Org on Goals (`/goals?org=<id>`).
     Anyone without people under them gets no My org. The people are read
     once per request, with the counts.
  5. **The person's menu (`.sb-person`)**, at the foot: their initials in a
     24px avatar and their Name, truncated with an ellipsis when long. It is
     a `<details>`, so it works without script. Its panel floats above it on
     `--shadow-pop`: "Signed in as" their Name (the Name shows their email,
     as everywhere), "(Admin)" for an Admin, then the Theme choices (§ Theme
     menu) and **Sign out**.

  **At 900px and below** the sidebar is a 48px strip across the top: the
  app's name, Home's count (so it isn't hidden), and **Menu**, a `<details>`.
  Opening Menu shows the rest of the sidebar (Search, the nav, My org and the
  person's menu) as a sheet under the strip, on `--shadow-pop`. The sheet is
  the sidebar's own body, shown by the open Menu beside it
  (`.sb-menu[open]~.sb-body`), so it works without script and the page has
  one copy of each nav item. Nothing scrolls sideways. On a coarse pointer
  the nav items, My org's people, the Theme choices and the person's button
  are at least 44px tall.

  **Signed-out pages** (sign-in, set password, Not found while signed out)
  have no sidebar. The app's name and a Theme disclosure sit in a bar over
  the page (`.signed-out-bar`), and the content is centred on the canvas.
- **Page header (`.page-header`, `pageHeader` in `layout.templ`):** a 48px
  strip across the top of the content column with a 1px `--color-border`
  under it. It holds the page's title, or a breadcrumb on a page under
  another ("Goals › <Goal title>"), with a muted count where the page has one,
  and the page's actions at its right. It isn't sticky. A page fills it by
  rendering through `framedLayout` with a header; a page that hasn't moved to
  the frame renders through `layout` and keeps its own head in its content.
- **Theme menu (`.theme-menu`):** three choices in words: **System** (the
  default: follow the operating system), **Light** and **Dark**. Signed in,
  they sit in the person's menu beside Sign out. Signed out, they sit in a
  quiet "Theme" disclosure (`.theme`) in the bar over the page, whose panel
  (`.theme-float`) floats under the button, anchored right and no wider than
  the screen, so it never widens the page. The current choice is marked
  with a `--color-primary-light` fill and a check, and `aria-current="true"`.
  Each choice is a plain form post, so it works without script.
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
    request's pending page, whichever the Reject came from). The templ
    component is `toastNotice` in `layout.templ`; a new Undo reuses it rather
    than making its own notice.
- **Not found page:** like a refused Undo, a missing page shows a page, not
  bare text. A page you open that doesn't exist (an unknown URL, asked with
  any method, or a Goal, Report, publication or other thing that doesn't
  exist or has been removed) answers 404 with a "Not found" page inside the
  normal chrome, the sidebar's counts included: one plain sentence, "There's
  nothing here. It may have been removed, or the link may be wrong.", and a
  way back: **Back to Home** when signed in, **Sign in** when not. The page
  is `notFoundPage` in `notfound.templ`, written by `s.notFound`; a handler
  that 404s calls it rather than `http.NotFound`. An action (a form's POST)
  refused as "not found" answers as before, with its area's plain-text
  error, and a known address asked with a method it doesn't take still
  answers 405.
- **Message page (`.message-page`):** a page that only says one thing (Not
  found, Can't undo, Can't remove link, a refused Nudge, a Check-in that
  can't be reached) sets its heading, sentence and way back in a 760px column
  with 16px gaps. The column starts at the page's left edge, like every
  other page's content, and isn't centred, except signed out, where every
  page's content is centred (§ Sidebar).
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
- **A person's Name (`.person`):** plain text in the surrounding ink, with no
  underline, so it never reads as a link. On hover and keyboard focus it takes a
  faint `--color-primary-subtle` tint, as a quiet button does, and focus keeps the focus ring. Clicking, tapping or activating
  it from the keyboard shows the person's email beside it, and doing it again
  hides it (CONTEXT.md: Name). `personPlain`, inside another control, and the
  Print view's "Name (email)" have no control.
- **Icon remove button (`.btn.rb-remove`):** the Report builder's × on a rule
  row and on a Goal chip. It is a 40px square, the height of the selects
  beside it, with a 1px `--color-border-strong` edge and the standard focus
  ring. On a Goal chip it is round and forms the pill's right end: the chip
  keeps only its 12px left padding, so it stays compact. On a coarse pointer
  it is 44px wide as well as tall. It shows only the ×, so it always carries
  an `aria-label` and a matching `title` naming what it removes ("Remove
  rule", "Remove <Goal title>").
- **Card (`.card`):** retired as a page container by the flat frame, and kept
  until each page moves. It sits quietly on the canvas: `--color-surface`
  (white on the white canvas in light, a step lighter than the canvas in
  dark), a 1px `--color-border` hairline, `--radius-lg` (10px), 24px padding,
  and no shadow in either theme. On hover a
  clickable card's border becomes `--color-border-strong` over `--dur-base`,
  and nothing else changes: it doesn't move. That is darker in light and
  lighter in dark, so either way the edge stands further out from the card.
  A card nested inside a section takes an `h3` (card title) for its own
  heading; `h2` is for page sections. The one exception is a page's primary
  section when it is a card (§ System: Cards are earned): it keeps the
  section-level `h2`, as Home's Needs you does.
  Where a card earns its place is § System: Cards are earned. The Goal page
  predates that rule: there a card is for something a person acts on or
  reads as a unit of status, such as the Check-in, each Metric and
  Milestones, and reference blocks are not cards. On the Goal page these are Action Items, Highlights,
  Comments, History and the sidebar's blocks.
- **Canvas block (`.ruled`):** a reference block sits on the canvas with no
  surface, border or shadow. A 1px `--color-border` rule sits above it with
  20px of padding below the rule. The first block in a column has no rule,
  because nothing sits above it. A canvas block in the main column takes an
  `h2`. **Sidebar exception:** a canvas block in the sidebar keeps the `h3`
  size, so the narrow column reads as one quiet list rather than a stack of
  sections. At 900px and below, the sidebar stacks under the main column, and
  its first block takes the rule too. The Goal's Why it matters is part of the page
  head (below). It sits on the canvas with no rule.
- **Goal page head (`.gp-head`):** read top to bottom, it is the title (H1),
  then the Why it matters, then one metadata line. The warning banners (Ownerless,
  Check-in overdue, Path to Green overdue, Incomplete) stay above it, and the
  actions sit top right.
  - **Why it matters lead (`.gp-lead`):** larger body text, 18 / 1.55 in
    `--color-ink`, at most 720px wide. No "Why it matters" heading shows; a visually
    hidden one (`.sr-only`) labels it for assistive technology, and a small
    "What goes here?" help button under it opens the definition in place.
  - **Metadata line (`.gp-meta`):** the Health badge leads, then plain
    `--color-ink-muted` text separated by `·`: Lifecycle, Dated or Ongoing,
    Owner (the usual person control), the delivery date with any slipped dates
    struck through when Dated, the Check-in cadence, and Top-level when it
    applies. Only Health is a badge; Dated, Ongoing and Top-level are help
    buttons (see Help on terms). The line wraps on narrow screens.
    Dimension values and Fields stay in the sidebar, never the head.
  - **Help on terms:** a glossary term a reader may meet cold carries a
    `disclose help` button that opens a one-line definition in place
    (`.explain`), with the same text as the button's `title` so it is still
    reachable without script. Why it matters has one under the lead; Top-level, Dated
    and Ongoing are the word in the metadata line itself; Delegates and
    Dimension values have one under their sidebar headings; and the Report
    builder's Also include and Leave out have one after each legend. Ownerless
    badges on the Goals list, the Goal page's link lists and the Follow-ups table
    carry the definition as a `title` only, because a button in every row
    would be too heavy.
  - **Header actions (`.gp-actions`):** top right, for whoever may check in
    (the Owner or a Delegate), a primary **Check in** link to the Goal's
    Check-in page and a quiet **No change** (`.btn.quiet`). Beside them sits
    the action menu. Anyone else sees only the menu.
  - **Action menu (`.gp-more`):** a secondary button reading "More ▾", named
    and titled "More actions". It is a `<details>`, so it opens without
    script, and its panel (`.gp-menu`) is a card anchored right under the
    button. It lists actions, never forms: each item is a plain link, and each
    person sees only the actions they may take. When there would be none, the
    menu is left out. For whoever may check in, the menu also offers, after
    the other items, the Lifecycle changes the Goal allows: **Put on hold…**,
    **Mark done…** and **Cancel…** on an Active Goal, **Cancel…** alone on an
    On Hold one, none otherwise. Unlike the other items, these lead to the
    Check-in page with that change picked and its section open
    (`/goals/{id}/checkin?lifecycle=<Lifecycle>`), for a Lifecycle change is
    made in a Check-in.
- **Forms that open in place (`.gp-open`):** a Goal page action's form opens
  in the part of the page it concerns, never in a modal, side panel or the
  menu. A menu item or sidebar link leads to `/goals/{id}?open=<form>`, which
  is the Goal page with that one form open and its first control focused. The
  form sits under a `.label` heading with a quiet **Cancel** link back to the
  plain page. A refused submit comes back as the same page with the same form
  open, the reason in an alert at its top and what was typed still in it. It
  is all links and plain form posts, so it works without script.
  - **Where each opens:** Edit title and Why it matters, for the Owner or an Admin,
    and Top-level open in the head, under the metadata line. Edit title and Why it
    matters holds the title and the Why it matters as they stand, with a Save.
    Hand off, Reassign, Mark owner departed and Mark returned open in People,
    under the Owner. Delegates and Contributors forms open in their groups.
    Link to a parent Goal opens under Contributes to, and the Dimension and
    Field forms in their blocks. Without script, the Draft Highlight form
    opens in the Draft Highlights block, from its **+ Log a highlight** link
    beside the heading, and each pending one has a **Delete**; with script
    the block is the Highlight editor instead (see Highlights under the
    Check-in form). A few menu items don't open in place but lead to a page of their
    own: Add a child Goal to the New goal form with this Goal picked as the
    parent (`/goals/new?parent=<id>`), Suggest a parent to its form, and the
    Lifecycle changes to the Check-in page with the change picked.
  - **Sidebar links (`.gp-group-head`):** a sidebar group whose form opens in
    place has a small link beside its heading, shown only to those who may use
    it and hidden while its form is open: **Manage** for Delegates and for
    Contributors, **Edit** for Dimensions and for Fields, **+ Add** for
    Contributes to. Each link's accessible name says what it acts on ("Edit
    Fields"). The sidebar blocks stay separate canvas blocks under rules.
- **Status cells (`.gp-cells`):** the Goal page's Latest status card opens with
  a row of cells, each a `.label` over its value: "Owner's Health" (a Health
  badge, with the Run off Green beside it in muted small text while the Goal
  is Yellow or Red: "Red · 5 wks"), "Rolled-up Health" (a Health badge, with the
  Check-in overdue children count beside it) and "Back to Green by" (a date,
  which shows a slipped date as a Milestone's does, counted within the current
  Run off Green: "Oct 3 ~~Sep 12~~ (3)"). A cell shows only when it has
  something to say: no Rolled-up cell without Active children and no Back to
  Green cell without a Path to Green. The cells wrap, 32px apart. The status
  text, the Path to Green and the "why this differs" explanation follow below.
  An open Ask for help sits directly under the Path to Green: a bold "Asks for
  help", then "of [Name]", or "· no one above the Owner", then an **Answer**
  link to the Goal's Comments.
  A Goal with no Health shows its message in place of the cells.
- **History timeline (`goalHistory` in `timeline.templ`):** the Goal page's
  History is one canvas block, open on load with no outer disclosure. It is
  a single list, newest first, of every Check-in, Why it matters revision, title
  change, ownership change (a Handoff with its outcome, or an Admin Reassign), change to a
  Dimension value or Field, change to one of its "contributes to" links,
  Parent suggestion, Nudge, Milestone added outside a Check-in, and Comment
  or reply about the Goal, made on the Goal itself or on a Report.
  - **Weeks:** entries are grouped by week, Monday to Sunday in the company's
    timezone. Each week is headed by a `.label` "Week of Sep 28", with the
    year added when it isn't this year. Each entry's time is a timestamp in
    that timezone too ("Jan 8, 15:04"; see Dates and times).
  - **The rule and markers (`.tl-list`, `.tl-mark`):** the week's entries hang
    off a 1px `--color-border` rule, each with a 12px marker on the rule.
    A Check-in's marker is its Health's dot, in the Health's ink and shape:
    ● Green, ▲ Yellow, ■ Red. It reads without colour, and its badge names the
    Health beside it. Every other kind has a neutral `--color-ink-muted`
    marker with its own shape: a hollow ring for Why it matters and for a title
    change, a hollow diamond for
    an ownership change, a bar for a value change, a hollow arch (round on
    top, square below) for a link change, a dashed ring for a Parent
    suggestion, a dashed square for a Nudge, a double ring for a Milestone
    added outside a Check-in, a hollow tab (square on the left, round on the
    right) for an Action Item raised, reassigned or closed, a hollow
    speech-drop (round but for its bottom-left corner) for a Comment or
    reply, and a hollow square for a Check-in with no Health, such as one
    that put the Goal On Hold. Every entry also opens with its kind as a bold
    text label (Check-in,
    Why it matters, Title, Handoff, Reassign, Value, Link, Parent suggestion, Nudge,
    Milestone, Action Item raised, Action Item reassigned, Action Item
    closed, Comment for a thread's first Comment, Reply for a reply), so the
    marker is never the only signal.
  - **A title entry** reads "*old* → *new*", then who changed it, by Name.
  - **A Check-in entry** shows its Health badge and status, then any
    Lifecycle change, Path to Green, an "Asks for help" line when it marked
    its Path to Green as asking, and the Date Slips it recorded (the
    delivery date or a Milestone, ~~old~~ new, and the reason). Last comes who
    wrote it, by Name, "for" the Owner when a Delegate wrote it. Its Metric
    readings and its explanation of a Health that differs from the Rolled-up
    Health sit in a small "Readings and explanation" disclosure on the entry.
  - **A folded "No change" entry (`historyFolded`):** a Check-in that
    repeats the entry just before it — another Check-in with the same Health,
    status, Path to Green, target date, ask for help and explanation, so
    ticking or unticking the ask never folds — while recording
    nothing more (no Date Slip, Milestone or Lifecycle change, Highlight,
    discarded Draft Highlight, or Metric reading that differs from the one
    before) is a repeat, as a No change Check-in is. The first Check-in of a
    run shows in full; the repeats after it fold into one Check-in entry at
    the newest one's place and week, reading "No change", or "No change ×3"
    for several, beside the Health marker and badge it repeats and the newest
    one's time. A small disclosure on it lists each folded Check-in's time and
    who wrote it, "for" the Owner when a Delegate did. Any other entry between
    two Check-ins keeps them apart, so folding follows the filter: two
    identical Check-ins with a Why it matters revision between them show in full
    under All and fold under Check-ins. Chips still count every Check-in.
  - **A link entry** is on both Goals' timelines, phrased for the Goal it's
    on, with the other Goal's title as a link: "Requested to contribute to
    *Reduce outages*" on the child is "*Migrate displays* asked to contribute
    to this" on the parent. An Undo reads "Undid:" before the change it
    undid. Who made the change follows, by Name.
  - **A Comment entry** shows the Comment, then a muted line saying who wrote
    it, by Name. One made on a Report adds " · in" and the Report's name,
    linking to the Goal's block in that publication.
  - **Health strip (`healthStrip`, `.hs`):** under the History heading, above
    the filter chips, the Goal's Health over its last 11 Check-in periods, one
    cell for each, oldest on the left and the current period on the right. A
    period is the Goal's cadence long (a week by default), counted in days of
    the company's timezone, and the current one ends on the Sunday that closes
    this week, so a weekly Goal's cells are the weeks History is grouped by.
    How a period ended decides its cell: the last Check-in made in it. If that
    Check-in left the Goal Active, the cell takes its Health: the
    Health's `--health-*-bg` fill with its dot in the Health's ink and shape
    (● Green, ▲ Yellow, ■ Red), so it reads without colour. A finished period
    the Goal was Active with no Check-in is an empty cell in a 1px dashed
    `--checkin-overdue-edge` border, the edge Check-in overdue uses, so a
    skipped week shows at a glance. Only a finished period can be missed: one
    whose last day is today or later (the one in progress, and any wholly ahead
    when the cadence is shorter than a week) that the Goal is Active in with no
    Check-in yet is "not yet due", an empty cell in a 1px solid
    `--color-border-strong` border, the edge of an input, a slot waiting to be
    filled. A period before the Goal became Active, or one it spent or ended On
    Hold, Done or Cancelled, owed no Check-in and is blank, with no edge. Cells
    are 24px tall, share the row 4px apart up to 480px wide, and take
    `--radius-sm`. Each cell's text equivalent is visually hidden inside it and
    repeated as its tooltip: "Week of Sep 28: Yellow", "Week of Sep 21: no
    Check-in", "Week of Oct 5: not yet due", "Week of Sep 7: On Hold", or "not
    yet Active". A Goal on another cadence names a period by its first and last
    days ("Sep 14 – Sep 27"). A muted line under the strip sums it up, counting
    the periods not yet due apart from the missed ones, and naming them only
    when there are any: "Last 11 weeks: 7 Green, 1 Yellow, 2 with no Check-in, 1
    not yet due." The strip shows for any Goal that has been Active, and it
    changes nothing: not Check-in overdue, not Health.
  - **Filter chips (`.tl-chips`):** a row of `--radius-full` chips above the
    list, wrapping on narrow screens: All, Check-ins, Date Slips, Why it matters,
    Title, Ownership, Links, Values, Action Items, Comments. Each carries its
    count.
    Check-ins counts and lists the Check-ins, the Nudges asking for one, and
    the Milestones added outside a Check-in; Date Slips the Check-ins that
    carry one; Links the link changes and Parent suggestions; and Action Items
    each Action Item about the Goal raised, reassigned and closed; and Comments
    every Comment and reply about the Goal. A chip is a plain link to
    `/goals/{id}?history=<filter>#history`, so the page reloads with the
    filter in the address and the History block in view. The current chip is
    `aria-current="page"`, with a `--color-primary-light` fill and a
    `--color-primary` edge, and the rest sit on `--color-surface`. Chips are
    32px tall, and 44px on a coarse pointer.
  - **Paging:** the latest 5 entries show, a folded run counting as one.
    When there are more, a quiet **Show earlier** link (`?shown=25`) reloads
    the page with 20 more under the same filter. Chips and paging are links,
    so History works without script.
- **Check-in page (`checkinPage` in `checkins.templ`):** one form read top
  to bottom under headings, its sections set apart by whitespace, not boxes.
  - **Header (`.checkin-head`):** under the breadcrumb, the H1 "Check in" and
    a muted line, "Week of Oct 19 · checks in every 7 days · last check-in
    Oct 5" (the week named by its Monday in the company's timezone, as
    History's weeks are; "every day" for a daily Goal; the last Check-in only
    when there is one). At the header's right, bottom-aligned and wrapping
    under the title on a narrow screen, sits No change: a muted "Nothing
    changed since Oct 5?" beside a secondary **No change** button, or the
    reason it is refused in its place.
  - **Add to this check-in (`.checkin-add-bar`, `checkinAddBar`):** with
    script, directly under the header and above the form (and the column
    beside it), spanning the page's width: a white panel (`--color-surface`,
    1px `--color-border`, `--radius-lg`, 12px 16px padding), a labelled group
    (`role="group"`, not a toolbar). In a row, 8px apart and wrapping: the
    label "Add to this check-in" (13px, 600, `--color-ink-muted`);
    **+ Insight**, **+ Accomplishment** and **+ Miss**, filled in
    `--color-primary-subtle` with `--color-primary-strong` text; a 1px by
    24px `--color-border` divider; then **Move a date or Milestone** and
    **Pause, finish or cancel** ("Resume or cancel" for an On Hold Goal),
    outlined (no fill, `--color-border` edge, `--color-ink` text). All are
    `btn sm` with `--radius-full` corners and one label at every width. The
    Highlight buttons stand for the Highlights section's own: each scrolls to
    it and opens its editor there, and they wait while an editor is open.
    The other two each show their section, scroll to it and move focus to
    its heading; once their section shows, on load or by a press, they are
    used: `--color-surface-alt` fill and `--color-ink-muted` text, still
    pressable to go back to it. A Highlight button is never used, as a
    Check-in may carry several. At 600px and below the label sits above the
    buttons, which keep to one line that scrolls sideways inside the bar, the
    divider hidden. Without script the bar doesn't show.
  - **Sections:** "How is it going?" (the three Health choices at full width,
    56px tall with `--radius-lg`; then "What changed since the last
    check-in?"; then the Rolled-up Health panel), "Path to Green" (shown while
    Yellow or Red is picked) and "Metrics" (when the Goal has any). Each is
    headed by an `<h2>` in the display face, 22px and 700, 20px at 600px and
    below, with no step number, and a muted 14px note on its baseline:
    "Required while Yellow or Red", with " · 3 wks off Green" while the Goal
    is in a Run off Green, or "Record each Metric's current value". Sections
    sit 40px apart, wider than the usual 20–32px because the form is long;
    fields within one sit 16px apart. Field labels are 14px and 600, hints
    14px in `--color-ink-muted`.
  - **Rolled-up Health panel (`.checkin-rollup`):** the page's one white
    panel (`--color-surface`, 1px `--color-border`, `--radius-lg`, 16px
    padding, no shadow): a muted "Rolled-up Health:", the Health badge and its
    muted count line ("1 of 5 Red · 1 Yellow"), then "Explain the difference
    from Rolled-up Health", asked whenever there is a Rolled-up Health.
  - **Path to Green:** Plan; Back to Green by, with "Last time: Oct 3" and "A
    later date needs a reason." beside the date on one wrapping row; then the
    checkbox, "**Ask for help** · goes to <Name>".
  - **Metrics (`.checkin-metric`):** each Metric is a row between 1px
    `--color-border` rules, shared by neighbours, 16px above and below. On
    the left, its name in 600 over a muted "per week · 100 → 500 by Jun 30 ·
    last 135.5" and, once it has a reading, a 6px progress bar from baseline
    toward target in `--color-primary-strong`. On the right, a 120px reading
    input with right-aligned numbers in tabular figures.
  - **Optional sections** follow, as below (Check-in form's optional
    sections), then the submit bar: with script, 40px apart like the rest,
    each a plain headed section; without, folded panels 12px apart.
  - **Submit bar (`.checkin-submit-bar`):** the form's last child, sticky at
    the bottom of the window (`position: sticky; bottom: 0`) while the form
    runs past it, and in its own place at the form's end once scrolled
    there, so it never covers the last field. It is as wide as the form
    column, not the window. Flat, like a card: `--color-surface` under a 1px
    `--color-border` top rule, no shadow, 12px 16px padding. The page's
    `scroll-padding-bottom` keeps a field focused by Tab or an error clear of
    it. On the left, a summary of what the Check-in will record, in 14px
    `--color-ink-2`, its parts joined by " · " and wrapping whole: the
    Health badge picked (left out when the Lifecycle picked takes the Goal
    out of Active), "Path to Green" while Yellow or Red has a Plan, "2
    Metrics" read, "1 Highlight" written or kept, "dates changed", and the
    Lifecycle change ("→ Done"). Script fills it and keeps it up to date;
    without script the bar holds only the buttons. On the right, a quiet
    **Cancel** back to the Goal and **Submit check-in**, primary and 44px
    tall. At 600px and below Cancel hides (the breadcrumb goes back to the
    Goal), the summary drops to 13px and Submit check-in grows to 48px.
- **Home (`homePage` in `home.templ`):** one column, read top to bottom: the
  page head, then **Needs you**, its one card, then My org and Your Yellow
  or Red goals, plain sections on the canvas (§ System: Cards are earned).
  All three are page sections headed by an `h2`, Needs you's on its card, so
  the outline reads Your week (H1), then an `h2` per section, then Needs
  you's `h4` group labels. Home sets these `h2`s at the H3 card-title size
  (19px, 600), so the headings stay no louder than the rows under them. A
  plain section is its `h2` over a compact list (`.hm-section`), its first
  row ruled off from the heading by a hairline as each row is from the next,
  so the context sections stay quieter than the card.
  - **Page head:** **Your week** (H1), and under it the Your goals line
    (`.hm-summary`), with **New goal** on the right. The line reads "Your
    goals:", the Health distribution bar, then plain muted text, "1 Green ·
    0 Yellow · 0 Red · Goals you own or help with →", counting the Goals the
    person Owns, a Check-in overdue one under its last Health. **Goals you own
    or help with →** opens the Goals list filtered to Mine (`/goals?org=mine`):
    the Goals the person Owns or is a Delegate on. For someone who is a Delegate
    on any Goal, the line ends "· Goals delegated to you N →", leading to the
    Delegate page. The head carries no count of what needs the person: the
    Needs you heading does.
  - **Your Yellow or Red goals:** a plain section after My org listing the Goals
    the person Owns that are Red, then Yellow, each its Health badge, muted
    and dated "as of 9 days ago" when Check-in overdue, then its title. With
    none, the section is left out.
- **Home's Needs you list (in `homePage`):** the page's one card, holding
  everything waiting on the person, titled with how many things that is
  ("Needs you · 2", plain "Needs you" with nothing waiting), in four
  subgroups, each a `.label` heading over ruled rows (`.hm-list`):
  - **Check-ins due:** the Goals the person owes a Check-in on, most overdue
    first. Each row leads with the Goal's title, linking to the Goal, over
    one muted status line (`.hm-status`): the Health badge, "last check-in
    14 days ago" ("no check-in since activation 9 days ago" before the
    first), "every 7 days", the Check-in overdue mark when it applies and, for a
    Goal they are a Delegate on, a "for [Owner]" tag. The age is said once, and
    the age and the cadence each wrap whole. A secondary **No change** and a
    primary **Check in** sit on the right. A Check-in overdue Goal's Health is
    its last Check-in's, so its badge is muted (`.badge.as-of`, Home's own rule:
    card fill, muted ink, a 1px `--color-border` edge), keeping the Health's
    name and dot shape; the "last check-in" beside it dates it. A Goal that
    isn't Check-in overdue, or has no Health yet, keeps its full-colour badge. A
    Goal they are a Delegate on appears only here.
  - **Asks for help:** the open Asks for help addressed to the person (see
    CONTEXT.md: Ask for help), Red first, then Yellow, then by Goal title.
    Each row has the Goal's Health badge, then its title linking to the
    Comments on its Goal page (`#comments`), where the answer is written,
    then a muted line "from [Owner]". It has no action of its own: the row
    stays until the Owner unticks the ask in a later Check-in or the Goal is
    no longer Yellow or Red and Active, however it is answered.
  - **Action Items:** the open Action Items the person owns, from every
    Report, soonest due first, so an overdue one leads. Each row has a
    `badge lc` reading "Action Item", the item's text linking to the Action
    Items on the Goal page of the Goal it is about, or on its publication
    when it is about the whole Report, then a muted line naming its Report,
    its Goal, if any, and its due date. An overdue one carries a red
    **Overdue** badge (`badge r`). Its action is the Report page's **Close**:
    a small button opening a closing note, which closes the item and returns
    to Home. Home carries its own copy of the `.rp-inline-form` rules for it.
  - **Requests:** link requests and Handoffs waiting on the person, oldest
    first. **Reject** is a quiet text button (`.btn.quiet`) and **Accept** a
    secondary, outlined one (`.btn`). Reject sends at once and returns to
    Home, which offers Undo in the toast. Where accepting needs choices, as a
    Handoff of a Goal with Delegates does, the outlined button reads
    **Review** and leads to the Pending handoffs page, where those choices
    are made.

  A subgroup with nothing in it is left out, and with all four empty the card
  shows one muted "You're all caught up." line. The heading's count and the
  nav's Home count both count every row. Goals the person is a Delegate
  on aren't listed again elsewhere: the Your goals line has one link to the
  Delegate page with their number, shown only above zero.
- **Home's My org section (`homeOrgCard` in `home.templ`):** shown only to
  someone with anyone under them by Manager, so never before a directory sync
  (ADR 0008). It sits directly under Needs you, a plain section titled **My
  org**, and counts what is in trouble in the person's Org, leaving out the
  Goals they Own themselves, which Your goals covers. A Goal is in the Org by
  its Owner, never its Delegates, and an Ownerless Goal stays where its
  Departed Owner was. It lists people and counts, never Goals:
  - **Total line:** across everyone under the person, plain text, one count
    per signal with anything in it, in the order Red · Path to Green overdue
    · Check-in overdue · Yellow, separated by `·` ("2 Red · 1 Path to Green
    overdue · 3 Check-in overdue · 1 Yellow"). It is text, not badges, so the
    totals stand apart from the per-person badges beneath. A Goal counts under
    every signal it has, so the counts can add up to more than the Goals behind
    them.
  - **Rows (`.hm-list.hm-compact`):** one per person directly under the
    viewer whose Org has anything flagged: the person's name, a link to their
    Org on Goals (`/goals?org=<their ID>`, the name through `personPlain`,
    email on hover), a `badge lc` "departed" beside a Departed one, then
    their Org's badges in the same order. Each reads as a count and a name
    ("1 Red"), so it reads without colour. Red and Yellow take their
    Health's badge; Path to Green overdue and Check-in overdue take the Check-in
    overdue mark's `badge st`, as on Follow-ups. The most Red come first, then the
    most Path to Green overdue, the most Check-in overdue and the most Yellow,
    then by Name. Someone further down counts in the row of the person directly
    under the viewer they're under, so the rows add up to the total.
  - **All clear:** the people directly under the viewer with nothing flagged
    share one muted line after the rows, "2 others all clear" ("1 other all
    clear"), left out when there are none. With nothing flagged anywhere,
    the section shows only one muted "Everyone in My org is all clear." line.
  - **Links:** the section ends with one line, **My org on Goals →** ·
    **My org on Follow-ups →** (`?org=<viewer ID>`), whose pages include the
    person's own Goals.

  The section adds nothing to Needs you or the nav's Home count.
- **Scope (Goals and Follow-ups, `pageScopeSelect` in `scope.templ`):** the one
  select scoping the Goal list and the Follow-ups page, submitted as `?org=`:
  **Everyone** (`all`), **Mine** (`mine`, the Goals the person Owns or is a
  Delegate on), **My org** for someone with anyone under them, then each other
  person's Org under **Other orgs** (their ID).
  - **The default scope:** an address naming no scope, or one that is
    malformed or names no one, opens on the person's default: My org for
    someone with anyone under them, Mine for everyone else, Admins included.
    The select shows it chosen, and the count lines and empty states read as
    for that scope chosen. A bare `/goals`, `/goals/download` or `/follow-ups`
    therefore means "my default", and links meant to land there stay bare
    (the Goals breadcrumb, Back to Goals, the Nudge page's Back to Follow-ups).
  - **Everyone is explicit:** `?org=all`. Goals' **Clear** and Follow-ups' **Show
    everything** link to it, and a page on Everyone keeps it in every link
    that keeps the page's filters. A chosen scope is never overridden.
  - **An empty default:** the Goal list says "No Goals in My org." or "No
    Goals in Mine." with a **See everyone's →** link to the same view with
    `?org=all`. Follow-ups keeps its own empty line, whose Show everything is the
    way out.
  - **Needs an Admin is company-wide:** an Admin's job there is the company's,
    so the Needs an Admin card counts, and chips, every Ownerless and
    halted-parent Goal in the company under the page's Dimension values,
    while the other cards count the page's own rows. Its card keeps the
    page's scope in its link like every other card, so Show all and the other
    cards come back to it. `/follow-ups?group=admin` always lists the company's
    rows whatever the scope, as its card counts them, heading "N Goals need an
    Admin across the company." or saying "Nothing needs an Admin." with no
    Show everything. An Admin whose own scope has nothing flagged still sees
    the cards after the empty line while the company has a Goal that Needs an
    Admin.
- **Health distribution bar (`.hm-bar`):** on Home's Your goals line, a 96px
  × 8px `--radius-full` bar of the person's own Active Goals by Health, one
  equal unit per Goal, Green then Yellow then Red. Units fill with the
  Health's `--health-*-ink`, which keeps 3:1 on the canvas in both themes,
  and a 4px gap shows where one Health's run gives way to the next. It counts
  Goals and never aggregates Metrics (ADR 0003). Goals with no Health yet
  aren't drawn, and with none to draw the bar is left out. The counts beside
  it on the line are its labels, so it reads without colour, and its
  `aria-label` ("2 Green, 0 Yellow, 1 Red") says the same to a screen reader.
- **Inputs:** `--color-surface`, 1px `--color-border-strong`, `--radius-md`,
  12px × 16px padding. On focus, the border becomes `--color-focus` and the
  field takes the same ring as everything else, `2px solid var(--color-focus)`,
  offset 1px. The ring shows on mouse focus too and never transitions. See
  Deviations.
- **Choice cards (`.gf-segment`):** the New goal form's Kind cards, Dated
  and Ongoing, side by side (stacked at phone width), with neither chosen at
  first. Each card draws its own radio dot. An unchosen card is an outline: a
  2px `--color-border-strong` edge, no fill, and an empty dot with a 2px
  `--color-border-strong` ring. The chosen card has a 2px `--color-primary`
  edge, a `--color-primary-subtle` fill and a filled `--color-primary` dot. The
  edge is 2px either way, so the card keeps its size. The Dated card holds the
  Delivery date, shown once Dated is chosen, its white input on the card's
  fill. Keyboard focus takes the usual ring. All of this needs `:has()`. A
  browser without it shows the native radio and the date, and the radio still
  chooses.
- **Segmented control (`.gf-segctl`):** the New goal form's Check-in cadence,
  Weekly | Every 2 weeks | Monthly | Custom, one radio each. A 1px
  `--color-border-strong` edge and `--radius-md` go round the whole control,
  and 1px `--color-border` lines divide the segments. The chosen segment fills
  with `--color-ink`, its text `--color-ink-inverse` in 600 weight. The focused
  segment takes the usual ring. The native radio hides only with `:has()`. At
  phone width the control becomes a 2×2 grid the width of the form.
- **New goal page (`goalFormPage`):** no outer card. The form's column is
  sections on the canvas, 48px apart: the Title and Why it matters with no heading
  (a muted "New goal · Proposed until it's activated" line holds the `<h1>`,
  and the Title is a 34px display-face input on a 2px `--color-primary-strong`
  underline), then **Delivery** and **How you'll know** as 22px `<h2>`s, then
  **More detail**, an 18px `<h2>` over a 1px `--color-border` rule. More
  detail's Contributes to and Where it fits are `<details>` rows, one 48px line
  each: the name, a muted "· " and what it holds, and **Set** or **Change** at
  the right. A row opens by itself when it holds a refused input or, for Where
  it fits, a required value not set. The sticky Ready to activate card beside
  it shows each item's mark only, ✓ in a `--health-g-ink` circle or ○ as an
  empty `--color-border-strong` ring, its done or to do for screen readers
  alone. The Finish defining page is the same page.
  - **How you'll know (the Milestone and Metric editor):** with script, it
    starts as two quiet small buttons, **+ Milestone** and **+ Metric**, the
    Highlight editor's add buttons, and no blank rows. Each opens one inline
    editor below the items, in the Highlight editor's look
    (`itemEditorStyles`: the bordered box, its 15px heading, the Save and
    Cancel row and the item row): "New Milestone" asks for Name and Date, the
    Name two-thirds of the row; "New Metric" for Name, Unit, Baseline, Target,
    Target date, and Direction only while the target equals the baseline, two
    to a row. **Save** (`.btn.primary.sm`) stays disabled until the server
    would take what's typed, Enter in a field presses it, and **Cancel**
    (`.btn.sm`) drops a new editor or puts back the item it edits. While an
    editor is open the add buttons and every Edit wait, and the Ready to
    activate card's How you'll know item reads "Save the Milestone you
    started" (or Metric), to do, linked to the editor. Save makes an item: its
    name, then a muted line in body type, not mono, with a Milestone's date or
    a Metric's "baseline → target unit by date", and quiet small **Edit**
    (reopening the editor in place, "Edit Milestone") and **Remove**. The
    "Milestones" and "Metrics" `<h3>`s are `.label` caps, shown only once
    their section has an item; a Finish defining Goal's own Milestones and
    Metrics stay a read-only list above. Saving records nothing: the items are
    sent with the form, and an editor still open is sent as if saved. A
    refused submit brings a refused row back as an open editor showing why.
    **Without script,** How you'll know is rows as before: a blank Milestone
    row and a blank Metric row, or those typed.
- **Check-in page's column (`.checkin-context`):** every Goal's Check-in
  page has a column of context for checking in, three sections in order,
  separated by 1px `--color-border` rules with 20px above and below, and no
  boxes:
  - **Last time (`.checkin-context-last`):** a `.label` heading "Last time ·
    Oct 5", the latest Check-in's Health badge and a muted "back to Green by
    Nov 2" when it had a Path to Green, then its Status in full, 14px in
    `--color-ink-muted`. Left out before the Goal's first Check-in.
  - **Contributed to by (`.checkin-context-children`):** with children only,
    the Goal page's "Contributed to by" list read-only (no Remove), so the
    Owner sees which children set the Rolled-up Health.
  - **Dates (`.checkin-context-dates`):** a `.label` heading "Dates" and a
    14px line, "Delivers Jul 2 · 2 planned Milestones, next Beta Apr 2", or
    "Ongoing · no Milestones" (`checkinContextDates`). It strikes no slipped
    date and marks no overdue Milestone; the form's dates section does.

  Above 900px the column is 240–320px on the right of the form, 32px away,
  under the breadcrumb, header and add bar, and sticky (`top: 24px`), scrolling within
  itself when it is taller than the window. At 900px and below it sits above
  the form, after the header and add bar, and doesn't stick. At 600px and below it drops
  Last time and Dates, which the form's ghost text already covers, so only
  Contributed to by stays, and a Goal with no children shows no column.
- **Check-in form's optional sections:** Highlights, dates and Milestones,
  and Lifecycle each sit in a `<details>` (`.checkin-more`) that starts folded
  and opens by itself when it holds an error or something typed.
  - **With script** (`checkinSectionsScript`, which marks the page
    `.checkin-js`), no summary and no ▸/▾ shows. The Highlights section always
    shows. The dates and Milestones section and the Lifecycle section show
    only while their `<details>` is open, as a plain section with no panel,
    under their own heading in the form's section-heading look (22px, display
    face, 700): "Dates and Milestones" and "Lifecycle". The add bar's buttons
    open them. The server's rules still open them on load, and their bar
    button shows as used: an error in the section, something typed in it, an
    overdue Milestone, `?open=dates` (a Schedule conflict's Fix on Follow-ups), and
    a Lifecycle picked by `?lifecycle=` (the Goal page's menu). A shown
    section never hides again; its fields are sent whether it shows or not.
    The script runs again on the form a refused submit swaps back, so the bar
    marks used whatever then shows.
  - **Highlights (the Highlight editor, `highlighteditor.templ`):** with
    script, the Highlights section isn't folded. It is a 22px `<h2>`
    "Highlights" with a muted "A Report author can pull each one in" on its
    baseline, then three quiet small buttons, **+ Insight**,
    **+ Accomplishment** and **+ Miss** (`highlightAddButtons`), then the
    Highlights so far. An add button opens one inline editor above them,
    headed "New Miss" (or Insight, or Accomplishment), asking only for the
    Note, with **Save** (`.btn.sm`) and **Cancel** (`.btn.quiet.sm`). Save
    stays disabled until the Note is filled, and while an editor is open the
    add buttons and every Edit wait. Save makes it an item: a bordered row
    with the kind in bold and the full Note, as the Goal page lists
    Highlights ("**Miss**: …"), and quiet small **Edit** and **Remove**.
    Edit reopens the editor in place with a Kind select, so a wrong button
    can be corrected, and Cancel puts the item back as it was. Draft
    Highlights come in as items already saved, under a muted "Draft · logged
    by <Name>" line. Removing one discards it with the Check-in. A Draft
    logged with no Kind comes in as an open editor with the Kind select and
    Remove, not Cancel. Saving records nothing: the Highlights are sent with
    the Check-in, and an editor still open with a Note is sent as if saved.
  - **Without script,** the page has no add bar and no section headings,
    and each section is a folded panel opened by its summary ("Change dates
    or milestones · …", "Pause, finish, or cancel this goal · …"). The
    Highlights section is as before: a folded
    `<details>`, "Flag a highlight (optional)", with a numbered row of Kind
    and Note per Highlight, a Keep box on each Draft's row, and **Add
    another**, which posts the form back for one more row. The script
    upgrades that markup in place, so the server renders the same form either
    way.
  - **On the Goal page,** the Draft Highlights block uses the same editor.
    With script, **+ Log a highlight** beside the heading is a quiet small
    button that opens the editor at the top of the list, headed "New Draft
    Highlight", with the Kind select (optional here: a Draft Highlight may
    have no Kind) and the Note. **Save**, enabled once the Note is filled,
    logs it at once with htmx and the block comes back from the server with
    it listed, with no page load; **Cancel** closes the editor. Each pending
    one gains a quiet small **Edit**, which opens it in the editor in place
    ("Edit Draft Highlight"), its Save posting the edit and its Cancel
    putting the row back, and its **Delete** reads **Remove** and deletes it
    at once, with no confirmation. A refused Save comes back with the editor
    open, the reason under the Note and what was typed kept. One editor is
    open at a time. Without script the block is as before: the link opening
    the log form in place, and **Delete** on each row.
- **Sizes and touch targets:** buttons and text, email, date, number and search
  inputs and selects are 40px tall, and `.btn.sm` is 32px. On a coarse pointer
  (`@media (pointer:coarse)`) all of them get a 44px `min-height`, so every
  touch target is at least 44px and a button beside an input still shares its
  height. A small button keeps its smaller type and padding. Textareas,
  checkboxes and radios are left alone.
- **Phone width (600px and below):** nothing scrolls sideways, and no control
  is clipped. The Goal list's Table layout, a wide data table and the
  Check-in page's add bar buttons may scroll inside their own container. List tables stack each row as a card, the Goal
  or name first (Follow-ups, the Reports list, On track, the Goal list). The Goal
  list's phone rows show the Goal, then Health and Owner, then Due and Last
  check-in (each led by a small muted label), with Dimension tags hidden. A
  card head's badges drop under its title. Filter bars and field-plus-button
  forms wrap.
- **Tables:** headers on `--color-surface-alt` with `.label` type. Rows are
  divided by `--color-border`. Selected rows use `--color-primary-light`.
- **Tags (`.tag`):** `--radius-full`, outlined with no fill: a 1px solid
  `--color-border-strong` edge and `--color-ink` text. A retired Dimension
  value's tag takes a dashed edge instead, struck through and muted.
- **Check-in overdue chip (`.badge.st`):** marks a Check-in overdue Goal
  wherever its Health shows (the Goal list, Home, a Report), with its age
  ("Check-in overdue · 9 days" on the Goal list; on Follow-ups, with its cadence too:
  "Check-in overdue · 42 days, checks in weekly", naming the cadence as the New
  goal form's segments do, or "every 3 days"). It is a warning about freshness,
  never a tag, so it can't share a tag's look: a `--checkin-overdue-bg` grey
  fill, `--checkin-overdue-ink` text and a 1px dashed `--checkin-overdue-edge`
  border, at a Health badge's `--radius-sm`. The Goal list's "Path to Green
  overdue" chip takes the same style, as does Follow-ups' ("Path to Green 12 days
  overdue"). So does the "Milestone overdue" chip on a Green Goal past a
  Milestone's date: "Milestone overdue" on the Goal list, "Milestone 3 days
  overdue" on Follow-ups.
- **Check-in overdue banner (`.alert.st`):** at the top of a Check-in overdue
  Goal's page, a `--color-surface` banner in a 1px dashed
  `--checkin-overdue-edge` border, with `--color-ink` text: **Check-in
  overdue**, then how long it has gone without a Check-in against its cadence.
  For whoever may check in (the Owner or a Delegate) it ends, at the right, with
  a **Check in now** link to the Goal's Check-in page. Anyone else sees no link.
  The "Path to Green overdue" banner beside it takes the same style with no
  link, because CONTEXT.md surfaces both the same way.

  The ratios are in § Deviations 9.
- **Nav counts (`.count`):** a count after a nav item's label, at the item's
  right edge, shown only above zero. Two styles, so the one that needs the
  person stands out:
  - **Home's count (`.count.needs-you`)** counts what needs the person, so it
    is a filled `--radius-full` pill: `--color-ink` with `--color-canvas`
    numbers, dark on light and light on dark.
  - **Every other count (`.count`)**, such as Follow-ups, is muted
    `--color-ink-muted` tabular figures with no pill.

  The ratios are in § Deviations 2.
- **Health badges (`.g` / `.y` / `.r`):** `--health-*-bg` fill with
  `--health-*-ink` text; see Deviations. They keep their dot
  shapes (circle, triangle, square) so color is never the only signal. A list
  that shows the shape alone draws it in `--health-*-mark` (§ Direction:
  Components in the frame).

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
- The app serves Source Serif 4 itself, from `internal/web/static/fonts/`, by
  `@font-face` rules in the print page's own `<style>`: a variable font,
  upright and italic.

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
   and teal is kept for links, buttons, focus and the logo mark.

   Each ink's contrast on every surface text sits on (`--color-primary-light`
   is the current choice in a menu, and `--color-primary-subtle` the hover
   fill of a quiet control and of a Name):

   | Theme | Ink | Canvas | Surface | Surface hover | Surface alt | Primary light | Primary subtle |
   | --- | --- | --- | --- | --- | --- | --- | --- |
   | Light | `--color-ink` `#1B1D22` | 16.86 | 16.86 | 15.61 | 15.75 | 14.96 | 16.17 |
   | Light | `--color-ink-2` `#33363C` | 12.11 | 12.11 | 11.22 | 11.31 | 10.75 | 11.61 |
   | Light | `--color-ink-muted` `#5F636B` | 6.03 | 6.03 | 5.58 | 5.63 | 5.35 | 5.78 |
   | Dark | `--color-ink` `#EDEEF0` | 16.40 | 15.60 | 14.52 | 16.40 | 11.60 | 16.04 |
   | Dark | `--color-ink-2` `#C9CBD0` | 11.73 | 11.15 | 10.38 | 11.73 | 8.29 | 11.47 |
   | Dark | `--color-ink-muted` `#9A9EA6` | 7.08 | 6.74 | 6.27 | 7.08 | 5.01 | 6.93 |

   In the sidebar, a nav item's label and its count reach 4.5:1 at rest, on
   hover and as the current item, and Home's count pill reaches 3:1 against
   the sidebar so its shape shows:

   | Theme | Mark | On | Ratio |
   | --- | --- | --- | --- |
   | Light | Nav item `--color-ink-2` `#33363C` | sidebar | 11.31 |
   | Light | Hovered or current item `--color-ink` `#1B1D22` | hovered item `#EEEEF0` / current item `#E8E8EB` | 14.55 / 13.79 |
   | Light | Other count `--color-ink-muted` `#5F636B` | sidebar / hovered item `#EEEEF0` / current item `#E8E8EB` | 5.63 / 5.20 / 4.93 |
   | Light | Home's number `#FFFFFF` | pill `#1B1D22` | 16.86 |
   | Light | Home's pill `--color-ink` `#1B1D22` | sidebar / current item `#E8E8EB` | 15.75 / 13.79 |
   | Dark | Nav item `--color-ink-2` `#C9CBD0` | sidebar | 11.15 |
   | Dark | Hovered or current item `--color-ink` `#EDEEF0` | hovered item `#1C1D20` / current item `#222327` | 14.52 / 13.52 |
   | Dark | Other count `--color-ink-muted` `#9A9EA6` | sidebar / hovered item `#1C1D20` / current item `#222327` | 6.74 / 6.27 / 5.84 |
   | Dark | Home's number `#0F1012` | pill `#EDEEF0` | 16.40 |
   | Dark | Home's pill `--color-ink` `#EDEEF0` | sidebar / current item `#222327` | 15.60 / 13.52 |
3. **Goal Health.** The source's success teal (`#2DD4BF`) is almost the same
   as the primary colour, so a Green Health badge would read as a button.
   Health badges therefore keep their own hues, as a pill's fill and ink and
   as the shape a list shows alone (§ Direction: Tokens):
   - Green: `#E5F3EA` / `#1C6435`, shape `#2E7D4F`
   - Yellow: `#FFF3D1` / `#7A4F00`, shape `#B27B00`
   - Red: `#FDECEC` / `#A61B1B`, shape `#C62828`

   In dark mode, the same hues sit on 20%-alpha fills with light text
   (`--health-*` in the dark block), and the shapes go bright. The
   `--color-success`/`warning`/`error` tokens are for alerts and form
   validation, not for Health. A pill's text reaches 4.5:1 on its fill, and
   the shapes' ratios are in § Deviations 10:

   | Theme | Mark | On | Ratio |
   | --- | --- | --- | --- |
   | Light | Red ink `#A61B1B` | Red fill `#FDECEC` | 6.58 |
   | Light | Yellow ink `#7A4F00` | Yellow fill `#FFF3D1` | 6.45 |
   | Light | Green ink `#1C6435` | Green fill `#E5F3EA` | 6.27 |
4. **Danger text.** `--color-error` (`#EF4444`) reaches only 3.76:1 on white,
   too low for 13–15px text. Destructive buttons and inline errors use
   `--color-danger-ink` (`#9A231B`, the Red Health ink before the flat frame)
   in light mode instead;
   `--color-error` stays for borders and icons.
5. **Focus ring.** The source rings focus in `#14B8A6`, which reaches only
   2.41:1 on the off-white canvas it had, below WCAG's 3:1 for focus
   indicators. Light mode rings focus in `--color-focus: #0D9488` instead:
   3.74:1 on the white canvas, 3.47:1 on a hovered row, its lowest on a
   surface, and 3.06:1 on the sidebar's current item. Dark mode keeps
   `#2DD4BF` everywhere.

   The source also rings a focused input in a 25%-alpha halo of the primary,
   which reaches only 1.36:1 on white. Inputs take the solid ring instead, and
   the halo token is dropped.

6. **Input borders and other non-text marks.** The source's input border
   (`#D5D2BD`) reaches 1.52:1 on white, and the dark preview's scale step 2
   (`#0A5C53`) reaches 1.87:1 on the dark surface. WCAG asks 3:1 of the edge
   that identifies a control. `--color-border-strong` is `#8A8D93` in light
   mode (3.33:1 on the canvas, 3.08:1 on a hovered row, its lowest) and
   `#6B6F77` in dark mode (3.34:1 on a hovered card, its lowest). The current
   choice's `--color-primary-light` fill holds text only, so no edge is
   measured on it. Two more marks follow the same rule:
   - A Metric's sparkline, the one data accent allowed teal (§ System), strokes
     in `--color-primary-strong`, not `--color-primary`, which reaches only
     2.49:1 on white. The Check-in page's Metric progress bar fills in the
     same token.
   - On the Check-in form, an unselected Health choice is an outline (surface
     fill, `--color-border-strong` edge, Health ink) and the selected one is
     the filled Health badge with an edge in its ink. Nothing is dimmed.

7. **Neutral canvas.** The source's light canvas is a pale yellow
   (`#FEFCE8`), and its panel, hover and rule tokens carry the same ivory
   tint. Beside it, Yellow Health's fill lost its hue, and white cards looked
   cut out of the page (UX Review). The canvas went a neutral off-white, and
   the flat frame then made it white (§ Direction: One surface), with neutral
   steps off it for the sidebar, a hovered row and a group header. The dark
   theme's teal canvas and cards went neutral near-black with it.

   | Token | Source | Now |
   | --- | --- | --- |
   | `--color-canvas` | `#FEFCE8` | `#FFFFFF` |
   | `--color-surface-hover` | `#FDFCF7` | `#F6F6F7` |
   | `--color-surface-alt` | `#F7F6E8` | `#F7F7F8` |
   | `--color-border` | `#E8E6D5` | `#EDEDEF` |

   Each light-theme text colour reaches 4.5:1, and each control edge 3:1, on
   the canvas and every card surface beside it (the frame's own surfaces are
   in § Deviations 10):

   | Mark | Canvas | Surface | Surface hover | Surface alt |
   | --- | --- | --- | --- | --- |
   | `--color-ink` `#1B1D22` | 16.86 | 16.86 | 15.61 | 15.75 |
   | `--color-ink-2` `#33363C` | 12.11 | 12.11 | 11.22 | 11.31 |
   | `--color-ink-muted` `#5F636B` | 6.03 | 6.03 | 5.58 | 5.63 |
   | Links, `--color-primary-strong` `#0F766E` | 5.47 | 5.47 | 5.07 | 5.11 |
   | `--color-danger-ink` `#9A231B` | 7.98 | 7.98 | 7.39 | 7.46 |
   | Input edge, `--color-border-strong` `#8A8D93` | 3.33 | 3.33 | 3.08 | 3.11 |
   | Focus ring, `--color-focus` `#0D9488` | 3.74 | 3.74 | 3.47 | 3.50 |

   `--color-surface-alt` takes the sidebar's grey: the warmer `#F2F2EE` it had
   would hold the new input edge at only 2.96:1.
8. **Flat cards.** The source lifts every card off the canvas on a large
   teal-tinted shadow (`--shadow-card`, deepening to `--shadow-card-hover` on
   hover), so nothing read as nearer than anything else (UX Review). Cards are
   flat instead: a 1px `--color-border` edge and no shadow, in both themes. In
   dark, the card's `--color-surface` against the darker `--color-canvas`
   sets it apart. A clickable card's hover changes its border colour alone
   (§ Components). Only menus and popovers keep the source's `--shadow-pop`:
   the person's menu, the signed-out Theme menu, the phone Menu's sheet, the
   Goal page's More menu and the toast. The two card shadow tokens are gone.
9. **Check-in overdue, apart from tags.** Check-in overdue chips and banners,
   tags and nav counts all shared the pale teal `--color-primary-light` fill, so
   "Check-in overdue" read like a label such as "Platform" (UX Review). Check-in
   overdue is now a grey chip in a dashed edge, its banners a surface in the
   same edge, and tags an outline with no fill (§ Components). The UX Review
   gave the light edge as `#8C939C`, which reached only 2.87:1 on the
   off-white canvas the banners then sat on. The edge is one step darker in
   the same hue, `#888F97`, which reaches 3:1 there and on every surface since.

   Chip and banner text reaches 4.5:1 on its fill, and the dashed edge 3:1 on
   the surfaces around it. A chip sits on a card, plain or as a hovered row. A
   banner sits on the canvas with a surface inside.

   | Theme | Mark | On | Ratio |
   | --- | --- | --- | --- |
   | Light | Chip text `#3B4048` | chip fill `#E9EAEC` | 8.67 |
   | Light | Chip edge `#888F97` | surface `#FFFFFF` / surface hover `#F6F6F7` | 3.27 / 3.03 |
   | Light | Banner text `--color-ink` `#1B1D22` | surface `#FFFFFF` | 16.86 |
   | Light | Banner link `--color-primary-strong` `#0F766E` | surface `#FFFFFF` | 5.47 |
   | Light | Banner edge `#888F97` | canvas `#FFFFFF` / surface `#FFFFFF` | 3.27 / 3.27 |
   | Light | Tag text `--color-ink` `#1B1D22` | canvas / surface / surface hover | 16.86 / 16.86 / 15.61 |
   | Light | Tag edge `--color-border-strong` `#8A8D93` | canvas / surface | 3.33 / 3.33 |
   | Dark | Chip text `#E8E8E8` | chip fill `#3B4048` | 8.51 |
   | Dark | Chip edge `#8C939C` | surface `#151618` / surface hover `#1C1D20` | 5.84 / 5.43 |
   | Dark | Banner text `--color-ink` `#EDEEF0` | surface `#151618` | 15.60 |
   | Dark | Banner link `--color-primary-strong` `#14B8A6` | surface `#151618` | 7.27 |
   | Dark | Banner edge `#8C939C` | canvas `#0F1012` / surface `#151618` | 6.14 / 5.84 |
   | Dark | Tag text `--color-ink` `#EDEEF0` | canvas / surface / surface hover | 16.40 / 15.60 / 14.52 |
   | Dark | Tag edge `--color-border-strong` `#6B6F77` | canvas / surface | 3.78 / 3.59 |
10. **The flat frame's surfaces.** § Direction's tokens add the sidebar, a
    hovered row and a group header beside the white canvas. Every text ink
    reaches 4.5:1 on each, and every input edge, focus ring and Health shape
    3:1, in both themes. The lowest are the light muted ink on a hovered row
    (5.58), the light input edge on a hovered row (3.08), the Yellow shape on
    a hovered row (3.40) and the dark input edge on a hovered row (3.42).

    | Theme | Mark | Canvas | Sidebar | Row hover | Group |
    | --- | --- | --- | --- | --- | --- |
    | Light | `--color-ink` `#1B1D22` | 16.86 | 15.75 | 15.61 | 16.16 |
    | Light | `--color-ink-2` `#33363C` | 12.11 | 11.31 | 11.22 | 11.61 |
    | Light | `--color-ink-muted` `#5F636B` | 6.03 | 5.63 | 5.58 | 5.78 |
    | Light | Links, `--color-primary-strong` `#0F766E` | 5.47 | 5.11 | 5.07 | 5.25 |
    | Light | `--color-danger-ink` `#9A231B` | 7.98 | 7.46 | 7.39 | 7.65 |
    | Light | Input edge, `--color-border-strong` `#8A8D93` | 3.33 | 3.11 | 3.08 | 3.19 |
    | Light | Focus ring, `--color-focus` `#0D9488` | 3.74 | 3.50 | 3.47 | 3.59 |
    | Light | Red shape, `--health-r-mark` `#C62828` | 5.62 | 5.25 | 5.21 | 5.39 |
    | Light | Yellow shape, `--health-y-mark` `#B27B00` | 3.67 | 3.43 | 3.40 | 3.52 |
    | Light | Green shape, `--health-g-mark` `#2E7D4F` | 5.05 | 4.71 | 4.67 | 4.84 |
    | Dark | `--color-ink` `#EDEEF0` | 16.40 | 15.60 | 14.83 | 15.29 |
    | Dark | `--color-ink-2` `#C9CBD0` | 11.73 | 11.15 | 10.61 | 10.94 |
    | Dark | `--color-ink-muted` `#9A9EA6` | 7.08 | 6.74 | 6.41 | 6.61 |
    | Dark | Links, `--color-primary-strong` `#14B8A6` | 7.65 | 7.27 | 6.92 | 7.13 |
    | Dark | `--color-danger-ink` `#F87171` | 6.88 | 6.55 | 6.23 | 6.42 |
    | Dark | Input edge, `--color-border-strong` `#6B6F77` | 3.78 | 3.59 | 3.42 | 3.52 |
    | Dark | Focus ring, `--color-focus` `#2DD4BF` | 10.23 | 9.73 | 9.25 | 9.54 |
    | Dark | Red shape, `--health-r-mark` `#F07171` | 6.62 | 6.30 | 5.99 | 6.18 |
    | Dark | Yellow shape, `--health-y-mark` `#E0B23A` | 9.61 | 9.14 | 8.69 | 8.96 |
    | Dark | Green shape, `--health-g-mark` `#4CC38A` | 8.59 | 8.17 | 7.77 | 8.01 |

    Signal chips and tags reach 4.5:1 on their fill:

    | Theme | Mark | On | Ratio |
    | --- | --- | --- | --- |
    | Light | Chip text `--color-chip-ink` `#4A4D54` | chip fill `#F1F1F3` | 7.50 |
    | Dark | Chip text `--color-chip-ink` `#C9CBD0` | chip fill `#222327` | 9.67 |

## Accepted audit findings

Places where the app follows the source on purpose, though a design audit has
flagged them. They aren't deviations, and later audits shouldn't raise them
again.

1. **White card surface.** The 2026-10-01 audit called the light theme's
   `--color-surface` (`#FFFFFF`) flat beside the canvas, then ivory. It stays
   white, and the flat frame made the canvas white too.
   The light-theme contrast ratios in § Deviations are quoted against it, and
   two of them have little room to lose: the input border at 3.33:1 and the
   focus ring at 3.74:1, against WCAG's 3:1. Tinting the surface would mean
   recomputing every one.
2. **Hex and rgba tokens.** The same audit asked for tokens in OKLCH. They
   stay in hex and rgba, the source's notation, so they can be checked against
   it line by line. Converting them would change nothing a reader sees.
