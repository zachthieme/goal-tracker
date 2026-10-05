.PHONY: build test test-quick lint generate generate-check check test-scripts e2e clean help seed serve serve-app authentik-up start stop restart

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

test: ## Run the Go tests with the race detector
	go test -race -count=1 ./...

# The tests without the race detector, for a quick local loop. `make test`
# stays the gate.
test-quick: ## Run the Go tests without the race detector
	go test -count=1 ./...

lint: ## Run golangci-lint
	golangci-lint run

# The tests of the verification scripts in scripts/ (see scripts/README.md).
# Not a gate.
test-scripts: ## Run the scripts/ tests: shots.mjs and contrast.py's doctest
	node --test scripts/*.test.mjs
	python3 -B -m doctest scripts/contrast.py

# The Playwright end-to-end suite in e2e/ (see e2e/README.md): it builds the
# binaries, seeds template databases, and drives each test's own server in
# headless Chromium. Not a gate, and needs outbound network (htmx and fonts
# load from CDNs). Runs `npm ci` in e2e/ when its dependencies are missing or
# older than its lockfile; with CHROME unset it also installs Playwright's
# Chromium. E2E_ARGS goes to `playwright test`, e.g. E2E_ARGS=smoke for one
# spec. Node >= 22.13 (seedLookup uses node:sqlite, whose experimental warning
# is silenced).
E2E_ARGS ?=
e2e: e2e/node_modules/.package-lock.json ## Run the Playwright end-to-end suite in e2e/; not a gate (CHROME, E2E_ARGS)
	@if [ -z "$$CHROME" ]; then cd e2e && npx playwright install chromium; fi
	cd e2e && NODE_OPTIONS=--disable-warning=ExperimentalWarning npx playwright test $(E2E_ARGS)

# npm ci writes node_modules/.package-lock.json, so this reruns it when the
# lockfile changes, such as a Playwright version bump.
e2e/node_modules/.package-lock.json: e2e/package-lock.json
	cd e2e && npm ci

# What `make build` and `make restart` leave behind. Never a database.
clean: ## Remove bin/ and serve.log
	rm -rf bin
	rm -f serve.log

help: ## List the targets
	@awk 'BEGIN { FS = ":[^#]*## " } /^[a-zA-Z_-]+:[^#]*## / { printf "  %-15s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

# Fill a fresh database with a fake org for demos (see README). SEED_DB defaults
# to the server's default database; the seed refuses one that already has Goals.
SEED_DB ?= goal-tracker.db
seed: ## Fill a fresh database with a fake org (SEED_DB)
	go run ./cmd/seed -db $(SEED_DB)

# `make start` records how it started the app here, so `make stop` and
# `make restart` act on the same port, database and sign-in. Only they read it;
# `make start` and `make serve` take their settings fresh. Git-ignored.
SERVE_STATE := .serve-state.mk
ifneq ($(filter stop restart,$(MAKECMDGOALS)),)
-include $(SERVE_STATE)
endif

# Serve the app on the tailnet: the server listens on localhost only and
# `tailscale serve` proxies https://<this machine>.<tailnet>.ts.net:$(SERVE_PORT)
# to it, so emailed links point at the tailnet URL. The proxy outlives the
# server; `tailscale serve --https=$(SERVE_PORT) off` removes it.
#
# People sign in through the local Authentik in dev/authentik/ (see its README;
# it needs dev/authentik/.env), single sign-on as the org would have it.
# NO_SSO=1 uses the development email form instead, and leaves Authentik be. Authentik is started, told the app's tailnet callback, and put on the
# tailnet at https://<this machine>.<tailnet>.ts.net:$(AUTHENTIK_PORT), so
# sign-in works from any device on the tailnet.
SERVE_PORT ?= 8090
SERVE_DB ?= goal-tracker.db
SERVE_ADMINS ?= admin@example.com
SERVE_LOG ?= serve.log
NO_SSO ?= 0
SSO = $(if $(filter 1,$(NO_SSO)),0,1)
AUTHENTIK_PORT ?= 9443
TAILNET_HOST = $$(tailscale status --json | jq -r '.Self.DNSName | rtrimstr(".")')
WITH_AUTHENTIK = $(if $(filter 1,$(SSO)),authentik-up)

serve: build $(WITH_AUTHENTIK) serve-app ## Build and serve the app on the tailnet with Authentik sign-in (SERVE_PORT, SERVE_DB, SERVE_ADMINS, NO_SSO=1)

# The server itself, in the foreground, for `make serve` and `make start`.
serve-app:
	tailscale serve --bg --https=$(SERVE_PORT) http://127.0.0.1:$(SERVE_PORT)
	host=$(TAILNET_HOST) && \
	GOAL_TRACKER_ADDR=127.0.0.1:$(SERVE_PORT) \
	GOAL_TRACKER_DB=$(SERVE_DB) \
	GOAL_TRACKER_ADMINS=$(SERVE_ADMINS) \
	GOAL_TRACKER_BASE_URL=https://$$host:$(SERVE_PORT) \
	$(if $(filter 1,$(SSO)),\
	GOAL_TRACKER_OIDC_ISSUER=https://$$host:$(AUTHENTIK_PORT)/application/o/goal-tracker/ \
	GOAL_TRACKER_OIDC_CLIENT_ID=goal-tracker \
	GOAL_TRACKER_OIDC_CLIENT_SECRET=$$(sed -n 's/^GOAL_TRACKER_OIDC_CLIENT_SECRET=//p' dev/authentik/.env) \
	) \
	exec bin/goal-tracker

# Start the local Authentik knowing the app's tailnet address (for its sign-in
# callback, its sign-out return, and the app tile), and put it on the tailnet. apply.sh re-applies the blueprint, since Authentik doesn't
# notice a changed callback by itself.
authentik-up:
	@test -f dev/authentik/.env || { echo "dev/authentik/.env is missing: copy dev/authentik/.env.example and fill it in (see dev/authentik/README.md), or pass NO_SSO=1 for the development sign-in form" >&2; exit 1; }
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
start: build $(WITH_AUTHENTIK) ## Build and start the app on the tailnet in the background, with Authentik sign-in (SERVE_*, NO_SSO=1)
	@if [ -f $(SERVE_STATE) ]; then \
		old=$$(sed -n 's/^SERVE_PORT := //p' $(SERVE_STATE)); \
		if [ -n "$$old" ] && [ "$$old" != "$(SERVE_PORT)" ]; then $(call stop_server,$$old); fi; \
		if grep -qx 'NO_SSO := 0' $(SERVE_STATE) && [ "$(SSO)" != 1 ]; then \
			echo "stopping Authentik"; (cd dev/authentik && docker compose stop); fi; \
	fi
	@$(call stop_server,$(SERVE_PORT))
	@printf 'SERVE_PORT := %s\nSERVE_DB := %s\nSERVE_ADMINS := %s\nSERVE_LOG := %s\nNO_SSO := %s\n' \
		'$(SERVE_PORT)' '$(SERVE_DB)' '$(SERVE_ADMINS)' '$(SERVE_LOG)' '$(if $(filter 1,$(SSO)),0,1)' >$(SERVE_STATE)
	setsid nohup $(MAKE) serve-app SERVE_PORT=$(SERVE_PORT) SERVE_DB=$(SERVE_DB) SERVE_ADMINS=$(SERVE_ADMINS) NO_SSO=$(NO_SSO) AUTHENTIK_PORT=$(AUTHENTIK_PORT) >$(SERVE_LOG) 2>&1 </dev/null &
	@for i in $$(seq 100); do \
		if ss -ltnH 'sport = :$(SERVE_PORT)' src 127.0.0.1 | grep -q .; then \
			echo "goal-tracker is serving on :$(SERVE_PORT)$(if $(filter 1,$(SSO)), with Authentik sign-in, with the development sign-in form) (log: $(SERVE_LOG))"; exit 0; fi; \
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
	@$(MAKE) --no-print-directory start SERVE_PORT=$(SERVE_PORT) SERVE_DB=$(SERVE_DB) SERVE_ADMINS=$(SERVE_ADMINS) SERVE_LOG=$(SERVE_LOG) NO_SSO=$(NO_SSO) AUTHENTIK_PORT=$(AUTHENTIK_PORT)
