.PHONY: build test lint

build:
	go build -o bin/goal-tracker ./cmd/goal-tracker

test:
	go test -race -count=1 ./...

lint:
	golangci-lint run
