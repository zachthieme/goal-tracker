.PHONY: build test lint generate generate-check seed serve

# templ and sqlc are pinned as `tool` deps in go.mod and run via `go tool`, so
# the generator versions travel with the repo, not the developer's machine.
generate:
	go tool templ generate
	go tool sqlc generate

# Drift gate: regenerate, then fail if anything changed. Run in CI and as a
# vetinari gate so stale generated code (a hand-edited *_templ.go, a query.sql
# out of sync with query.sql.go) parks instead of merging.
generate-check: generate
	git diff --exit-code

build:
	go build -o bin/goal-tracker ./cmd/goal-tracker

test:
	go test -race -count=1 ./...

lint:
	golangci-lint run

# Fill a fresh database with a fake org for demos (see README). SEED_DB defaults
# to the server's default database; the seed refuses one that already has Goals.
SEED_DB ?= goal-tracker.db
seed:
	go run ./cmd/seed -db $(SEED_DB)

# Serve the app on the tailnet: the server listens on localhost only and
# `tailscale serve` proxies https://<this machine>.<tailnet>.ts.net:$(SERVE_PORT)
# to it, so emailed links point at the tailnet URL. The proxy outlives the
# server; `tailscale serve --https=$(SERVE_PORT) off` removes it.
SERVE_PORT ?= 8090
SERVE_DB ?= goal-tracker.db
SERVE_ADMINS ?= admin@example.com
serve: build
	tailscale serve --bg --https=$(SERVE_PORT) http://127.0.0.1:$(SERVE_PORT)
	host=$$(tailscale status --json | jq -r '.Self.DNSName | rtrimstr(".")') && \
	GOAL_TRACKER_ADDR=127.0.0.1:$(SERVE_PORT) \
	GOAL_TRACKER_DB=$(SERVE_DB) \
	GOAL_TRACKER_ADMINS=$(SERVE_ADMINS) \
	GOAL_TRACKER_BASE_URL=https://$$host:$(SERVE_PORT) \
	exec bin/goal-tracker
