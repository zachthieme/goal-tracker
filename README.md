# Goal Tracker

An MBR-caliber goal-tracking tool: an org states its Goals, updates them on a
regular cadence with Check-ins, and produces Reports. The domain language is in
[`CONTEXT.md`](CONTEXT.md) and the key decisions are in [`docs/adr/`](docs/adr/).

## Modes

| Command | Make target | What it does |
| ------- | ----------- | ------------ |
| `go run ./cmd/goal-tracker` | `make build` (builds `bin/goal-tracker`) | Serves the web app. |
| | `make serve` | Builds and serves the web app on your tailnet at `https://<machine>.<tailnet>.ts.net:8090` via `tailscale serve`, with `admin@example.com` as Admin. Override with `SERVE_PORT`, `SERVE_DB`, and `SERVE_ADMINS`. People sign in through the [local Authentik](dev/authentik/README.md), which it starts and serves on the tailnet at `:9443` (`AUTHENTIK_PORT`); it needs `dev/authentik/.env`. `NO_SSO=1` uses the development sign-in form instead. |
| | `make start` | Like `make serve`, but in the background, logging to `serve.log` (`SERVE_LOG`). Stops the server a previous `make start` left running first. Takes the same settings, `NO_SSO=1` included, and records them for `make stop` and `make restart`. |
| | `make stop` | Stops the server `make start` started, and its Authentik unless it started with `NO_SSO=1` (Authentik's data is kept). |
| | `make restart` | Rebuilds and starts the server again with the settings the last `make start` recorded. A setting given to `make restart` replaces the recorded one. |
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
| `GOAL_TRACKER_BASE_URL` | from `GOAL_TRACKER_ADDR`, e.g. `http://localhost:8080` | Where people reach the app. Links in emails (the weekly emails and comment alerts) point here. |
| `GOAL_TRACKER_OIDC_ISSUER` | _(none)_ | The org's OpenID Connect issuer URL. With the client ID and secret below, people sign in through it (see [Sign-in](#sign-in)). Set all three or none. |
| `GOAL_TRACKER_OIDC_CLIENT_ID` | _(none)_ | Goal Tracker's client ID at the provider. |
| `GOAL_TRACKER_OIDC_CLIENT_SECRET` | _(none)_ | Goal Tracker's client secret at the provider. |
| `GOAL_TRACKER_SESSION_KEY` | _(random at each start)_ | Base64 key, at least 32 bytes, that signs session cookies (`openssl rand -base64 32`). Unset, the app makes a random one and logs a WARN: everyone is signed out when it restarts. |
| `GOAL_TRACKER_START_AT` | _(none: the wall clock)_ | **Test-only.** An RFC 3339 instant (e.g. `2026-10-05T18:05:00Z`) the app's clock starts at, then ticks normally from; startup logs a WARN when it's set. The e2e suite uses it to run against a seed frozen at one date. Never set it in production. |

## Sign-in

With the three `GOAL_TRACKER_OIDC_*` variables set, people sign in through the
org's OpenID Connect provider: the sign-in page has one button, "Sign in with
your organization". Register `GOAL_TRACKER_BASE_URL` + `/auth/callback` (by
default `http://localhost:8080/auth/callback`) as the redirect URI, and allow
the authorization code grant with the scopes `openid email profile`. The
provider must send a verified `email`; its `name` claim becomes the person's
Name. Signing out of the app also signs the person out of the provider when it
offers RP-initiated logout (`end_session_endpoint`), and the provider sends
them back to the sign-in page; register `GOAL_TRACKER_BASE_URL` + `/signin` as
a post-logout redirect URI for that. Startup stops if the issuer can't be
discovered or only some of the variables are set. A local [Authentik](dev/authentik/README.md) with the seed's
people and their managers is set up for this.

Without them, sign-in is the development form: any email address signs in, and
an account is created on first sign-in. The seed, `scripts/scratch-app` and the
e2e suite use it.

Either way the session cookie is signed with `GOAL_TRACKER_SESSION_KEY`, and is
`Secure` when `GOAL_TRACKER_BASE_URL` is `https`.

Other targets: `make test`, `make test-quick` (the tests without the race
detector, for a quick local loop), `make lint`, `make generate` (templ and
sqlc, and what a bare `make` runs), `make generate-check`, `make check` (every
gate: `generate-check`, `lint`, then `test`), `make test-scripts` (the tests
of the [verification scripts](scripts/README.md)), `make e2e` (the
[Playwright end-to-end suite](e2e/README.md) over a freshly seeded app; not a
gate, and `CHROME=/usr/bin/chromium make e2e` uses a local Chromium), `make clean` (removes
`bin/` and `serve.log`), and `make help` (lists every target).

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

## Comment alerts

Readers comment on a Goal in a published Report from its publication page. The
comment emails the Goal's current Owner. Replies in the thread email the Owner
and everyone else who has written in it, except the reply's author. As with the
weekly emails, the prototype logs these instead of sending them.

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
Everyone the seed creates has a Name, which the pages show in place of their
email; you still sign in with the email.

The seed is deterministic. The same seed and end date build the same org,
Check-in for Check-in. It only seeds a fresh database: it refuses one that
already has Goals, so to reseed, delete the database file and run it again.

| Flag | Default | Meaning |
| ---- | ------- | ------- |
| `-db` | `$GOAL_TRACKER_DB`, else `goal-tracker.db` | Database to seed (`make seed SEED_DB=…`). |
| `-seed` | `23` | Random seed. It decides dates, Owners, and how each project's weeks play out. |
| `-end` | now | Last day of the history, `YYYY-MM-DD`. The history ends on the day you seed, so Stale is judged against that day. Reseed later on to keep the demo current. |
| `-admin` | `admin@example.com` | The Admin who defines the Team Dimension and runs the import. |
