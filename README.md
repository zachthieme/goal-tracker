# Goal Tracker

An MBR-caliber goal-tracking tool: a company states its Goals, updates them on a
regular cadence with Check-ins, and produces Reports. The domain language is in
[`CONTEXT.md`](CONTEXT.md) and the key decisions are in [`docs/adr/`](docs/adr/).
To install it on a server, follow [`docs/install.md`](docs/install.md).

## Modes

| Command | Make target | What it does |
| ------- | ----------- | ------------ |
| `GOAL_TRACKER_SIGNIN=dev go run ./cmd/goal-tracker` | `make build` (builds `bin/goal-tracker`) | Serves the web app, with the development sign-in form (see [Sign-in](#sign-in)). |
| | `make serve` | Builds and serves the web app on your tailnet at `https://<machine>.<tailnet>.ts.net:8090` via `tailscale serve`, with `admin@example.com` as Admin. Override with `SERVE_PORT`, `SERVE_DB`, and `SERVE_ADMINS`. People sign in through the [local Authentik](dev/authentik/README.md), which it starts and serves on the tailnet at `:9443` (`AUTHENTIK_PORT`); it needs `dev/authentik/.env`, and reads the app's `GOAL_TRACKER_SESSION_KEY` from there too, stopping if it's missing. `NO_SSO=1` uses the development sign-in form instead. |
| | `make start` | Like `make serve`, but in the background, logging to `serve.log` (`SERVE_LOG`). Stops the server a previous `make start` left running first. Takes the same settings, `NO_SSO=1` included, and records them for `make stop` and `make restart`. |
| | `make stop` | Stops the server `make start` started, and its Authentik unless it started with `NO_SSO=1` (Authentik's data is kept). |
| | `make restart` | Rebuilds and starts the server again with the settings the last `make start` recorded. A setting given to `make restart` replaces the recorded one. |
| `goal-tracker password-link <email>` | | With `GOAL_TRACKER_SIGNIN=password`, prints a one-time set-password link for the email and exits, refusing anything that isn't an email address: how the first Admin gets a password (see [Passwords](#passwords-password)). |
| `go run ./cmd/seed` | `make seed` | Fills a fresh database with a fake company for demos (see [Seed a fake company](#seed-a-fake-company)). |

The server is configured from the environment:

| Variable | Default | Meaning |
| -------- | ------- | ------- |
| `GOAL_TRACKER_SIGNIN` | _(required)_ | How people sign in: `oidc`, through the company's OpenID Connect provider; `password`, with passwords set through Admin-issued links; or `dev`, the development form that signs anyone in as anyone (see [Sign-in](#sign-in)). Startup stops when it's unset or anything else. |
| `GOAL_TRACKER_ADDR` | `:8080` | Address to listen on. |
| `GOAL_TRACKER_DB` | `goal-tracker.db` | SQLite database file; migrations run on start. |
| `GOAL_TRACKER_ADMINS` | _(none)_ | Comma-separated emails that get the Admin flag when their account is first created. Admins can make others Admins later from the Admin page. |
| `GOAL_TRACKER_TIMEZONE` | `UTC` | The company's IANA timezone (e.g. `America/Los_Angeles`). `Local` is refused: name the zone. It is used for company-wide dates (a Report's baseline, the weekly emails' schedule, History's weeks, a Nudge's once a day), and for a Goal whose Owner has no timezone in the directory or which is Ownerless. A Goal's own dates are judged in its Owner's timezone: when its Check-in goes overdue, when a Path to Green goes overdue, when a Metric misses its target, when a Milestone passes its date. The pages show dates and times in the company's timezone. |
| `GOAL_TRACKER_REMINDER_DAY` | `Monday` | Day of the week the weekly emails go out (see [Weekly emails](#weekly-emails)). |
| `GOAL_TRACKER_REMINDER_TIME` | `09:00` | 24-hour time of day, in the company's timezone, the weekly emails go out. |
| `GOAL_TRACKER_BASE_URL` | from `GOAL_TRACKER_ADDR`, e.g. `http://localhost:8080` | Where people reach the app. Links in emails (the weekly emails, comment alerts, and Action Item emails) point here. |
| `GOAL_TRACKER_OIDC_ISSUER` | _(none)_ | The company's OpenID Connect issuer URL. Required, with the client ID and secret below, for `GOAL_TRACKER_SIGNIN=oidc`, and refused with `password` and `dev` (see [Sign-in](#sign-in)). |
| `GOAL_TRACKER_OIDC_CLIENT_ID` | _(none)_ | Goal Tracker's client ID at the provider. |
| `GOAL_TRACKER_OIDC_CLIENT_SECRET` | _(none)_ | Goal Tracker's client secret at the provider. |
| `GOAL_TRACKER_DIRECTORY_URL` | _(none)_ | The company's directory, Authentik's base URL (e.g. `https://auth.example.com`). With the token below, the app syncs people and their Managers from it (see [Managers](#managers)). Set both or neither. |
| `GOAL_TRACKER_DIRECTORY_TOKEN` | _(none)_ | An Authentik API token that can read users. |
| `GOAL_TRACKER_DIRECTORY_FILE` | _(none)_ | A CSV file of people and their Managers, exported from the company's HR system: the directory to sync from instead of Authentik (see [Managers](#managers)). Startup stops if it's set with either Authentik variable above. |
| `GOAL_TRACKER_CHROME` | _(none: looks on `PATH`)_ | The headless Chromium, a path or a name on `PATH`, that renders a published Report's PDF download. Unset, the app looks on `PATH` for `chromium`, `chromium-browser`, `google-chrome` and `google-chrome-stable`, in that order. Startup logs the one it uses; with none, PDF downloads fall back to the print page, which the reader prints to PDF from the browser. Startup stops when it names a file that isn't there or can't run. |
| `GOAL_TRACKER_SESSION_KEY` | _(required for `oidc` and `password`)_ | Base64 key, at least 32 bytes, that signs session cookies (`openssl rand -base64 32`). Required for `GOAL_TRACKER_SIGNIN=oidc` and `password`. With `dev` it may be unset: the app then makes a random one and logs a WARN, and everyone is signed out when it restarts. |
| `GOAL_TRACKER_START_AT` | _(none: the wall clock)_ | **Test-only.** An RFC 3339 instant (e.g. `2026-10-05T18:05:00Z`) the app's clock starts at, then ticks normally from; startup logs a WARN when it's set. The e2e suite uses it to run against a seed frozen at one date. Never set it in production. |

## Sign-in

`GOAL_TRACKER_SIGNIN` says how people sign in. It has no default: startup stops
when it's unset or names a method the app doesn't know, so a missing variable
can never open the app to everyone.

### `oidc`: the company's provider

People sign in through the company's OpenID Connect provider: the sign-in page
has one button, "Sign in with your organization". It needs the three
`GOAL_TRACKER_OIDC_*` variables and `GOAL_TRACKER_SESSION_KEY`; startup stops,
naming what's missing, without them, or if the issuer can't be discovered.
Register `GOAL_TRACKER_BASE_URL` + `/auth/callback` (by default
`http://localhost:8080/auth/callback`) as the redirect URI, and allow the
authorization code grant with the scopes `openid email profile`. The provider
must send a verified `email`; its `name` claim becomes the person's Name.
Signing out of the app also signs the person out of the provider when it offers
RP-initiated logout (`end_session_endpoint`), and the provider sends them back
to the sign-in page; register `GOAL_TRACKER_BASE_URL` + `/signin` as a
post-logout redirect URI for that. A local [Authentik](dev/authentik/README.md)
with the seed's people and their managers is set up for this, and `make serve`
uses it.

### Passwords (`password`)

For a company without an SSO provider. People sign in with their email and a
password, hashed with argon2id. Nobody chooses their own way in: a password can
only be set through a one-time set-password link that an Admin hands to the
person, so each person reaches only their own account. It needs
`GOAL_TRACKER_SESSION_KEY`, and setting any `GOAL_TRACKER_OIDC_*` variable with
it stops startup. Nothing is emailed; the Admin sends the link however they
like.

- **The first Admin.** No one can sign in until someone has a password, so the
  operator prints the first link from the command line, with the same
  environment the server runs with:

  ```sh
  GOAL_TRACKER_SIGNIN=password GOAL_TRACKER_SESSION_KEY=... GOAL_TRACKER_ADMINS=ada@example.com \
    goal-tracker password-link ada@example.com
  ```

  It runs the migrations, creates the account if there's none (so
  `GOAL_TRACKER_ADMINS` applies to it), prints
  `GOAL_TRACKER_BASE_URL/set-password?token=...` and exits. It refuses unless
  `GOAL_TRACKER_SIGNIN=password`, and refuses anything that isn't an email
  address, creating no account.
- **Issuing links.** On the Admin page, the **Set-password links** card takes
  an email and shows the whole link once. Issuing one for an email with no
  account creates the account. A link is only issued for an email address, so
  a typo like `ada` or `Ada <ada@example.com>` is refused and creates nothing;
  a Departed person can't be issued one either. A link
  works once and expires after 72 hours, and issuing a new one for a person
  replaces their old one.
- **Setting and resetting.** The link opens a page showing the person's email
  and two password fields. A password is at least 12 characters and at most
  1024 bytes; nothing else is required. Setting it signs the person out of
  every other browser and signs them in. A forgotten password is reset the same
  way: an Admin issues a new link.
- **Lockout.** Every failed sign-in says the same thing, whatever went wrong.
  Five wrong passwords in a row lock the account for 15 minutes, even against
  the right one; using a new set-password link lifts the lock at once. Apart
  from that, 50 failed sign-ins from one IP address within 15 minutes hold
  every sign-in from that address until the window passes. The address is the
  connection's, unless it is one of `GOAL_TRACKER_TRUSTED_PROXIES`: then it's
  the client its `X-Forwarded-For` names (see
  [docs/install.md § 7](docs/install.md#7-put-a-reverse-proxy-in-front)).
  Without that setting, **behind a reverse proxy the limit counts the proxy's
  address**, and one person's mistakes count against everyone behind it. The
  counts are kept in memory, so a restart clears them.
  Each password hash takes 64 MiB, so at most one runs per CPU at once and a
  burst of sign-ins waits its turn rather than exhausting memory.

### `dev`: the development form

Any email address signs in, and an account is created on first sign-in: anyone
who can reach the app can act as anyone, so startup logs a WARN saying so. It's
for development and tests only. Setting any `GOAL_TRACKER_OIDC_*` variable with
it stops startup, so a half-made provider setup can't fall back to the open
form. `make serve NO_SSO=1`, `scripts/scratch-app` and the e2e suite use it.

### Sessions

Whatever the method, the session cookie is signed with
`GOAL_TRACKER_SESSION_KEY`, and is `Secure` when `GOAL_TRACKER_BASE_URL` is
`https`. It also carries the person's session generation, so all of their
sessions can be ended at once: moving the generation on signs every browser
they're signed in on out, and their next sign-in works as usual. Signing out
does that, as do a password reset and being marked Departed, so marking someone
returned doesn't revive a session from before they left.

A session ends 30 days after sign-in, or after 7 days without a request. A
request more than a day after the cookie was last issued re-issues it, so the
idle limit counts from then; the 30 days don't move.

## Managers

Each person's Manager comes from the company's directory, never from the tool
([ADR 0008](docs/adr/0008-managers-come-from-a-directory-sync.md)). The
directory is one of two sources, synced at startup and then hourly: Authentik's
users API, or a CSV file. Set one or the other; startup stops if
`GOAL_TRACKER_DIRECTORY_FILE` is set with either Authentik variable.

With `GOAL_TRACKER_DIRECTORY_URL` and `GOAL_TRACKER_DIRECTORY_TOKEN` set, the
app reads every user from Authentik's users API. Each user with an email gets
an Account if they have none, their Name, their Manager from the `manager`
attribute (an email), and their timezone from the `timezone` attribute (an
IANA name such as `America/Los_Angeles`); service accounts are skipped. Startup
stops if only one of the two variables is set.

With `GOAL_TRACKER_DIRECTORY_FILE` set, the app reads that file instead, afresh
on every sync, so an edit to it applies at the next hourly sync or **Sync
now**. It's a UTF-8 CSV whose first row is the header `email,name,manager_email`,
with an optional `timezone` column:

```csv
email,name,manager_email
ada@example.com,Ada Lovelace,grace@example.com
grace@example.com,Grace Hopper,
```

The columns may come in any order, and their names in any case. Other
columns, empty or repeated, are ignored, but naming `email`, `name`,
`manager_email` or `timezone` twice is an error. A stray `"` inside an unquoted field is kept
as part of it. A field that runs onto the next line, the sign of a quote that
isn't closed, fails the whole sync, naming its line. Each later row is one person, who gets an Account if they have
none. An empty `name` leaves the Name
they have, an empty `manager_email` means no Manager, and `timezone` is an
IANA name such as `America/Los_Angeles`. Blank lines and a
leading BOM are skipped. A file that's missing, lacks one of the three columns,
or has a row with the wrong number of fields fails the whole sync, with an
error naming the file and the bad row's line.

Either way, a blank or invalid timezone means none: the person uses the
company's timezone (`GOAL_TRACKER_TIMEZONE`), and an invalid one is logged at
WARN. The sync never marks anyone Departed, and someone the directory stops
listing keeps their last Manager and their last timezone. A sync that fails changes nothing, logs
the cause at ERROR, and the next one tries again; Manager cycles are kept and
logged at WARN. The Admin page shows when it last ran and how it went, with
**Sync now** to run it at once.

With neither source set, there is no sync, and no one has a Manager.

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

- **Reminder**, to each Owner and Delegate with a Check-in due, and to each
  owner of an open Action Item. It lists their Active Goals that are Check-in
  overdue, or whose Check-in will be overdue before next week's reminder unless
  someone checks in. Each Goal links to its Check-in form, which shows last
  week's answers. It then lists every open Action Item the person owns, overdue
  first, each with its Report, the Goal it is about, its due date, and a link to
  it.
- **Parent digest**, to each parent Owner. It lists the link requests waiting
  on them. It also lists problems with the Goals that contribute to theirs: a
  child whose Health went Yellow or Red, that recorded a Date Slip, or whose
  Check-in went overdue in the past week. A child that is Ownerless or has a
  parent On Hold or Cancelled is listed every week until that's resolved.

People with nothing to report get no email, and people marked departed get
none. The prototype has no mail transport, so it logs each email's recipient
and subject instead of sending it.

## Comment alerts

Readers comment on a Goal in a published Report from its publication page. The
comment emails the Goal's current Owner. Replies in the thread email the Owner
and everyone else who has written in it, except the reply's author.

Raising an Action Item emails its owner, unless they raised it themselves.
Reassigning one emails its new owner, unless they reassigned it to themselves,
and says who had it before. The email names the Report, the Goal it is about,
and its due date, and links to it.

As with the weekly emails, the prototype logs these instead of sending them.

## Seed a fake company

To demo the prototype with a realistic organisation and no real data:

```sh
make seed                                        # seeds goal-tracker.db
GOAL_TRACKER_SIGNIN=dev go run ./cmd/goal-tracker  # then sign in as admin@example.com
```

The seed builds about 50 Goals and 12 weeks of history. It uses the same paths a
real company would: an Admin defines a **Team** Dimension and loads the Goals
through the [spreadsheet import](docs/import-format.md). The Owners then
activate their Goals and write weekly Check-ins through the domain commands.
Nothing is written to the database directly.

- **Three levels of the graph:** three company outcomes (Ongoing, owned by the CEO,
  CTO and CPO) are driven by two team Goals from each of six teams (Platform,
  Payments, Growth, Mobile, Data, Support). Each team Goal is driven by the
  team's projects.
- **Check-in history:** team Goals and projects check in weekly, and company
  outcomes every other week on a 14-day cadence. Check-ins carry Health, a
  status, Paths to Green, and Metric readings. A parent's Owner usually reports
  its Rolled-up Health. Sometimes they report better than it and explain why.
- **A mix of Health:** most projects stay Green. Troubled ones turn Yellow, then
  Red, and some recover to Yellow.
- **Date Slips:** troubled projects move their delivery date and the Milestones
  that fall due while they're behind, each with a reason.
- **Milestone Churn:** some projects add a Milestone partway through and later
  remove one, with a reason.
- **Check-in overdue:** a few Owners stop checking in, so those Goals' last
  Check-in is older than their cadence when the seed finishes.
- **Unlinked:** five side projects contribute to no other Goal.
- **Lifecycle:** a few projects are Done with an outcome, one is On Hold, one
  is Cancelled, and two are still Proposed.
- **Managers:** everyone has a Manager, recorded through the same directory
  sync a real company uses (see [Managers](#managers)). The CEO is at the top,
  with the CTO, CPO and Admin under them. The Platform, Mobile and Data leads
  report to the CTO, and the Payments, Growth and Support leads to the CPO.
  Each team's people report to their lead. Sign in as `ceo@example.com` to see
  the whole company as one Org, or as a team lead such as
  `growth-lead@example.com` to see a team's.

Sign in as `admin@example.com` (an Admin), or as any Owner in the company: the
leads (`platform-lead@example.com` and so on), the company outcome owners
(`ceo@example.com`, `cto@example.com`, `cpo@example.com`), or a project Owner.
Everyone the seed creates has a Name, which the pages show in place of their
email; you still sign in with the email.

The seed is deterministic. The same seed and end date build the same company,
Check-in for Check-in. It only seeds a fresh database: it refuses one that
already has Goals, so to reseed, delete the database file and run it again.

| Flag | Default | Meaning |
| ---- | ------- | ------- |
| `-db` | `$GOAL_TRACKER_DB`, else `goal-tracker.db` | Database to seed (`make seed SEED_DB=…`). |
| `-seed` | `23` | Random seed. It decides dates, Owners, and how each project's weeks play out. |
| `-end` | now | Last day of the history, `YYYY-MM-DD`. The history ends on the day you seed, so Check-in overdue is judged against that day. Reseed later on to keep the demo current. |
| `-admin` | `admin@example.com` | The Admin who defines the Team Dimension and runs the import. |

## License

MIT. See [LICENSE](LICENSE). You can use, copy, modify and distribute the code as you like, as long as the copyright notice stays with it.
