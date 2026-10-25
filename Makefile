.PHONY: build test test-quick lint generate generate-check check test-scripts e2e e2e-typecheck clean help seed serve serve-app authentik-up start stop restart password-link

# A bare `make` runs generate. Each target's `## ` comment is its line in
# `make help`.
.DEFAULT_GOAL := generate

# templ and sqlc are pinned as `tool` deps in go.mod and run via `go tool`, so
# the generator versions travel with the repo, not the developer's machine.
generate: ## Generate the templ and sqlc code
	go tool templ generate
	go tool sqlc generate

# What `generate` writes: templ's *_templ.go under internal/web, and sqlc's
# output in internal/db (one <area>.sql.go per query file, plus models.go and
# db.go, as sqlc.yaml configures).
GENERATED := 'internal/web/*_templ.go' 'internal/db/*.sql.go' internal/db/models.go internal/db/db.go

# Drift gate, run as a vetinari gate: regenerate, then fail if any generated
# path is modified or untracked — a hand-edited *_templ.go, a query out of sync
# with its .sql.go, or a new .templ or query file whose output was never
# committed. Uncommitted changes outside the generated paths are ignored. Also
# fails if go.mod or go.sum isn't tidy. --untracked-files=all so a personal
# status.showUntrackedFiles setting can't hide a missing file.
generate-check: generate ## Fail if generated code is stale or uncommitted, or go.mod isn't tidy
	@drift=$$(git status --porcelain --untracked-files=all -- $(GENERATED)); \
	if [ -n "$$drift" ]; then \
		echo "generated files are out of date or not committed:" >&2; \
		echo "$$drift" >&2; \
		exit 1; \
	fi
	go mod tidy -diff

# Every gate, in the order vetinari runs them; keep this in step with the gates
# in vetinari/config.mts. Make runs the prerequisites in order and stops at the
# first that fails. vetinari runs each gate separately, so each has a label.
check: generate-check lint test ## Run every gate: generate-check, lint, test

build: ## Build bin/goal-tracker
	go build -o bin/goal-tracker ./cmd/goal-tracker

# Nearly all of the race detector's cost is modernc's SQLite (ADR 0004), so two
# flags trim it without dropping -race from any package:
#
# -gcflags turns pointer checks off for modernc's packages alone. -race turns on
# -d=checkptr everywhere, and modernc's C translated to Go converts
# unsafe.Pointer constantly: checkptr was about half the CPU of a -race run.
# -race still checks modernc's memory accesses, and our own packages keep
# checkptr. On 48 cores, the suite took 3m29s with this flag against about
# 4m50s without it, and internal/web 198s against 288s (#525).
#
# -parallel caps how many of a package's tests run at once (the default is
# GOMAXPROCS). Every SQLite allocation in every parallel test takes modernc's
# one process-wide allocator lock, so past a few tests more of them only queue
# on it. The whole suite with the flag above, on 48 cores (#525): -parallel 2
# took 263s, 4 took 183s and 155s, 6 took 186s and 8 took 201s.
test: ## Run the Go tests with the race detector
	go test -race -count=1 -gcflags=modernc.org/...=-d=checkptr=0 -parallel 4 ./...

# The tests without the race detector, for a quick local loop. `make test`
# stays the gate.
test-quick: ## Run the Go tests without the race detector
	go test -count=1 ./...

# golangci-lint is pinned here and in vetinari/Dockerfile (CI installs none, so
# it takes the checksum-verified `go run` below): a newer release can enable new
# checks, and one built with an older Go than go.mod's refuses to run. It also has to read the
# export data go.mod's Go writes: version 5, from go1.27.2, needs
# golang.org/x/tools v0.52 or later, which golangci-lint bundles from v2.15.0,
# so move this and the Go pins together. A golangci-lint on PATH
# at this version runs as is; otherwise `go run` builds this version with the
# repo's Go, slowly the first time and from the build cache after.
GOLANGCI_LINT_VERSION := v2.15.0
GOLANGCI_LINT := $(shell [ "$$(golangci-lint version --short 2>/dev/null)" = "$(GOLANGCI_LINT_VERSION:v%=%)" ] && echo golangci-lint || echo go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION))

lint: ## Run golangci-lint (linters and gofmt)
	$(GOLANGCI_LINT) run

# The tests of the verification scripts in scripts/ (see scripts/README.md).
# Not a gate.
test-scripts: ## Run the scripts/ tests: shots.mjs and contrast.py's doctest
	node --test scripts/*.test.mjs
	python3 -B -m doctest scripts/contrast.py

# The Playwright end-to-end suite in e2e/ (see e2e/README.md): it builds the
# binaries, seeds template databases, and drives each test's own server in
# headless Chromium. A vetinari gate for changes that reach the browser (the
# e2e gate's `when` in vetinari/config.mts); `make check` doesn't run it.
# Needs outbound network to install Playwright and its npm packages; the pages
# themselves load nothing from other hosts.
# Runs `npm ci` in e2e/ when its dependencies are missing or older than its
# lockfile; with CHROME unset it also installs Playwright's Chromium. E2E_ARGS
# goes to `playwright test`, e.g. E2E_ARGS=smoke for one spec. Node >= 22.13
# (seedLookup uses node:sqlite, whose experimental warning is silenced).
E2E_ARGS ?=
e2e: e2e/node_modules/.package-lock.json ## Run the Playwright end-to-end suite in e2e/; a vetinari gate, not in check (CHROME, E2E_ARGS)
	@if [ -z "$$CHROME" ]; then cd e2e && npx playwright install chromium; fi
	cd e2e && NODE_OPTIONS=--disable-warning=ExperimentalWarning npx playwright test $(E2E_ARGS)

# Type-checks e2e/: `tsc -p .` over every .ts file in it, with the strict
# settings in e2e/tsconfig.json. Needs no Chromium and no Go. Not a gate:
# `make check` doesn't run it.
e2e-typecheck: e2e/node_modules/.package-lock.json ## Type-check e2e/ with tsc in strict mode; not a gate
	cd e2e && npm run typecheck

# npm ci writes node_modules/.package-lock.json, so this reruns it when the
# lockfile changes, such as a Playwright version bump.
e2e/node_modules/.package-lock.json: e2e/package-lock.json
	cd e2e && npm ci

# What `make build` and `make restart` leave behind. Never a database.
clean: ## Remove bin/ and serve.log
	rm -rf bin
	rm -f serve.log

help: ## List the targets
	@awk 'BEGIN { FS = ":[^#]*## " } /^[a-zA-Z0-9_-]+:[^#]*## / { printf "  %-15s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

# Fill a fresh database with a fake org for demos (see README). SEED_DB defaults
# to the server's default database; the seed refuses one that already has Goals.
SEED_DB ?= goal-tracker.db
seed: ## Fill a fresh database with a fake org (SEED_DB)
	go run ./cmd/seed -db $(SEED_DB)

# `make start` records how it started the app here, so `make stop` and
# `make restart` act on the same port, database and sign-in. Only they read it;
# `make start` and `make serve` take their settings fresh. Git-ignored.
SERVE_STATE := .serve-state.mk
ifneq ($(filter stop restart password-link,$(MAKECMDGOALS)),)
-include $(SERVE_STATE)
endif

# Serve the app on the tailnet: the server listens on localhost only and
# `tailscale serve` proxies https://<this machine>.<tailnet>.ts.net:$(SERVE_PORT)
# to it, so emailed links point at the tailnet URL. The proxy outlives the
# server; `tailscale serve --https=$(SERVE_PORT) off` removes it.
#
# People sign in through the local Authentik in dev/authentik/ (see its README;
# it needs dev/authentik/.env, GOAL_TRACKER_SESSION_KEY included), single
# sign-on as the org would have it (GOAL_TRACKER_SIGNIN=oidc), and the app
# syncs people and their Managers from its directory.
# SIGNIN picks how people sign in: oidc (the default, Authentik), password
# (GOAL_TRACKER_SIGNIN=password: passwords set through one-time links, with the
# session key from dev/authentik/.env; `make password-link EMAIL=<email>` prints
# one for the app `make start` started), or dev. NO_SSO=1 is SIGNIN=dev.
# NO_SSO=1 uses the development email form instead (GOAL_TRACKER_SIGNIN=dev),
# and leaves Authentik be. SERVE_DIRECTORY_FILE=<csv> syncs people and their
# Managers from that file instead of Authentik's directory
# (GOAL_TRACKER_DIRECTORY_FILE); dev/directory/people.csv is the Authentik
# blueprint's people, exported. Authentik is started, told the app's tailnet callback, and put on the
# tailnet at https://<this machine>.<tailnet>.ts.net:$(AUTHENTIK_PORT), so
# sign-in works from any device on the tailnet.
SERVE_PORT ?= 8090
SERVE_DB ?= goal-tracker.db
SERVE_ADMINS ?= admin@example.com
SERVE_LOG ?= serve.log
SERVE_DIRECTORY_FILE ?=
NO_SSO ?= 0
SIGNIN ?= $(if $(filter 1,$(NO_SSO)),dev,oidc)
ifneq ($(filter-out oidc password dev,$(SIGNIN)),)
$(error SIGNIN must be oidc, password or dev, not $(SIGNIN))
endif
SSO = $(if $(filter oidc,$(SIGNIN)),1,0)
AUTHENTIK_PORT ?= 9443
TAILNET_HOST = $$(tailscale status --json | jq -r '.Self.DNSName | rtrimstr(".")')
WITH_AUTHENTIK = $(if $(filter 1,$(SSO)),authentik-up)

# env_value name: the value of name in dev/authentik/.env.
env_value = $$(sed -n 's/^$(1)=//p' dev/authentik/.env)

# Stops the recipe unless dev/authentik/.env has the session key that
# GOAL_TRACKER_SIGNIN=oidc needs.
CHECK_SESSION_KEY = test -n "$(call env_value,GOAL_TRACKER_SESSION_KEY)" || { echo "dev/authentik/.env has no GOAL_TRACKER_SESSION_KEY: add one, e.g. GOAL_TRACKER_SESSION_KEY=\$$(openssl rand -base64 32), or pass NO_SSO=1 for the development sign-in form" >&2; exit 1; }

serve: build $(WITH_AUTHENTIK) serve-app ## Build and serve the app on the tailnet with Authentik sign-in (SERVE_PORT, SERVE_DB, SERVE_ADMINS, NO_SSO=1)

# The server itself, in the foreground, for `make serve` and `make start`.
serve-app:
	$(if $(filter oidc password,$(SIGNIN)),@$(CHECK_SESSION_KEY))
	tailscale serve --bg --https=$(SERVE_PORT) http://127.0.0.1:$(SERVE_PORT)
	host=$(TAILNET_HOST) && \
	GOAL_TRACKER_ADDR=127.0.0.1:$(SERVE_PORT) \
	GOAL_TRACKER_DB=$(SERVE_DB) \
	GOAL_TRACKER_ADMINS=$(SERVE_ADMINS) \
	GOAL_TRACKER_BASE_URL=https://$$host:$(SERVE_PORT) \
	$(if $(filter 1,$(SSO)),\
	GOAL_TRACKER_SIGNIN=oidc \
	GOAL_TRACKER_SESSION_KEY=$(call env_value,GOAL_TRACKER_SESSION_KEY) \
	GOAL_TRACKER_OIDC_ISSUER=https://$$host:$(AUTHENTIK_PORT)/application/o/goal-tracker/ \
	GOAL_TRACKER_OIDC_CLIENT_ID=goal-tracker \
	GOAL_TRACKER_OIDC_CLIENT_SECRET=$(call env_value,GOAL_TRACKER_OIDC_CLIENT_SECRET) \
	$(if $(SERVE_DIRECTORY_FILE),,\
	GOAL_TRACKER_DIRECTORY_URL=http://127.0.0.1:9000 \
	GOAL_TRACKER_DIRECTORY_TOKEN=$(call env_value,GOAL_TRACKER_DIRECTORY_TOKEN) \
	)\
	,\
	GOAL_TRACKER_SIGNIN=$(SIGNIN) \
	$(if $(filter password,$(SIGNIN)),GOAL_TRACKER_SESSION_KEY=$(call env_value,GOAL_TRACKER_SESSION_KEY)) \
	) \
	$(if $(SERVE_DIRECTORY_FILE),GOAL_TRACKER_DIRECTORY_FILE=$(abspath $(SERVE_DIRECTORY_FILE))) \
	exec bin/goal-tracker

# Start the local Authentik knowing the app's tailnet address (for its sign-in
# callback, its sign-out return, and the app tile), and put it on the tailnet. apply.sh re-applies the blueprint, since Authentik doesn't
# notice a changed callback by itself.
authentik-up:
	@test -f dev/authentik/.env || { echo "dev/authentik/.env is missing: copy dev/authentik/.env.example and fill it in (see dev/authentik/README.md), or pass NO_SSO=1 for the development sign-in form" >&2; exit 1; }
	@$(CHECK_SESSION_KEY)
	host=$(TAILNET_HOST) && cd dev/authentik && \
	GOAL_TRACKER_APP_URL=https://$$host:$(SERVE_PORT) docker compose up -d && \
	./apply.sh
	tailscale serve --bg --https=$(AUTHENTIK_PORT) http://127.0.0.1:9000

# stop_server port: stop whatever serves 127.0.0.1:port (only the local
# listener, never tailscaled's proxy on the tailnet address).
define stop_server
	pid=$$(ss -ltnpH "sport = :$(1)" src 127.0.0.1 | grep -o 'pid=[0-9]*' | cut -d= -f2 | head -1); \
	if [ -n "$$pid" ]; then \
		echo "stopping goal-tracker on :$(1) (pid $$pid)"; kill $$pid; \
		for i in $$(seq 50); do kill -0 $$pid 2>/dev/null || break; sleep 0.1; done; \
		if kill -0 $$pid 2>/dev/null; then echo "pid $$pid did not stop" >&2; exit 1; fi; \
	fi
endef

# Build and start the app in the background, logging to $(SERVE_LOG), first
# stopping the one a previous `make start` left running. Starting with NO_SSO=1
# after a start without it also stops Authentik.
start: build $(WITH_AUTHENTIK) ## Build and start the app on the tailnet in the background, with Authentik sign-in (SERVE_*, SERVE_DIRECTORY_FILE, SIGNIN=password|dev, NO_SSO=1)
	@if [ -f $(SERVE_STATE) ]; then \
		old=$$(sed -n 's/^SERVE_PORT := //p' $(SERVE_STATE)); \
		if [ -n "$$old" ] && [ "$$old" != "$(SERVE_PORT)" ]; then $(call stop_server,$$old); fi; \
		if grep -qx 'NO_SSO := 0' $(SERVE_STATE) && [ "$(SSO)" != 1 ]; then \
			echo "stopping Authentik"; (cd dev/authentik && docker compose stop); fi; \
	fi
	@$(call stop_server,$(SERVE_PORT))
	@printf 'SERVE_PORT := %s\nSERVE_DB := %s\nSERVE_ADMINS := %s\nSERVE_LOG := %s\nSERVE_DIRECTORY_FILE := %s\nSIGNIN := %s\nNO_SSO := %s\n' \
		'$(SERVE_PORT)' '$(SERVE_DB)' '$(SERVE_ADMINS)' '$(SERVE_LOG)' '$(SERVE_DIRECTORY_FILE)' '$(SIGNIN)' '$(if $(filter 1,$(SSO)),0,1)' >$(SERVE_STATE)
	setsid nohup $(MAKE) serve-app SERVE_PORT=$(SERVE_PORT) SERVE_DB=$(SERVE_DB) SERVE_ADMINS=$(SERVE_ADMINS) SERVE_DIRECTORY_FILE=$(SERVE_DIRECTORY_FILE) SIGNIN=$(SIGNIN) NO_SSO=$(NO_SSO) AUTHENTIK_PORT=$(AUTHENTIK_PORT) >$(SERVE_LOG) 2>&1 </dev/null &
	@for i in $$(seq 100); do \
		if ss -ltnH 'sport = :$(SERVE_PORT)' src 127.0.0.1 | grep -q .; then \
			echo "goal-tracker is serving on :$(SERVE_PORT)$(if $(filter oidc,$(SIGNIN)), with Authentik sign-in,$(if $(filter password,$(SIGNIN)), with password sign-in (make password-link EMAIL=<email>), with the development sign-in form)) (log: $(SERVE_LOG))"; exit 0; fi; \
		sleep 0.1; done; \
	echo "goal-tracker did not come up; see $(SERVE_LOG)" >&2; tail -20 $(SERVE_LOG) >&2; exit 1

# Stop what `make start` started: the app, and Authentik if it started with
# Authentik sign-in (its data is kept). The tailnet proxies stay, as with serve.
stop: ## Stop the app `make start` started, and its Authentik
	@$(call stop_server,$(SERVE_PORT))
	@if [ -f $(SERVE_STATE) ] && [ "$(SSO)" = 1 ]; then echo "stopping Authentik"; cd dev/authentik && docker compose stop; fi

# Rebuild and start the app again as the last `make start` did; any setting
# given here replaces the recorded one.
restart: ## Rebuild and restart the app as the last `make start` did
	@$(MAKE) --no-print-directory start SERVE_PORT=$(SERVE_PORT) SERVE_DB=$(SERVE_DB) SERVE_ADMINS=$(SERVE_ADMINS) SERVE_LOG=$(SERVE_LOG) SERVE_DIRECTORY_FILE=$(SERVE_DIRECTORY_FILE) SIGNIN=$(SIGNIN) NO_SSO=$(NO_SSO) AUTHENTIK_PORT=$(AUTHENTIK_PORT)

# Print a one-time set-password link for EMAIL against the app `make start`
# started with SIGNIN=password: its database, Admins and tailnet address. It is
# how the first Admin gets a password; Admins issue the rest on the Admin page.
password-link: build ## Print a set-password link for EMAIL for the app `make start` started with SIGNIN=password
	@test -n "$(EMAIL)" || { echo "usage: make password-link EMAIL=<email>" >&2; exit 1; }
	@test "$(SIGNIN)" = password || { echo "the app wasn't started with SIGNIN=password; run make start SIGNIN=password first" >&2; exit 1; }
	@$(CHECK_SESSION_KEY)
	@host=$(TAILNET_HOST) && \
	GOAL_TRACKER_DB=$(SERVE_DB) \
	GOAL_TRACKER_ADMINS=$(SERVE_ADMINS) \
	GOAL_TRACKER_BASE_URL=https://$$host:$(SERVE_PORT) \
	GOAL_TRACKER_SIGNIN=password \
	GOAL_TRACKER_SESSION_KEY=$(call env_value,GOAL_TRACKER_SESSION_KEY) \
	bin/goal-tracker password-link $(EMAIL)
