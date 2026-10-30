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

The flat frame, adopted 2026-10-20 from the maintainer's mockups of Home, the
Goal list and the Goal page, which were modelled on Linear. A page is one
surface, and type, space and hairlines carry the hierarchy.

- **One surface.** The page is a single plain background, `--color-canvas`:
  white in light, near-black in dark. No section sits in a box. A section is
  a heading over rows separated by 1px `--color-border` hairlines, and space
  between sections does the grouping. A form is a plain section too, and its
  fields have their own edges (§ Components: Sections).
- **Surfaces are for what floats.** A raised surface (`--color-surface`, a
  1px edge, `--radius-lg` and `--shadow-pop`) means "this is on top", so an
  open one reads as nearer than anything under it (see § Deviations 8). These
  float:
  - the person's menu, the signed-out Theme panel and the phone Menu's sheet;
  - the Goal page's More menu;
  - Follow-ups' popovers;
  - the Download menu;
  - the Check-in page's sticky submit bar;
  - the toast, which takes a `--color-border-strong` edge.

  A draft Report's baseline panel floats with no shadow, and its Publish…
  panel on a shadow of its own. Nothing else is raised.
- **Type leads.** One face, Inter (§ Type scale). A page rendered through
  `framedLayout` names itself in its header strip; the rest keep their own
  `h1` (§ Page header). A page about one thing (Home's week, a Goal, a
  publication, the Report builder) also opens its content with a 28px title.
  Sections take real `h2` headings at 13/600, and `.label` is a small
  sentence-case label, never caps. A Report keeps its own four heading levels
  (§ Type scale).
- **Health is the colour.** Red, Yellow and Green, as their shape and pill,
  are the page's strongest colour. Teal is the accent for what a person acts
  on: primary buttons, links, focus, the logo mark and the current choice in
  a menu or filter.
- **Mood:** calm and dense, with neutral ink and hairlines. The one data
  accent is a Metric's trend: its sparkline on the Goal page and its progress
  bar on the Check-in page, both in teal (see § Deviations 6). No neon, and no
  tint on the canvas, so Health's colours stand out from the page (see
  § Deviations 7).
- **Density:** tight and aligned. It is built for long lists on a laptop:
  rows of 40–44px, 13–14px text, a 4px grid, and numbers in tabular figures
  so columns line up. Sections sit 24–40px apart. A touch screen trades some
  density for size: a 16px body, and 15px secondary text and section headings
  (§ Type scale).
- **The frame.** Every signed-in page is a 240px sidebar beside a content
  column that opens with a 48px header strip (§ Components: Sidebar, Page
  header). Within the column:
  - a list page (the Goal list, Follow-ups, the Reports list) fills it edge to
    edge, with 28px sides;
  - a reading or form page centres a column: the Goal page's 860px document
    column beside its 300px properties rail, a publication's 880px, and the
    720px form of New goal, the Check-in page and the Report builder, the first
    two with a rail beside it;
  - every other page sits in the default content column (`.page`), up to
    1152px.

  At 900px and below the sidebar becomes a 48px strip across the top. Nothing
  scrolls sideways: a wide list scrolls inside its own container.

## Tokens

The flat frame's tokens, as `app.css` declares them. The light frame's
values come from the maintainer's mockups; a role neither they nor the source
give is marked *(chosen)*, as are the dark values § Provenance's preview
leaves out.

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

  /* (chosen) What floats, and buttons and inputs; a hovered button or menu
   * item; a neutral badge's fill and a used add-bar button */
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
  /* A Health's fill at half strength: the Check-in page's unpicked Health
   * choices. See Deviations */
  --health-g-tint: #F2F9F5; --health-y-tint: #FFF9E8; --health-r-tint: #FEF6F6;

  /* Type */
  --font-body: "Inter", system-ui, sans-serif;

  /* Shape */
  --radius-sm: 4px;      /* badges, Health strip cells */
  --radius-md: 6px;      /* buttons, inputs, chips, nav items */
  --radius-lg: 10px;     /* menus, popovers, the toast, the submit bar */
  --radius-full: 9999px; /* pills, avatars, Home's count */

  /* Elevation (teal-tinted): what floats only. See Deviations */
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

  /* (chosen) what floats lifts by lightness, as dark themes show
   * elevation; a neutral badge sinks to the canvas */
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
  --health-g-tint: rgba(34,160,95,.10); --health-y-tint: rgba(234,179,8,.10); --health-r-tint: rgba(239,68,68,.10);

  --shadow-pop:        0 25px 50px -12px rgba(0,0,0,.65);

  /* Dark hover goes lighter: --color-ink-inverse on it is 7.77:1 */
  --btn-primary-hover: var(--color-primary-hover);
}
```

In the stylesheet, the empty dark `@media` block above repeats the full
`[data-theme="dark"]` declarations.

## Type scale

One face, Inter, for headings and text alike. The Print variant keeps its own
serif, and no other face is loaded.

| Role | Size / line-height | On a touch screen | Weight | Tracking |
| --- | --- | --- | --- | --- |
| Page title (`h1` in the content) | 28 / 1.2 | 28 | 600 | -0.02em |
| Header strip title (`.ph-title h1`) | 14 / 1.4, `--text-body` | 16 | 600 | — |
| Section heading (`h2`) | 13 / 1.4, `--text-h2` | 15 | 600 | — |
| Sub-heading (`h3`), unless its page sets its own | 14 / 1.4, `--text-body` | 16 | 600 | — |
| Row title | 14, `--text-body` | 16 | 500 | — |
| Body | 14 / 1.5, `--text-body` | 16 | 400 | — |
| Meta (`.small`), usually muted (`.muted`, `--color-ink-muted`) | 13 / 1.5, `--text-secondary` | 15, and `.muted` takes `--color-ink-2` | 400 | — |
| Small label (`.label`, `th`), in `--color-ink-muted` | 12, `--text-label`, sentence case, never uppercase | 12, still `--color-ink-muted` | 500 | — |
| Numbers (`.num`) | inherit, `font-variant-numeric: tabular-nums` | inherit | 500 | — |

The sizes are four tokens on `:root` in `app.css`, outside the theme blocks
since they don't change with the theme: `--text-body` (14px), `--text-secondary`
(13px, meta and other secondary copy), `--text-h2` (13px, a section heading's
role: `h2`, `.gp-doc h2`, `.hm-section>h2`) and `--text-label` (12px). No screen
style sets a 12, 13 or 14px size of its own; it takes the token for its role,
inside a media block too. Other sizes (10–11px marks, 15px and up) stay literal.
Where § Components gives a 12, 13 or 14px size, it is the laptop's value of
the token for that role.

**On a touch screen** (`@media (pointer:coarse)`, a tablet read at arm's
length) one rule raises the scale: `--text-body` to 16px, `--text-secondary`
and `--text-h2` to 15px (a section heading keeps 600), and `.muted` copy takes
`--color-ink-2`. A label keeps its 12px and `--color-ink-muted`, so it recedes
by colour rather than size. A mouse-driven window keeps the laptop's scale at
any width, and the Report's own levels below don't change.

A Report, on its draft and its publication, reads on its own four levels
(`reportStyles`), so its name leads and its labels recede:

| Level | What | Size / line-height | Weight |
| --- | --- | --- | --- |
| 1 | The Report's name (`.rp-head h1`) | 26 / 1.2, 24 at 600px and below | 700 |
| 2 | Its section headings (`.rp-main h2`, `.rp-read h2`): Open Action Items, Narrative, Exceptions, On track, Goals that entered or left | 18 / 1.3, under a full-width 1px `--color-border` rule, about 44px below what comes before | 650 |
| 3 | A Goal block's title (`.rp-card-head h3`) | 16 / 1.4 | 600 |
| 4 | A Goal block's labels (`.rp-facts dt.label`): Why it matters, Status, Milestones, Metrics, Rolled-up Health, Supported by; the Health summary's "Health" (`.label`) | 12, `--color-ink-muted` | 500 |

The Exceptions count sits beside its heading in muted tabular figures at
`--text-body` (14px, 16px on a touch screen),
and the narrative's Insights, Accomplishments and Misses are 15px, 600. The
print page keeps its own serif scale (§ Print variant).

Dates, counts and Metric values line up in tabular figures, not a monospace
face. The app serves Inter itself, from `internal/web/static/fonts/`, as a
variable font by `@font-face` rules at the top of `app.css`. No page loads a
font from another host.

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

`app.css` holds the tokens, the frame and the shared components. Each page
adds its own rules in a `<style>` block in its template, under a prefix of
its own (`.hm-` for Home, `.gl-` for the Goal list, `.gp-` for the Goal page,
and so on).

### The frame

- **Sidebar (`.sidebar`, `sidebar` in `layout.templ`):** every signed-in page
  sits in the flat frame (`.frame`).
  - **Size and surface:** a 240px column on the left in `--color-sidebar`,
    with a 1px `--color-border` right edge, beside the content column
    (`.frame-content`). It has 12px padding, 16px gaps and 13px type. It is
    sticky and as tall as the window, and scrolls inside itself when its
    content is longer.
  - **Order, top to bottom:**
    1. **The app's name (`.brand`):** 14px, 600, `--color-ink`, after an 18px
       `--color-primary-strong` square mark with 5px corners. It links to
       Home.
    2. **Search (`.sb-search`):** a GET form to the Goal list's search
       (`/goals?q=`), `role="search"`. Its field is 32px tall on
       `--color-canvas`, with the placeholder "Search" and a visually hidden
       label, "Search Goals". It shows no shortcut hint, as the app has none.
    3. **The nav (`.navitems`):** **Home · Goals · Follow-ups · Reports**,
       then **Admin** for an Admin, 4px apart.
       - **Items (`.navitem`):** each is 32px tall with `--radius-md` and 8px
         sides. It holds a 16px stroke icon 12px from its 500-weight label in
         `--color-ink-2`, and its count at the right edge (§ Nav counts).
       - **States:** the icon is `--color-ink-muted` at rest. Hover takes
         `--color-sidebar-hover` and the current item (`.on`,
         `aria-current="page"`) `--color-sidebar-on`, both turning the label
         and icon `--color-ink`.
       - **The current item:** an item is current on its own address and
         every page under it, so a Goal's page, New goal and a Check-in all
         mark Goals.
       - **Counts:** Home counts what needs the person. Follow-ups counts the
         Goals it flags in the person's default scope (see Scope), as many as
         a bare `/follow-ups` lists for them. Goals, Reports and Admin carry no
         count.
    4. **My org (`.sb-org`):** for someone with people directly under them by
       Manager, a muted 12px "My org" heading over each of those people by
       Name, sorted by Name. Each is a 30px row in `--color-ink-2`, cut short
       with an ellipsis, with their email as its tooltip and the nav's hover.
       It links to their Org on Goals (`/goals?org=<id>`). Anyone without
       people under them gets no My org. The people are read once per
       request, with the counts.
    5. **The person's menu (`.sb-person`):** at the foot, a 40px `<details>`
       button holding their initials in a 24px avatar and their Name in
       500-weight `--color-ink`, truncated with an ellipsis when long. It
       takes `--color-sidebar-hover` on hover and while open, and works
       without script. Its panel floats 4px above it, as wide as the sidebar
       (§ System: Surfaces are for what floats): "Signed in as" their Name in
       muted 12px (the Name shows their email, as everywhere), "(Admin)" for
       an Admin, then the Theme choices (§ Theme menu) and **Sign out**, a
       full-width quiet small button.
  - **At 900px and below** the sidebar is a 48px strip across the top, with
    a 1px `--color-border` bottom edge, and it scrolls away with the page.
    - **The strip:** the app's name at the left. Home's count pill (only above
      zero, as a 44px link to Home, so it isn't hidden) and **Menu**, a small
      secondary button on a `<details>`, sit at the right.
    - **The sheet:** opening Menu shows the rest of the sidebar (Search, the
      nav, My org and the person's menu) as a sheet under the strip, in
      `--color-sidebar` on `--shadow-pop`, scrolling inside itself when it is
      taller than the window. The person's menu opens in place in it, with no
      shadow.
    - **No script needed:** the sheet is the sidebar's own body (`.sb-body`),
      shown by the open Menu beside it (`.sb-menu[open]~.sb-body`), so it
      works without script and the page has one copy of each nav item.
- **Content column (`.page`):** the page under the header strip, a centred
  column up to 1152px wide with 32px padding (64px at its foot) and 32px
  between its parts. At 900px and below it is 20px and 16px, with 24px gaps.
  A list page and the Goal page lift the width limit and the padding and run
  edge to edge (§ System: The frame).
- **Page header (`.page-header`, `pageHeader` in `layout.templ`):** a 48px
  strip across the top of the content column, with 28px sides (16px at 900px
  and below) and a 1px `--color-border` under it.
  - **The title:** at its left, the page's title as an `h1` at 14/1.4, 600,
    cut short with an ellipsis when long. On a page under another, its parent
    leads it as a muted link and `›` ("Goal title › Check in"), the `›` hidden
    from screen readers. A muted count in tabular figures follows where the
    page has one ("24 in My org").
  - **Breadcrumbs:** the Goal page, a publication and the Report builder
    put a `<nav aria-label="Breadcrumb">` there instead ("Goals › <Goal
    title>"), ending in the current page in `--color-ink`, marked
    `aria-current="page"`. The Goal page's strip holds no `h1`, because the
    Goal's title heads its document column.
  - **The actions:** at its right, kept at full width, with the primary one
    as a filled small button (New goal, Check in, New report).
  - **Scroll:** it isn't sticky. A Report's draft page is the one exception
    (§ A Report's draft page).
  - **How a page gets one:** it renders through `framedLayout` with a header.
    A page rendered through `layout` has no strip and keeps its own head in
    its content: the Message pages, Graph signals and Freshness signals.
- **Signed-out pages (`.signed-out`):** sign-in, set password, and Not found
  while signed out, have no sidebar. A 56px bar (`.signed-out-bar`), with no
  edge or fill, holds the app's name at the left and a quiet small **Theme**
  disclosure at the right, with 24px sides (16px at 900px and below). The
  content is centred on the canvas.
- **Sign-in and Set password (`signInColumn` in `signin.templ`):** one column
  up to 360px wide, centred 56px under the bar. It opens with
  the app's mark at 32px with 8px corners, 24px above the form (`.si-form`),
  whose parts are stacked 16px apart: the centred 28px `h1`, a muted note
  where there is one, the fields, then a full-width primary button. A refusal
  shows above the fields in `--color-danger-ink`, `role="alert"`, with the
  email kept.
  - **Development sign-in:** a note saying any email signs in, then Email and
    **Sign in**.
  - **The company's provider:** one button, **Sign in with your
    organization**, or **Try again** after a refusal.
  - **Password sign-in:** Email, Password and **Sign in**, then a muted
    "Forgot your password? Ask an Admin for a set-password link."
  - **Set password:** "For <email>. At least 12 characters.", **New password**
    and **New password again**, then **Set password**. A link that can't be
    used shows only the heading and the reason.
- **Theme menu (`.theme-menu`, `themeMenu` in `layout.templ`):** three
  choices in words, under a muted "Theme" label: **System** (the default:
  follow the operating system), **Light** and **Dark**.
  - **Where:** signed in, they sit in the person's menu above Sign out.
    Signed out, they sit in the bar's "Theme" disclosure (`.theme`), whose
    160px panel (`.theme-float`) floats 8px under the button, anchored right
    and no wider than the screen less 32px, so it never widens the page.
  - **A choice (`.theme-choice`):** 36px, `--radius-md`, 500-weight
    `--color-ink`, with `--color-primary-subtle` on hover. The current choice
    takes a `--color-primary-light` fill, 600 weight, a
    `--color-primary-strong` ✓ at its right, and `aria-current="true"`.
  - **No script needed:** each choice is a plain form post, and comes back to
    the page it was made on.
  - **Where the choice is kept:** per browser, in the `gt_theme` cookie, not
    on the Account, so it holds before sign-in and after sign-out. It lasts a
    year, and each choice renews it. Choosing System clears it. The server
    pins `data-theme="light"` or `data-theme="dark"` on `<html>` from the
    cookie, so the page has the right theme on first paint with no script.
    With no cookie, or a value it doesn't know, it pins nothing and the page
    follows the system. A Report's Print view and emails ignore the choice.
- **Toast (`.toast`, `toastNotice` in `layout.templ`):** the one shared
  notice for an action that can be undone. It says what was done, in
  `--color-ink` on `--color-surface`, with a small secondary **Undo** 16px
  beside it, inside a 1px `--color-border-strong` edge, `--radius-lg` and
  `--shadow-pop`. It is fixed 24px above the bottom of the window, centred,
  over the page content (`z-index` 30) and no wider than the window less
  32px, and the page under it gains 128px of bottom padding so nothing ends
  up hidden behind it. At 600px and below it spans the window less 16px each
  side, 16px from the bottom, and its message wraps.
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
    renders a "Can't undo" Message page, with one plain sentence saying why
    (no internal prefixes or IDs) and a **Back** link to the page the Undo was
    offered on, query included. An Undo that is done lands back on that same
    page, query included. An Undo posted without that page goes to Home for a
    link removal or a decline, the pending page for a rejection, and
    Dimensions for a retired value. The page is `refusalPage` in
    `refusal.templ`, the one page every refused action shows (`refuse`).
  - **Announced politely:** an `<aside role="status">`, so a screen reader
    reads it without moving focus.
  - **Where:** removing a link (Goal page), retiring a Dimension value
    (Dimensions page), rejecting a link request or a Handoff (Home, or the
    request's pending page, whichever the Reject came from), and declining a
    Parent suggestion (Home or the Goal page). A new Undo reuses `toastNotice`
    rather than making its own notice.
- **Message page (`.message-page`):** a page that only says one thing (Not
  found, Can't undo, Can't remove link, a refused Nudge, a refused No
  change). It renders through `layout`, with no header strip: its
  28px `h1`, its sentence and its way back sit in a column up to 760px wide,
  12px apart, centred 64px down with its text centred, as is every
  signed-out page's content.
- **Not found page (`notFoundPage` in `notfound.templ`):** like a refused
  Undo, a missing page shows a page, not bare text. A page you open that
  doesn't exist (an unknown URL, asked with any method, or a Goal, Report,
  publication or other thing that doesn't exist or has been removed) answers
  404 with a "Not found" Message page inside the normal chrome, the sidebar's
  counts included. It is centred, 64px down, its text centred and 12px apart:
  one plain sentence, "There's nothing here. It may have been removed, or the
  link may be wrong.", and a way back, **Back to Home** when signed in,
  **Sign in** when not. `s.notFound` writes it; a handler that 404s calls it
  rather than `http.NotFound`. An action (a form's POST) refused as "not
  found" answers as before, with its area's plain-text error, and a known
  address asked with a method it doesn't take still answers 405.

### Shared components

- **Sections:** no page container has a fill, border or padding box. A page
  section is an `h2` (13/1.4, 600, `--color-ink`) over its rows, each ruled
  off by a 1px `--color-border` hairline, with space between sections doing
  the grouping. Each page styles its own sections
  (`.hm-section` on Home, `.gp-section` on the Goal page, `.ad-section` on
  Admin, and so on); `app.css` has no shared section class, and no card.
- **List rows:** the Goal list (`.gl-list`) is the model, and Home,
  Follow-ups and the Reports list follow it.
  - **Rows:** 40px on the Goal list, 44px elsewhere, separated by
    `--color-border` hairlines, with `--color-row-hover` under the pointer on
    the Goal list, Follow-ups and the Reports list. A list page's rows run
    edge to edge with 28px sides (16px at 900px and below).
  - **Column labels:** a 32px row of muted 12px, 500 labels at the top, over a
    hairline, with no fill.
  - **Groups:** a 36px `--color-group` header row with the group's shape, its
    name in 13px, 600 `--color-ink` and its count in muted tabular figures
    ("Red 4").
  - **Phone:** at 600px and below the column labels hide and each row stacks
    between its hairlines, the Goal or name first and the rest wrapping under
    it.
- **Health:** shown three ways, each keeping its shape, so colour is never
  the only signal. The shapes are the road signs': Red a stop sign's regular
  octagon, Yellow a yield sign's triangle pointing down, Green a circle for
  go. One shape set draws them all: `.dot` inside a `.r`, `.y` or `.g`
  (app.css), filled with `currentColor`. Its size is `--dot-size`, the
  circle's width, 8px by default; the triangle is drawn 1.25 times as wide
  and 1.125 times as tall, and the octagon 1.125 times the size but never
  under 12px, below which it reads as a circle on a 1x screen, so each reads
  as its shape beside the circle. A place that draws the shape bigger sets `--dot-size` on
  its `.dot` and never draws a shape of its own.
  - **The shape alone (`.gl-mark`, `goalHealthMark`):** in the Goal list's
    rows and group headers, a `.dot` at 11px in `--health-*-mark`. In a row
    it is `role="img"` with an `aria-label` naming the Health ("Red", "No
    Health"). A Goal with no Health shows a hollow 1.5px `--color-ink-muted`
    ring.
  - **The badge (`.badge` with `.g` / `.y` / `.r`, `healthChip`):** a
    `--health-*-bg` fill with its 8px shape (`.dot`) in `--health-*-ink`
    (see § Deviations 3), 22px tall with `--radius-sm` corners. It is how
    Health shows everywhere else: Follow-ups, the Goal page, the Check-in
    page, the Reports, Home and Delegates. No Health badge gives up its
    colour: a dated one and an unpicked Check-in choice keep their Health's
    hue.
  - **The word is screen-reader text.** Where a badge shows its colour and
    shape, those say the Health, so the word (Red, Yellow, Green) is not
    shown: it is an `.sr-only` span inside the badge, so the badge reads
    "Red" to a screen reader and in the page's text order, and the badge's
    `title` gives it as a tooltip (a Rolled-up Health's opens with it: "Red:
    the lowest Health among Active children"). The badge shrinks to its shape
    (`.badge.shape`): centred, 4px sides, still 22px tall, at least 24px
    wide, and at least 24px tall where it is a link. The Check-in choice keeps its 44px height.
    The one exception is a Report's Health summary, a row of counts read at a
    glance by someone who may not know the shapes: there the word stands on
    screen beside each count ("2 Red"), so the chip is its shape alone, with
    no `.sr-only` word, hidden from a screen reader (`aria-hidden`), and keeps
    its `title`; the pair reads once, "2 Red".
  - **What has no colour keeps its words:** the Health filter's options, "No
    Health yet", "No Health", "—", a Lifecycle badge standing in for a
    Health, and counts ("3 Red", My org's "Red 4", Follow-ups' "Red · 3",
    the Report's Health summary's "2 Red").
  - **Without colour, the word stays.** The Print variant shows the word
    beside the shape, since paper may be black and white (§ Print variant),
    and the Markdown export, which has no colour at all, writes the word
    first: an Other Goal reads "- Red — Title — Owner — due …" ("- — —
    Title …" with no Health yet), an exception's heading "### Red · Title"
    over "Owner · Active · 5 wks · was Green · due …", and a linked Goal "Red
    Title".
  - **Health comes first.** Wherever a Goal's Health shows with its title, the
    Health comes before it, at every width, on screen, in print and in the
    Markdown export: [Health] [title], never the title then its Health.
  - **The pill:** in Home's rows, the badge takes `--radius-full` and 12px,
    500 type.
- **Other badges:** the neutral badge (`.lc`), for a Lifecycle and the like,
  is `--color-surface-alt` with `--color-ink-2` and a 1px `--color-border`
  edge. Ownerless (`.ol`) is `--color-surface` with `--color-ink` in a 1px
  dashed `--color-ink-muted` edge.
- **Signal chips:** a signal about a Goal (Check-in overdue, Path to Green
  overdue, Milestone overdue, Ownerless, Incomplete, a Lifecycle) beside its
  title in a list.
  - **On the Goal list (`.gl-marks`):** a 22px `--radius-full` pill in 12px,
    500 type on `--color-chip-bg` with `--color-chip-ink`. Check-in overdue,
    Path to Green overdue and Milestone overdue keep their dashed
    `--checkin-overdue-edge`, and Ownerless its dashed `--color-ink-muted`
    edge, so each still reads as a warning, not a tag.
  - **On Home:** the same pill. A Lifecycle badge and a tag take
    `--color-chip-bg` with `--color-chip-ink` and no visible edge, and
    Check-in overdue keeps its own grey fill.
  - **On Follow-ups and the Reports:** a signal is the badge-shaped
    Check-in overdue chip (`.badge.st`) or an outlined tag (Follow-ups'
    `.rk-tag`), not a pill.
- **Check-in overdue chip (`.badge.st`):** marks a Check-in overdue Goal
  wherever its Health shows, with its age. It is a warning about freshness,
  never a tag, so it can't share a tag's look: a `--checkin-overdue-bg` grey
  fill, `--checkin-overdue-ink` text and a 1px dashed `--checkin-overdue-edge`
  border, 22px tall at a Health badge's `--radius-sm`. On the Goal list it is
  a signal chip, keeping only the dashed edge (§ Signal chips).
  - **Wording:** "Check-in overdue · 9 days" on the Goal list. On Follow-ups
    it adds the cadence, naming it as the New goal form's segments do or
    "every 3 days": "Check-in overdue · 42 days, checks in weekly".
  - **The same style** marks Path to Green overdue ("Path to Green overdue" on
    the Goal list, "Path to Green 12 days overdue" on Follow-ups), Milestone
    overdue on a Green Goal past a Milestone's date ("Milestone overdue" on
    the Goal list, "Milestone 3 days overdue" on Follow-ups) and, on
    Follow-ups only, "Action Item 3 days overdue".

  The banner on a Check-in overdue Goal's page is in § Goal page banners, and
  the ratios in § Deviations 9.
- **Tags (`.tag`):** a Dimension value. A `--radius-full` outline with no
  fill: a 1px solid `--color-border-strong` edge and 13px `--color-ink` text.
  A retired value's tag takes a dashed edge instead, struck through and muted.
  A tag is 22px and 12px on the Goal list. In Home's rows it is a chip
  (§ Signal chips).
- **Buttons (`.btn`):** 40px tall, or 32px small (`.btn.sm`), with 16px sides
  (12px small), `--radius-md` and 14px, 600 type (13px small) on one line.
  Home's row actions and its New goal are 30px. Each colour change runs over
  `--dur-fast`, and active is `scale(0.98)`. A disabled button is at half
  opacity, with a not-allowed cursor.
  - **Primary (`.btn.primary`):**
    - Resting: a `--color-primary-strong` fill and edge, with
      `--color-ink-inverse` text.
    - Hover: the fill and edge change to `--btn-primary-hover`, and nothing
      else does: no lift, no shadow.
      - Light: the fill darkens to Teal 800 (`#115E59`), and white text on it
        is 7.58:1. Light `--color-primary-hover` is only 3.74:1 under white
        text, so it can't be the fill.
      - Dark: the fill lightens to `--color-primary-hover` (`#2DD4BF`),
        because dark themes show elevation by lightness.
        `--color-ink-inverse` text on it is 7.77:1.
  - **Secondary (`.btn`):** a `--color-surface` fill, a 1px `--color-border`
    edge and `--color-ink` text. On hover, the fill becomes
    `--color-surface-hover` and the edge `--color-primary`.
  - **Quiet (`.btn.quiet`):** no fill or edge, with `--color-primary-strong`
    text. On hover it takes a `--color-primary-subtle` fill.
  - **Danger (`.btn.danger`):** a secondary button with `--color-danger-ink`
    text.
- **A person's Name (`.person`, `person` in `layout.templ`):** plain text in
  the surrounding ink, with no underline, so it never reads as a link. It is
  a button whose tooltip is the person's email. On hover and keyboard focus it
  takes a faint `--color-primary-subtle` tint, and focus keeps the focus ring.
  Clicking, tapping or activating it from the keyboard shows the person's
  email beside it, muted at 13px, and doing it again hides it (CONTEXT.md:
  Name). `personPlain`, inside another control, shows the email on hover only,
  and the Print view's "Name (email)" has no control. A Name in brackets, as
  a Schedule conflict's or a link request's Owner, goes through
  `personInBrackets`, which keeps the "(" with the Name (`.person-nobreak`,
  `white-space:nowrap`), so a narrow window never leaves "(" ending a line;
  the Name itself and the email it shows can still wrap.
- **Avatars (`.avatar`):** a circle with a person's initials (`initials` in
  `layout.templ`), hidden from assistive technology, beside their Name, which
  stays the control that shows their email. It is 24px on the sidebar's
  person button and 22px in Home's My org rows, both `--color-primary-strong`
  with `--color-ink-inverse` initials, and 22px on the Goal list's Owner
  column (`.gl-avatar`), `--color-chip-bg` with 10px, 600 `--color-chip-ink`
  initials. Every person's avatar is the same colour.
- **Segmented controls:** a few choices in one track. There are three looks:
  - **A tinted track:** the Goal list's List / Table switch (`.gl-seg`, two
    links) and the New goal form's Check-in cadence (`.gf-segctl`, four
    radios) sit on a `--color-chip-bg` track with `--radius-md`, their
    segments 13px, 500 `--color-ink-2`. The current segment is
    `--color-canvas` with `--color-ink` text and a 1px
    `--color-primary-strong` edge, which every segment reserves as a
    transparent border, so choosing one shifts nothing; the cadence's also
    takes 600 weight. The fill alone is only 1.13:1 on the track.
  - **An outlined track:** the Report builder's mode (`.rb-segments`) and a
    draft's section picker sit in a 1px `--color-border-strong` edge with
    `--radius-md`. The chosen segment is filled with `--color-ink`, its text in
    `--color-surface`.

  Each page's section below gives its sizes. The radios hide only with
  `:has()`; without it each choice shows its native radio and still chooses.

  **A chosen option's edge is `--color-primary-strong`,** never
  `--color-primary` or `--color-border-strong`: on a tinted track's current
  segment, a Kind tile and its radio dot, and a Where it fits pick. It is the
  edge that tells the chosen option apart, so it needs WCAG's 3:1, which
  `--color-primary` (2.49:1 on the canvas) and `--color-border-strong` (2.95:1
  on the track) miss in light (Deviations 6).
- **Inputs:** text, email, password, date, number and search inputs and
  selects take a `--color-surface` fill, a 1px `--color-border-strong` edge,
  `--radius-md`, 40px height with 16px sides, and `--color-ink` text, with
  `--color-ink-muted` placeholders. Textareas are at least 88px tall with
  12px × 16px padding, and resize vertically.
  - **Focus:** the edge becomes `--color-focus` and the field takes the same
    ring as everything else, `2px solid var(--color-focus)`, offset 1px
    (2px elsewhere). The ring shows on mouse focus too and never transitions.
    See Deviations.
  - **A refused field** (`aria-invalid="true"`) takes a `--color-danger-ink`
    edge.
  - **Checkboxes and radios** take `--color-primary-strong` as their accent.
  - **Field labels (`.field`):** 13px, 600 `--color-ink`, 8px above the field.
    A hint (`.hint`) is 13px `--color-ink-muted`.
- **Tables:** cells have 12px × 16px padding and a 1px `--color-border`
  between rows, and the head row is muted 12px, 500 labels (`th`). The base
  `th` rule fills it with `--color-surface-alt`, and every table in the app
  overrides that to no fill. A wide table scrolls inside `.table-scroll`.
- **Nav counts (`.count`):** a count after a nav item's label, at the item's
  right edge, in 12px tabular figures, shown only above zero. Two styles, so
  the one that needs the person stands out:
  - **Home's count (`.count.needs-you`)** counts what needs the person, so it
    is an 18px filled `--radius-full` pill, at least 20px wide:
    `--color-ink` with 600-weight `--color-canvas` numbers, dark on light and
    light on dark. It also shows on the phone strip.
  - **Every other count (`.count`)**, such as Follow-ups, is 500-weight
    `--color-ink-muted` figures with no pill.

  The ratios are in § Deviations 2.
- **Help on terms:** a glossary term a reader may meet cold carries a
  `disclose help` button with a dotted muted underline that opens a one-line
  definition in place (`.explain`, muted 13px), with the same text as the
  button's `title`, so it is still reachable without script. Why it matters
  has one under the Goal page's lead; Dated, Ongoing and Top-level are the
  word itself in the Goal page's rail; Delegates and Dimension values have one
  in the rail; and the Report builder's Also include and Leave out have one
  after each legend. Ownerless badges on the Goal list, the Goal page's link
  lists and the Follow-ups list carry the definition as a `title` only,
  because a button in every row would be too heavy.
- **Sizes and touch targets:** buttons, inputs and selects are 40px tall, and
  `.btn.sm` 32px. A `<summary>` is at least 44px unless its page sets its
  own height. On a coarse pointer (`@media (pointer:coarse)`), buttons (small
  ones included), text, email, date, number, search and password inputs,
  selects, the sidebar's nav items, My org's people, the person's button and
  the Theme choices get a 44px `min-height`, so every one of those is at
  least 44px and a button beside an input still shares its height. A small
  button keeps its smaller type and padding. Textareas, checkboxes and
  radios are left alone.
- **Phone width:** at 900px and below the sidebar becomes the top strip and
  the side padding drops to 16px. At 600px and below nothing scrolls
  sideways, and no control is clipped. A list's rows stack as plain rows on
  hairlines, the Goal or name first: the Goal list, Follow-ups, the Reports
  list and a Report's On track. The Goal list's Table layout, a wide data
  table and the Check-in page's add bar buttons may scroll inside their own
  container. Filter bars and field-plus-button forms wrap.

### Pages

- **Home (`homePage` in `home.templ`):** one column on the page's one
  surface, in the default content column, read top to bottom: the header
  strip, the page title, then **Needs you**, **My org** and **Your Yellow or
  Red goals** as plain sections.
  - **Header strip (`homePageHeader`):** "Home", with **New goal**
    (`.btn.primary`, 30px tall, 13px) at its right, leading to `/goals/new`.
    It carries no count.
  - **Page title (`.hm-head`):** three lines 8px apart: **Your week** (28/1.2,
    600, an `h1`), today's date in the company's timezone in muted 13px
    ("Wednesday, Oct 21"), then the Your goals line.
  - **Your goals line (`.hm-summary`):** muted 13px, wrapping. It reads "Your
    goals:", the Health distribution bar, then "1 Green · 0 Yellow · 0 Red ·
    Goals you own or help with →", counting the Active Goals the person Owns
    by their latest Check-in's Health, so a Check-in overdue one counts under
    its last Health. A Goal with no Health yet isn't counted. **Goals you own
    or help with →** opens the Goal list on Mine (`/goals?org=mine`): the
    Goals the person Owns or is a Delegate on. For someone who is a Delegate
    on any Goal, the line ends "· Goals delegated to you N →", leading to the
    Delegate page. The line carries no count of what needs the person: the
    Needs you heading does.
  - **Sections (`.hm-section`):** 32px apart. Each is an `h2` at 13/1.4, 600
    with its count beside it in muted 500 tabular figures (`.hm-count`, left
    out at zero), and its rows start 8px under it.
  - **Rows (`.hm-list`):** at least 44px tall, each with a 1px
    `--color-border` hairline above it and one under the last. A pill or chip
    leads (§ Health, § Signal chips), then the title as a 14px, 500 link, then
    a muted 13px meta line (`.hm-meta`) on the same line while it fits and
    under it when it doesn't. The actions sit at the right, 8px apart, as
    30px buttons, and wrap under the row on a narrow column. Rows take no
    hover fill, save My org's.
  - **A dated pill (`.badge.as-of`, Home's own rule):** a Check-in overdue
    Goal's Health is its last Check-in's, so its pill is marked stale: it
    keeps its Health's fill, ink and shape, edged in a 1px dashed line
    of its ink (`currentColor`). The "last check-in" or "as of" beside it
    dates it.
  - **Your Yellow or Red goals:** the last section: the Active Goals the
    person Owns that are Red, then Yellow, each its Health pill (dated when
    Check-in overdue, with a muted "as of 9 days ago" after the title) and
    its title linking to the Goal. Its rows have no actions. With none, the
    section is left out.
  - **No script needed:** every action is a link or a plain form post, and
    Close is a `<details>`.
- **Home's Needs you list (in `homePage`):** the first section, always
  shown, holding everything waiting on the person, its heading counting the
  rows ("Needs you 3"). The heading's count and the sidebar's Home count are
  the same number. With nothing waiting, the heading has no count and sits
  over one muted line, "You're all caught up." Its rows come in up to four
  groups, each a muted 12px, 500 `h3` (`.hm-group-head`) over its rows,
  16px below what comes before it; a group with nothing in it is left out.
  - **Check-ins due:** the Active Goals the person Owns or is a Delegate on
    whose Check-in is due before next week's reminder, the reminder email's
    rule, most days past cadence first. Each row is the Health pill (dated
    when Check-in overdue, or a "No Health yet" chip), the title linking to
    the Goal, then the meta: "last check-in 14 days ago" ("no check-in since
    activation 9 days ago" before the first), "every 7 days", each wrapping
    whole, the Check-in overdue chip when it applies, and a "for [Owner]" chip
    on a Goal they are a Delegate on. A secondary **No change**, shown when the
    Goal has a Check-in to repeat and a No change wouldn't be refused, and a
    primary **Check in** sit at the right. A Goal they are a Delegate on
    appears only here.
  - **Asks for help:** the open Asks for help addressed to the person (see
    CONTEXT.md: Ask for help), Red first, then Yellow, then by Goal title.
    Each row has the Goal's Health pill, its title linking to the Comments on
    its Goal page (`#comments`), where the answer is written, then a muted
    "from [Owner]". It has no action of its own: the row stays until the Owner
    unticks the ask in a later Check-in or the Goal is no longer Yellow or Red
    and Active, however it is answered.
  - **Action Items:** the open Action Items the person owns, from every
    Report, soonest due first, so an overdue one leads. Each row has an
    "Action Item" chip, the item's text linking to the Action Items on the
    Goal page of the Goal it is about, or on its publication when it is about
    the whole Report, a red **Overdue** pill (`badge r`) when it is past due,
    then a muted "Weekly ops · Reduce outages · due Nov 7, 2026". Its action is
    **Close** (§ Close and Reassign), which returns to Home. Home carries its
    own copy of the `.rp-inline-form` rules.
  - **Requests:** link requests and Handoffs waiting on the person, and Parent
    suggestions on Goals they Own, oldest first, each led by a chip naming its
    kind. **Reject** (**Decline** for a Parent suggestion) is a quiet button
    and **Accept** a secondary one. Reject and Decline send at once and return
    to Home, which offers Undo in the toast. Where accepting needs choices, as
    a Handoff of a Goal with Delegates does, the button reads **Review** and
    leads to the Pending handoffs page, where those choices are made.

  Goals the person is a Delegate on aren't listed again elsewhere: the Your
  goals line has one link to the Delegate page with their number, shown only
  above zero.
- **Home's My org section (`homeOrgSection` in `home.templ`):** shown only to
  someone with anyone under them by Manager, so never before a directory sync
  (ADR 0008). It sits directly under Needs you, headed **My org** with the
  number of people it lists, and counts what is in trouble in the person's
  Org, leaving out the Goals they Own themselves, which Your goals covers. A
  Goal is in the Org by its Owner, never its Delegates, and an Ownerless Goal
  stays where its Departed Owner was. It lists people and counts, never
  Goals:
  - **Total line:** across everyone under the person, a plain body line in
    `--color-ink-2`, one count per signal with anything in it, in the order
    Red · Path to Green overdue · Check-in overdue · Yellow ("2 Red · 1 Path
    to Green overdue · 3 Check-in overdue · 1 Yellow"). It is text, not
    pills, so the totals stand apart from the per-person counts beneath. A
    Goal counts under every signal it has, so the counts can add up to more
    than the Goals behind them.
  - **Rows (`.hm-org-row`):** one per person directly under the viewer whose
    Org has anything flagged. The whole row is one link, at least 44px, to
    their Org on Goals (`/goals?org=<their ID>`): their initials in a 22px
    avatar, their Name in 14px, 500 `--color-ink` (`personPlain`, email on
    hover), a "departed" chip beside a Departed one, then their Org's counts
    in the total line's order. Each reads as a count and a name ("1 Red"), so
    it reads without colour: Red and Yellow as Health pills with their
    shape (`.dot`) before the count, Path to Green
    overdue and Check-in overdue as Check-in overdue chips. On hover the row
    takes `--color-row-hover`. The most Red come first, then the most Path to
    Green overdue, the most Check-in overdue and the most Yellow, then by
    Name. Someone further down counts in the row of the person directly under
    the viewer they're under, so the rows add up to the total.
  - **All clear:** the people directly under the viewer with nothing flagged
    share one muted line after the rows, "2 others all clear" ("1 other all
    clear"), left out when there are none. With nothing flagged anywhere, the
    section shows only one muted "Everyone in My org is all clear." line.
  - **Links:** the section ends with one line, **My org on Goals →** ·
    **My org on Follow-ups →** (`?org=<viewer ID>`), whose pages include the
    person's own Goals.

  The section adds nothing to Needs you or the sidebar's Home count.
- **Health distribution bar (`.hm-bar`, `healthBar` in `home.templ`):** on
  Home's Your goals line, a 96px × 8px `--radius-full` bar of the Goals the
  line counts, one equal unit per Goal, Green then Yellow then Red, each at
  least 2px wide. Units fill with the Health's `--health-*-mark`, which keeps
  3:1 on the canvas in both themes (§ Deviations 10), and a 4px gap shows
  where one Health's run gives way to the next. It counts Goals and never
  aggregates Metrics (ADR 0003). With none to draw the bar is left out. The
  counts beside it on the line are its labels, so it reads without colour,
  and its `role="img"` and `aria-label` ("2 Green, 0 Yellow, 1 Red") say the
  same to a screen reader.
- **Goal list (`goalsPage` in `goals.templ`):** one surface under the header
  strip. The page drops its width cap and padding, so the filter bar and the
  list run edge to edge.
  - **Header strip (`goalsHeader`):** **Goals** with a muted count of the
    Goals listed, naming an Org when scoped to one ("24 in My org", "3 in
    Priya Raman's org"). At its right sit the List / Table switch and
    **New goal** (`.btn.primary.sm`). The switch (`.gl-seg`, a
    `<nav aria-label="Layout">`) is a 32px track (§ Segmented controls) whose
    links keep every filter, the current one `aria-current="page"`. At 600px
    and below the strip wraps.
  - **Filter bar (`goalFilters`, `.gl-filters`):** one GET form on the canvas,
    with 12px × 28px padding and 8px gaps, over a 1px `--color-border`
    hairline.
    - **Chips (`.filter-chip`, shared in `app.css`; Follow-ups' filters use
      them too):** 28px tall with 8px sides, `--radius-md`, a 1px
      `--color-border-strong` edge on `--color-canvas`, 13px, 500
      `--color-ink-2`. A chip holding a select draws its own chevron. In
      order: **Scope** (see Scope), **Health** (Any Health, Red, Yellow,
      Green, No Health), **Lifecycle** (Any Lifecycle, then Proposed through
      Cancelled), **Incomplete** (a checkbox chip), **+ Filter** (a dashed
      edge, only when there are Dimensions) and **Group by** (Health, or a
      Dimension, in the List layout when there are Dimensions).
    - **Set chips:** a set chip takes `--color-primary-subtle` with
      `--color-ink` text and reads its key before its value in
      `--color-ink-muted` ("Health: Red"). An unset Health or Lifecycle chip
      reads "Any Health" or "Any Lifecycle", with no key.
    - **More filters:** + Filter opens a fieldset per Dimension on a line of
      its own below the chips, in a 1px `--color-border` edge with
      `--radius-md`, ticking values. Filtering ORs within a Dimension and ANDs
      across Dimensions. It starts open when a value is ticked, and + Filter
      takes the set fill while it is open or anything in it is ticked.
    - **Clear and Search:** **Clear**, a quiet small link to a bare `/goals`,
      follows the chips when the bar differs from it. A 28px search field,
      "Search title or Owner", 160–240px wide, sits at the row's right.
    - **Under htmx** the filters apply themselves on a change, on Enter, or
      400ms after typing stops in the search. The form swaps in only the list
      (`#goal-list`), takes the header's count and switch out of band, and
      pushes the address, so it keeps its focus and an open More filters.
      `hx-history="false"` makes Back reload the earlier address afresh.
      **Without script** an **Apply** button follows the chips, and the form
      is an ordinary GET.
  - **List layout (`goalListLayout`, `.gl-list`):** a `<table>` styled as
    list rows (§ List rows), under a label row reading Goal · Owner · Due ·
    Last check-in (the Health column's label visually hidden).
    - **Groups:** Health groups by default (Red, Yellow, Green, No Health),
      each left out when empty, problems first within each: Red, then
      Ownerless, then Check-in, Path to Green or Milestone overdue, then
      Yellow, Green and no Health, by title within each. When a Goal with
      no Health is Ownerless, Check-in overdue or Milestone overdue, the
      whole No Health group leads instead (No Health, Red, Yellow, Green),
      that Goal first in it, so it is as loud as a Red one. Grouped by a
      Dimension, each value is a group, with "Unassigned" for Goals with no
      value, and the header carries no shape.
    - **A row (`goalListRow`):** its Health shape; the title, a 14px, 500
      `--color-ink` link cut short with an ellipsis; its signal chips
      (Lifecycle for a Goal with no Health, Ownerless with its definition as
      a `title`, Check-in overdue · 9 days, Path to Green overdue, Milestone
      overdue, Incomplete); its Dimension values as tags, a Retired one with a
      muted "(retired)"; the Owner with an avatar and the usual Name control;
      Due in 13px tabular `--color-ink-2`, any slipped dates struck through
      before it, or a muted "—" for an Ongoing Goal; and Last check-in in 13px
      muted tabular figures ("today", "9 days ago", or "—"). The chips and
      tags wrap under the title when the row is narrow.
    - **Empty:** one muted line in place of the rows: "No Goals yet.", "No
      Goals match." under a filter, or "No Goals in My org." or "No Goals in
      Mine." on a scope, with **See everyone's →** on the default scope (see
      Scope).
  - **Table layout (`goalTableLayout`, `?layout=table`):** the same Goals, not
    grouped, with no totals row (ADR 0005).
    - **Above the table:** a **Columns** disclosure (`.gl-columns`) with a
      tick for every column but Title and an **Apply**, remembered in this
      browser by the `goal_table_hidden` cookie, then quiet small **Download
      CSV** (the filtered Goals in the import format) and **Edit values**.
    - **Columns:** Health (the badge), Title, Owner, Lifecycle, Delivery date
      and Last check-in, then one per offered Dimension and Field. Each label
      is a link that sorts by it, marked ↑ or ↓ and by `aria-sort`. Unsorted,
      the rows keep the list's problems-first order.
    - **Look:** 40px rows in 14px with `--color-row-hover`, no wrapping except
      a Long text Field's, which wraps from 240px, and the list's label row.
      The table scrolls sideways inside its own container.
    - **Edit values (`?edit=1`):** the table becomes one plain form with a
      muted hint, **Save** (primary) and **Cancel** (quiet). Its inputs are the
      Goal page's, in the Dimension and Field cells of the Goals the person
      Owns or is a Delegate on, and of any Goal for an Admin. A refused save
      saves nothing: an alert reads "Nothing was saved." and why, and each
      refused cell is marked invalid, with its reason under it in 13px
      `--color-danger-ink`.
  - **At 600px and below:** the search takes a full line under the chips, the
    label row hides, and each row stacks on its hairline: the Goal (title
    wrapping, chips under it) with its Health shape in the 40px gutter at its
    left, then the Owner, "Due …" and "Last check-in …", each date led by a
    muted label. Dimension tags are hidden, and group headers become
    full-width strips. The Table layout doesn't stack; it scrolls. It keeps
    every column, and its container fades the edge it can scroll toward: a
    12px `--color-border-strong` fade, covered by `--color-canvas` layers
    that scroll with the table, so a fade shows only while there is more to
    that side (the right at first, both mid-scroll, the left at the end, and
    neither on a table that fits).
  - **On a coarse pointer** the chips, + Filter, the search and the switch are
    at least 44px tall.
- **Scope (Goals and Follow-ups, `pageScopeSelect` in `scope.templ`):** the
  one select scoping the Goal list and the Follow-ups page, labelled "Scope"
  and submitted as `?org=`: **Everyone** (`all`), **Mine** (`mine`, the Goals
  the person Owns or is a Delegate on), **My org** for someone with anyone
  under them, then each other person whose Org holds Goals under **Other
  orgs** (their ID), by Name with their email so people with the same Name can
  be told apart. On both pages it is a chip (`pageScopeChip`), the same
  select behind a "Scope:" key, always on the set chip's
  `--color-primary-subtle`.
  - **The default scope:** an address naming no scope, or one that is
    malformed or names no one, opens on the person's default: My org for
    someone with anyone under them, Mine for everyone else, Admins included.
    The select shows it chosen, and the count lines and empty states read as
    for that scope chosen. A bare `/goals`, `/goals/download` or `/follow-ups`
    therefore means "my default", and links meant to land there stay bare
    (the Goals breadcrumb, Back to Goals, the refused Nudge page's Back, and
    the Goal list's Clear).
  - **Everyone is explicit:** `?org=all`. The Goal list's **See everyone's →**
    and Follow-ups' **Show everything** link to it, and a page on Everyone
    keeps it in every link that keeps the page's filters. A chosen scope is
    never overridden.
  - **An empty default:** the Goal list says "No Goals in My org." or "No
    Goals in Mine." with a **See everyone's →** link to the same view with
    `?org=all`. Follow-ups keeps its own empty line, whose Show everything is
    the way out.
  - **Needs an Admin is company-wide:** an Admin's job there is the company's,
    so the Needs an Admin group counts, and chips, every Ownerless and
    halted-parent Goal in the company under the page's Dimension values,
    while the other groups count the page's own rows. Its block keeps the
    page's scope in its link like every other block, so Show all and the
    other blocks come back to it. `/follow-ups?group=admin` always lists the
    company's rows whatever the scope, as its block counts them, heading "N
    Goals waiting on an Admin, most urgent first." or saying "Nothing needs
    an Admin." with no Show everything. An Admin whose own scope has nothing
    flagged still sees the blocks after the empty line while the company has
    a Goal that Needs an Admin.
- **Follow-ups (`followUpsPage` in `followups.templ`):** one full-bleed
  surface, its parts between hairlines, with 28px sides (16px at 900px and
  below).
  - **Header strip (`followUpsHeader`):** **Follow-ups** and, while any Goal is
    shown, a muted line with the count in `--color-ink`: "12 Goals to review in
    My org, most urgent first." At its right sits **What each signal means**, a
    small button that opens, by native `popover` with no script, a panel
    listing every signal by name and meaning. The strip wraps rather than
    overflow.
  - **Filters (`.rk-filters`):** a row of the Goal list's filter chips
    (Goal list, Filter bar), with its 12px padding and 8px gaps, over a 1px
    `--color-border` hairline: the Scope chip, then a chip per offered
    Dimension, reading "Team: any" unset and "Team: Platform" on the set
    chip's `--color-primary-subtle` once a value is chosen (a Retired value
    marked "(retired)"). Under htmx a change swaps in only `#follow-ups-body`, takes
    the header out of band and pushes the address. Without script an
    **Apply** button shows.
  - **Groups:** a `<nav>` of five plain blocks in one row, split by vertical
    hairlines: **Not Green**, **Waiting on a Check-in**, **Action Items
    overdue**, **Placement and dates** and **Needs an Admin**. A block shows
    its name (500, `--color-ink`), its count of Goals (24px, 600, tabular),
    then a chip per signal flagging any, with its count ("Check-in overdue ·
    3"). A block with Goals links to the page narrowed to it, keeping the
    scope and values, and takes `--color-row-hover` on hover. The current
    block takes the same fill, a 2px `--color-primary-strong` rule along its
    foot and `aria-current="page"`, and a **Show all** link follows the
    blocks. A block with none is muted, says so ("No Goal is Red or
    Yellow."), and links nowhere. At 1151px and below the blocks go two to a
    row, the fifth spanning both.
  - **The list:** column labels Health · Goal · Owner, and a last column,
    "Next step", named for assistive technology only. Rows are 44px, with the
    Goal column taking the slack, so the Owner and the Fix sit at the right.
    - **Health:** its badge, with the Run off Green after it in muted small
      text ("· 5 wks"). No Health is a neutral badge reading "—".
    - **Goal:** its title (500, `--color-ink`), then its chips: the Check-in
      overdue chip for Check-in overdue · 42 days, checks in weekly, Path to
      Green 12 days overdue, Milestone 3 days overdue and Action Item 3 days
      overdue; the Ownerless badge; and an outlined `.rk-tag` (1px
      `--color-border-strong`, `--radius-sm`, 13px `--color-ink`) for Asks for
      help ("of [Name]" or "· no one above the Owner"), Unlinked, Schedule
      conflict ("· due Nov 7, 22 days after its parent [parent]"), and Parent
      On Hold or Parent Cancelled. A Not Green row whose back-to-Green date
      slipped adds a `.rk-tag` reading "Back to Green Oct 3 ~~Sep 12~~ · 3 slips".
      Red and Yellow take no chip, since the badge says so.
    - **Chip popovers:** each chip's words are a button with a dotted
      underline that opens, with no script, a popover naming the signal and
      what it means: up to 360px, on `--color-surface` in a 1px
      `--color-border-strong` edge, `--radius-md` and `--shadow-pop`, never
      wider than the window. Above 600px, where the browser supports anchor
      positioning, it opens under its chip.
    - **Owner:** their Name, or "You".
    - **Fix:** one small button for the first that applies: **Reassign** (an
      Admin, on an Ownerless Goal), **Check in** (primary, on the person's
      own or Delegated overdue Goal), **Nudge** (a form post back to this
      page; once a Goal is nudged today, a disabled "Nudged today by
      [Name]"), **Link to a parent**, a Schedule conflict's **Check in** with
      its dates open, **Open parent**, **Suggest a parent**, **See Action
      Items** or **Open Goal**.
  - **Order:** most urgent first: Active Goals, then Ownerless, then by Health
    (Red, Yellow, none, Green), then the longest Path to Green, Milestone and
    Action Item overdue, then the furthest past its Check-in cadence, then the
    most kinds of signal, then by title.
  - **Empty:** one muted line replaces the groups and the list, and the
    header holds no count: "No Goals need attention.", or naming the scope,
    values and group, with **Show everything** (`?org=all`) when filtered.
  - **At 600px and below** the labels hide, and each row stacks on its
    hairline: its Health then the Goal on one line, the Owner under the Goal,
    then the Fix. No cell is reordered.
    The meanings panel puts each name over its meaning.
- **Goal page (`goalPage` in `goals.templ`):** a header strip, any banners,
  then a document column with a properties rail beside it. The page drops its
  width cap and padding. The column is up to 860px, centred in the space left
  of the rail. The rail is 300px at the right, behind a 1px `--color-border`
  left edge that runs the page's full height. At 900px and below the rail
  stacks under the column, with a hairline above it in place of its left
  edge.
- **Goal page header strip (`goalPageHeader`):** the 48px page header, with
  no `h1` (§ Page header).
  - **Breadcrumb:** "Goals › <first Goal it contributes to> › <its title>",
    the parent only when there is one. Goals and the parent are muted links
    that go to `--color-ink` on hover; the current title is `--color-ink` at
    500 and `aria-current="page"`. Each title is cut short with an ellipsis.
  - **Header actions (`.gp-actions`):** at the right, 8px apart. For whoever
    may check in (the Owner or a Delegate) on an Active or On Hold Goal, a
    primary **Check in** (`.btn.primary.sm`) to the Goal's Check-in page. On
    an Active Goal with a Check-in to repeat, where No change isn't refused, a
    quiet **No change** (`.btn.quiet.sm`) beside it, posting with htmx, or as
    a plain form post without script. The action menu comes last. At 600px
    and below the strip wraps, so the actions can drop under the breadcrumb.
  - **Action menu (`.gp-more`):** a secondary small button reading "More ▾",
    named and titled "More actions". It is a `<details>`, so it opens without
    script. Its panel (`.gp-menu`) floats 4px under the button, anchored right,
    240px wide and no wider than the screen less 32px, on `--color-surface`
    with a 1px `--color-border` edge, `--radius-lg`, 8px padding and
    `--shadow-pop`. It lists actions, never forms: each item is a plain link,
    36px tall (44px on a coarse pointer), in `--color-ink` at 500, with a
    `--color-primary-subtle` fill on hover. Each person sees only the actions
    they may take: Edit title and Why it matters, Hand off, Add a delegate,
    Link to a parent Goal, Suggest a parent, Add a child Goal, Edit Dimension
    values, Edit Fields, Mark or Unmark Top-level, Mark owner departed…, Mark
    returned… and Reassign. Add a child Goal is open to anyone, so every
    viewer sees the menu. For whoever may check in, the Lifecycle changes the
    Goal allows come last: **Put on hold…**, **Mark done…** and **Cancel…**
    on an Active Goal, **Cancel…** alone on an On Hold one. These lead to the
    Check-in page with that change picked and its section open
    (`/goals/{id}/checkin?lifecycle=<Lifecycle>`), for a Lifecycle change is
    made in a Check-in. Suggest a parent leads to its own form, and Add a
    child Goal to the New goal form with this Goal picked as the parent
    (`/goals/new?parent=<id>`). Every other item opens its form in place.
- **Goal page banners (`.gp-banners`):** under the header strip and across
  the full width, above the column and the rail. In order: Ownerless,
  Check-in overdue, Path to Green overdue, the graph signals (Unlinked,
  Schedule conflict, Parent On Hold or Cancelled), then Incomplete. Each is a
  strip with no box: a `--color-group` fill, 8px × 28px padding (16px sides at
  900px and below), 13/1.5 `--color-ink`, its bold name first, then what it
  means. A 1px edge along its foot ends each strip: dashed
  `--checkin-overdue-edge` for Check-in overdue and Path to Green overdue
  (`.alert.st`), so freshness keeps its warning edge; dashed
  `--color-ink-muted` for Ownerless; a `--color-border` hairline for the rest.
  For whoever may check in, the Check-in overdue strip ends, at the right,
  with a 600-weight **Check in now** link to the Goal's Check-in page; anyone
  else sees no link. The Path to Green overdue strip has no link, because
  CONTEXT.md surfaces both the same way. Strips stack with no gap. The ratios
  are in § Deviations 9.
- **Goal page head (`.gp-head`):** it opens the document column: the title as
  the page's `h1` (28/1.2, 600, -0.02em), the Why it matters lead, then any
  note the Lifecycle came with ("Outcome: …" for a Done Goal, "Reason: …" for
  an On Hold or Cancelled one) in muted text, 12px apart. The Goal's
  properties are in the rail, never the head.
  - **Why it matters lead (`.gp-lead`):** 16/1.6 in `--color-ink-2`, as wide as
    the column. No "Why it matters" heading shows; a visually hidden one
    (`.sr-only`) labels it for assistive technology, and a small "What goes
    here?" help button under it opens the definition in place.
- **Goal page sections (`.gp-section`):** the document column's parts, 40px
  apart (32px at 900px and below), each a plain section on the canvas. Its
  `h2` sits in a 32px row with a 1px `--color-border` hairline at its foot,
  with any open-in-place link at the row's right, and its content follows
  under it. A section's rows are each ruled off by a hairline. In order:
  - **Define this Goal**, for the Owner of a Proposed Goal;
  - **Latest status** (§ Status cells);
  - **Metrics (`.gp-metric`):** one row per Metric, on a grid: its name (14,
    500) over a muted "100 → 500 per week by Jun 30"; its sparkline, 160px wide
    (§ Deviations 6); and the current value in tabular figures (20, 600) with
    its unit muted, or a muted "—". Under the row sit a muted "Readings (n)"
    disclosure listing each reading against its target and, for the Owner, an
    **Edit Metric** disclosure. At 600px and below the sparkline drops to its
    own line.
  - **Milestones:** a muted "3 slips · 1 added or removed since Active" line,
    then a table with no fill: muted 12px Status, Date and Milestone labels and
    hairline rows. Status is the Milestone's mark badge or blank: a badge
    showing its word, **Overdue** in the Red tint (`badge r`), **Slipped** in
    the Yellow (`badge y`), and **Done**, **Removed** and **New** neutral
    (`badge lc`). Overdue and Slipped take the Red and Yellow tints but are not
    a Health, so they show their word and no Health shape, and a Report lists
    them the same way. Date is in tabular figures, a slipped date shown as
    § Status cells shows it. A Removed Milestone is struck through, with its
    reason under it. The table scrolls inside its own container on a phone.
  - **Highlights**, as "**Miss**: <Note>" rows, when there are any;
  - **Draft Highlights**, for the Owner and Delegates (§ Check-in form's
    optional sections: On the Goal page);
  - **Action Items:** the open ones about the Goal, when there are any: text ·
    Owner · "due Nov 7, 2026", a red **Overdue** badge when it is, and "from"
    its Report. Their owner gets **Close** (§ Close and Reassign).
  - **Comments (`#comments`):** threads oldest first. Each comment shows its
    author by Name, a muted timestamp and the body, with replies indented 24px
    and a **Reply** disclosure. After the threads comes the form to start one,
    open to everyone signed in.
  - **History** (§ History timeline).

  Metrics, Milestones and Comments show a muted "No … yet." line when empty.
- **Properties rail (`.gp-rail`, `goalRail`):** an `<aside>` named
  "Properties", in 13px, with 20px padding. Its groups are 16px apart, each
  after the first under a 1px `--color-border` hairline.
  - **Rows (`.gp-prop`):** a grid of a 96px label, the value and an optional
    link, at least 32px tall. The label is 12.5px, 500, `--color-ink-muted`,
    sentence case; the value is `--color-ink`. In order:
    - **Health:** the Health badge, or a muted "No Health";
    - **Lifecycle**;
    - **Kind:** Dated or Ongoing, as a help button, or "Not yet Dated or
      Ongoing";
    - **Owner:** the person control at 500;
    - **Delegates:** each Name on its own line beside the label, "departed"
      on a Departed Delegate, or a muted "No Delegates yet.", with a
      **Manage** link at the row's right, and "What is a Delegate?" under
      the row;
    - **Cadence:** "every 7 days";
    - **Delivers**, on a Dated Goal: every date it slipped from struck
      through, then the current date in bold;
    - **Level**, on a Top-level Goal: Top-level, as a help button.
  - **Groups:** each headed by a 12/1.5, 600 `h3` in `--color-ink`, with its
    link at the right:
    - **Dimensions:** with "What are Dimension values?" under its heading, then
      one row per Dimension, its values as tags (a retired one with a muted
      "(retired)") or a muted "unassigned", ending with "All Dimensions →". A
      Retired Dimension shows only when the Goal carries a value in it.
    - **Fields:** each Field's name in `.label` over its value, or a muted "not
      set", ending with "All Fields →".
    - **Contributes to**, with its Suggested parents under it, and
      **Contributed to by:** each linked Goal is its Health badge (its
      Lifecycle badge when it has no Health) then its title as a link in
      `--color-ink` at 500, on one line (`.gp-link-head`) with Ownerless when
      it is and a quiet **Remove** at the right for the Owner of either Goal.
      A muted "Owner <Name>" ends the entry. Removing posts at once, and the page that follows offers Undo
      (§ Toast).
- **Forms that open in place (`.gp-open`):** a Goal page action's form opens
  in the part of the page it concerns, never in a modal, side panel or the
  menu. A menu item or a heading's link leads to `/goals/{id}?open=<form>`,
  which is the Goal page with that one form open and its first control
  focused. The form sits under a `.label` heading naming it, with a quiet
  small **Cancel** link back to the plain page. A refused submit comes back as
  the same page with the same form open, the reason in an alert at its top
  and what was typed still in it. It is all links and plain form posts, so it
  works without script.
  - **Where each opens:** Edit title and Why it matters (the Owner or an
    Admin; the title and the Why it matters as they stand, with a **Save**)
    and Top-level, in the head under the lead. Hand off, Reassign, Mark owner
    departed and Mark returned in the rail under the Owner row. Delegates in
    their row: the current people with **Remove**, and an email input to add
    one. Link to a parent Goal under Contributes to, and
    the Dimension and Field forms in their groups. Add a Milestone under the
    Milestones table (a name, a date and **Add Milestone**, for the Owner or a
    Delegate until the Goal is Done or Cancelled). Without script, the Draft
    Highlight form in its section.
  - **Heading links (`.gp-group-head`):** a section or rail group whose form
    opens in place has a small link at the right of its heading, shown only to
    those who may use it and hidden while its form is open: **Manage** for
    Delegates, **Edit** for Dimensions and for Fields,
    **+ Add** for Contributes to and for Milestones, and **+ Log a highlight**
    for Draft Highlights. Each link's accessible name says what it acts on
    ("Edit Fields").
- **Status cells (`.gp-cells`):** the Latest status section opens with a row
  of cells, each a `.label` over its value, wrapping 12px × 32px apart:
  "Owner's Health" (a Health badge, with the Run off Green beside it in muted
  small text while the Goal is Yellow or Red: "Red · 5 wks"), "Rolled-up
  Health" (a Health badge, titled "Red: the lowest Health among Active children", with
  "· 3 of 12 overdue" when any child is Check-in overdue) and "Back to Green
  by" (a date, which shows a slipped date as a Milestone's does, counted
  within the current Run off Green: "Oct 3 ~~Sep 12~~ · 3 slips"). A cell shows only
  when it has something to say: no Rolled-up cell without Active children and
  no Back to Green cell without a Path to Green. The status text follows, then
  "**Path to Green:** <plan> — back to Green by Oct 3", any open Ask for help
  (a bold "Asks for help", then "of [Name]" or "· no one above the Owner",
  then an **Answer** link to the Goal's Comments), "**Why this differs from
  the Rolled-up Health:** …", and a muted byline: who wrote it by Name, "for"
  the Owner when a Delegate did, and its date. A Goal with no Health shows its
  message in place of the cells: "A Done Goal has no Health." over its last
  status, "A Proposed Goal has no Health until it is Active.", or "No
  Check-ins yet.".
- **History timeline (`goalHistory` in `timeline.templ`):** the Goal page's
  last section (`#history`), open on load with no outer disclosure. It is a
  single list, newest first, of every Check-in, Why it matters revision,
  title change, ownership change (a Handoff with its outcome, or an Admin
  Reassign), change to a Dimension value or Field, change to one of its
  "contributes to" links, Parent suggestion, Nudge, Milestone added outside a
  Check-in, Action Item about it raised, reassigned or closed, and Comment or
  reply about the Goal, made on the Goal itself or on a Report.
  - **Weeks:** entries are grouped by week, Monday to Sunday in the company's
    timezone. Each week is headed by a `.label` "Week of Sep 28", with the
    year added when it isn't this year. Each entry's time is a timestamp in
    that timezone too ("Jan 8, 15:04"; see Dates and times).
  - **The rule and markers (`.tl-list`, `.tl-mark`):** the week's entries hang
    16px apart off a 1px `--color-border` rule, each with a 12px marker on the
    rule, on a `--color-canvas` ground so the rule doesn't run through it.
    A Check-in's marker is its Health's dot (`--dot-size: 10px`), in the
    Health's ink and shape: a circle for Green, a triangle pointing down for
    Yellow, an octagon for Red. It reads without colour, and its badge names the
    Health beside it. Every other kind has a neutral `--color-ink-muted`
    marker with its own shape: a hollow ring for Why it matters and for a
    title change, a hollow diamond for an ownership change, a bar for a value
    change, a hollow arch (round on top, square below) for a link change, a
    dashed ring for a Parent suggestion, a dashed square for a Nudge, a double
    ring for a Milestone added outside a Check-in, a hollow tab (square on the
    left, round on the right) for an Action Item raised, reassigned or closed,
    a hollow speech-drop (round but for its bottom-left corner) for a Comment
    or reply, and a hollow square for a Check-in with no Health, such as one
    that put the Goal On Hold. Every entry also opens with its kind as a bold
    text label (Check-in, Why it matters, Title, Handoff, Reassign, Value,
    Link, Parent suggestion, Nudge, Milestone, Action Item raised, Action Item
    reassigned, Action Item closed, Comment for a thread's first Comment,
    Reply for a reply), its time muted beside it, so the marker is never the
    only signal.
  - **A title entry** reads "*old* → *new*", then who changed it, by Name.
  - **A Check-in entry** shows its Health badge and status, then any
    Lifecycle change, Path to Green, an "Asks for help" line when it marked
    its Path to Green as asking, the Date Slips it recorded (the delivery
    date, the Back to Green date or a Milestone, ~~old~~ new, and the reason),
    the Milestones it added, marked Done or Removed, and the Draft Highlights
    it discarded. Last comes who wrote it, by Name, "for" the Owner when a
    Delegate wrote it. Its Metric readings and its explanation of a Health that
    differs from the Rolled-up Health sit in a small "Readings and
    explanation" disclosure on the entry.
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
    identical Check-ins with a Why it matters revision between them show in
    full under All and fold under Check-ins. Chips still count every Check-in.
  - **A link entry** is on both Goals' timelines, phrased for the Goal it's
    on, with the other Goal's title as a link: "Requested to contribute to
    *Reduce outages*" on the child is "*Migrate displays* asked to contribute
    to this" on the parent. An Undo reads "Undid:" before the change it
    undid. Who made the change follows, by Name.
  - **An Action Item entry** shows the item's text: raised, "for [Name] · due
    Nov 7 · from <Report>"; reassigned, "[Name] → [Name]"; closed, its closing
    note. Who did it follows, by Name.
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
    (Green's circle, Yellow's downward triangle, Red's octagon), so it reads
    without colour. A finished period
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
  - **Filter chips (`.tl-chips`):** a row of chips above the list, 8px apart
    and wrapping on narrow screens: All, Check-ins, Date Slips, Why it
    matters, Title, Ownership, Links, Values, Action Items, Comments. Each
    carries its count in muted tabular figures. Check-ins counts and lists the
    Check-ins, the Nudges asking for one, and the Milestones added outside a
    Check-in; Date Slips the Check-ins that carry one; Links the link changes
    and Parent suggestions; Action Items each Action Item about the Goal
    raised, reassigned and closed; and Comments every Comment and reply about
    the Goal. A chip is a plain link to `/goals/{id}?history=<filter>#history`,
    so the page reloads with the filter in the address and History in view. A
    chip is 32px tall (44px on a coarse pointer) with `--radius-full`, a 1px
    `--color-border-strong` edge on `--color-surface`, and 13px, 500
    `--color-ink-2` text, taking `--color-primary-subtle` on hover. The
    current chip is `aria-current="page"`, with a `--color-primary-light`
    fill, a `--color-primary-strong` edge (§ Segmented controls: a chosen
    option's edge) and 600-weight `--color-ink` text. Both edges reach 3:1
    (§ Deviations 6).
  - **Paging:** the latest 5 entries show, a folded run counting as one.
    When there are more, a quiet **Show earlier** link (`?shown=25`) reloads
    the page with 20 more under the same filter. With nothing under a filter,
    a muted line says so ("No Date Slips."). Chips and paging are links, so
    History works without script.
- **Check-in page (`checkinPage` in `checkins.templ`):** one form read top to
  bottom, its sections plain and divided by hairlines, with no panel or
  box. The form is a column up to 720px, with a rail of context beside it
  (§ Check-in page's column), the two centred together within 1048px.
  - **Header:** the page header reads "<Goal title> › Check in", the title
    linking back to the Goal, with no actions. Under it a muted line
    (`.checkin-head`): "Week of Oct 19 · checks in every 7 days · last check-in
    Oct 5" (the week named by its Monday in the company's timezone, as
    History's weeks are; "every day" for a daily Goal; the last Check-in only
    when there is one). At the line's right, bottom-aligned and wrapping under
    it on a narrow screen, sits No change, for an Active Goal with a Check-in
    to repeat: a muted "Nothing changed since Oct 5?" beside a secondary **No
    change** button, or the reason it is refused in its place.
  - **Add to this check-in (`.checkin-add-bar`, `checkinAddBar`):** with
    script, for an Active or On Hold Goal, directly under the header line and
    spanning the form and the rail. It is not a panel: a row ended by a 1px
    `--color-border` hairline, a labelled group (`role="group"`, not a
    toolbar). In a row, 8px apart and wrapping: the label "Add to this
    check-in" (13px, 500, `--color-ink-muted`); **+ Insight**,
    **+ Accomplishment** and **+ Miss**, filled in `--color-primary-subtle`
    with `--color-primary-strong` text; a 1px × 24px `--color-border`
    divider; then **Move a date or Milestone** and **Pause, finish or cancel**
    ("Resume or cancel" for an On Hold Goal), outlined (no fill,
    `--color-border` edge, `--color-ink` text). All are `btn sm` with
    `--radius-full` corners and one label at every width. The Highlight
    buttons stand for the Highlights section's own: each scrolls to it and
    opens its editor there, and they wait while an editor is open. The other
    two each show their section, scroll to it and move focus to its heading;
    once their section shows, on load or by a press, they are used:
    `--color-surface-alt` fill and `--color-ink-muted` text, still pressable
    to go back to it. A Highlight button is never used, as a Check-in may
    carry several. At 600px and below the label sits above the buttons, which
    keep to one line that scrolls sideways inside the bar, the divider hidden.
    Without script the bar doesn't show.
  - **Sections:** "How is it going?", "Path to Green" (shown while Yellow or
    Red is picked, by CSS alone, or while it holds an error) and "Metrics"
    (when the Goal has any). Each is headed by an `<h2>` at the section-heading
    size (13/1.4, 600), with no step number, and a muted 13px note on its
    baseline: "Required while Yellow or Red", with " · 3 wks off Green" while
    the Goal is in a Run off Green, or "Record each Metric's current value".
    Sections sit 24px apart, each after the first opening with a 1px
    `--color-border` hairline and 24px above its heading; the heading sits
    16px above the fields, and fields sit 16px apart. Field labels are 13px,
    600 `--color-ink`, hints 13px in `--color-ink-muted`. A field that starts
    empty shows what the last Check-in said as ghost text.
  - **How is it going?:** the three Health choices (`.health-toggle`) share
    the full width, 12px apart, each 44px tall with `--radius-md` (their look
    is in § Deviations 6). Then "What changed since the last check-in?", then
    the Rolled-up Health block.
  - **Rolled-up Health (`.checkin-rollup`):** when the Goal has one. Not a
    panel: a 1px `--color-border` hairline, then a muted "Rolled-up Health:",
    the Health badge and its muted count line ("1 of 5 Red · 1 Yellow"), then
    "Explain the difference from Rolled-up Health", asked whenever there is a
    Rolled-up Health.
  - **Path to Green:** Plan; Back to Green by, with "Last time: Oct 3" and "A
    later date needs a reason." beside the date on one wrapping row; "Reason
    for moving it later", shown only while the date is later than last time's;
    then the checkbox, "**Ask for help** · goes to <Name>".
  - **Metrics (`.checkin-metric`):** each Metric is a row with 16px above and
    below and a 1px `--color-border` hairline between neighbours. On the left,
    its name in 600 over a muted 13px "per week · 100 → 500 by Jun 30 · last
    135.5" and, once it has a reading, a 6px `--radius-full` progress bar up to
    240px wide from baseline toward target, `--color-primary-strong` on a
    `--color-border` track. On the right, a 120px reading input with
    right-aligned numbers in tabular figures.
  - **Optional sections** follow under a hairline (§ Check-in form's optional
    sections).
  - **Errors and Off target:** an error about the whole Check-in shows at the
    form's top as an `.alert.r`, one about a field under that field. When a
    Green is submitted while a Metric is off target, an `.alert.y` notice at
    the form's top says so with a **See below** link to a warning after the
    optional sections (`.checkin-off-target`, `role="alert"`, on
    `--health-y-bg` inside a 1px `--color-border` edge and `--radius-md`),
    which lists each Metric's reading and target and offers a secondary
    **Confirm Green**.
  - **Submit bar (`.checkin-submit-bar`):** the form's last child, and the
    page's one floating surface: `--color-surface` inside a 1px
    `--color-border` edge, `--radius-lg` and `--shadow-pop`, with 12px × 16px
    padding. While the form runs past the window it sticks 16px above its
    bottom (`position: sticky; bottom: 16px`), and once scrolled there it
    sits in its own place at the form's end, so it never covers the last
    field. It is as wide as the form column, not the window. The page's
    `scroll-padding-bottom` keeps a field focused by Tab or an error clear of
    it. On the left, a summary of what the Check-in will record, in 14px
    `--color-ink-2`, its parts joined by " · " and wrapping whole: the Health
    badge picked (left out when the Lifecycle picked takes the Goal out of
    Active), "Path to Green" while Yellow or Red has a Plan, "2 Metrics" read,
    "1 Highlight" written or kept, "dates changed", and the Lifecycle change
    ("→ Done"). Script fills it and keeps it up to date; without script the bar
    holds only the buttons. On the right, a quiet **Cancel** back to the Goal
    and **Submit check-in**, primary and 44px tall. At 600px and below Cancel
    hides (the page header goes back to the Goal), the summary drops to 13px
    and Submit check-in grows to 48px.
  - **A Done or Cancelled Goal** gets the header line and the rail, and in
    place of the add bar and the form, "A Done Goal takes no Check-ins."
  - **A Check-in that can't be reached, or a refused No change:** the
    refusal page (`refuse`), headed "Can't check in", with **Back** to the
    Goal page: 403 when only the Goal's Owner or a Delegate may check in, or
    a 500 saying nothing was recorded. A Goal that doesn't exist gets the Not
    found page (404).
- **Check-in page's column (`.checkin-context`):** a rail of context for
  checking in, behind a 1px `--color-border` left edge with 24px inside it,
  in 13px text and with no boxes. It has three sections in order, 20px apart,
  each after the first under a 1px `--color-border` hairline, each headed by
  a muted 12px, 500 `h3` (`.checkin-rail-head`), sentence case:
  - **Last time (`.checkin-context-last`):** "Last time · Oct 5", the latest
    Check-in's Health badge and a muted "back to Green by Nov 2" when it had a
    Path to Green, then its Status in full in `--color-ink-2`. Left out before
    the Goal's first Check-in.
  - **Contributed to by (`.checkin-context-children`):** with children only,
    the Goal page's "Contributed to by" list read-only (no Remove), so the
    Owner sees which children set the Rolled-up Health.
  - **Dates (`.checkin-context-dates`):** "Dates" over one line, "Delivers
    Jul 2 · 2 planned Milestones, next Beta Apr 2", or "Ongoing · no
    Milestones" (`checkinContextDates`). It strikes no slipped date and marks
    no overdue Milestone; the form's dates section does.

  Above 900px the rail is 240–280px wide, on the right of the form and 48px
  from it, under the header line and the add bar, and sticky (`top: 24px`),
  scrolling within itself when it is taller than the window. At 900px and
  below it sits above the form, after the header line and the add bar, loses
  its left edge, ends in a hairline, and doesn't stick. At 600px and below it
  drops Last time and Dates, which the form's ghost text already covers, so
  only Contributed to by stays, and a Goal with no children shows no rail.
- **Check-in form's optional sections:** Highlights, dates and Milestones,
  and Lifecycle each sit in a `<details>` (`.checkin-more`) that starts folded
  and opens by itself when it holds an error or something typed.
  - **With script** (`checkinSectionsScript`, which marks the page
    `.checkin-js`), no summary and no ▸/▾ shows. The Highlights section always
    shows. The dates and Milestones section and the Lifecycle section show
    only while their `<details>` is open, as a plain section under its own
    `<h2>` at the form's section-heading size: "Dates and Milestones" and
    "Lifecycle". The add bar's buttons open them. The server's rules still
    open them on load, and their bar button shows as used: an error in the
    section, something typed in it, an overdue Milestone, `?open=dates` (a
    Schedule conflict's Fix on Follow-ups), and a Lifecycle picked by
    `?lifecycle=` (the Goal page's menu). A shown section never hides again;
    its fields are sent whether it shows or not. The script runs again on the
    form a refused submit swaps back, so the bar marks used whatever then
    shows.
  - **Dates and Milestones:** for a Dated Goal, Delivery date and its reason;
    then the Planned Milestones as a grid under muted Milestone, Date and
    Status labels (each its name in 600, its date, and Planned, Done or
    Removed, with a reason for a date change and for Removed), then any being
    added and one blank row, "Add a Milestone".
  - **Lifecycle:** radios in a wrapping row (Keep Active, Put On Hold, Mark
    Done and Cancel; Resume and Cancel for an On Hold Goal), then the Reason
    and, for any Goal not On Hold, the Outcome. With script, a Check-in that
    Cancels the Goal asks to confirm before it is sent.
  - **Highlights (the Highlight editor, `highlighteditor.templ`):** with
    script, the Highlights section isn't folded. It is an `<h2>` "Highlights"
    with a muted "A Report author can pull each one in" on its baseline, then
    three quiet small buttons, **+ Insight**, **+ Accomplishment** and
    **+ Miss** (`highlightAddButtons`), then the Highlights so far. An add
    button opens one inline editor (`.ie-editor`) at the top of the list: not
    a box, but a 2px `--color-border-strong` rule down its left with 16px
    inside it, headed "New Miss" (or Insight, or Accomplishment) in 13px, 600
    `--color-ink`, asking only for the Note, with **Save** (`.btn.sm`) and
    **Cancel** (`.btn.quiet.sm`). Save stays disabled until the Note is
    filled, and while an editor is open the add buttons and every Edit wait.
    Save makes it an item (`.ie-item`): a row under a hairline with the kind
    in bold and the full Note, as the Goal page lists Highlights ("**Miss**:
    …"), and quiet small **Edit** and **Remove**. Edit reopens the editor in
    place with a Kind select, so a wrong button can be corrected, and Cancel
    puts the item back as it was. Draft Highlights come in as items already
    saved, under a muted "Draft · logged by <Name>" line. Removing one
    discards it with the Check-in. A Draft logged with no Kind comes in as an
    open editor with the Kind select and Remove, not Cancel. Saving records
    nothing: the Highlights are sent with the Check-in, and an editor still
    open with a Note is sent as if saved.
  - **Without script,** the page has no add bar and no section headings, and
    each optional section is a folded `<details>` row between hairlines,
    opened by its 44px summary in 600 with a muted ▸ (▾ when open) at its
    right: "Flag a highlight (optional)", "Change dates or milestones · …" and
    "Pause, finish, or cancel this goal · …". The Highlights section has a
    numbered row of Kind and Note per Highlight, a Keep box on each Draft's
    row, and **Add another**, which posts the form back for one more row. The
    script upgrades that markup in place, so the server renders the same form
    either way.
  - **On the Goal page,** the Draft Highlights section uses the same editor.
    It shows to the Owner and Delegates while the Goal takes Check-ins or
    still has Draft Highlights pending. Each pending one is a ruled row: its
    kind in bold ("No kind" when it has none) and its Note, a muted "<Name> ·
    Nov 7, 15:04", and **Delete** at the right. With script, **+ Log a
    highlight** beside the heading is a quiet small button that opens the
    editor at the top of the list, headed "New Draft Highlight", with the Kind
    select (optional here: a Draft Highlight may have no Kind) and the Note.
    **Save**, enabled once the Note is filled, logs it at once with htmx and
    the section comes back from the server with it listed, with no page load;
    **Cancel** closes the editor. Each pending one gains a quiet small
    **Edit**, which opens it in the editor in place ("Edit Draft Highlight"),
    its Save posting the edit and its Cancel putting the row back, and its
    **Delete** reads **Remove** and deletes it at once, with no confirmation.
    A refused Save comes back with the editor open, the reason under the Note
    and what was typed kept. One editor is open at a time. Without script the
    heading's link opens the log form in place (`?open=draft-highlight`), and
    each row keeps **Delete**.
- **New goal page (`goalFormPage` in `goalform.templ`):** one form read top
  to bottom. The Finish defining page is the same page.
  - **Header strip (`goalFormHeader`):** **New goal**, or on Finish defining
    the Goal's title, linking back to it, › **Finish defining**. It holds no
    actions: the submit buttons are in Ready to activate.
  - **Layout:** a column up to 720px (`.gf-main`), with Ready to activate as
    a 240px rail 40px to its right, the pair centred in the content. Its
    sections are plain, each after the first under a 1px `--color-border`
    hairline with 24px above and below it. Section headings are `<h2>`s at
    13/1.4, 600, with no step number. Fields sit 16px apart, labels 13px, 600
    `--color-ink`, hints 13px `--color-ink-muted`.
  - **The Title and Why it matters** open the column with no heading of their
    own, under a muted 13px "Proposed until it's activated". The Title is a
    heading-style input at the page title's size (28/1.2, 600, -0.02em), on the
    canvas with no box, only a 1px `--color-border-strong` underline
    (`--color-danger-ink` when refused). It reads "Name the outcome" when
    empty and takes the focus on load. Why it matters follows, a textarea.
  - **Delivery:** the Kind tiles under a "Kind" legend, then the Check-in
    cadence, 24px apart.
  - **How you'll know:** its `<h2>`, with a muted note at the right of the
    line saying what activation needs ("At least one Milestone or Metric to
    activate", or "At least one Metric to activate" once Ongoing is chosen),
    then the Milestone and Metric editor (below).
  - **More detail:** its `<h2>` with a muted "optional, and editable later"
    beside it. **Contributes to** and **Where it fits** are `<details>` rows,
    48px each with a hairline between them: the name in 600 `--color-ink`, a
    muted "· " and what it holds ("· none", "· not set", or the names), and
    **Set** or **Change** at the right in 600 `--color-primary-strong`. A row
    opens by itself when it holds a refused input or, for Where it fits, a
    required value not set. Where it fits shows only when there are
    Dimensions or Fields. A Dimension taking several values offers a pill
    checkbox for each (`.gf-pick`) in a 1px `--color-border-strong` edge; a
    chosen one takes a `--color-primary-subtle` fill and a 1px
    `--color-primary-strong` edge. Contributes to is a search, with a chip for each
    Goal picked (its Health badge, its title and ×); without script it is a
    multiple select, with a Remove box on each chip.
  - **Ready to activate (`goalFormReady`, `.gf-ready`):** a rail, not a raised
    surface, behind a 1px `--color-border` left edge with 24px inside it, and
    sticky (`top: 24px`), so it stays in view as the form scrolls. Top to
    bottom, 12px apart: its `<h2>`; "3 of 6 done" in muted small text; a 6px
    `--radius-full` bar, `--color-ink-muted` on a `--color-chip-bg` track; the
    checklist, each item's label linking to the field that meets it and its
    mark ✓ in an 18px `--health-g-ink` circle or ○ as an empty 2px
    `--color-border-strong` ring, its done or to do for screen readers alone;
    the buttons, stacked and as wide as the rail, **Create and activate**
    (primary) over **Save as Proposed** (Enter in a field saves as Proposed);
    a muted line on what saving needs; and a small **Cancel** link. With
    script, the form posts to the checklist on load and as it is typed, and
    the rail is swapped for the answer, Create and activate staying disabled
    until every item is done. Without script the rail shows the checklist as
    the page rendered it, and the server decides.
  - **A refused submit** comes back as the same page, with what was typed. A
    summary leads the column (`.alert.r`): **The Goal wasn't created.** (**The
    Goal wasn't saved.** on Finish defining), over each problem, linked to its
    input. Each refused input takes a `--color-danger-ink` edge, with the
    reason under it in 13px `--color-danger-ink`.
  - **Finish defining** (`/goals/<id>/define`): the Title is the Goal's and
    read-only, and Why it matters takes the focus. The Goal's own Milestones
    and Metrics are a read-only list at the top of How you'll know, and
    Contributes to lists the Goals already asked, a Pending one "waiting on
    <Owner>".
  - **At 960px and below** the rail follows the column as its last section,
    under a hairline, and doesn't stick. At 600px and below the Kind tiles
    stack, the cadence is a 2×2 grid, and an editor's fields go one to a row.
  - **How you'll know (the Milestone and Metric editor):** with script
    (`goalFormRowsScript`), it starts as two quiet small buttons,
    **+ Milestone** and **+ Metric**, and no blank rows. Each opens one inline
    editor below the items, in the Highlight editor's look
    (`itemEditorStyles`: a 2px `--color-border-strong` rule at its left, a
    13px, 600 heading, the fields, and the Save and Cancel row): "New
    Milestone" asks for Name and Date, the Name two-thirds of the row; "New
    Metric" for Name, Unit, Baseline, Target, Target date, and Direction only
    while the target equals the baseline, two to a row. **Save**
    (`.btn.primary.sm`) stays disabled until the server would take what's
    typed, Enter in a field presses it, and **Cancel** (`.btn.sm`) drops a new
    editor or puts back the item it edits. While an editor is open the add
    buttons and every Edit wait, and Ready to activate's How you'll know item
    reads "Save the Milestone you started" (or Metric), to do, linked to the
    editor. Save makes an item under a hairline: its name in bold, then a
    muted line with a Milestone's date or a Metric's "baseline → target unit
    by date", and quiet small **Edit** (reopening the editor in place, "Edit
    Milestone") and **Remove**. The "Milestones" and "Metrics" `<h3>`s are
    `.label`s, shown only once their section has an item. Saving records
    nothing: the items are sent with the form, and an editor still open is
    sent as if saved. A refused submit brings a refused row back as an open
    editor showing why. **Without script,** How you'll know is rows of fields:
    a blank Milestone row and a blank Metric row, or those typed.
- **Kind tiles (`.gf-tile`, `.gf-segment`):** the New goal form's Kind,
  **Dated** and **Ongoing**, two tiles side by side, 12px apart (stacked at
  600px and below), with neither chosen at first. Each shows its Kind in bold
  over a muted 13px line saying what it means, beside a radio dot the tile
  draws. An unchosen tile is an outline: a 2px `--color-border-strong` edge at
  `--radius-md`, no fill, and an empty 16px dot in a 2px
  `--color-border-strong` ring. The chosen tile has a 2px
  `--color-primary-strong` edge, a `--color-primary-subtle` fill and a
  `--color-primary-strong` dot (§ Segmented controls: a chosen option's edge).
  The edge is 2px either way, so the tile keeps its size. The Dated tile
  holds the Delivery date, shown once Dated is chosen, its input on the
  tile's fill. Keyboard focus rings the whole tile. All of this needs
  `:has()`; a browser without it shows the native radio and the date, and the
  radio still chooses.
- **Check-in cadence (`.gf-segctl`):** the New goal form's Weekly | Every 2
  weeks | Monthly | Custom, one radio each, Weekly chosen at first: a 30px
  tinted track with 4px inside it (§ Segmented controls), its segments at
  `--radius-sm`, 44px on a coarse pointer. The focused segment takes the usual
  ring. Custom shows a "Days between Check-ins" number input under the
  control. At 600px and below the control becomes a 2×2 grid the width of the
  form.
- **Reports list (`reportsPage` in `reports.templ`):** a full-bleed list
  page: it drops its width cap and side padding (`.rp-index`), and every
  heading, row and cell keeps a 28px gutter (16px at 900px and below).
  - **Header strip:** **Reports**, with no count, and **New report**
    (`.btn.primary.sm`) at its right, leading to the Report builder.
  - **Order, 32px apart:** a muted lead line, then **Open Action Items** when
    there are any, then **Report Definitions**, each an `h2` with its rows 8px
    under it. With no Report Definitions, a muted "No reports yet." takes the
    list's place.
  - **Open Action Items (`.rp-chase`):** two groups, **Yours** and **My org**,
    either left out when empty, each soonest due first. A group opens with a
    32px `--color-group` header row (`.rp-group`): its name in 12px, 500
    `--color-ink-2`, then its count in muted tabular figures. Each row is at
    least 44px under a hairline, with `--color-row-hover` under the pointer,
    and reads: an "Action Item" `badge lc`, the item's text as a 500
    `--color-ink` link (to the Goal page's Action Items, or the publication's
    when it is about the whole Report), a red **Overdue** badge when it is
    past due, then a muted "Report · Goal · due Nov 7, 2026" and the Owner's
    Name. A row has no actions: closing and reassigning stay on the
    publication, the draft and Home.
  - **Report Definitions (`.rp-table`):** a table with no fill under a muted
    label row: Report, Goals, Last published, Draft, and an unlabelled
    Download column. Rows are 44px between hairlines, with
    `--color-row-hover` under the pointer, and don't wrap; the table scrolls
    in its own container when it is too wide. Never published come first,
    then those with the most changes, then the rest by when they were last
    published, oldest first.
    - **Report:** the name links to the draft, in 500 `--color-ink`, with its
      scope in small muted text beside it, sharing one ellipsis.
    - **Goals:** the count, then a 120px × 8px Health bar (`.rp-bar`) drawn as
      Home's is (§ Health distribution bar), when any Goal has a Health.
    - **Last published:** the date with its year ("Nov 7, 2026"), linking to
      that publication in 500 `--color-ink` (`.rp-published`), and a small
      muted "by <Name>", or a muted "Never", linking nowhere.
    - **Draft**, right-aligned: a muted "Not published" or "0 changes", or
      "3 changes" as a Yellow pill (`.rp-changes-hot`: `--health-y-bg`,
      `--health-y-ink`, 600, `--radius-full`).
    - **Download:** the Download menu for the last publication, once there is
      one.
  - **At 600px and below** the label row hides and each Report stacks as a
    ruled block: the name, then the scope, then each cell on its own line,
    empty ones left out.
- **Download menu (`downloadMenu`, `.rp-download-menu`):** a secondary small
  **Download** button that opens a native popover, without script and from
  the keyboard too, closing on Escape or a click elsewhere. Where the browser
  supports anchor positioning it hangs 4px under the button, lined up with
  its right edge; elsewhere it opens centred in the window. It floats on
  `--color-surface` in a 1px `--color-border` edge with `--radius-lg`,
  `--shadow-pop` and 8px padding, at least 200px wide. Its links are 36px
  (44px on a coarse pointer), 500 `--color-ink`, taking
  `--color-surface-hover` under the pointer: **Markdown (.md)**, then **PDF
  (.pdf)** when the app has a PDF renderer, or **PDF (print from browser)**,
  leading to the Print view, when it hasn't. It sits on each published
  Report's row of the Reports list and at the right of a publication's
  header.
- **A Report's draft page (`reportDraftPage` in `reports.templ`):** a header
  strip over one page in two columns: the main column is the Report as
  readers get it, its narrative composed in place, and a narrow column beside
  it holds its Delegates and History.
  - **Header (`.rp-draft-head`):** the page's strip, but sticky (`top: 0`, on
    `--color-canvas`), so the save status and Publish… stay in reach, and
    wrapping. Left to right: the breadcrumb "Reports › <name>", the name
    muted and not a heading (the main column's `h1` is), with a muted
    "Draft"; the save status (`.rp-save`), small and muted, which
    reads "Saving…" while **Save narrative** saves, then "Saved"; "Unsaved
    changes" once the author changes the Narrative form; or "Not saved" and
    why, when a save or an Add another is refused or fails; the
    baseline chip; then **Edit definition** (a secondary small button) and
    **Publish…** (`.btn.primary.sm`), both only for those who may manage the
    Report Definition: its creator, a Report Delegate or an Admin; anyone
    else gets no actions there. From 1280px it holds one row, whatever the save status says:
    the status keeps room for "Unsaved changes" (9em), so the strip doesn't
    shift as it changes, and takes only what is left beyond that, a long
    "Not saved" reason cut short; the chip and the actions keep their width,
    and the name gives way, cut short. Narrower, beside the sidebar, one row
    doesn't fit, and it wraps. At 600px and below it stops sticking and
    stacks into three rows.
  - **Baseline chip (`.rp-chip`):** a `<details>` whose summary is a 32px
    `--radius-full` pill in a 1px `--color-border-strong` edge, "Changes since
    **<baseline>** ▾". Its panel (`.rp-pop`), anchored left, is
    `--color-surface` in a 1px `--color-border` edge with `--radius-lg` and no
    shadow, its rows between hairlines: **Last publication** (once there is
    one), **30 days ago**, and **Pick a date…**, which opens a GET form with a
    "Changes since" date and a secondary **Apply**. Each choice is a link or a
    GET, so the baseline lives in the URL (`?baseline=`).
  - **Publish…:** a `<details>` whose panel (`.rp-publish`), anchored right,
    up to 560px wide with 24px padding, floats on a shadow of its own. It
    reads "Publish <name>?", then a muted summary of what is saved, which
    each save keeps current ("12 Goals · 3 exceptions · 4 Highlights ·
    changes since <baseline>. Readers can comment on the frozen copy."), then
    a quiet **Cancel** and a primary **Publish**. While the Narrative has
    unsaved changes it refuses: under the summary it says "**Save the
    narrative first.**" and Publish is disabled. Without script there's no
    unsaved state to track, and Publish works as ever. At 600px and below it
    spans the window less 16px each side.
  - **Columns (`.rp-draft-page`):** the main column, at most 780px, then
    56px, then the narrow column, 300px, both from the top. The page widens
    to 1200px for them (`.page:has(>.rp-draft-page)`). At 960px of the
    page's content width and under — a container query on the page body
    (`rp-draft`), so the sidebar counts — they stack, the narrow column under
    the main one at full width, 40px apart. A 1280px window leaves about
    1040px of content, so it keeps both columns, the main one about 680px. Each shrinks to the window rather than to its
    widest content, so at 390px nothing scrolls sideways.
  - **Main column (`report-draft`):** the Report as readers get it, top to
    bottom:
    - **Name:** the Report's name as the page's `h1` (§ Type scale, the
      Report's level 1), over a muted meta line
      (`report-meta`): what the definition selects in plain words, the Goal
      count, and what it reads changes against ("Team: Growth · 1 left out ·
      6 Goals · changes since Sep 4, 2026", or "changes since the previous
      publication, Sep 4, 2026").
    - **Introduction (`.rp-intro`):** the Report Definition's, once and
      read-only, in 15/1.7 `--color-ink`, a touch larger than the body text;
      left out when there is none. It is edited only with the definition
      (**Edit definition**, in the header).
    - **The Report's content:** the Health summary, the movement line, the Open
      Action Items, keeping Close and Reassign, each coming back to the draft,
      the narrative, then the exceptions, On track, and the Goals that
      entered or left, as a publication shows them (§ A publication). The
      narrative is the published one, left out when empty, for anyone who
      may not manage the Report; for its creator, a Report Delegate or an
      Admin it is the composer, **Narrative**.
  - **Narrative (`narrativeCuration`, `.rp-curation`):** a "Narrative" `h2`
    under the section heading's rule, with a small muted "Highlights since <baseline> · 2 of 5 in" beside it
    while there are Highlights to pick, or else "No Highlights to pick since
    <date>." Then each section in turn, Insights, Accomplishments and Misses:
    - **Its notes:** a `<details>` between hairlines whose 44px summary is
      the section's name in 15px, 600 `--color-ink`, with "+ Add note" in
      `--color-primary-strong` at its right while the section has no note.
      Inside, each note is its own textarea, then a quiet **Add another**.
    - **Its Highlights:** those whose picker selects the section (the one the
      author picked, or else the Highlight's own kind), each a hairline row:
      a checkbox and its Note, then a small muted Owner's Name · Goal link ·
      kind. Ticking it shows, in place of the kind, a section picker, Insight
      | Accomplishment | Miss: an outlined segmented control (§ Segmented
      controls) in 13px. A Highlight moved to another section moves under it
      when the narrative is saved.
    - **Saving:** one secondary **Save narrative** ends the section, and only
      it saves, so the Report log records only what the author meant to
      change. With htmx it swaps in the Narrative section as saved, with the
      save status, Publish…'s summary and History out of band; if the
      author changed the form while it saved, what they typed stays, only the
      out-of-band parts are taken, and the status still says "Unsaved
      changes". Without script it's a plain submit that reloads the draft.
      Typing marks the status "Unsaved changes" at once, and the "2 of 5 in"
      follows the boxes ticked; nothing posts as the author types. **Add
      another** saves nothing either: it swaps in the section with one more
      box. The form posts which Highlights it offered, so picks it doesn't
      list, of Highlights from before a later baseline, stay as saved and
      don't read as unsaved. Leaving or reloading the page with unsaved
      changes asks the browser's "Leave site?" question; the form is
      `autocomplete="off"`, so a reload shows what is saved, not typing the
      browser kept.
  - **Narrow column (`draft-side`):** everyone sees it, **Delegates** over
    **History**, 24px apart.
  - **Delegates (`reportDelegates`, `.rp-delegates`):** a "Delegates" `h2`
    over the Report Delegates as hairline rows,
    each its person, a `badge lc` "departed" on a Departed one, and for the
    creator and Admins a quiet small **Remove** at the right; "No Report
    Delegates yet." when there are none. Then the "What is a Report
    Delegate?" help disclosure, and for the creator and Admins an add form: an
    email field ("Delegate email") and a secondary small **Add Delegate**,
    wrapping 8px apart. A refused email comes back on the page, 422, in an
    `alert r` above the form, the email kept in its field. Everyone sees the
    list; a Report Delegate gets neither form, as only the creator and Admins
    add and remove them.
  - **History (`reportHistory`, `.rp-history`):** a "History" `h2` over one
    timeline, its entries 12px apart hanging off a 1px `--color-border` rule
    (`.rp-history-list`), each on a 10px round marker (`.rp-mark`) on the
    rule, ringed in `--color-canvas` so the rule doesn't run through it.
    First, muted, the current **Draft**: a hollow marker (`.rp-mark-draft`),
    "Draft", and "changes since <baseline>". Then, newest first, the Report
    Definition's publications merged with the rest of its Report log:
    - **A publication:** a marker filled with the accent, `--color-primary`
      (`.rp-mark-published`); "Published", linking to its publication; then a
      small muted "<date, time> · <Name>", with its year. A publication from
      before the Report log was kept shows the same.
    - **Any other Report log entry:** a plain `--color-ink-muted` marker;
      what changed in words ("Title: EU MBR → EU monthly review", "Rule
      added: Owner is in the org of Priya", "Report Delegate added: Dana",
      "Narrative: picked “Signed Acme” into Accomplishments; Insights notes
      changed"); then a small muted "<date, time> · <Name>". The log's own
      "Published" entries are left out, as the publications stand for them
      and link.

    Times are in the company's timezone. The newest 20 entries after the
    Draft show, the rest behind **Show all N**, N counting them all. Everyone
    who can open the Report reads it, and nothing on it can be changed or
    undone.
- **A publication (`publicationPage` in `reports.templ`):** a published
  Report, frozen, in a reading column.
  - **Header strip:** the breadcrumb "Reports › <Report Definition> › Nov 7,
    2026", the Report Definition linking to the draft and the date the current
    page in 500 `--color-ink`, with the Download menu at the right.
  - **Reading column (`.rp-read`):** up to 880px, centred, its parts 24px
    apart: the Report's name (an `h1`, 26px, the Report's level 1 in § Type
    scale), two muted lines ("Published Nov
    7, 2026, 15:04 by <Name> · changes since the previous publication, …" and
    "Selection: …"), the Report's content, then Action Items raised here.
  - **The Report's content (`reportContent`), which the draft's main column
    reads too:**
    - **Health summary (`healthSummary`, `.rp-summary`):** one row sized to
      its content, not a grid of tiles and with no rules: a "Health" label
      (`.label`, the Report's level 4), then four pairs, Red, Yellow, Green
      and Check-in overdue, split by a muted "·". A pair (`.rp-summary-pair`)
      is the chip, its shape alone (Check-in overdue's the grey, dashed-edged
      `.st` chip with its `.dot`), then its count in `--color-ink` tabular
      figures at the row's size and its word: "● 2 Red · ▼ 1 Yellow · ● 3
      Green · ■ 6 Check-in overdue". It reads like the movement line under
      it. On a narrow column or a phone the row wraps between pairs, never
      inside one (`white-space:nowrap`), so it needs no breakpoint or
      container of its own. On the draft its rule names `.rp-body` to stay a
      row inside `.rp-body section`'s column.
    - **Movement line:** "**Since the previous publication:** 2 turned Red ·
      1 back to Green", or "No movement since …".
    - **Open Action Items:** as rows (`.rp-rows`), at least 44px between
      hairlines: "text · <Owner> (was <Name>'s) · due Nov 7, 2026 · about
      <Goal>", **[Closed since]** on one closed since, then Close and Reassign.
    - **Introduction and narrative (`.rp-intro`, `.rp-narrative`):** the
      introduction, then the narrative, both in 15/1.7 `--color-ink`, a touch
      larger than the body text: under its "Narrative" heading, Insights,
      Accomplishments and Misses, each a 15px, 600 heading over the author's
      notes and the picked Highlights as a bulleted list, each credited in
      muted 13px " — <Owner>, <Goal>". A section with neither is left out,
      and an empty narrative is left out whole, heading and all. The print
      page heads it with its own `h2`, as it does Exceptions.
    - **Section headings:** each section of the Report (Open Action Items,
      Narrative, Exceptions, On track, and Goals that entered or left, on
      the draft and the publication alike) opens with an 18px, 650 `h2` under a full-width 1px
      `--color-border` rule, 20px above it and 20px of margin over it, so
      with the column's 24px gap it stands about 44px below what comes before.
    - **Exceptions N:** the count beside the heading, 14px muted. Each
      exception is an entry (`.rp-block`), separated from the next by 20px
      of space and one 1px hairline; the first, right under the heading, has
      no rule of its own. Its
      head reads Health first: its Health badge with "· 2 wks" and "· was
      Green" small and muted straight after it (`.rp-health`), then its title
      (16px, linking to the Goal) over a small muted "Owner · Lifecycle · due
      Nov 7, 2026", slipped dates struck through, then at the right its other
      badges (Check-in overdue as the chip, Ownerless in Red, New Date in
      Yellow, the rest as `badge lc`). At 600px and below, and in a block
      28rem wide or narrower (a container query), the Health and title keep
      their line and the badges drop
      under them; nothing is reordered.
      Under it sit "**Changed:** …", "**Contributes to:** …" and the Report
      Definition's Fields. Then its labelled rows (`.rp-facts`, a `dl`): each
      label a `.label` (12px, 500, `--color-ink-muted`) in a 120px column
      beside what it labels, 16px apart, rows 8px apart; at 600px and below,
      and in a block 28rem wide or narrower, the label goes above it. Why it
      matters and Status come first, as plain text with no tint or box.
    - **Path to Green, the block's one tinted box (`.rp-tint`):** only on a
      Red or Yellow Goal, in that Health's tint (`.rp-tint-r`,
      `--health-r-bg`, or `.rp-tint-y`, `--health-y-bg`), `--radius-md`,
      12px × 16px padding: "**Path to Green:** …" with its back-to-Green date,
      the label in the Health's ink (`--health-r-ink` / `--health-y-ink`),
      and any **Asks for help** with it. The back-to-Green date shows the date
      it last slipped from in the Run off Green struck through, and " · n
      slips" once it slipped more than once, as a Milestone's date does ("back
      to Green by Oct 3, 2026 ~~Sep 12, 2026~~ · 3 slips"). A Path to Green past its date keeps
      the Health's tint and carries a Red **Overdue** badge. A Green Goal's
      block, and one that is only Check-in overdue, has no tinted box.
      The rest of the labelled rows follow: Milestones and Metrics, each a
      list, Rolled-up Health with its count, Supported by, and Why the Health
      differs.
    - **On track:** a table with no fill (Health, Goal, Owner, Due) in its own
      scrolling container; at 600px and below each row becomes a ruled grid,
      its Health then the Goal on one line, the Owner and Due under the Goal.
    - **Linked Goals** ("Contributes to:", "Supported by:") read each Goal's
      Health badge then its title, joined by " · ": "[Red] Alpha ledger ↓".
    - **Goals that entered or left this report:** a Goal, Change, Why table,
      when there are any.
  - **Discussion (`goalDiscussion`, `.rp-discussion`):** a footer under each
    exception and each On track row, behind a hairline: "N comments" (a
    `--color-primary-strong` disclosure of the threads) or a muted "No
    comments", then a secondary small **Comment** that opens its form. Each
    thread hangs off a 1px left rule, replies indented 24px behind their own
    **Reply**. The Report's author also gets **Make action item** on each
    comment. It is all `<details>` and form posts, so it works without script.
  - **Action Items raised here:** every Action Item raised on this
    publication, as rows: an open one with Close and Reassign, a closed one
    "**[Closed Nov 9, 2026]** — <note>", or "None yet." The Report's author
    also gets a secondary small **Add Action Item**, opening an inline form
    of Action Item, Owner (email), Due and About.
- **Close and Reassign (`closeActionItemForm`, `reassignActionItemForm`,
  `.rp-inline-form`):** an open Action Item's owner gets **Close**, and those
  who may hand it on **Reassign**. Each is a `<details>` whose summary is a
  secondary small button inline beside the item. Opened, it takes the row's
  full width and shows its form: labelled fields that wrap 8px apart, each
  label a 13px, 600 caption over its input, aligned on the inputs' bottom
  edge. Close asks for a "Closing note" and Reassign a "New owner" email,
  each with a primary small submit, and the form comes back to the page it
  was on. Home and the Goal page carry their own copy of the
  `.rp-inline-form` rules.
- **Report builder (`reportBuilderPage` in `reportbuilder.templ`):** one
  centred column up to 720px of plain sections, 24px apart.
  - **Header strip:** the breadcrumb only, "Reports › New" or "Reports ›
    <Report Definition> › Edit", with no actions.
  - **Order:** the title, "New report" or the edit title, as the column's
    28px `h1`; when a save is refused, an alert (`.rb-problems`, a 1px
    `--color-danger-ink` edge, `--radius-md`, "**The report wasn't saved.**"
    and the reasons in `--color-danger-ink`); **Name** and **Introduction**;
    **What's in this report**, an outlined segmented control (§ Segmented
    controls), Goals matching rules | Goals I pick, which with `:has()` shows
    only its own section; **Rules**, under a hairline: each rule a wrapping
    row (`.rb-rule`) of What the rule tests, Operator, Values and its ×, then
    a quiet small **+ Add rule**, then **Also include** and **Leave out**,
    each a Goal picker; or **Goals I pick**, one Goal picker; **+ Fields to
    show**, a quiet small disclosure when there are Fields; **Matches**
    (`.rb-rail`); and **Save report**, primary, with a secondary **Show
    matches** beside it without script.
  - **Goal picker (`reportGoalPicker`):** a legend (with a help button for Also
    include and Leave out), then the picked Goals as chips (`.rb-chip`):
    `--radius-full` pills in a 1px `--color-border` edge, each its title and
    its round ×. With script, a full-width search lists matches as buttons,
    each a title and a muted Owner; picking one adds its chip and refreshes
    Matches. Without script, a multiple select of every Goal not yet picked,
    and a "Remove" checkbox on each chip in place of its ×.
  - **Matches:** an `h2` "Matches 12 Goals" over 40px rows between hairlines,
    each a Goal's Health badge (or its Lifecycle as a `badge lc`), its title
    in 500 `--color-ink`, and a muted "added" for a Goal only Also include
    brings in. The first 10 show, then a muted "+ 2 more · 1 left out". With
    htmx it refreshes as the inputs change, and it is `aria-live="polite"`.
- **Icon remove button (`.btn.rb-remove`):** the Report builder's × on a rule
  row and on a Goal chip. It is a 40px square, the height of the selects
  beside it, with a 1px `--color-border-strong` edge, the secondary button's
  hover and the standard focus ring. On a rule row it submits the form
  without the row, so it works without script. On a Goal chip it is round and
  forms the pill's right end: the chip keeps only its 12px left padding, so it
  stays compact. On a coarse pointer it is 44px wide as well as tall. It
  shows only the ×, so it always carries an `aria-label` and a matching
  `title` naming what it removes ("Remove rule", "Remove <Goal title>").
- **Admin page (`adminPage` in `admin.templ`):** for an Admin only. "Admin"
  is the header strip's title, with no actions, and every part is a plain
  section, 32px apart (24px at 900px and below):
  - **Intro:** a muted "The tools only an Admin uses."
  - **Tools (`.ad-tools`):** four link rows, **Dimensions**, **Fields**,
    **Import goals** and **Definition log**, each at least 44px between
    hairlines: its name (14px, 500, `--color-ink`) in a 200px column and a
    muted line saying what it is for beside it. On hover the name turns
    `--color-primary-strong` and is underlined. At 600px and below the line
    drops under the name.
  - **Sections (`.ad-section`):** each an `h2` over a muted sentence saying
    what it is for, then its rows. A table in a section has no fill: muted
    12px labels over a hairline, and hairline rows. A row's action is a small
    secondary button at its right. In order:
    1. **Directory sync**, only when the app has one: its status ("Last synced
       Nov 7, 15:04. Read 42 people.") and **Sync now**, which with script
       reads "Syncing…" while the sync runs.
    2. **Set-password links**, only with password sign-in: an Email field and
       **Issue link** on one row, up to 560px wide. A link just issued shows
       once, in a full-width read-only input, with a muted line saying it works
       once and expires in 72 hours.
    3. **Admins:** every Admin by Name, each with **Remove Admin…** (a muted
       "you" on the viewer's own row), then an Email field and **Make Admin**.
    4. **Ownerless Goals:** Goal, Former Owner and its Lifecycle as a
       `badge lc`, or a muted "No Goals are waiting for reassignment."
    5. **Departed people:** each by Name, with **Mark returned…**, or a muted
       "No departed people."
  - **Confirming:** Remove Admin… and Mark returned… ask to confirm with
    script, and post at once without it. A refusal shows under its form in
    `--color-danger-ink`. At 600px and below a field-plus-button form wraps.
- **Dimensions page (`dimensionsPage` in `dimensions.templ`):** anyone signed
  in can read it, and only an Admin sees its controls. "Dimensions" is the
  header strip's title, over a muted intro line.
  - **Define a Dimension:** for an Admin, a primary button that is a
    `<details>` summary, so it works without script, opening the form under it
    as a plain section up to 560px wide: Name, Values ("comma-separated"),
    "Each Goal takes" (one value or several) and "Its list is" (Fixed or
    Extendable), then **Create Dimension**.
  - **The list (`.dm-list`):** one row per Dimension between hairlines: its
    Name as an `h3`, a muted "retired" after it once Retired, a muted small
    rules line ("Each Goal takes one value · Fixed list · Required"), then its
    values as tags, a Retired value's struck through and muted in a dashed
    edge. With no Dimensions, a muted "No Dimensions yet."
  - **Edit (`.dm-edit`):** for an Admin, a small secondary **Edit**
    `<details>` at the end of each row, opening the switches (one button each,
    naming the change it makes), a row per value (**↑** and **↓**, a rename
    input with **Rename**, a danger **Retire**, and **Merge** into another
    value; a Retired value shows **Restore** instead, which is not an Undo:
    it takes no token and works at any time), **Sort alphabetically**, a "New
    value" input with **Add value**, and **Retire this Dimension** or
    **Restore this Dimension**.
  - **Confirming and Undo:** retiring a value happens at once, and the page
    that follows offers Undo in the toast. Merge and Retire this Dimension ask
    to confirm with script.
  - **A refused switch to one value (`.dm-refusal`):** "<Dimension> still takes
    several values" as an `h2`, with a list linking each Goal that carries
    several, behind a 2px `--color-danger-ink` rule down its left, announced as
    an alert.
- **Fields page (`fieldsPage` in `fields.templ`):** laid out as Dimensions
  is. "Fields" is the header strip's title, over a muted intro line. For an
  Admin, **Define a Field** opens a form of Name, "It holds" (a number, a
  short text, a long text or a date; "this can't change later") and Unit,
  then **Create Field**. The list (`.fd-list`) has a row per Field between
  hairlines: its Name as an `h3` ("retired" once Retired), a muted type line
  ("Number in $ · Required"), and for an Admin **Make it required** (or
  optional) and **Retire this Field** (or **Restore this Field**). With no
  Fields, a muted "No Fields yet."
- **Import Goals page (`importsPage` in `imports.templ`):** "Import Goals" is
  the header strip's title, over a muted line naming the column format's
  document. Anyone but an Admin sees only "Only an Admin may import Goals."
  An Admin sees the form (`.im-form`), a plain section up to 560px wide: a
  refused upload's reason as an alert, the Spreadsheet file input (CSV or
  XLSX), **Dry run** (primary) and **Commit** (secondary), and three muted
  hints. After a run, the report (`.im-report`): a summary alert, then a table
  of Row, Goal and Errors laid out as Admin's, each row's errors a list in
  `--color-danger-ink` or a muted "None", scrolling inside its own container
  at phone width.
- **Definition log (`definitionLogPage` in `definitionlog.templ`):** anyone
  signed in reads it, and Admin's tools link to it. "Definition log" is the
  header strip's title, with no actions, over a muted intro line: "Every
  change to the Dimensions and Fields, newest first", or "Every change to
  <name>" when narrowed to one.
  - **Narrowing (`.dl-scope`):** a GET form, so it needs no script: "Show
    changes to", a select of All Dimensions and Fields then each Dimension
    and each Field, and a small secondary **Show**.
  - **Changes (`.dl-section`):** an `h2`, "Changes", over hairline rows
    (`.dl-list`), newest first. A row is the change in words, then a muted
    small "— <Name> at <date and time>". With none, a muted "No changes
    logged yet."
  - **Older changes:** the whole log shows its newest 100 changes. When there
    are older ones, a muted small line under the rows holds a plain link,
    **Older changes**, to the 100 before the last one shown
    (`/definition-log?before=<id>`), on a page just like this one. A log
    narrowed to one Dimension or Field shows every change to it, unpaged.
- **Delegated to me (`delegatePage` in `delegates.templ`):** the Delegate
  page, linked from Home's Your goals line. "Delegated to me" is the header
  strip's title, with no actions.
  - **The section (`.dg-section`):** an `h2`, "Goals delegated to me", over a
    muted "Goals whose Owner has authorized you to write Check-ins on their
    behalf.", then a row per Goal (`.dg-list`). With none, a muted "No Goals
    have been delegated to you yet." instead of the rows.
  - **A row (`.dg-row`):** at least 44px between hairlines. Its Health badge
    for an Active Goal with a Health, or its Lifecycle as a `badge lc`, in a
    96px column; then the Goal's title as an `h3` link, over a muted small
    "Owner <Name> · Last check-in 3 days ago by <Name>" ("· No Check-ins
    yet." before the first); then, at the right, a small primary **Check in**
    on an Active or On Hold Goal, or a muted "A Proposed Goal takes no
    Check-ins." naming any other Lifecycle. The row wraps on a narrow screen.
- **Pending links and Pending handoffs (`pendingLinksPage` in `links.templ`,
  `pendingHandoffsPage` in `handoffs.templ`):** the requests waiting on the
  person's decision, laid out alike (`pendingStyles`). The page's name is the
  header strip's title, with no actions, and its one section (`.pd-section`)
  is an `h2` over a muted sentence, then a row per request (`.pd-list`).
  - **A row (`.pd-row`):** at least 44px between hairlines, what is asked at
    the left and its decision at the right, wrapping under it on a narrow
    screen. **Accept** is a small primary button and **Reject** a small
    danger one, which sends at once; the page that follows offers Undo in the
    toast. With nothing pending, one muted row says so.
  - **Pending links:** "Link requests", "Goals asking to contribute to yours.
    Accept one to link it into the graph." A row reads "<Goal> (Owner <Name>)
    wants to contribute to <Goal>", any note under it indented 12px from a
    1px `--color-border` rule (`.pd-note`). "No pending requests."
  - **Pending handoffs:** "Handoffs to you", "Goals someone wants to hand to
    you. Nothing changes hands until you accept." A row reads "<Goal> —
    <Name> wants to hand this Goal to you". When the Goal has Delegates who
    aren't Departed, Accept sits under "Delegates to keep", a box for each,
    checked. "No pending handoffs."
- **Suggest a parent (`suggestParentPage` in `suggestions.templ`):** the form
  by which someone other than the Owner suggests a parent for a Goal, from
  the Goal page's More menu or a Follow-ups Fix. "Suggest a parent" is the
  header strip's title, with no actions. One plain section (`.sg-section`),
  up to 560px wide: an `h2`, "Parent suggestion", over a muted "For <Goal>,
  owned by <Name>, who decides whether to link it."; a refusal's reason as an
  alert; **Parent Goal**, a select of the Goals it could contribute to;
  **Note (optional)**; then a primary **Suggest** and a quiet **Cancel** back
  to the Goal page. A refused submit comes back as sent.

## Motion

- Transitions run 150–200ms on `--ease-out`. Every click and hover gets
  immediate feedback.
- Nothing loops. Under `prefers-reduced-motion`, drop the transforms and keep
  opacity changes of 150ms or less.

## Print variant

The Report Print view (`publicationPrintPage` in `internal/web/reports.templ`)
is a document for paper and PDF, so it doesn't use the screen system. It is
set in a serif and declares its own tokens in its own style block. Its text is
black on white, and its marks of Health and trouble keep the screen's colours,
so a colour printer or a PDF shows Red, Yellow and Green as the app does. The
colour degrades to black and white: every coloured mark also carries a shape or
a word, so a black-and-white printer loses nothing. It doesn't load `app.css`,
so it copies the values it needs, and it ignores the Theme menu's choice: the
page stays light whatever is chosen.

```css
:root {
  --print-font:  'Source Serif 4', Georgia, serif; /* weights 400 and 600 */
  --print-ink:   #000; /* text and heavy rules; true black prints cleanest */
  --print-ink-2: #444; /* bylines, meta lines, small-caps labels */
  --print-rule:  #999; /* hairlines between Goals and table rows */
  /* Health's tint and ink: the screen's light --health-* and .lc values */
  --print-health-r-bg: #FDECEC; --print-health-r-ink: #A61B1B;
  --print-health-y-bg: #FFF3D1; --print-health-y-ink: #7A4F00;
  --print-health-g-bg: #E5F3EA; --print-health-g-ink: #1C6435;
  --print-health-none-bg: #F7F7F8; --print-health-none-ink: #33363C;
  /* Check-in overdue's grey chip: the screen's light --checkin-overdue-* */
  --print-overdue-bg: #E9EAEC; --print-overdue-ink: #3B4048; --print-overdue-edge: #888F97;
}
```

Each coloured badge's ink on its tint, and on bare paper in case a browser
drops the tint anyway (`scripts/contrast.py` checks these against the hex
given):

| Mark | On | Ratio |
| --- | --- | --- |
| Red ink `--print-health-r-ink` `#A61B1B` | Red tint `#FDECEC` / paper `#FFFFFF` | 6.58 / 7.52 |
| Yellow ink `--print-health-y-ink` `#7A4F00` | Yellow tint `#FFF3D1` / paper `#FFFFFF` | 6.45 / 7.13 |
| Green ink `--print-health-g-ink` `#1C6435` | Green tint `#E5F3EA` / paper `#FFFFFF` | 6.27 / 7.18 |
| No Health ink `--print-health-none-ink` `#33363C` | No Health tint `#F7F7F8` / paper `#FFFFFF` | 11.31 / 12.11 |
| Check-in overdue ink `--print-overdue-ink` `#3B4048` | its tint `#E9EAEC` / paper `#FFFFFF` | 8.67 / 10.43 |
| Check-in overdue edge `--print-overdue-edge` `#888F97` | paper `#FFFFFF` | 3.27 |
| Body ink `--print-ink` `#000000` | Red tint `#FDECEC` (a Red Path to Green) | 18.39 |
| Body ink `--print-ink` `#000000` | Yellow tint `#FFF3D1` (a Yellow Path to Green) | 18.99 |

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
- A Goal's Health comes before its title, as on screen: an exception's head
  reads its Health, its "· 2 wks" and "· was Green", then the title, then the
  other badges, and a linked Goal reads "[Red] Alpha ledger ↓". Health shows
  as a badge in its tint and ink, with its shape and its word shown beside it
  (on screen the word is screen-reader text), so it reads without the
  colour. The shape is the screen's drawn `.dot` (Red's octagon, Yellow's
  downward triangle, Green's circle), copied into the page's style block, in
  the Health's ink and kept when printed (`print-color-adjust: exact`), not a
  character.
- The badges the screen colours keep their colour: Health, a Milestone's
  Overdue or Slipped mark, Ownerless, New Date and a Path to Green's Overdue
  in the Health colour they take on screen, and Check-in overdue in its grey
  chip with a dashed edge. Body text stays black.
- A Goal block lays out as on screen. Why it matters, Status, Milestones,
  Metrics and the block's other facts sit in a label column (`.rp-facts`): the
  label 7.5rem wide in the small-caps `.label` style and `--print-ink-2`, its
  value beside it with no indent.
- A Red or Yellow Goal's Path to Green, with its Ask for help, sits in a box
  tinted in that Health (`.rp-tint-r` or `.rp-tint-y`), as on screen, with
  its "Path to Green:" label in the Health's ink. The Health decides the
  tint, not the date: a Path to Green past its date keeps its Health's tint
  and says so in its Overdue badge, so an overdue Yellow one prints Yellow.
- Coloured badges and the Path to Green box set `print-color-adjust: exact`
  (and `-webkit-print-color-adjust: exact`), so browsers keep their tints when
  printing and in PDF instead of dropping background colour.
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
   as the shape a list shows alone (§ Tokens):
   - Green: `#E5F3EA` / `#1C6435`, shape `#2E7D4F`
   - Yellow: `#FFF3D1` / `#7A4F00`, shape `#B27B00`
   - Red: `#FDECEC` / `#A61B1B`, shape `#C62828`

   In dark mode, the same hues sit on 20%-alpha fills with light text
   (`--health-*` in the dark block), and the shapes go bright. The
   `--color-success`/`warning`/`error` tokens are for alerts and form
   validation, not for Health. A badge's text reaches 4.5:1 on its fill, and
   the shapes' ratios are in § Deviations 10. A dark fill is 20% alpha, so it
   is measured as it lands: over the canvas, a hovered row and a raised
   surface (the Check-in submit bar's summary). A Health's tint
   (`--health-*-tint`), the fill at half strength under an unpicked Check-in
   choice, is measured too, a dark one over the canvas the choices sit on.

   | Theme | Mark | On | Ratio |
   | --- | --- | --- | --- |
   | Light | Red ink `#A61B1B` | Red fill `#FDECEC` | 6.58 |
   | Light | Yellow ink `#7A4F00` | Yellow fill `#FFF3D1` | 6.45 |
   | Light | Green ink `#1C6435` | Green fill `#E5F3EA` | 6.27 |
   | Dark | Red ink `--health-r-ink` `#F8A39B` | Red fill over canvas `#3C1A1C` / over row hover `#452326` / over surface `#411F21` | 7.89 / 7.04 / 7.42 |
   | Dark | Yellow ink `--health-y-ink` `#F5D06B` | Yellow fill over canvas `#3B3110` / over row hover `#44391A` / over surface `#403515` | 8.65 / 7.65 / 8.12 |
   | Dark | Green ink `--health-g-ink` `#8EDDAF` | Green fill over canvas `#132D21` / over row hover `#1C362B` / over surface `#183226` | 9.20 / 8.13 / 8.61 |
   | Light | Red ink `--health-r-ink` `#A61B1B` | Red tint `--health-r-tint` `#FEF6F6` | 7.06 |
   | Light | Yellow ink `--health-y-ink` `#7A4F00` | Yellow tint `--health-y-tint` `#FFF9E8` | 6.78 |
   | Light | Green ink `--health-g-ink` `#1C6435` | Green tint `--health-g-tint` `#F2F9F5` | 6.72 |
   | Dark | Red ink `--health-r-ink` `#F8A39B` | Red tint over canvas `#251517` | 8.93 |
   | Dark | Yellow ink `--health-y-ink` `#F5D06B` | Yellow tint over canvas `#252011` | 10.92 |
   | Dark | Green ink `--health-g-ink` `#8EDDAF` | Green tint over canvas `#111E1A` | 10.71 |
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
   the halo token is retired.

6. **Input borders and other non-text marks.** The source's input border
   (`#D5D2BD`) reaches 1.52:1 on white, and the dark preview's scale step 2
   (`#0A5C53`) reaches 1.87:1 on the dark surface. WCAG asks 3:1 of the edge
   that identifies a control. `--color-border-strong` is `#8A8D93` in light
   mode (3.33:1 on the canvas, 3.08:1 on a hovered row, its lowest) and
   `#6B6F77` in dark mode (3.34:1 on `--color-surface-hover`, a hovered
   button, its lowest). The current
   choice's `--color-primary-light` fill holds text only, so no edge is
   measured on it. Two more marks follow the same rule:
   - A Metric's sparkline, the one data accent allowed teal (§ System), strokes
     in `--color-primary-strong`, not `--color-primary`, which reaches only
     2.49:1 on white. The Check-in page's Metric progress bar fills in the
     same token.
   - On the Check-in form, an unselected Health choice shows its colour
     before one is picked: its Health's tint (`--health-*-tint`, the fill at
     half strength: half-way to white in light, 10% alpha in dark) with its
     shape in the Health's ink, its word screen-reader text and its title,
     inside a transparent 2px edge. The
     selected one is the full Health badge (`--health-*-bg`) with a 2px edge
     in its ink, so a choice keeps its size either way. The ink on each tint
     is in § Deviations 3's table, and `scripts/contrast.py` checks it.
   - A chosen option's edge is `--color-primary-strong` (§ Segmented
     controls): the current segment of a tinted track, a chosen Kind tile, its
     radio dot and a chosen Where it fits pick. The edge sits between the
     option's fill, `--color-canvas` or `--color-primary-subtle`, and what is
     around it, the canvas or the `--color-chip-bg` track.

     | Theme | Mark | On | Ratio |
     | --- | --- | --- | --- |
     | Light | Chosen edge `--color-primary-strong` `#0F766E` | canvas / primary subtle / chip bg | 5.47 / 5.25 / 4.85 |
     | Dark | Chosen edge `--color-primary-strong` `#14B8A6` | canvas / primary subtle / chip bg | 7.65 / 7.48 / 6.31 |
   - History's filter chips (§ History timeline) are edged like the rest: a
     resting chip in `--color-border-strong`, between its `--color-surface`
     fill (or `--color-primary-subtle` on hover) and the canvas, and the
     current chip in `--color-primary-strong`, between its
     `--color-primary-light` fill and the canvas. `--color-border` (1.17:1)
     and `--color-primary` (2.49:1) missed in light.

     | Theme | Mark | On | Ratio |
     | --- | --- | --- | --- |
     | Light | Resting chip edge `--color-border-strong` `#8A8D93` | canvas / surface / primary subtle | 3.33 / 3.33 / 3.19 |
     | Light | Current chip edge `--color-primary-strong` `#0F766E` | canvas / primary light | 5.47 / 4.86 |
     | Dark | Resting chip edge `--color-border-strong` `#6B6F77` | canvas / surface / primary subtle | 3.78 / 3.59 / 3.69 |
     | Dark | Current chip edge `--color-primary-strong` `#14B8A6` | canvas / primary light | 7.65 / 5.41 |
   - An unticked Where it fits pick on New goal (§ Pages) rests in a 1px
     `--color-border-strong` edge, with no fill of its own, on the canvas.
     `--color-border` (1.17:1) missed in light.

     | Theme | Mark | On | Ratio |
     | --- | --- | --- | --- |
     | Light | Resting pick edge `--color-border-strong` `#8A8D93` | canvas | 3.33 |
     | Dark | Resting pick edge `--color-border-strong` `#6B6F77` | canvas | 3.78 |

7. **Neutral canvas.** The source's light canvas is a pale yellow
   (`#FEFCE8`), and its panel, hover and rule tokens carry the same ivory
   tint. Beside it, Yellow Health's fill lost its hue, and white cards looked
   cut out of the page (UX Review). The canvas went a neutral off-white, and
   the flat frame then made it white (§ System: One surface), with neutral
   steps off it for the sidebar, a hovered row and a group header. The dark
   theme's teal canvas and cards went neutral near-black with it.

   | Token | Source | Now |
   | --- | --- | --- |
   | `--color-canvas` | `#FEFCE8` | `#FFFFFF` |
   | `--color-surface-hover` | `#FDFCF7` | `#F6F6F7` |
   | `--color-surface-alt` | `#F7F6E8` | `#F7F7F8` |
   | `--color-border` | `#E8E6D5` | `#EDEDEF` |

   Each light-theme text colour reaches 4.5:1, and each control edge 3:1, on
   the canvas and the raised surfaces beside it (the frame's own surfaces are
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
8. **Flat cards, then no cards.** The source lifts every card off the canvas
   on a large teal-tinted shadow (`--shadow-card`, deepening to
   `--shadow-card-hover` on hover; both tokens retired), so nothing read as
   nearer than anything else (UX Review). Cards went flat first, a 1px
   `--color-border` edge and no shadow in both themes, and the flat frame then
   retired them, `.card` and all (§ System: One surface). Only what floats
   keeps the source's `--shadow-pop`: the person's menu, the signed-out Theme
   menu, the phone Menu's sheet, the Goal page's More menu, Follow-ups'
   popovers, the Download menu, the Check-in page's submit bar and the toast.
   In dark, a floating `--color-surface` against the darker `--color-canvas`
   sets it apart too.
9. **Check-in overdue, apart from tags.** Check-in overdue chips and banners,
   tags and nav counts all shared the pale teal `--color-primary-light` fill, so
   "Check-in overdue" read like a label such as "Platform" (UX Review). Check-in
   overdue is now a grey chip in a dashed edge, its banners carry the same
   edge, and tags are an outline with no fill (§ Components). The UX Review
   gave the light edge as `#8C939C`, which reached only 2.87:1 on the
   off-white canvas the banners then sat on. The edge is one step darker in
   the same hue, `#888F97`, which reaches 3:1 there and on every surface since.

   Chip and banner text reaches 4.5:1 on its fill, and the dashed edge 3:1 on
   what it sits on. A chip sits on the canvas, plain or as a hovered row (on
   the Goal list it takes the signal chip's fill, in § Deviations 10). The
   Goal page's banner is a `--color-group` strip with the dashed edge along
   its foot (§ Components: Goal page banners).

   | Theme | Mark | On | Ratio |
   | --- | --- | --- | --- |
   | Light | Chip text `#3B4048` | chip fill `#E9EAEC` | 8.67 |
   | Light | Chip edge `#888F97` | canvas / row hover | 3.27 / 3.03 |
   | Light | Banner text `--color-ink` `#1B1D22` | group | 16.16 |
   | Light | Banner link `--color-primary-strong` `#0F766E` | group | 5.25 |
   | Light | Banner edge `#888F97` | group | 3.13 |
   | Light | Tag text `--color-ink` `#1B1D22` | canvas / row hover | 16.86 / 15.61 |
   | Light | Tag edge `--color-border-strong` `#8A8D93` | canvas / row hover | 3.33 / 3.08 |
   | Dark | Chip text `#E8E8E8` | chip fill `#3B4048` | 8.51 |
   | Dark | Chip edge `#8C939C` | canvas / row hover | 6.14 / 5.55 |
   | Dark | Banner text `--color-ink` `#EDEEF0` | group | 15.29 |
   | Dark | Banner link `--color-primary-strong` `#14B8A6` | group | 7.13 |
   | Dark | Banner edge `#8C939C` | group | 5.72 |
   | Dark | Tag text `--color-ink` `#EDEEF0` | canvas / row hover | 16.40 / 14.83 |
   | Dark | Tag edge `--color-border-strong` `#6B6F77` | canvas / row hover | 3.78 / 3.42 |
10. **The flat frame's surfaces.** The flat frame adds the sidebar, a
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

    The sidebar's hovered and current nav item and a chip's or tag's fill hold
    text only, so `scripts/contrast.py` measures only the inks on them, in
    both themes. Each reaches 4.5:1 but one pair, which no text may use: a
    link, `--color-primary-strong`, on the current nav item (4.48:1 in light).
    That pair is listed rather than the link colour raised, as the Note says.

    | Theme | Mark | Sidebar hover | Sidebar on | Chip bg | Note |
    | --- | --- | --- | --- | --- | --- |
    | Light | `--color-ink` `#1B1D22` | 14.55 | 13.79 | 14.95 | |
    | Light | `--color-ink-2` `#33363C` | 10.45 | 9.91 | 10.74 | |
    | Light | `--color-ink-muted` `#5F636B` | 5.20 | 4.93 | 5.34 | |
    | Light | Chip text, `--color-chip-ink` `#4A4D54` | 7.31 | 6.92 | 7.50 | |
    | Light | Links, `--color-primary-strong` `#0F766E` | 4.72 | 4.48 | 4.85 | Not for text on the current nav item. Its label and count are `--color-ink` and `--color-ink-muted`, and no link sits on it, while raising the link colour would move every link and filled button in the app. |
    | Dark | `--color-ink` `#EDEEF0` | 14.52 | 13.52 | 13.52 | |
    | Dark | `--color-ink-2` `#C9CBD0` | 10.38 | 9.67 | 9.67 | |
    | Dark | `--color-ink-muted` `#9A9EA6` | 6.27 | 5.84 | 5.84 | |
    | Dark | Chip text, `--color-chip-ink` `#C9CBD0` | 10.38 | 9.67 | 9.67 | |
    | Dark | Links, `--color-primary-strong` `#14B8A6` | 6.77 | 6.31 | 6.31 | Not for text on the current nav item either, so the pair is the same in both themes. |

## Accepted audit findings

Places where the app follows the source on purpose, though a design audit has
flagged them. They aren't deviations, and later audits shouldn't raise them
again.

1. **White surface.** The 2026-10-01 audit called the light theme's
   `--color-surface` (`#FFFFFF`) flat beside the canvas, then ivory. It stays
   white, and the flat frame made the canvas white too, so what floats is set
   off by its edge and shadow, not its fill.
   The light-theme contrast ratios in § Deviations are quoted against it, and
   two of them have little room to lose: the input border at 3.33:1 and the
   focus ring at 3.74:1, against WCAG's 3:1. Tinting the surface would mean
   recomputing every one.
2. **Hex and rgba tokens.** The same audit asked for tokens in OKLCH. They
   stay in hex and rgba, the source's notation, so they can be checked against
   it line by line. Converting them would change nothing a reader sees.
