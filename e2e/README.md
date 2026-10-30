# End-to-end suite

Playwright tests that drive the real `goal-tracker` binary in headless
Chromium, each over its own freshly seeded database. It's where the keystone
scenarios in [`docs/scenarios.md`](../docs/scenarios.md) are checked in a
browser. It's a vetinari gate, after `make check`'s three, for a branch that
changes what the browser gets or the suite itself: a `.templ` file or a Go file
other than a `_test.go` in `internal/web/`, anything in `internal/web/static/`,
`e2e/`, `internal/seed/` or `cmd/seed/`, where the seed picks the specs' Goals
and people ([`manifest.ts`](manifest.ts)), or to anything in `internal/db/`,
since the seed and its manifest are built on the schema and the generated
queries. The pattern is the e2e gate's `when` in
[`vetinari/config.mts`](../vetinari/config.mts). That pattern only decides
whether it runs on a branch: after each wave, vetinari runs the suite on the
merged result whatever the wave changed. `make check` doesn't run it.

## Running it

```sh
make e2e                          # from the repo root
CHROME=/usr/bin/chromium make e2e # on a host where Playwright's Chromium can't run (Arch)
make e2e E2E_ARGS=smoke           # one spec; E2E_ARGS goes to `playwright test`
make e2e-typecheck                # type-check every .ts file here; not a gate
```

`make e2e` runs `npm ci` here when `e2e/node_modules` is missing. When `CHROME`
is unset it also runs `npx playwright install chromium`, which does nothing if
the browser is already there. It needs:

- Go, to build the binaries
- Node, for Playwright
- outbound network, to install Playwright and its npm packages. The pages
  themselves don't need it: the app serves htmx and the fonts

The vetinari image (`vetinari/Dockerfile`) has Playwright's Chromium and its
system libraries installed, pinned to the version in `package.json`. The two
pins move together. The image sets `PLAYWRIGHT_BROWSERS_PATH` to that browser,
so `playwright install chromium` finds it and downloads nothing, and vetinari's
setup runs `npm ci --prefix e2e` once per sandbox, so the gate needs no
network. The suite takes about 25 seconds there.

From `e2e/`, `npx playwright test` runs the suite too, and `npx playwright show-report`
opens the last run's report. A failed test keeps its trace and has the server's
log attached.

`make e2e-typecheck` runs `tsc` in strict mode over every `.ts` file here, with
the settings in `tsconfig.json`. Playwright strips the types without checking
them, so this is the only check they get. It isn't a gate: `make check` and
vetinari don't run it. From `e2e/`, `npm run typecheck` does the same.

## How the fixtures work

[`global-setup.ts`](global-setup.ts) runs once per run. It builds
`cmd/goal-tracker` and `cmd/seed` into a temp dir, then seeds two template
databases with `-seed 23`, both frozen at the **reference date, Monday 5
October 2026**, not the machine's date. Beside each it writes the template's
manifest (`-manifest`), the specs' picks from it, judged at `startAt`
(`-manifest-at`):

| Template  | Seeded with       | What it gives                                                                                                                                                                                    |
| --------- | ----------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `default` | `-end 2026-10-05` | History ends on the reference date: the seed's last Check-ins land at about 15:49 UTC, a couple of hours before each test's app starts its clock. A Check-in a test writes is the Goal's latest. |
| `due`     | `-end 2026-10-01` | The last Check-ins are 4 days old at the reference date, so 7-day Goals are due on Home without being Check-in overdue.                                                                          |

Every test's app starts its clock at **2026-10-05T18:05:00Z**
(`GOAL_TRACKER_START_AT`, exported as `startAt`) and ticks on from there, so
every run sees the same company at the same moment, whatever the machine's date.
The seed's company isn't the same company shifted in time, so the date can't float:
#226's follow-up, the seed invariants test in `internal/seed`, pins the same
date, and the two move together.

On `default`, Home's Needs you lists the Check-in overdue Goals, plus any whose
next Check-in falls due before the next weekly reminder. A test that needs a
Check-in due uses `due`.

[`fixtures.ts`](fixtures.ts) exports `test` and `expect`. Import them from
there, not from `@playwright/test`. Each test gets:

- **`app`**: the test's own server, over its own copy of a template. It starts
  on a free port with `admin@example.com` as Admin, the way
  `scripts/scratch-app start` does, and stops after the test. `default` is the
  template unless the test asks for another: `test.use({ template: "due" })`.
  Every test starts from the same seeded company and can't see another test's
  writes, so the suite runs `fullyParallel`. `page` and `as` resolve paths
  such as `"/home"` against it. The server runs in UTC with the default weekly
  reminder (Monday 09:00) and its clock offset to start at `startAt`; no
  `GOAL_TRACKER_*` variable from your shell reaches it, `GOAL_TRACKER_CHROME`
  included: a Report's PDF download is a rendered file only when a Chromium is
  on `PATH`, and otherwise its print page. Playwright's own Chromium isn't on
  `PATH`, so in the vetinari image it's the print page. **`app.now()`** is the
  app's current time: `startAt` plus the time since that test's server
  started.
- **`appToday()`**: the app's date, `2026-10-05`, as `YYYY-MM-DD`. No run
  crosses midnight from 18:05.
- **`signIn(page, email)`**: signs in through the development sign-in form, and
  waits for Home. Any email signs in.
- **`as(email)`**: opens a page in a new browser context, already signed in,
  for scenarios with two people. `page.context()` is the context. It closes
  after the test.
- **`manifest`**: the seed's picks for the test's template, typed `Manifest`
  ([`manifest.ts`](manifest.ts)): the Goals and people the specs act on, by
  role, such as `quietGoal`, an Owner who can check in Green with only a
  status, or `badNewsChain`, a project and the Goal it contributes to. The
  seed (`seed.BuildManifest` in `internal/seed/manifest.go`) picks them
  through the domain, judging "today", Check-in overdue and overdue
  Milestones at `startAt`, where each test's app starts, not at `-end`. A
  role with no pick is `null`, and a list with none is empty:
  `requirePick(role, pick)` returns the pick or fails naming the role. Use it
  only to choose actors and Goals, never to assert an outcome. Outcomes are
  asserted on the page.
- **`serverLog()`**: what the server has logged so far. Use it for what the app
  only logs: email goes to the log through `email.LogSender`, not out.

The seed's accounts are `admin@example.com` (Admin), `ceo@`, `cto@` and
`cpo@example.com` (company outcomes), `<team>-lead@example.com` for Platform,
Payments, Growth, Mobile, Data and Support, and the project Owners. They form
a Manager tree, so Orgs show up: the CEO has no Manager, the CTO, CPO and Admin
report to the CEO, the Platform, Mobile and Data leads to the CTO, the
Payments, Growth and Support leads to the CPO, and each team's people to their
lead. Sign in as `ceo@` to see the whole company as one Org, or as a lead to
see a team's. A fresh seed has Not Green (Red and Yellow), Check-in overdue,
Unlinked and Schedule conflict Goals on Follow-ups. **Needs an Admin** is empty, and
no Path to Green is overdue: every back-to-Green date is 3–6 weeks ahead. A test
that needs an overdue one makes it. The app accepts a back-to-Green date in the
past.

`fixtures.ts` also exports `admin`, the seed's Admin's email, and `startAt`,
the reference instant each test's app starts its clock at.

[`smoke.spec.ts`](smoke.spec.ts) checks that the app comes up, that people can
sign in, one at a time and two at once, and that the `due` template is due. [`isolation.spec.ts`](isolation.spec.ts) checks that two tests
writing to the same Goal at once each see only their own write.
[`phone-width.spec.ts`](phone-width.spec.ts) checks that at 390px Home, with
a Check-in due in Needs you, the Goals list, Follow-ups, the Admin page, a
Report draft, with a Schedule conflict's Owner's email shown by clicking their
Name (and hidden by clicking again), the New goal page, with its Metric editor
open, and the Check-in page, with Yellow picked, don't clip or scroll
sideways, and keep every control on screen. It also checks the Goals Table's
scroll cue: at 390px the table keeps every column and scrolls sideways inside
its own container, which fits the window and fades the edge it can scroll
toward.
[`download-menu.spec.ts`](download-menu.spec.ts) checks the Download menu on
a Report's row on the Reports list and in a publication's header: at 390px
the open popover lies inside the window and nothing passes the window's edge,
and without script it still opens, with its Markdown and PDF items as links.
[`checkin-submit-bar.spec.ts`](checkin-submit-bar.spec.ts) checks the
Check-in page's submit bar: its summary showing the Health picked, Path to
Green, the Metrics read, a Highlight written, dates changed and a Lifecycle
change as each is filled in, and again after a refused submit; Submit
check-in staying in the window at 1280×800 while the bar sits below the last
optional section at the page's end; and at 390px, Cancel hidden and nothing
past the window's edge.
[`highlight-editor.spec.ts`](highlight-editor.spec.ts) checks the Check-in
form's Highlight editor with script on: the + Insight, + Accomplishment
and + Miss buttons, the Note editor's Save, Edit, Cancel and Remove, an editor
left open at submit, and Draft Highlights shown as items or, with no Kind, as
an open editor.
[`draft-highlight-editor.spec.ts`](draft-highlight-editor.spec.ts) checks the
Goal page's Draft Highlights with script on: + Log a highlight opening the
Highlight editor and its Save logging with no page load, Edit and Cancel,
Remove, a refused Save coming back in the editor with the reason and what was
typed, and Remove disabled while an editor is open. Without script, it checks
the link to the log form and Delete.
[`checkin-add-bar.spec.ts`](checkin-add-bar.spec.ts) checks the Check-in
page's "Add to this check-in" bar: the sections it shows and the heading it
focuses, its buttons marked used, sections the page opens on load and after a
refused submit, its Highlight buttons opening the Highlights section's
editor, Enter still submitting, its row scrolling sideways at 390px, and the
folded sections without script.
[`goal-measures-editor.spec.ts`](goal-measures-editor.spec.ts) checks the New
goal form's Milestone and Metric editor with script on: + Milestone
and + Metric, each editor's Save and the fields it waits for, the items with
Edit, Cancel and Remove, the Ready card's "Save the Milestone you started",
Enter in an editor, a refused submit reopening its editor, and Finish defining
adding to a Goal's existing Milestone. With two refused editors open, it checks
where focus goes after Cancel and Save, and that the Milestones label waits for
a saved item.

## Conventions for the scenario specs

- **One spec per scenario**, at `scenarios/<nn>-<slug>.spec.ts`. A helper only
  one scenario uses stays in that spec.
- **A scenario spec ticket edits only its own spec file**, and the manifest's
  files for a role it adds. It doesn't edit `fixtures.ts`, this README,
  `playwright.config.ts` or `package.json`. Its
  file-set marker doesn't list them, so a co-wave ticket that did could
  collide. If a scenario needs a helper in `fixtures.ts`, keep a local copy in
  the spec and report a finding.
- **Choose Goals and actors by role from `manifest`**, or actors by email,
  never a Goal by a fixed id or title. A Goal's seeded Health differs between
  the two templates, but not from day to day: both are frozen at the reference
  date. Where the page decides, as which of a lead's team Goals shows
  Rolled-up Health Green, read it there over the role's ordered candidates. A
  new role is added in `internal/seed/manifest.go`, `manifest.ts` and
  `TestE2EScenarioSituations` (`internal/seed/scenarios_test.go`) together.
  When the seed or a role changes, regenerate the golden manifests in
  `internal/seed/testdata/` with `UPDATE_GOLDEN=1 go test ./internal/seed/`.
- **Read "now" and "today" from the app, never the machine.** Use
  `app.now()` and `appToday()`, not `Date.now()` or `new Date()`. Timeouts and
  polling deadlines stay on the real clock.
- **The scenarios' people (Elena, Priya, Marcus) are roles, not accounts.**
  The seed's own Names belong to other accounts (`cto@` is Priya Raman, `cpo@`
  is Marcus Bell, `growth-lead@` is Elena Petrova). Assert on the seeded Name
  the page shows, never on a scenario's name.
- **Find elements by role and visible text**, in `CONTEXT.md`'s terms. Use the
  existing `data-testid`s where text is ambiguous. Don't use CSS classes.
- **Name each `test.step` after the scenario step it checks**, e.g.
  `"1.3 Follow-ups groups by who has to act"`. Assert each "It worked if" clause
  somewhere, and name it in a comment there.
- **When the product falls short of a step, don't change the app** in that
  issue. Mark that step's test `test.fixme`, with a comment quoting the step and
  saying what the page does instead, and report it as a finding. The rest of
  the scenario still runs.
