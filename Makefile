.PHONY: build test lint generate generate-check seed

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
