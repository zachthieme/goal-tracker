# CLAUDE.md

Goal Tracker: an MBR-caliber goal-tracking tool. Goals, Metrics, Milestones, Check-ins, and Reports. The domain language is in `CONTEXT.md` and the key decisions are in `docs/adr/`.

## Code layout

One file per feature area, so tickets that touch different areas don't collide
and can run in parallel waves. Add a new area by adding files, not by editing
another area's file.

- **Queries** (`internal/db/queries/`): one `<area>.sql` per feature area (e.g.
  `accounts.sql`, `goals.sql`); sqlc generates a matching `<area>.sql.go`.
  `models.go` stays shared — only schema-changing tickets touch it.
- **Web** (`internal/web/`): `routes.go` is the only place routes are
  registered — a new area adds one line there. A refused command answers
  through `answerRefused` (`formrefusal.go`), choosing `onPage` for a form on
  another page and `asPage` for a form that is the page; don't write a new
  switch on the error kind. `server.go` holds only the
  `Server` type, `ServeHTTP`, rendering, and the auth middleware. Each area's
  handlers live in `<area>.go`, its pages in `<area>.templ` (plus the generated
  `<area>_templ.go`), and its HTTP tests in `<area>_test.go`. Shared page chrome
  lives in `layout.templ`. Shared HTTP and page test helpers live in
  `internal/web/helpers_test.go`; a helper more than one area needs goes there,
  not in an area's `_test.go`. New web tests assert through its parser
  helpers (`findByTestID`, `textOf`, `formField`, `linkTo`, `buttonNamed`) on
  what a person sees and can do, not by matching markup, and style rules and
  the classes they select are tested in `styles_test.go`.
- **Domain** (`internal/domain/`): `domain.go` holds only the `Service`, its
  constructor, the Service-wide options (`Option`, `WithTimezone`,
  `WithBaseURL`), and the Service-wide helpers (`WithinTx`, `Now`, `Timezone`,
  `Ping`). Each concept's types, `Service` methods and own options live in
  their own file (`account.go`, `goal.go`, …; the email outbox's
  `WithEmailDeliveredOnCommit` and `WithEmailOutboxLogger` in
  `emailoutbox.go`) — a new concept is a new file. A page whose
  view model repeats reads gets a page loader, one `Service` method in its own
  `<page>.go` file (ADR 0010).
- **Test support** (`internal/testsupport/`): `harness.go` keeps the core
  harness (fresh database, clock, email recorder, sign-in). Scenario builders
  live one file per area (e.g. `goals.go`).

## Gates and commits

Run the gates (`make generate-check`, `make lint`, `make test`; `make check`
runs all three in that order and stops at the first failure) in the
foreground and read each to the end; `make check` takes about a minute, most
of it `make test`, which runs `internal/web` under the race detector as five
processes of about 40 seconds each alongside the other packages. Give a
foreground gate run a tool timeout of at least 5 minutes, which also covers a
cold build cache.
Never
background a gate and wait for it. **Commit before you end your turn.** A
campaign run is a single turn, so a turn that ends waiting on a background job,
or with work uncommitted, parks the issue as "stalled, no-commit".
Never run `git worktree add`, `remove` or `prune`, or export `GIT_DIR`: in a
campaign container the host's `.git` is shared, and `prune` there deletes every
other agent's worktree (jjforge/vetinari#494).
Don't run `templ fmt`: the repo isn't kept in its format, and where Prettier is
installed it rewrites every inline `<style>` and `<script>` block. Format .templ
files by hand to match the surrounding code.

`make e2e` runs the Playwright suite in `e2e/` over a freshly seeded app (see
`e2e/README.md`), in about 25 seconds. It's a gate too, alongside `make check`,
for a change to a `.templ` file or a non-test Go file in `internal/web/`, or to
anything in `internal/web/static/`, `e2e/`, `internal/seed/`, `cmd/seed/` or
`internal/db/` (the e2e gate's `when` in `vetinari/config.mts`). For such a change, run
`make e2e` in the foreground after `make check`, read it to the end, and commit
only when it passes. `make check` doesn't run it. `make e2e-typecheck`
type-checks `e2e/` with `tsc` in strict mode, and isn't a gate.

## Changelog

**Log every change, and tag who it reaches.** Every change, internal work
included, gets a bullet opening with an audience tag (`[user]`, `[ops]`,
`[api]`, `[internal]`). Where the bullet goes depends on who is writing it:

- A **campaign agent** writes a fragment to `changelog.d/<issue>.md` and **never
  edits `CHANGELOG.md`**. vetinari folds each wave's fragments into the top
  milestone when the wave merges. Never cite the fragment in a file-set marker.
- **Interactive work** that isn't racing a wave adds the bullet to the top
  milestone directly, or writes a fragment and runs `vetinari changelog collect`.

Use jjforge's section labels (`New features`, `Improvements`, `Bug fixes`, …),
not Keep-a-Changelog's `Added`/`Changed`. A breaking change goes in a
`**Breaking changes:**` section that names the contract it broke. The format,
tags, and contracts are in
[`docs/changelog-conventions.md`](docs/changelog-conventions.md).

## Agent skills

### Issue tracker

Issues and specs live in GitHub Issues for `zachthieme/goal-tracker`, managed with the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

Uses the five default labels: `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `CONTEXT.md` and `docs/adr/` at the repo root. See `docs/agents/domain.md`.

### Verifying merged work

`/verify-pending`, `/fileset` and `/triage-placement` come from vetinari (`scripts/install-skills.sh` in its checkout). How to check a change in the browser: `docs/agents/verifying.md`.
