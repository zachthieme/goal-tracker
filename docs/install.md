# Installing Goal Tracker

For the person installing and running Goal Tracker on a company server. It
covers one Linux host running the app under systemd behind a TLS reverse proxy,
with people and their Managers read from a CSV file exported from HR.
[README.md](../README.md) is the full reference for every setting; this guide
is the order to do things in.

## Before you start: what this release does and doesn't do

Read these first; some may decide whether the install goes ahead now.

- **No email is sent.** The app has no mail transport yet. Check-in
  reminders, the parent digest, comment alerts and Action Item emails are
  written to the log (recipient and subject) instead of sent. Everything else
  works; people just aren't notified.
- **One instance only.** The database is a single SQLite file, sign-in lockout
  counts are kept in memory, and the weekly email job runs in the process. Run
  exactly one copy against one database file. There is no clustering or
  failover.
- **The first Admins come from `GOAL_TRACKER_ADMINS`.** It gives the Admin flag
  only when an account is first created, and the CSV sync creates an account
  for everyone in the file on the first start, so list at least one Admin
  **before the first start**. Admins can make others Admins later on the Admin
  page; see [Making someone an Admin later](#making-someone-an-admin-later).
- **Stopping is abrupt.** The server doesn't wait for in-flight requests on
  `SIGTERM`. Each change is one database transaction, so nothing is left half
  written, but a request in progress at the moment of a restart fails and the
  person has to resubmit.

## Requirements

- A Linux host (x86-64 or arm64). Any size works for a company of hundreds;
  each password sign-in briefly uses 64 MiB, one at a time per CPU.
- A DNS name for the app, and a TLS certificate for it (the reverse proxy
  handles TLS; the app speaks plain HTTP on localhost).
- To build: Go 1.26 or newer, on any machine. The binary is static (pure-Go
  SQLite, no cgo), so you can build elsewhere and copy it over.
- Optional but recommended: the `sqlite3` command-line tool on the host, for
  backups.

## 1. Build the binary

```sh
git clone https://github.com/zachthieme/goal-tracker.git
cd goal-tracker
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o goal-tracker ./cmd/goal-tracker
```

Use `GOARCH=arm64` for an arm64 host. The generated code is committed, so the
build needs nothing but Go. The pages, styles, migrations and timezone data are
all embedded: the binary is the whole app (about 28 MB).

## 2. Lay out the host

```sh
sudo useradd --system --home /var/lib/goal-tracker --shell /usr/sbin/nologin goal-tracker
sudo install -m 0755 goal-tracker /usr/local/bin/goal-tracker
sudo install -d -m 0750 -o goal-tracker -g goal-tracker /var/lib/goal-tracker
sudo install -d -m 0750 -o root -g goal-tracker /etc/goal-tracker
```

| Path | What |
| ---- | ---- |
| `/usr/local/bin/goal-tracker` | The binary. |
| `/etc/goal-tracker/env` | Settings, including secrets. |
| `/etc/goal-tracker/people.csv` | The directory file from HR. |
| `/var/lib/goal-tracker/goal-tracker.db` | The database. **This is the only data to back up.** |

The database's directory must exist and be writable by the service user:
SQLite writes a journal file next to the database during each change. The app
creates the database and runs its migrations on first start.

## 3. Choose how people sign in

Set `GOAL_TRACKER_SIGNIN` to one of these. Never use `dev` on a real server:
it lets anyone sign in as anyone.

- **`oidc`**: the company's single sign-on (Okta, Entra ID, Google, Authentik,
  anything that speaks OpenID Connect). Register an application at the
  provider with:
  - redirect URI `https://goals.example.com/auth/callback`;
  - post-logout redirect URI `https://goals.example.com/signin`;
  - the authorization code grant, with scopes `openid email profile`.

  The provider must send a verified `email`; its `name` claim becomes the
  person's display name. Each person's email at the provider must match their
  email in the CSV file.
- **`password`**: for a company without SSO. People set a password through a
  one-time link an Admin gives them (nothing is emailed). See
  [the README](../README.md#passwords-password) for the rules and lockout.

## 4. Write the settings

`/etc/goal-tracker/env` (owner `root`, group `goal-tracker`, mode `0640`):

```sh
# How people sign in: oidc or password.
GOAL_TRACKER_SIGNIN=password

# Where people reach the app. Emailed links and the OIDC callback use it, and
# session cookies are marked Secure because it is https.
GOAL_TRACKER_BASE_URL=https://goals.example.com

# Listen on localhost only; the reverse proxy is the way in.
GOAL_TRACKER_ADDR=127.0.0.1:8080
GOAL_TRACKER_DB=/var/lib/goal-tracker/goal-tracker.db

# Signs session cookies. Generate once with `openssl rand -base64 32` and keep
# it: changing it signs everyone out.
GOAL_TRACKER_SESSION_KEY=

# The first Admins, comma-separated. Applies when an account is first created,
# so set it before the first start; Admins can make others Admins later on the
# Admin page.
GOAL_TRACKER_ADMINS=it-admin@example.com,ops-lead@example.com

# The company's timezone as an IANA name, and the timezone of anyone the
# directory gives none.
GOAL_TRACKER_TIMEZONE=America/New_York

# People and their Managers.
GOAL_TRACKER_DIRECTORY_FILE=/etc/goal-tracker/people.csv

# When the weekly emails go out (logged only in this release).
GOAL_TRACKER_REMINDER_DAY=Monday
GOAL_TRACKER_REMINDER_TIME=09:00

# With GOAL_TRACKER_SIGNIN=oidc, also:
# GOAL_TRACKER_OIDC_ISSUER=https://login.example.com/
# GOAL_TRACKER_OIDC_CLIENT_ID=
# GOAL_TRACKER_OIDC_CLIENT_SECRET=
```

Startup checks this and stops with a message naming the problem: an unset or
unknown sign-in method, a missing or short session key, OIDC variables set
with `password`, a missing OIDC variable with `oidc`, or a
`GOAL_TRACKER_TIMEZONE` that isn't an IANA name (`Local` included). Never set
`GOAL_TRACKER_START_AT`; it's for tests.

## 5. The CSV file

The directory sync reads `GOAL_TRACKER_DIRECTORY_FILE` at startup, every hour,
and when an Admin presses **Sync now** on the Admin page. It re-reads the file
each time, so replacing the file is how HR changes reach the app.

```csv
email,name,manager_email,timezone
ceo@example.com,Dana Whitfield,,
ada@example.com,Ada Lovelace,ceo@example.com,America/Los_Angeles
grace@example.com,Grace Hopper,ada@example.com,
```

- UTF-8, with a header row naming `email`, `name` and `manager_email`, in any
  order and any case, and optionally `timezone`. Other columns are ignored.
- Everyone in the file gets an account (without a password) if they don't have
  one. An empty `name` keeps the name they have; an empty `manager_email`
  means no Manager.
- `timezone` is where the person works, an IANA name such as
  `America/Los_Angeles`. A blank or invalid one, or no column at all, means
  they use the company's timezone (`GOAL_TRACKER_TIMEZONE`); an invalid one is
  logged at `WARN`.
- A file that's missing, lacks a column, has a row with the wrong number of
  fields, or has a field that runs onto the next line (an unclosed quote)
  fails the sync and changes nothing. The Admin page shows when the last
  sync ran and whether it worked, and failures are logged at `ERROR`.
- The sync never removes anyone. Someone who leaves the company stays until an
  Admin marks them **Departed**, from the Owner section of any Goal they own;
  they then can't sign in, get no email, and their Goals show as Ownerless.

Have HR's export land somewhere the service can read
(`sudo install -m 0640 -o root -g goal-tracker export.csv /etc/goal-tracker/people.csv`),
replacing the file whole rather than editing it in place, so a sync never reads
a half-written file.

## 6. Run it under systemd

`/etc/systemd/system/goal-tracker.service`:

```ini
[Unit]
Description=Goal Tracker
After=network-online.target
Wants=network-online.target

[Service]
User=goal-tracker
Group=goal-tracker
EnvironmentFile=/etc/goal-tracker/env
ExecStart=/usr/local/bin/goal-tracker
Restart=on-failure
RestartSec=5
WorkingDirectory=/var/lib/goal-tracker

NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=/var/lib/goal-tracker

[Install]
WantedBy=multi-user.target
```

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now goal-tracker
journalctl -u goal-tracker -f
```

A healthy start logs, among others:

```
level=INFO msg="syncing people and their Managers from the directory file, now and hourly" file=/etc/goal-tracker/people.csv
level=INFO msg="directory sync read the company's people" people=412
level=INFO msg="goal-tracker listening" addr=127.0.0.1:8080 db=/var/lib/goal-tracker/goal-tracker.db timezone=America/New_York weekly_emails="Monday 09:00"
```

Logs are plain text (`time=… level=… msg=…`) on stderr, so journald keeps them.
Watch for `level=ERROR`. A `level=WARN` naming `GOAL_TRACKER_SIGNIN=dev` or
`GOAL_TRACKER_START_AT` means the settings are wrong for production.

## 7. Put a reverse proxy in front

The proxy terminates TLS and forwards to `127.0.0.1:8080`. Nothing special is
needed: no websockets, no long requests, no path prefix (the app must be served
at the root of its host name).

Caddy (gets its own certificate):

```
goals.example.com {
	reverse_proxy 127.0.0.1:8080
}
```

nginx:

```nginx
server {
    listen 443 ssl;
    server_name goals.example.com;
    ssl_certificate     /etc/ssl/goals.example.com.crt;
    ssl_certificate_key /etc/ssl/goals.example.com.key;
    client_max_body_size 10m;   # spreadsheet imports

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
    }
}
```

With password sign-in, the app limits failed sign-ins per IP address and
doesn't read `X-Forwarded-For`, so behind a proxy every sign-in counts against
the proxy's address: 50 failures within 15 minutes, from anyone, hold
everyone's sign-ins until the window passes. The per-account lockout (five
wrong passwords, 15 minutes) is unaffected.

For a health check, `GET /signin` returns `200` while the app is up.

## 8. The first sign-in

- **OIDC:** an Admin from `GOAL_TRACKER_ADMINS` opens
  `https://goals.example.com` and signs in with the company account.
- **Passwords:** nobody has a password yet, so print the first Admin a link,
  with the service's settings and as its user:

  ```sh
  sudo -u goal-tracker sh -c 'set -a; . /etc/goal-tracker/env; exec /usr/local/bin/goal-tracker password-link it-admin@example.com'
  ```

  It prints `https://goals.example.com/set-password?token=…` and exits; the
  server can stay running. The link works once, for 72 hours. The Admin then
  issues everyone else's links from the Admin page's **Set-password links**
  card and sends them however the company sends such things.

Then check the Admin page: the directory sync's last run should be a success
with the right number of people. Goals can be loaded in bulk with the
[spreadsheet import](import-format.md), or entered by hand.

## Running it

### Backups

The database file is the whole state. Back it up while the app runs with
SQLite's online backup, which copies a consistent snapshot:

```sh
sqlite3 /var/lib/goal-tracker/goal-tracker.db ".backup /var/backups/goal-tracker/$(date +%F).db"
```

Don't copy the file with `cp` while the app is running; you can catch it
mid-change. Run the backup nightly from cron or a systemd timer and keep
copies off the host. To check a backup:
`sqlite3 <backup> "pragma integrity_check"` prints `ok`.

To restore: stop the service, replace `goal-tracker.db` with the backup (owned
by `goal-tracker`), delete any `goal-tracker.db-journal` beside it, and start
the service.

### Upgrading

1. Back up the database.
2. Build the new binary and install it over `/usr/local/bin/goal-tracker`.
3. `sudo systemctl restart goal-tracker`.

Database migrations run automatically on start and only move forward: to go
back to an older binary, restore the backup taken before the upgrade. Read the
top of [CHANGELOG.md](../CHANGELOG.md) first; anything tagged `[ops]`, and
especially a **Breaking changes** entry, may need a settings change.

### Making someone an Admin later

An Admin does it on the Admin page, in the **Admins** section: type the
person's email and press **Make Admin**. They need an account already (the
directory sync, a sign-in or a set-password link gives them one); a Departed
person must be marked returned first. **Remove Admin…** beside another Admin
takes the role away; no Admin can remove their own, so there is always at
least one. Either change takes effect on the person's next page load.

### Signing everyone out

Change `GOAL_TRACKER_SESSION_KEY` and restart. Everyone signs in again; with
passwords, their passwords still work.

## Troubleshooting

| Symptom | Cause |
| ------- | ----- |
| `run migrations: … unable to open database file (14)` | The database's directory doesn't exist or isn't writable by `goal-tracker`. |
| `GOAL_TRACKER_SIGNIN must be one of oidc, password, dev` | The env file isn't being read, or the variable is misspelled. |
| `GOAL_TRACKER_SESSION_KEY is N bytes, want at least 32` | Generate it with `openssl rand -base64 32`. |
| OIDC: startup fails discovering the issuer | The issuer URL is wrong, or the host can't reach the provider. |
| OIDC: the provider says the redirect URI doesn't match | `GOAL_TRACKER_BASE_URL` + `/auth/callback` must match what's registered exactly. |
| Signed out on every request, over https | `GOAL_TRACKER_BASE_URL` is `https`, so cookies are `Secure`; the browser must reach the app over https too. |
| Nobody has a Manager | The sync failed (Admin page, and `ERROR` in the log), or `GOAL_TRACKER_DIRECTORY_FILE` is unset. |
| Someone who should be an Admin isn't | They were created before being listed in `GOAL_TRACKER_ADMINS`. An Admin can make them one on the Admin page; see [above](#making-someone-an-admin-later). |
| Emails never arrive | Expected in this release: they're logged, not sent. |
