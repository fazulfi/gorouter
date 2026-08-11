# P5-T12 performance measurement procedure

Run from the repository root with a pinned Go toolchain:

```bash
go test ./tests/performance/... -count=1 -run 'TestRouterOverheadP95|TestConcurrentStreams|TestLightweightRPM|TestStreamingCancellation|TestRouterOverheadIsMeasuredWithoutProvider' -timeout 30m -args -perf-iterations=200 -perf-concurrency=16 -perf-output=performance-result.json
```

The authoritative run specification is `tests/performance/lab-manifest.json`. It is the single source for reference-host metadata, exact executable commands, load parameters, thresholds, pass criteria, evidence files, and owners for every deferred gate: router p95, 2,000 concurrent streams, 100,000 RPM lightweight endpoints, PostgreSQL-backed latency/pool behavior, and provider-latency separation. The manifest deliberately marks the latter two as pending until their attached CI/lab scenarios provide the required measurements; it does not fabricate results. Run each listed command from the repository root with `PERF_ENFORCE_THRESHOLDS=1` and retain the specified sanitized outputs.

For the authoritative reference run, use the same command on the reference host (4 vCPU / 16 GB per the Phase 5 plan), increasing only the bounded flags as approved by the lab operator. Set `PERF_ENFORCE_THRESHOLDS=1` to fail closed against the plan gates: router-overhead p95 <20 ms, concurrent streams >=2,000, and lightweight throughput >=100,000 RPM. The JSON bundle records command, Go/runtime metadata, architecture, CPU count, threshold mode, and measurements. It contains no credentials or request payloads.

The local harness deliberately measures router-only handlers and uses `httptest`; PostgreSQL-backed latency, provider latency, and 2,000-stream lab capacity are not inferred from it. Run PostgreSQL and provider scenarios only in the CI/lab environment with the exact command captured in the result bundle.

## Re-baseline (user-approved)

Per P5-T12 authority gap resolution, the reference environment was user-approved as host `143.198.86.164` (Ubuntu 24.04, 4 CPU / 7.8 GiB RAM / 153 GiB disk). The plan target baseline remains 4 vCPU / 16 GiB; the variance (16 GiB vs 7.8 GiB) is documented in `tests/performance/lab-manifest.json`. All original performance thresholds are retained (router p95 < 20 ms, concurrent streams >= 2000, lightweight throughput >= 100k RPM). Authoritative measurements on this host remain pending; no results were fabricated. See `lab-manifest.json` for the complete rebaseline block and `phase5-t12-rebaseline.md` for the full re-baseline evidence report.
