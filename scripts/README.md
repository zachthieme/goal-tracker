# Verification scripts

Tools for checking a change by hand or from an agent session. None of them
ships in the app. Each runs from a clean checkout with what's already on the
machine: Go, Python 3, Node 22, and Chromium for `shots.mjs`.

## `scratch-app`: a throwaway copy of the app

`start` builds the working tree into a scratch directory, seeds a fresh
database with the [fake org](../README.md#seed-a-fake-org), and serves it on a
free port with `admin@example.com` as an Admin. It waits until the app answers,
then prints the base URL and how to sign in. Running `start` again replaces the
running copy with a new build and a fresh database.

```sh
scripts/scratch-app start    # Goal Tracker is running at http://127.0.0.1:41273 (pid 5120) …
scripts/scratch-app url      # http://127.0.0.1:41273
scripts/scratch-app stop     # Stopped the scratch app (pid 5120, port 41273).
```

`stop` signals only the pid that `start` recorded, and only while that pid is
still the scratch binary. It never matches processes by name, so your own
server, such as `make serve` on port 8090, keeps running. Each checkout gets
its own scratch directory under `$TMPDIR` (set `SCRATCH_DIR` to choose another),
so parallel worktrees don't collide. `SCRATCH_PORT` picks the port. The
database, `server.log` and `seed.log` stay in the scratch directory until the
next `start`.

## `shots.mjs`: screenshots and the facts behind them

Takes a base URL and a list of shots. It saves a PNG for each shot and prints
the facts a reviewer checks without opening the image: the theme pin,
horizontal overflow, the focused element, any toast, and any dialog that
opened (a native `confirm()` or an in-page dialog). It drives the Chromium
already on the machine over the DevTools protocol, so there's nothing to
install. Set `CHROME` to the browser's path if it isn't on `PATH`.

```sh
node scripts/shots.mjs "$(scripts/scratch-app url)" '[
  {"path": "/home", "as": "admin@example.com", "theme": "dark", "width": 390},
  {"path": "/dimensions", "as": "admin@example.com", "width": 1280, "full": false,
   "action": "document.querySelector(\"form[action^=\\\"/dimension-values/\\\"][action$=\\\"/retire\\\"]\").requestSubmit()"}
]'
```

```
/tmp/goal-tracker-shots/02-dimensions-light-1280.png
  shot:     /dimensions as admin@example.com, OS light, 1280px, after the action
  page:     /dimensions (HTTP 200) "Dimensions · Goal Tracker"
  theme:    not pinned, follows the OS (light); body background rgb(246, 246, 243)
  overflow: none
  focus:    nothing (body)
  toast:    "Retired Platform from Team. Undo"
  dialog:   none
```

A shot has a `path` and can also set:

- `as`: the account to sign in as.
- `theme`: the OS colour scheme, `light` or `dark`.
- `pin`: a Theme menu choice to pin.
- `width` and `height`.
- `full`: `false` captures only the window.
- `action`: JavaScript to run in the page first. See below.
- `dialog`: `accept` or `dismiss` a native dialog. The default is `dismiss`.
- `status`: the HTTP status the page should end on, such as `404`.
- `name`: the PNG's file name, with or without `.png`.

An `action` is usually a script, such as `a(); b()` or the `requestSubmit()`
above, and the shot waits for any promise it ends on. An action that uses
`return` at its top level, or `await` in any form, including `await (x)`,
runs as the body of an async function instead:

```sh
node scripts/shots.mjs "$(scripts/scratch-app url)" '[
  {"path": "/goals/999999", "as": "admin@example.com", "status": 404},
  {"path": "/home", "as": "admin@example.com",
   "action": "const menu = document.querySelector(\"[data-testid=theme-menu]\"); if (!menu) return; menu.open = true; await new Promise(requestAnimationFrame)"}
]'
```

After the action, the script checks the HTTP status of the page's final
document. A shot without `status` fails if that is 400 or more, and a shot
with `status` fails if it's anything else. The page's htmx requests don't
count, and a status the script couldn't read, printed as `?`, never fails a
shot. A page with no title prints its title as `(none)`.

The list can be inline JSON, a file, or `-` for stdin. PNGs go to
`$TMPDIR/goal-tracker-shots` unless you pass `--out <dir>`. The script exits 1
if any shot fails to load, sign in, run its action, or end on the status it
expects. A shot that fails on its status still saves its PNG and prints its
facts. The script exits 2 if the arguments or a shot's fields are invalid.
Where the OS gives Chromium no sandbox, as in some containers, it says so and
runs Chromium without one.

`node --test scripts/*.test.mjs` (or `make test-scripts`, which also runs
`contrast.py`'s self-test) runs the tests of the parts that don't need
a browser: checking the shots, including the examples above, wrapping
actions, the status check, and the printed facts.

## `contrast.py`: WCAG contrast of the colour tokens

Reads the colour tokens from `internal/web/static/app.css` for the light theme
and both dark blocks. For each text token and each control-edge token, it
prints the contrast ratio on each surface, and marks with `!` any ratio under
4.5:1 for text or 3:1 for edges. It then checks every ratio that `DESIGN.md`'s
tables state.

```sh
scripts/contrast.py
```

```
light: each mark on each surface, ! under 4.5:1 for text or 3:1 for edges
                               canvas  surface  surface-hover  surface-alt  primary-light
--color-ink (text)             15.20    16.46          16.02        14.66          14.60
--color-ink-muted (text)        4.73     5.12           4.98         4.56           4.54
--color-border-strong (edge)    3.12     3.38           3.29         3.01           3.00
…
Every ratio meets its minimum.
DESIGN.md: all 82 ratios its tables state match.
```

It exits 1 if a ratio is under its minimum, if the two dark blocks differ, or
if `DESIGN.md` states a ratio the stylesheet no longer gives. In that last
case it prints the `DESIGN.md` line. Ratios stated in prose rather than
tables aren't checked. `python3 -B -m doctest scripts/contrast.py` runs its
self-test, as does `make test-scripts`.
