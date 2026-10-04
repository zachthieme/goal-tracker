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
  registered — a new area adds one line there. `server.go` holds only the
  `Server` type, `ServeHTTP`, rendering, and the auth middleware. Each area's
  handlers live in `<area>.go`, its pages in `<area>.templ` (plus the generated
  `<area>_templ.go`), and its HTTP tests in `<area>_test.go`. Shared page chrome
  lives in `layout.templ`.
- **Domain** (`internal/domain/`): `domain.go` holds only the `Service` and its
  constructor. Each concept's types and `Service` methods live in their own file
  (`account.go`, `goal.go`, …) — a new concept is a new file.
- **Test support** (`internal/testsupport/`): `harness.go` keeps the core
  harness (fresh database, clock, email recorder, sign-in). Scenario builders
  live one file per area (e.g. `goals.go`).

## Gates and commits

Run the gates (`make generate-check`, `make lint`, `make test`; `make check`
runs all three in that order and stops at the first failure) in the
foreground and read each to the end; `make test` takes about 50 seconds. Never
background a gate and wait for it. **Commit before you end your turn.** A
campaign run is a single turn, so a turn that ends waiting on a background job,
or with work uncommitted, parks the issue as "stalled, no-commit".

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
