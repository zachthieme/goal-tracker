# Goal Tracker

An MBR-caliber goal-tracking tool: an org states its Goals, updates them on a
regular cadence with Check-ins, and produces Reports. The domain language is in
[`CONTEXT.md`](CONTEXT.md) and the key decisions are in [`docs/adr/`](docs/adr/).

## Modes

| Command | Make target | What it does |
| ------- | ----------- | ------------ |
| `go run ./cmd/goal-tracker` | `make build` (builds `bin/goal-tracker`) | Serves the web app. |
| `go run ./cmd/seed` | `make seed` | Fills a fresh database with a fake org for demos (see [Seed a fake org](#seed-a-fake-org)). |

The server is configured from the environment:

| Variable | Default | Meaning |
| -------- | ------- | ------- |
| `GOAL_TRACKER_ADDR` | `:8080` | Address to listen on. |
| `GOAL_TRACKER_DB` | `goal-tracker.db` | SQLite database file; migrations run on start. |
| `GOAL_TRACKER_ADMINS` | _(none)_ | Comma-separated emails that get the Admin flag when their account is first created. |
| `GOAL_TRACKER_TIMEZONE` | `UTC` | The org's IANA timezone (e.g. `America/Los_Angeles`). Check-in cadences are counted in its days, so a Goal turns Stale and a Path to Green goes overdue at its midnight. |
| `GOAL_TRACKER_REMINDER_DAY` | `Monday` | Day of the week the weekly emails go out (see [Weekly emails](#weekly-emails)). |
| `GOAL_TRACKER_REMINDER_TIME` | `09:00` | 24-hour time of day, in the org's timezone, the weekly emails go out. |
| `GOAL_TRACKER_BASE_URL` | from `GOAL_TRACKER_ADDR`, e.g. `http://localhost:8080` | Where people reach the app. Links in emails point here. |

Sign-in is a development sign-in by email: any address works, and an account is
created on first sign-in.

Other targets: `make test`, `make lint`, `make generate` (templ and sqlc), and
`make generate-check`.

## Weekly emails

The server sends two emails once a week, at the configured day and time:

- **Check-in reminder**, to each Owner and Delegate. It lists their Active
  Goals that are Stale, or that will go Stale before next week's reminder
  unless someone checks in. Each Goal links to its pre-filled Check-in form.
- **Parent digest**, to each parent Owner. It lists the link requests waiting
  on them. It also lists problems with the Goals that contribute to theirs: a
  child whose Health went Yellow or Red, that recorded a Date Slip, or that went
  Stale in the past week. A child that is Ownerless or has a parent On Hold or
  Cancelled is listed every week until that's resolved.

People with nothing to report get no email, and people marked departed get
none. The prototype has no mail transport, so it logs each email's recipient
and subject instead of sending it.

## Seed a fake org

To demo the prototype with a realistic organisation and no real data:

```sh
make seed                        # seeds goal-tracker.db
go run ./cmd/goal-tracker        # then sign in as admin@example.com
```

The seed builds about 50 Goals and 12 weeks of history. It uses the same paths a
real org would: an Admin defines a **Team** Dimension and loads the Goals
through the [spreadsheet import](docs/import-format.md). The Owners then
activate their Goals and write weekly Check-ins through the domain commands.
Nothing is written to the database directly.

- **Three levels of the graph:** three org outcomes (Ongoing, owned by the CEO,
  CTO and CPO) are driven by two team Goals from each of six teams (Platform,
  Payments, Growth, Mobile, Data, Support). Each team Goal is driven by the
  team's projects.
- **Check-in history:** team Goals and projects check in weekly, and org
  outcomes every other week on a 14-day cadence. Check-ins carry Health, a
  status, Paths to Green, and Metric readings. A parent's Owner usually reports
  its Rolled-up Health. Sometimes they report better than it and explain why.
- **A mix of Health:** most projects stay Green. Troubled ones turn Yellow, then
  Red, and some recover to Yellow.
- **Date Slips:** troubled projects move their delivery date and the Milestones
  that fall due while they're behind, each with a reason.
- **Milestone Churn:** some projects add a Milestone partway through and later
  remove one, with a reason.
- **Stale:** a few Owners stop checking in, so those Goals' last Check-in is
  older than their cadence when the seed finishes.
- **Unaligned:** five side projects contribute to no other Goal.
- **Lifecycle:** a few projects are Done with an outcome, one is On Hold, one
  is Cancelled, and two are still Proposed.

Sign in as `admin@example.com` (an Admin), or as any Owner in the org: the
leads (`platform-lead@example.com` and so on), the org outcome owners
(`ceo@example.com`, `cto@example.com`, `cpo@example.com`), or a project Owner.

The seed is deterministic. The same seed and end date build the same org,
Check-in for Check-in. It only seeds a fresh database: it refuses one that
already has Goals, so to reseed, delete the database file and run it again.

| Flag | Default | Meaning |
| ---- | ------- | ------- |
| `-db` | `$GOAL_TRACKER_DB`, else `goal-tracker.db` | Database to seed (`make seed SEED_DB=…`). |
| `-seed` | `23` | Random seed. It decides dates, Owners, and how each project's weeks play out. |
| `-end` | now | Last day of the history, `YYYY-MM-DD`. The history ends on the day you seed, so Stale is judged against that day. Reseed later on to keep the demo current. |
| `-admin` | `admin@example.com` | The Admin who defines the Team Dimension and runs the import. |
