BINARY ?= opsmask

.PHONY: build test lint fmt fuzz bench

build:
	go build -o bin/$(BINARY) ./cmd/opsmask

test:
	go test ./...

lint:
	go vet ./...
	@command -v staticcheck >/dev/null && staticcheck ./... || echo "staticcheck not installed; skipped"
	@command -v govulncheck >/dev/null && govulncheck ./... || echo "govulncheck not installed; skipped"
	@command -v gosec >/dev/null && gosec ./... || echo "gosec not installed; skipped"

fmt:
	gofmt -w cmd internal skill

fuzz:
	go test ./... -fuzz=Fuzz -fuzztime=10s

bench:
	go test ./internal/engine -bench=. -benchmem
