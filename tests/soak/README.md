# P5-T13 Soak Harness and Evidence Framework

## Scope

This directory implements the **P5-T13 72-hour mixed-load soak harness** per the Phase 5 implementation plan (§4.13). It includes:

- Mixed-load generator (router HTTP + SSE streaming + provider simulation + auth/API mix)
- Configurable duration via `-soak-duration` flag (default small for unit verification)
- Periodic snapshot collection (latency percentiles, error counts, goroutine count, heap)
- Fail-closed on config errors
- Injectable clock/time support for deterministic testing without waiting 72 hours
- Signed trend evidence framework with HMAC-SHA256 signing envelope

## Files Created

| File | Purpose |
|------|---------|
| `harness.go` | Core soak harness with mixed-load generator |
| `snapshot.go` | Snapshot collector for periodic trend points |
| `signed.go` | Signed evidence envelope format and verification |
| `manifest.go` | Lab manifest definition and read/write |
| `bundle.go` | Bundle type for test output serialization |
| `soak_test.go` | Main soak scenario tests (RED→GREEN verified) |
| `harness_test.go` | Harness lifecycle and validation unit tests |
| `snapshot_test.go` | Snapshot collection unit tests |
| `signed_test.go` | Signed envelope round-trip tests with TEST KEY |
| `manifest_test.go` | Manifest read/write unit tests |
| `lab-manifest.json` | Authoritative reference-environment spec (pending run) |
| `README.md` | This file |

## RED→GREEN Verification

**RED Observation (compiled failure):** Initial state had no harness (`Config`, `NewHarness`, `Harness` undefined), tests failed to compile with `undefined: Config`.

**VERBATIM RED:**
```
gorouter/tests/soak [build failed]
tests\soak\harness_test.go:9:9: undefined: Config
tests\soak\harness_test.go:46:15: undefined: NewHarness
...
FAIL	gorouter/tests/soak [build failed]
FAIL
RED_EXIT=1
```

**GREEN Observation:** After implementation, all short tests pass with coverage ≥80%.

**VERBATIM GREEN:**
```
ok  	gorouter/tests/soak	24.598s	coverage: 95.1% of statements
PASS
GREEN_SHORT_EXIT=0
```

## Usage

### Smoke Run (local, bounded)

Run a smoke proof with minimal duration to prove end-to-end functionality:

```bash
go test ./tests/soak/... -run TestSoakSmokeRun -args -soak-duration=5s -soak-sample-interval=1s -soak-concurrency=4 -soak-output=artifacts/local-smoke.json
```

Output is written to `artifacts/local-smoke.json` containing trend points captured during the run.

### Full 72-hour Soak (release lab / VPS stage - DEFERRED)

The authoritative 72-hour run command:

```bash
go test ./tests/soak/... -run TestSoakSmokeRun -args -soak-duration=72h -soak-sample-interval=5m -soak-concurrency=256 -soak-fail-closed=true -soak-provider-count=10 -soak-auth-ratio=0.15 -soak-output=artifacts/p5-t13/trend.json
```

**NOTES:**
- Threshold enforcement requires `SOAK_ENFORCE_THRESHOLDS=1` environment variable
- The actual run is DEFERRED to the release-lab/VPS stage (T14/T16 window) per task instructions
- No fabricated results were claimed; evidence shows `status: "pending_authoritative_run"`

## Coverage

Coverage on owned packages at `95.1%` exceeds the 80% requirement. Per-package breakdown:

- `harness.go`: 87.5–100.0% (all core functions)
- `snapshot.go`: 100.0% (collector lifecycle and collection)
- `signed.go`: 71.4–100.0% (envelope sign/verify round-trip)
- `manifest.go`: 85.7–88.9% (read/write operations)
- All test files: 87.5–100.0% coverage on assertions

## Signed Evidence Framework

Evidence will be signed using HMAC-SHA256 with a release key (provisioned at T14/T16). Unit tests demonstrate round-trip verification with a TEST KEY ONLY:

```go
func TestSignedEnvelopeRoundTrip(t *testing.T) {
    if err := RoundTripVerify("test-sha"); err != nil {
        t.Fatalf("round trip failed: %v", err)
    }
}
```

The `SignedEnvelope` structure includes:
- `Content`: Trend point JSON bundle
- `Metadata`: Candidate SHA, timestamps, sample interval, thresholds, status
- `Signature`: HMAC-SHA256 over content bytes

## Deferral Statement

The authoritative 72-hour soak is **explicitly DEFERRED** to the release-lab/VPS stage per task instructions. No results were fabricated. The `lab-manifest.json` documents `status: "pending_authoritative_run"` and contains the exact commands to run when capacity is allocated.

## Authoritative Candidates

Current HEAD commit SHA: `c1532fcd6268bfdd7b419673e32e4c34fcd8ac7e`

The signed envelope will record this exact SHA as the candidate being tested.

---

END OF REPORT
