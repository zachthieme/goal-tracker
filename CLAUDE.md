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

## Agent skills

### Issue tracker

Issues and specs live in GitHub Issues for `zachthieme/goal-tracker`, managed with the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

Uses the five default labels: `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `CONTEXT.md` and `docs/adr/` at the repo root. See `docs/agents/domain.md`.
