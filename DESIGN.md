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
  current top-bar item. The one data accent is a Metric's trend: its sparkline
  on the Goal page and its progress bar on the Check-in page, both in teal
  (see § Deviations 6). No neon, and no tint on the canvas, so Health's
  fills stand out from the page (see § Deviations 7). Cards sit on the canvas
  in plain white (see § Accepted audit findings).
- **Density:** generous. Cards use 24px padding, sections sit 20–32px apart,
  and everything aligns to a 4px grid.
- **Cards are earned:** a card is for a form, a table, or a page's single
  primary task list, the one thing the page is there for a person to do now.
  Every other section is a plain section on the canvas: a heading over rows
  separated by 1px `--color-border` hairlines, with no surface, border or
  padding box. So the eye finds the page's job first, and the context around
  it reads lighter. Home follows the rule (Needs you is its one card); other
  pages are brought in line one at a time.
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

  /* Check-in overdue and Path to Green overdue: chip and banner. See Components */
  --checkin-overdue-bg:   #E9EAEC; /* chip fill */
  --checkin-overdue-ink:  #3B4048; /* chip text */
  --checkin-overdue-edge: #888F97; /* dashed edge of chip and banner. See Deviations */

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

  /* (chosen) Check-in overdue: the light chip's ink becomes the fill, and the UX
   * Review's grey the edge */
  --checkin-overdue-bg:   #3B4048;
  --checkin-overdue-ink:  #E8E8E8;
  --checkin-overdue-edge: #8C939C;

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

The app serves the faces itself, from `internal/web/static/fonts/`, by
`@font-face` rules at the top of `app.css`: Inter and Plus Jakarta Sans as
variable fonts, JetBrains Mono at 500. No page loads a font from another host.

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
- **Monospace only in tables:** a date takes `.num` only in a table column,
  such as the Goal list's Due or the Milestones table. Elsewhere it is body
  type.

## Components (mapped to `internal/web/static/app.css`)

- **Top bar (`.nav`):** `--nav-bg`, which is deep teal `#134E4A` in light
  mode and `--color-surface-alt` in dark mode, with `--nav-ink` text. The light
  value is the top bar's own, not `--color-ink`, so the bar stays teal while the
  ink is neutral. Resting items use `--nav-ink-2`; hover is the
  translucent-white `--nav-hover`.
  The nav items read **Home · Goals · Risks · Reports**, then **Admin** for an
  Admin. Home counts what needs the person and Risks counts the Goals it flags
  in the person's default scope (see Scope), as many as a bare `/risks` lists
  for them; Goals and Reports carry no count. The brand link goes to Home.
  The current item (`.on`) gets a `--color-primary` underline or pill. An item
  is current on its own page and every page under it, so a Goal's page, New
  goal and a Check-in all mark Goals.
  Above 900px it is one 56px row. At 900px and below it takes two rows and
  never scrolls sideways. The brand, the person and Sign out are on the first
  row and the nav items (`.navitems`) are on the second, each label on one line.
  The person shows by Name alone, and a long Name truncates with an ellipsis.
  "Signed in as" and "(Admin)" are hidden visually with `font-size:0`, so screen
  readers still read them. At 360px and below the nav items' padding tightens
  to fit an Admin's five items at 320px with two-digit counts. If they still
  don't fit on one line, the nav items' row wraps onto another line
  (`flex-wrap:wrap` on `.navitems`) rather than overflow, each label still on
  one line. The gaps and padding beside Sign out tighten too, so the Theme menu
  fits beside it.
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
    request's pending page, whichever the Reject came from). The templ
    component is `toastNotice` in `layout.templ`; a new Undo reuses it rather
    than making its own notice.
- **Not found page:** like a refused Undo, a missing page shows a page, not
  bare text. A page you open that doesn't exist (an unknown URL, asked with
  any method, or a Goal, Report, publication or other thing that doesn't
  exist or has been removed) answers 404 with a "Not found" page inside the
  normal chrome, top-bar counts included: one plain sentence, "There's
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
  other page's content, and isn't centred.
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
  faint `--color-primary-subtle` tint (`--nav-hover` in the top bar), as a quiet
  button does, and focus keeps the focus ring. Clicking, tapping or activating
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
- **Card (`.card`):** `--color-surface`, 1px `--color-border`,
  `--radius-lg`, 24px padding, and no shadow in either theme. On hover a
  clickable card's border becomes `--color-border-strong` over `--dur-base`,
  and nothing else changes: it doesn't move. That is darker in light and
  lighter in dark, so either way the edge stands further out from the card.
  A card's own heading is an `h3` (card title); `h2` is for page sections
  that sit outside cards.
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
    badges on the Goals list, the Goal page's link lists and the Risks table
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
    input with right-aligned numbers in `--font-mono`.
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
  or Red goals, plain sections on the canvas (§ System: Cards are earned). A
  plain section is an `h3` over a compact list (`.hm-section`), its first row
  ruled off from the heading by a hairline as each row is from the next, so
  the context headings stay quieter than the card's.
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
  top bar's Home count both count every row. Goals the person is a Delegate
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
    overdue mark's `badge st`, as on Risks. The most Red come first, then the
    most Path to Green overdue, the most Check-in overdue and the most Yellow,
    then by Name. Someone further down counts in the row of the person directly
    under the viewer they're under, so the rows add up to the total.
  - **All clear:** the people directly under the viewer with nothing flagged
    share one muted line after the rows, "2 others all clear" ("1 other all
    clear"), left out when there are none. With nothing flagged anywhere,
    the section shows only one muted "Everyone in My org is all clear." line.
  - **Links:** the section ends with one line, **My org on Goals →** ·
    **My org on Risks →** (`?org=<viewer ID>`), whose pages include the
    person's own Goals.

  The section adds nothing to Needs you or the top bar's Home count.
- **Scope (Goals and Risks, `pageScopeSelect` in `scope.templ`):** the one
  select scoping the Goal list and the Risks page, submitted as `?org=`:
  **Everyone** (`all`), **Mine** (`mine`, the Goals the person Owns or is a
  Delegate on), **My org** for someone with anyone under them, then each other
  person's Org under **Other orgs** (their ID).
  - **The default scope:** an address naming no scope, or one that is
    malformed or names no one, opens on the person's default: My org for
    someone with anyone under them, Mine for everyone else, Admins included.
    The select shows it chosen, and the count lines and empty states read as
    for that scope chosen. A bare `/goals`, `/goals/download` or `/risks`
    therefore means "my default", and links meant to land there stay bare
    (the Goals breadcrumb, Back to Goals, the Nudge page's Back to Risks).
  - **Everyone is explicit:** `?org=all`. Goals' **Clear** and Risks' **Show
    everything** link to it, and a page on Everyone keeps it in every link
    that keeps the page's filters. A chosen scope is never overridden.
  - **An empty default:** the Goal list says "No Goals in My org." or "No
    Goals in Mine." with a **See everyone's →** link to the same view with
    `?org=all`. Risks keeps its own empty line, whose Show everything is the
    way out.
  - **Needs an Admin is company-wide:** an Admin's job there is the company's,
    so the Needs an Admin card counts, and chips, every Ownerless and
    halted-parent Goal in the company under the page's Dimension values,
    while the other cards count the page's own rows. Its card keeps the
    page's scope in its link like every other card, so Show all and the other
    cards come back to it. `/risks?group=admin` always lists the company's
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
    overdue Milestone, `?open=dates` (a Schedule conflict's Fix on Risks), and
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
  or name first (Risks, the Reports list, On track, the Goal list). The Goal
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
  ("Check-in overdue · 9 days" on the Goal list; on Risks, with its cadence too:
  "Check-in overdue · 42 days, checks in weekly", naming the cadence as the New
  goal form's segments do, or "every 3 days"). It is a warning about freshness,
  never a tag, so it can't share a tag's look: a `--checkin-overdue-bg` grey
  fill, `--checkin-overdue-ink` text and a 1px dashed `--checkin-overdue-edge`
  border, at a Health badge's `--radius-sm`. The Goal list's "Path to Green
  overdue" chip takes the same style, as does Risks' ("Path to Green 12 days
  overdue"). So does the "Milestone overdue" chip on a Green Goal past a
  Milestone's date: "Milestone overdue" on the Goal list, "Milestone 3 days
  overdue" on Risks.
- **Check-in overdue banner (`.alert.st`):** at the top of a Check-in overdue
  Goal's page, a `--color-surface` banner in a 1px dashed
  `--checkin-overdue-edge` border, with `--color-ink` text: **Check-in
  overdue**, then how long it has gone without a Check-in against its cadence.
  For whoever may check in (the Owner or a Delegate) it ends, at the right, with
  a **Check in now** link to the Goal's Check-in page. Anyone else sees no link.
  The "Path to Green overdue" banner beside it takes the same style with no
  link, because CONTEXT.md surfaces both the same way.

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
   and teal is kept for links, buttons, focus and the current top-bar item.
   The top bar keeps its teal through its own `--nav-bg`.

   Each ink's contrast on every surface text sits on (`--color-primary-light`
   is the selected-row fill, and `--color-primary-subtle` the hover fill of a
   quiet control and of a Name):

   | Theme | Ink | Canvas | Surface | Surface hover | Surface alt | Primary light | Primary subtle |
   | --- | --- | --- | --- | --- | --- | --- | --- |
   | Light | `--color-ink` `#18211F` | 15.20 | 16.46 | 16.02 | 14.66 | 14.60 | 15.78 |
   | Light | `--color-ink-2` `#3E4946` | 8.63 | 9.35 | 9.10 | 8.33 | 8.30 | 8.96 |
   | Light | `--color-ink-muted` `#66706D` | 4.73 | 5.12 | 4.98 | 4.56 | 4.54 | 4.91 |
   | Dark | `--color-ink` `#E8E8E8` | 16.01 | 11.99 | 10.99 | 15.19 | 10.99 | 15.19 |
   | Dark | `--color-ink-2` `#C2C2C2` | 11.01 | 8.25 | 7.56 | 10.45 | 7.56 | 10.45 |
   | Dark | `--color-ink-muted` `#9E9E9E` | 7.32 | 5.48 | 5.03 | 6.95 | 5.03 | 6.95 |

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
     2.49:1 on white. The Check-in page's Metric progress bar fills in the
     same token.
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
   the Theme menu, the Goal page's More menu and the toast. The two card
   shadow tokens are gone.
9. **Check-in overdue, apart from tags.** Check-in overdue chips and banners,
   tags and nav counts all shared the pale teal `--color-primary-light` fill, so
   "Check-in overdue" read like a label such as "Platform" (UX Review). Check-in
   overdue is now a grey chip in a dashed edge, its banners a surface in the
   same edge, and tags an outline with no fill (§ Components). The UX Review
   gave the light edge as `#8C939C`, which reaches only 2.87:1 on the canvas the
   banners sit on. The edge is one step darker in the same hue, `#888F97`, which
   reaches 3:1 there.

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
