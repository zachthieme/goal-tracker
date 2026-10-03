.PHONY: build test test-quick lint generate generate-check check test-scripts clean help seed serve restart

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

# Serve the app on the tailnet: the server listens on localhost only and
# `tailscale serve` proxies https://<this machine>.<tailnet>.ts.net:$(SERVE_PORT)
# to it, so emailed links point at the tailnet URL. The proxy outlives the
# server; `tailscale serve --https=$(SERVE_PORT) off` removes it.
SERVE_PORT ?= 8090
SERVE_DB ?= goal-tracker.db
SERVE_ADMINS ?= admin@example.com
serve: build ## Build and serve the app on the tailnet (SERVE_PORT, SERVE_DB, SERVE_ADMINS)
	tailscale serve --bg --https=$(SERVE_PORT) http://127.0.0.1:$(SERVE_PORT)
	host=$$(tailscale status --json | jq -r '.Self.DNSName | rtrimstr(".")') && \
	GOAL_TRACKER_ADDR=127.0.0.1:$(SERVE_PORT) \
	GOAL_TRACKER_DB=$(SERVE_DB) \
	GOAL_TRACKER_ADMINS=$(SERVE_ADMINS) \
	GOAL_TRACKER_BASE_URL=https://$$host:$(SERVE_PORT) \
	exec bin/goal-tracker

# Rebuild and restart the tailnet server in the background: stop whatever
# serves 127.0.0.1:$(SERVE_PORT) (only the local listener, never tailscaled's
# proxy on the tailnet address), then run `make serve` detached, logging to
# $(SERVE_LOG). Takes the same SERVE_* overrides as `make serve`.
SERVE_LOG ?= serve.log
restart: build ## Rebuild and restart the tailnet server in the background, logging to SERVE_LOG
	@pid=$$(ss -ltnpH 'sport = :$(SERVE_PORT)' src 127.0.0.1 | grep -o 'pid=[0-9]*' | cut -d= -f2 | head -1); \
	if [ -n "$$pid" ]; then \
		echo "stopping goal-tracker (pid $$pid)"; kill $$pid; \
		for i in $$(seq 50); do kill -0 $$pid 2>/dev/null || break; sleep 0.1; done; \
		if kill -0 $$pid 2>/dev/null; then echo "pid $$pid did not stop" >&2; exit 1; fi; \
	fi
	setsid nohup $(MAKE) serve SERVE_PORT=$(SERVE_PORT) SERVE_DB=$(SERVE_DB) SERVE_ADMINS=$(SERVE_ADMINS) >$(SERVE_LOG) 2>&1 </dev/null &
	@for i in $$(seq 100); do \
		if ss -ltnH 'sport = :$(SERVE_PORT)' src 127.0.0.1 | grep -q .; then \
			echo "goal-tracker is serving on :$(SERVE_PORT) (log: $(SERVE_LOG))"; exit 0; fi; \
		sleep 0.1; done; \
	echo "goal-tracker did not come up; see $(SERVE_LOG)" >&2; tail -20 $(SERVE_LOG) >&2; exit 1
