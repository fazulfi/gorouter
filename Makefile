.PHONY: format vet test test-full test-race frontend-install frontend-build frontend-test frontend-typecheck gate build all \
        coverage coverage-threshold sbom license-check secret-scan security-scan

GO ?= go
COVERAGE_THRESHOLD ?= 80.0

# ── Go targets ──────────────────────────────────────────────

format:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

test:
	$(GO) test -short -count=1 ./...

test-full:
	$(GO) test -count=1 ./...

test-race:
	$(GO) test -race -short -count=1 ./...

build:
	$(GO) build ./cmd/gorouter/...

# ── Coverage ────────────────────────────────────────────────

coverage:
	$(GO) test -short -count=1 -coverprofile=coverage.out -covermode=atomic ./...
	$(GO) tool cover -func=coverage.out

coverage-threshold:
	@total=$$($(GO) tool cover -func=coverage.out 2>/dev/null | grep '^total:' | awk '{print $$$$NF}' | sed 's/%//'); \
	if [ -z "$$total" ]; then \
		$(GO) test -short -count=1 -coverprofile=coverage.out -covermode=atomic ./... > /dev/null 2>&1; \
		total=$$($(GO) tool cover -func=coverage.out | grep '^total:' | awk '{print $$$$NF}' | sed 's/%//'); \
	fi; \
	echo "Total coverage: $$total%"; \
	if [ "$$(echo "$$total < $(COVERAGE_THRESHOLD)" | bc -l)" -eq 1 ]; then \
		echo "ERROR: coverage $$total% is below threshold $(COVERAGE_THRESHOLD)%"; \
		exit 1; \
	fi; \
	echo "Coverage $$total% meets threshold $(COVERAGE_THRESHOLD)%"

# ── Supply chain ────────────────────────────────────────────

sbom:
	cyclonedx-gomod mod -licenses -json -output gorouter.sbom.json .

license-check:
	go-licenses check ./... 2>&1; \
	go-licenses csv ./... > license-report.csv 2>&1; \
	echo "License report written to license-report.csv"

secret-scan:
	gitleaks detect --source . --verbose --no-git

security-scan:
	$(MAKE) gosec
	$(MAKE) govulncheck

gosec:
	gosec -fmt sarif -out gosec-results.sarif ./...

govulncheck:
	govulncheck ./...

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
	$(GO) run ./tools/gate/verify.go

# ── Meta targets ────────────────────────────────────────────

all: format vet test frontend-install frontend-build frontend-test gate
