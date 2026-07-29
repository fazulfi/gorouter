.PHONY: format vet test test-full test-race frontend-install frontend-build frontend-test frontend-typecheck gate build all

# ── Go targets ──────────────────────────────────────────────

format:
	go fmt ./...

vet:
	go vet ./...

test:
	go test -short -count=1 ./...

test-full:
	go test -count=1 ./...

test-race:
	go test -race -short -count=1 ./...

build:
	go build ./cmd/gorouter/...

# ── Frontend targets ────────────────────────────────────────

frontend-install:
	cd frontend && npm ci

frontend-build:
	cd frontend && npm run build

frontend-test:
	cd frontend && npm test

frontend-typecheck:
	cd frontend && npm run typecheck

# ── Governance / gate target ────────────────────────────────

gate:
	go run ./tools/gate/verify.go

# ── Meta targets ────────────────────────────────────────────

all: format vet test frontend-install frontend-build frontend-test gate
