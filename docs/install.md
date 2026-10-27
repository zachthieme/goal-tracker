# Installing Goal Tracker

For the person installing and running Goal Tracker on a company server. It
covers one Linux host running the app under systemd behind a TLS reverse proxy,
with people and their Managers read from a CSV file exported from HR.
[README.md](../README.md) is the full reference for every setting; this guide
is the order to do things in.

## Before you start: what this release does and doesn't do

Read these first; some may decide whether the install goes ahead now.

- **No email is sent.** The app has no mail transport yet. Check-in
  reminders, the parent digest, Nudges, comment alerts and Action Item emails are
  written to the log (the recipient's Account ID and the subject) instead of
  sent. Everything else
  works; people just aren't notified.
- **One instance only.** The database is a single SQLite file, the password
  sign-in throttle's per-address counts are kept in memory, and the weekly email job runs in the process. Run
  exactly one copy against one database file. There is no clustering or
  failover.
- **The first Admins come from `GOAL_TRACKER_ADMINS`.** It gives the Admin flag
  only when an account is first created, and the CSV sync creates an account
  for everyone in the file on the first start, so list at least one Admin
  **before the first start**. Admins can make others Admins later on the Admin
  page; see [Making someone an Admin later](#making-someone-an-admin-later).
- **Stopping waits up to 30 seconds.** On `SIGTERM` or `SIGINT` the server
  stops taking connections and gives the requests in flight up to 30 seconds
  to finish before it exits, so give your service manager a stop timeout
  longer than that. A weekly email run in progress is cut short; each change
  is one database transaction, so nothing is left half written. A run cut
  short has queued none of its emails, and runs when the server next starts.
- **A week missed while the server is down is sent late.** The database keeps
  which week's slot the weekly emails last went out for. Started after a slot
  that passed while it was down, the server sends that week's emails once,
  as it starts, however many weeks it missed. On the very first
  start it sends none until the next slot.
- **A weekly run that fails is tried again a minute later.** A run that fails,
  such as on a database error, queues none of its emails. The server tries it
  again each minute until it succeeds, without waiting for a restart or the
  next week's slot. Its first failure is logged at ERROR at once, and a run
  that keeps failing is logged again only after a wait that doubles, as for
  any [background job](#background-jobs).

## Requirements

- A Linux host (x86-64 or arm64). Any size works for a company of hundreds;
  each password sign-in briefly uses 64 MiB, one at a time per CPU.
- A DNS name for the app, and a TLS certificate for it (the reverse proxy
  handles TLS; the app speaks plain HTTP on localhost).
- To build: Go 1.27.3 or newer, on any machine (or Docker; see below). The binary is static (pure-Go
  SQLite, no cgo), so you can build elsewhere and copy it over.
- Optional but recommended: the `sqlite3` command-line tool on the host, for
  backups.
- Optional: Chromium or Google Chrome on the host (`apt install chromium` on
  Debian, `dnf install chromium` on Fedora), for Reports to download as PDF
  files. On Ubuntu, `chromium` is a snap, which can't start under the systemd
  unit's `NoNewPrivileges=yes`: install Google Chrome's `.deb` instead, or use
  the Docker image, which has its own. Without one, a Report's PDF download
  opens its print page, which the reader prints to PDF from the browser.

## 1. Build the binary

```sh
git clone https://github.com/zachthieme/goal-tracker.git
cd goal-tracker
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o goal-tracker ./cmd/goal-tracker
```

Use `GOARCH=arm64` for an arm64 host. The generated code is committed, so the
build needs nothing but Go. The pages, styles, migrations and timezone data are
all embedded: the binary is the whole app (about 30 MB).

### Or build the Docker image

The repo's `Dockerfile` builds the same binary into a Debian image with
Chromium for PDF downloads. It runs as a non-root user, listens on 8080, and
keeps the database in the `/data` volume (`GOAL_TRACKER_DB` is
`/data/goal-tracker.db`):

```sh
docker build -t goal-tracker .
docker run -d --name goal-tracker --restart unless-stopped -p 127.0.0.1:8080:8080 \
  -v goal-tracker-data:/data \
  -v /etc/goal-tracker/people.csv:/etc/goal-tracker/people.csv:ro \
  --env-file /etc/goal-tracker/env goal-tracker
```

The settings in step 4 go in the env file, without `GOAL_TRACKER_ADDR`,
`GOAL_TRACKER_DB` or `GOAL_TRACKER_CHROME`, which the image sets. Two differ
in a container:

- `GOAL_TRACKER_DIRECTORY_FILE` is the CSV's path inside the container:
  `/etc/goal-tracker/people.csv` with the mount above. The app runs as uid
  10001, so the file must be readable by it: `chown root:10001` with mode
  `0640`, or mode `0644`. Without the mount, startup still succeeds, but every
  sync logs an `ERROR` and nobody has a Manager.
- `GOAL_TRACKER_TRUSTED_PROXIES` is the address the container sees the proxy
  connect from: the bridge's gateway, usually `172.17.0.1`, not `127.0.0.1`.

Of step 2, only `/etc/goal-tracker` (the env file and the CSV) applies, and
step 6 (systemd) doesn't; the reverse proxy in step 7 does. A host directory
mounted at `/data` in place of the volume must be owned by uid 10001. The
image includes `sqlite3`, so the online backup in § Backups runs inside it,
writing into the volume:

```sh
docker exec goal-tracker sqlite3 /data/goal-tracker.db ".backup /data/backup-$(date +%F).db"
```

Then copy the backup off the volume and off the host.

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
the database runs in WAL mode, and SQLite keeps `goal-tracker.db-wal` and
`goal-tracker.db-shm` files next to it while the app runs. The app creates the
database and runs its migrations on first start.

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

# Where people reach the app. Required: startup stops without it. Emailed
# links, set-password links and the OIDC callback use it, and session cookies
# are marked Secure because it is https.
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

# When the weekly emails go out (logged only in this release). A slot missed
# while the server was down is sent once when it next starts.
GOAL_TRACKER_REMINDER_DAY=Monday
GOAL_TRACKER_REMINDER_TIME=09:00

# The Chromium that renders Reports' PDF downloads. Unset, the app looks on PATH
# for chromium, chromium-browser, google-chrome and google-chrome-stable.
# GOAL_TRACKER_CHROME=/usr/bin/chromium

# With GOAL_TRACKER_SIGNIN=password, the reverse proxies whose X-Forwarded-For
# names the client, as comma-separated CIDRs or IPs: the address the app sees
# the proxy connect from (see step 7). Unset, X-Forwarded-For is ignored.
GOAL_TRACKER_TRUSTED_PROXIES=127.0.0.1

# Logs go to stderr, which systemd's journal keeps. The least severe record
# written (debug, info, warn or error), and text or json lines.
# GOAL_TRACKER_LOG_LEVEL=info
# GOAL_TRACKER_LOG_FORMAT=text

# Where Prometheus scrapes GET /metrics, on a listener of its own. Unset, there
# are no metrics. It has no sign-in, so keep it on a private address (see
# § Logs and metrics).
# GOAL_TRACKER_METRICS_ADDR=127.0.0.1:9090

# With GOAL_TRACKER_SIGNIN=oidc, also:
# GOAL_TRACKER_OIDC_ISSUER=https://login.example.com/
# GOAL_TRACKER_OIDC_CLIENT_ID=
# GOAL_TRACKER_OIDC_CLIENT_SECRET=
```

Startup checks this and stops with a message naming the problem: an unset or
unknown sign-in method, an unset `GOAL_TRACKER_BASE_URL`, a missing or short
session key, OIDC variables set
with `password`, a missing OIDC variable with `oidc`, or a
`GOAL_TRACKER_TIMEZONE` that isn't an IANA name (`Local` included), a
session key that isn't base64, a `GOAL_TRACKER_REMINDER_DAY` that isn't a
weekday name or a `GOAL_TRACKER_REMINDER_TIME` that isn't a 24-hour `HH:MM`,
`GOAL_TRACKER_DIRECTORY_FILE` set together with the Authentik directory
variables, only one of the two Authentik directory variables
(`GOAL_TRACKER_DIRECTORY_URL` and `GOAL_TRACKER_DIRECTORY_TOKEN`) set, with
`oidc` an issuer whose discovery fails, a
`GOAL_TRACKER_CHROME` that isn't there or can't run, a
`GOAL_TRACKER_TRUSTED_PROXIES` entry that is neither a CIDR nor an IP (or is
an IPv4-mapped CIDR, `::ffff:a.b.c.d/n`, wider than `/96`), a
`GOAL_TRACKER_LOG_LEVEL` or `GOAL_TRACKER_LOG_FORMAT` it doesn't know, a
`GOAL_TRACKER_METRICS_ADDR` it can't listen on, or, with
a Chromium, a `TMPDIR` too long for it (see § PDF downloads). Never set
`GOAL_TRACKER_START_AT`; it's for tests.

### PDF downloads

A published Report downloads as Markdown or PDF. With a Chromium, named by
`GOAL_TRACKER_CHROME` or found on `PATH`, the app renders the Report's print
page to a PDF file itself: Chromium loads the page from a loopback server the
app starts for each render, which fetches the page's styles and fonts from the
app's own listen address (not `GOAL_TRACKER_BASE_URL`). At most two render at
once, and each download gets 30 seconds in all to wait for its turn and
render; one that can't start in that time gets a 503 "try again" page and
logs a `WARN`, and a render that fails logs an `ERROR`. Without
a Chromium, the PDF choice opens the print page instead, for the reader to
print to PDF from the browser, and startup logs that it falls back.

No script runs on the print page as Chromium renders it, as the page needs
none: the app serves the page to Chromium with a `Content-Security-Policy` of
`script-src 'none'`. It also passes `--disable-javascript`, which headless
Chromium ignores. The app runs Chromium without its sandbox: it runs it as a
non-root user in a container, where the sandbox can't start without a seccomp
profile that allows it, and setting one up is the operator's work.

With a Chromium, `TMPDIR` must be short: Chromium keeps a socket under it, and
a socket's path on Linux is at most 107 bytes. `/tmp`, the default when
`TMPDIR` is unset, is short enough, and so is the Docker image's. Up to 47
bytes, each render keeps Chromium's files in a directory of its own, removed
when it ends, so a render killed for time leaves nothing behind. Up to 62
bytes, renders still work, but one killed for time may leave
`org.chromium.Chromium.*` in `TMPDIR`, and startup logs a `WARN` saying so.
Longer, Chromium can't render at all, and startup stops with an error naming
`TMPDIR`.

### Logs and metrics

The app logs to stderr: under systemd, `journalctl -u goal-tracker` reads it.
`GOAL_TRACKER_LOG_LEVEL` sets the least severe record written, `debug`,
`info` (the default), `warn` or `error`, and `GOAL_TRACKER_LOG_FORMAT` how
records are written: `text` (the default), slog's `key=value` lines, or
`json`, one JSON object a line, for a log collector to parse. Either is read
in any case. Each request gets one `INFO` access line, `msg=request`, with
its method, route pattern, status, bytes and `duration_ms`, so `warn` leaves
them out.

Metrics are off unless `GOAL_TRACKER_METRICS_ADDR` is set. With it, say
`127.0.0.1:9090`, the app serves `GET /metrics` there, on a listener of its
own, in Prometheus's text format, and startup logs `goal-tracker serving
metrics` with the address. It exposes two metrics about the requests the app
answers:

- `goal_tracker_http_requests_total{method, route, status}`, a counter of
  requests answered;
- `goal_tracker_http_request_duration_seconds{route}`, a histogram of how long
  they took, with buckets from 5 ms to 10 s.

`route` is the route's pattern, such as `GET /goals/{id}`, never the path, so
IDs don't add series; a path no route serves is counted under `route="unmatched"`,
whatever its method, so scanned paths don't add series. A method other than
the standard ones is counted as `OTHER`. The counts start at zero each time
the app starts.

`/metrics` has no authentication, and the app's own address never serves it.
Listen on a private address the reverse proxy doesn't forward to, such as the
loopback for a Prometheus on the same host. In Docker, set it to
`0.0.0.0:9090` inside the container and publish it only to the host's loopback
or a private network (`-p 127.0.0.1:9090:9090`).

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

- UTF-8. From Excel, save as **CSV UTF-8**: its plain **CSV** is
  Windows-1252, and garbles accented names.
- A header row naming `email`, `name` and `manager_email`, in any
  order and any case, and optionally `timezone`. Other columns are ignored.
- Everyone in the file gets an account (without a password) if they don't have
  one. An empty `name` keeps the name they have; an empty `manager_email`
  means no Manager.
- `timezone` is where the person works, an IANA name such as
  `America/Los_Angeles`. A blank or invalid one, or no column at all, means
  they use the company's timezone (`GOAL_TRACKER_TIMEZONE`); an invalid one is
  logged at `WARN`.
- Give every Manager a row of their own. A `manager_email` with no row still
  gets an account, with no name, Manager or timezone, shown by its email. A
  row with a blank `email` is skipped.
- A file that's missing, lacks a column or names one twice, has a row with the wrong number of
  fields, or has a field that runs onto the next line (an unclosed quote)
  fails the sync and changes nothing. The Admin page shows when the last
  sync ran and whether it worked, and failures are logged at `ERROR`.
- The sync never removes anyone. Someone who leaves the company stays until an
  Admin marks them **Departed**, from the Owner section of any Goal they own;
  they then can't sign in, get no email, and their Goals show as Ownerless.
  Someone who owns no Goal can't be marked Departed yet, so with passwords
  their password keeps working. An Admin marks someone returned from the Goal
  page or the Admin page's **Departed people** list.

Have HR's export land somewhere the service can read, replacing the file
whole with a rename rather than writing it in place, so a sync never reads a
half-written file:

```sh
sudo install -m 0640 -o root -g goal-tracker export.csv /etc/goal-tracker/people.csv.new
sudo mv /etc/goal-tracker/people.csv.new /etc/goal-tracker/people.csv
```

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

A healthy start logs, among others and not always in this order:

```
level=INFO msg="syncing people and their Managers from the directory file, now and hourly" file=/etc/goal-tracker/people.csv
level=INFO msg="directory sync read the company's people" people=412
level=INFO msg="rendering PDF downloads with Chromium" chrome=/usr/bin/chromium
level=INFO msg="goal-tracker listening" addr=127.0.0.1:8080 db=/var/lib/goal-tracker/goal-tracker.db timezone=America/New_York weekly_emails="Monday 09:00"
```

Logs are plain text (`time=… level=… msg=…`) on stderr, so journald keeps them.
Watch for `level=ERROR`. A `level=WARN` naming `GOAL_TRACKER_SIGNIN=dev` or
`GOAL_TRACKER_START_AT` means the settings are wrong for production.

Each request gets one access line once it's answered:

```
level=INFO msg=request request_id=3TQ5DBV7XLMAWLQQ4ZK5R2NB6E method=GET route="GET /goals/{id}" status=200 bytes=18342 duration_ms=12.4
```

`route` is the pattern the request matched, not its path, so IDs stay out of
the log. A request refused before it reached its handler, a cross-site 403 or
an oversize 413, logs the route it was aimed at, and a path no route serves
logs `route=unmatched` with its 404. Every
record logged while handling a request, such as the `ERROR` for a 500, carries
the same `request_id`, which the response also returns in `X-Request-ID`. A
proxy that sends its own `X-Request-ID` of 1 to 64 letters, digits, `_` and
`-` has it reused; any other is replaced.

### Background jobs

Four jobs run in the background from startup until the server stops: the
email outbox (`job="email outbox"`), the prune (`job=prune`), the weekly
emails (`job="weekly emails"`) and, with a directory set, the directory sync
(`job="directory sync"`). Each runs as the server starts, then on its own
pace, and a run that fails is tried again at its next turn. A failure is
logged at `ERROR` as `msg="background job failed"`, naming the `job`, the
`err` and the count of `failures` in a row: the first at once, then, while
the job keeps failing, only after a wait that doubles from a minute up to an
hour. Once it succeeds again it logs `msg="background job recovered"` at
`INFO`, once, with the count of failures.

## 7. Put a reverse proxy in front

The proxy terminates TLS and forwards to `127.0.0.1:8080`. Nothing special is
needed: no websockets, no long requests, no path prefix (the app must be served
at the root of its host name). Nor does the proxy need to add
`Strict-Transport-Security`: the app sends it itself, with a one-year
`max-age`, when `GOAL_TRACKER_BASE_URL` is an https address.

### The certificate

The certificate must name the host in `GOAL_TRACKER_BASE_URL`, and the
browsers people use must trust it. Pick one:

- **A certificate from the company's own CA.** For a host only reachable
  inside the network, ask IT for a certificate for its name (say
  `goals.corp.example.com`); company-managed machines already trust that CA.
  Install the certificate (with any intermediates, in one file) and its key on
  the host, readable only by root and the proxy, and point the proxy at them:
  nginx's `ssl_certificate`/`ssl_certificate_key` below, or Caddy's
  `tls /etc/ssl/goals.crt /etc/ssl/goals.key`. Note when it expires and who
  renews it.
- **A public certificate from Let's Encrypt.** Caddy gets and renews one by
  itself when the name is in public DNS and the host is reachable from the
  internet on ports 80 and 443. For a host that isn't, Caddy can use the DNS
  challenge instead, which needs a Caddy build with your DNS provider's plugin
  and an API token for the zone.
- **Caddy's own CA (`tls internal`), for a short trial only.** Caddy issues
  the certificate from a CA it creates; every browser warns until that CA's
  root is trusted on their machine: copy `pki/authorities/local/root.crt`
  from Caddy's data directory to the people trying it, and have them add it
  to their machine's trusted roots.

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
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
```

With password sign-in, the app limits failed sign-ins per client IP address:
50 failures within 15 minutes hold that address's sign-ins until the window
passes. Behind a proxy, every request arrives from the proxy's address, so set
`GOAL_TRACKER_TRUSTED_PROXIES` to it: `127.0.0.1` for a proxy on the same host,
or, with Docker, the address the container sees it connect from (the bridge's
gateway, usually `172.17.0.1`). A request from a trusted proxy then counts
against the right-most address in its `X-Forwarded-For` that isn't a trusted
proxy, which Caddy sets by default and the nginx line above appends. A request
from any other address counts against that address, whatever it claims.
Without the setting, `X-Forwarded-For` is ignored, and 50 failures from anyone
hold everyone's sign-ins. Trust only proxies that set the header: a client
reaching the app directly from a trusted address can name any client it likes.
Startup logs the trusted proxies. The per-account lockout (five wrong
passwords, 15 minutes) is unaffected.

For a health check, `GET /healthz` needs no sign-in. It returns `200` with
`ok` while it can read the database within a second, and `503` when it
can't: it reads a table from the database, so a file that is unmounted or
unreadable fails it. It also checks that `GOAL_TRACKER_DB` still names the
file its first check found there, so once that file is deleted, or replaced
by another (even a good copy, such as a restored backup), it answers `503`
until the app restarts, even though connections the app already has open
keep reading the old file. A file replaced before the first check passes it
if the new file is a migrated database.
`goal-tracker healthcheck` checks it from the app's own host, where there may
be no curl: it GETs `/healthz` on the loopback at `GOAL_TRACKER_ADDR`'s port,
waits up to 3 seconds, and exits `0` on a `200`; otherwise it exits non-zero
with one line on stderr saying why. Besides `GOAL_TRACKER_ADDR` it reads only
the log settings (`GOAL_TRACKER_LOG_LEVEL`, `GOAL_TRACKER_LOG_FORMAT`), and
opens no database. It checks the loopback whatever host `GOAL_TRACKER_ADDR` names, so
an app listening only on another address fails it. The image's `HEALTHCHECK`
runs it every 30 seconds, so `docker ps` shows the container as `healthy` or
`unhealthy`.

## 8. The first sign-in

- **OIDC:** an Admin from `GOAL_TRACKER_ADMINS` opens
  `https://goals.example.com` and signs in with the company account.
- **Passwords:** nobody has a password yet, so print the first Admin a link,
  with the service's settings and as its user:

  ```sh
  sudo -u goal-tracker sh -c 'set -a; . /etc/goal-tracker/env; exec /usr/local/bin/goal-tracker password-link it-admin@example.com'
  ```

  With Docker, `docker exec goal-tracker goal-tracker password-link
  it-admin@example.com`, which uses the container's settings.

  It prints `https://goals.example.com/set-password?token=…` and exits; the
  server can stay running. Run it as the service's user, as above: run as root,
  it can leave a database file the service can't open. A second link for the
  same person replaces the first. The link works once, for 72 hours. The Admin then
  issues everyone else's links from the Admin page's **Set-password links**
  card and sends them however the company sends such things.

Then check the Admin page: the directory sync's last run should be a success
with the right number of people. Goals can be loaded in bulk with the
[spreadsheet import](import-format.md), or entered by hand.

## Running it

### Backups

The database is the whole state. It runs in WAL mode, so while the app runs,
`goal-tracker.db-wal` and `goal-tracker.db-shm` sit beside `goal-tracker.db`
and are part of its live state: recent changes are in the `-wal` file until
SQLite folds them into the main file. Back it up while the app runs with
SQLite's online backup, which copies a consistent snapshot of all three into
one file:

```sh
sqlite3 /var/lib/goal-tracker/goal-tracker.db ".backup /var/backups/goal-tracker/$(date +%F).db"
```

`sqlite3 /var/lib/goal-tracker/goal-tracker.db "VACUUM INTO '/var/backups/goal-tracker/$(date +%F).db'"`
works too, and writes a compacted copy.

Don't copy `goal-tracker.db` with `cp`: the main file alone is missing
whatever is still in the `-wal` file, and while the app runs you can catch it
mid-change. Run the backup nightly from cron or a systemd timer and keep
copies off the host. To check a backup:
`sqlite3 <backup> "pragma integrity_check"` prints `ok`.

To restore: stop the service, replace `goal-tracker.db` with the backup (owned
by `goal-tracker`), delete any `goal-tracker.db-wal` and `goal-tracker.db-shm`
beside it, and start the service.

### Upgrading

1. Back up the database.
2. Build the new binary and install it over `/usr/local/bin/goal-tracker`.
3. `sudo systemctl restart goal-tracker`.

Database migrations run automatically on start and only move forward: to go
back to an older binary, restore the backup taken before the upgrade.

A database created before the migrations were squashed into one baseline
(`0001_baseline.sql`) can't be upgraded: the app refuses to start on it, with
an error saying the database predates the baseline, and changes nothing in it.
Recreate it, as the error says: delete the database file (or point
`GOAL_TRACKER_DB`, or `SEED_DB` for `make seed`, at a new one), then run
`make seed`.

Read the top of [CHANGELOG.md](../CHANGELOG.md) first; anything tagged
`[ops]`, and especially a **Breaking changes** entry, may need a settings
change.

### Making someone an Admin later

An Admin does it on the Admin page, in the **Admins** section: type the
person's email and press **Make Admin**. They need an account already (the
directory sync, a sign-in or a set-password link gives them one); a Departed
person must be marked returned first. **Remove Admin…** beside another Admin
takes the role away; no Admin can remove their own, so there is always at
least one. Either change takes effect on the person's next page load.

### How long a session lasts

A session lasts 30 days from sign-in, however often it is used, and ends
sooner after about a week without a signed-in page (6 to 7 days: its last use
is recorded at most once a day). Only pages behind sign-in count:
the sign-in page, `/` and a page that isn't there don't keep a session from
idling.

### What is kept, and for how long

Every email, the weekly emails and Nudges included, is queued in the database
before it is sent, and tried again while sending fails, up to five times. An email that was sent is kept 90
days from sending, and one given up on after its fifth failed try 90 days
from when it was queued; then it's deleted. An
email still being tried is never deleted. The one-time token behind each Undo
is deleted once its 15 minutes have passed. The app looks for what to delete
when it starts and then hourly, and logs an `INFO` line, `msg="pruned stale
rows"`, saying how many of each it deleted, or, if it couldn't, an `ERROR`
for the `prune` [background job](#background-jobs).

Email log lines name the recipient by their Account ID (`account_id`), never
by their email address: the development sender's line, and the `WARN` and
`ERROR` lines for an email that failed, will be tried again, or was given up
on.

### Signing everyone out

Change `GOAL_TRACKER_SESSION_KEY` and restart. Everyone signs in again; with
passwords, their passwords still work.

## Troubleshooting

| Symptom | Cause |
| ------- | ----- |
| `run migrations: … unable to open database file (14)` | The database's directory doesn't exist or isn't writable by `goal-tracker`. |
| `run migrations: the database predates the baseline: …` | The database was created before the migrations were squashed into `0001_baseline.sql`: recreate it. Or an older binary was started on a database a newer one migrated: restore the backup taken before the upgrade. See [Upgrading](#upgrading). |
| `GOAL_TRACKER_SIGNIN must be one of oidc, password, dev` | The env file isn't being read, or the variable is misspelled. |
| `GOAL_TRACKER_SIGNIN=password needs GOAL_TRACKER_SESSION_KEY: …` | The env file has no session key: generate one with `openssl rand -base64 32`. |
| `GOAL_TRACKER_SESSION_KEY is N bytes, want at least 32` | Generate it with `openssl rand -base64 32`. |
| `GOAL_TRACKER_SIGNIN=… needs GOAL_TRACKER_BASE_URL` | Set it to the https address people reach the app at, through the reverse proxy. |
| OIDC: startup fails discovering the issuer | The issuer URL is wrong, or the host can't reach the provider. |
| OIDC: the provider says the redirect URI doesn't match | `GOAL_TRACKER_BASE_URL` + `/auth/callback` must match what's registered exactly. |
| Signed out on every request, over https | `GOAL_TRACKER_BASE_URL` is `https`, so cookies are `Secure`; the browser must reach the app over https too. |
| A Report's PDF download opens its print page, not a file | No Chromium was found: startup logs `PDF downloads fall back to the print page`. Install one, or set `GOAL_TRACKER_CHROME`. |
| `GOAL_TRACKER_METRICS_ADDR: listen tcp …: bind: address already in use` | Something else listens on the metrics address: pick another port, or stop it. |
| `GOAL_TRACKER_CHROME: … can't run` | The path is wrong, or the file isn't executable by the service's user. |
| Nobody has a Manager | The sync failed (Admin page, and `ERROR` in the log), or `GOAL_TRACKER_DIRECTORY_FILE` is unset. With Docker, the CSV isn't mounted into the container, or uid 10001 can't read it. |
| PDF downloads fail with an `ERROR` after startup found a Chromium | On Ubuntu, a snap Chromium can't run under the unit's hardening: install Google Chrome's `.deb`. |
| Someone who should be an Admin isn't | They were created before being listed in `GOAL_TRACKER_ADMINS`. An Admin can make them one on the Admin page; see [above](#making-someone-an-admin-later). |
| Emails never arrive | Expected in this release: they're logged, not sent. |
