# Phase 5 Release Readiness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the parity-complete product into signed, installable, recoverable, secure, accessible, performant, provider-validated beta and stable releases across all approved platforms and installation methods.

**Architecture:** Harden the single-binary modular monolith and its managed PostgreSQL lifecycle through platform-specific packaging adapters, reproducible builds, signed updates, backup/restore verification, protected live-provider validation, performance/security/soak labs, beta defect closure, and provenance-backed stable publication.

**Tech Stack:** Go cross-compilation, npm launcher, Winget, Homebrew, DEB/RPM, Docker/Compose, Windows installer/portable ZIP, signed/notarized macOS DMG/PKG, PostgreSQL runtimes, SBOM/provenance/signing tooling, Playwright, load/soak/security harnesses.

## Global Constraints

- Product Parity Gate must be approved.
- Stable release contains full approved parity; phases were implementation ordering, not reduced production scope.
- All official installation methods are equal and tested.
- npm UX is `npm install -g gorouter` then `gorouter`; npm downloads and verifies native binary.
- Desktop/npm do not autostart; server/service/Docker deployments do.
- Stable configured port never changes automatically and occupied port never causes kill/random fallback.
- Update waits indefinitely for stream drain and uses compatible atomic rollback rules.
- Ordinary uninstall preserves user data/config/backups; explicit purge removes all after strong confirmation.
- Backups are sensitive but not application-encrypted; storage/filesystem controls protect them.
- Every original provider must pass real-account validation before beta and stable.
- Beta and stable only; no stable publication until every mandatory gate passes.

---

## File and responsibility map

| Path | Responsibility |
| --- | --- |
| `packaging/npm` | Thin installer/launcher and checksum/signature verification |
| `packaging/windows` | Installer, service, portable ZIP, bundled PG lifecycle |
| `packaging/macos` | Signed/notarized app, DMG/PKG, bundled PG lifecycle |
| `packaging/linux` | DEB/RPM/tarball/service user/PG dependency |
| `packaging/docker` | Image, Compose, external PG, feature disables |
| `internal/host/service`, `tray`, `updater`, `postgresmanaged` | Platform lifecycle adapters |
| `internal/persistence/postgres/backup` | Scheduled backup, PITR, restore verification |
| `tests/performance`, `tests/soak`, `tests/security`, `tests/providers/live` | Release gates |
| `docs` | Complete enterprise user/operator/developer documentation |
| `.github/workflows/release*` | Reproducible signed release pipeline |

### Task P5-T01: Finalize reproducible binary and embedded frontend builds

**Files:**
- Create: `tools/build/main.go`
- Create: `build/manifest.schema.json`
- Create: `.github/workflows/reproducible-build.yml`
- Test: reproducibility and embedded-asset integrity tests

- [ ] Write RED tests requiring identical source/toolchain inputs to produce matching payload hashes and manifest with commit/version/platform/frontend hash.
- [ ] Implement deterministic frontend build/embed and Go build flags.
- [ ] Run GREEN across supported OS/arch matrix.
- [ ] Commit: `build: add reproducible release artifacts`

### Task P5-T02: Implement managed PostgreSQL lifecycle adapters

**Files:**
- Create: `internal/host/postgresmanaged/{manager,windows,macos,linux}.go`
- Create packaging runtime manifests/checksums
- Test: fake/process/platform integration/recovery tests

**Interfaces:**
- npm/Windows/macOS/portable automatically provision/init/run/upgrade/backup/restore PG; DEB/RPM use distro PG; Linux tarball/external server uses DATABASE_URL; Docker uses separate PG container.

- [ ] Write RED tests for first-run setup, upgrade, preserved data, failed setup guided recovery, no auto-reset, portable-tree relocation only when stopped.
- [ ] Implement platform adapters with verified runtime artifacts and least privilege.
- [ ] Run GREEN in clean VMs.
- [ ] Commit per platform family.

### Task P5-T03: Complete service, tray, start/stop/status/logs, and first-run lifecycle

**Files:**
- Complete: `internal/host/service`, `tray`, CLI lifecycle, first-run recovery UI
- Test: platform lifecycle suites

- [ ] Write RED tests for bare `gorouter`, already-running behavior, browser opening, headless no-open, terminal detach, autostart policy, port conflict, graceful/force exit, status fields, JSON/exit codes, live logs flags.
- [ ] Implement adapters and commands.
- [ ] Run GREEN on Windows/macOS/Linux/npm/Docker modes.
- [ ] Commit: `feat(lifecycle): complete cross-platform runtime control`

### Task P5-T04: Implement npm launcher and package-manager distributions

**Files:**
- Create: `packaging/npm/{package.json,postinstall.js,launcher.js}`
- Create: Winget/Homebrew/DEB/RPM metadata/scripts
- Test: clean install/update/uninstall/reinstall/purge matrices

- [ ] Write RED tests for version-exact native download, unsupported platform failure, bounded retry, atomic verified replacement, preserved data, no install-time service creation.
- [ ] Implement npm/manager packaging and checksum/signature verification.
- [ ] Run GREEN in clean containers/VMs.
- [ ] Commit per distribution channel.

### Task P5-T05: Implement Windows, macOS, Linux, Docker, and portable artifacts

**Files:**
- Create platform packaging scripts/configurations under approved directories.
- Test: install/runtime/uninstall/recovery workflows.

- [ ] Windows: installer + portable ZIP, managed PG, no shortcuts, service option, signed artifacts.
- [ ] macOS: signed/notarized app + DMG/PKG, managed PG, tray behavior.
- [ ] Linux: DEB/RPM dedicated non-login user, tarball external PG, system service.
- [ ] Docker: image/Compose with official PG, disabled incompatible host features.
- [ ] Run clean-environment GREEN tests for every artifact.
- [ ] Commit per platform.

### Task P5-T06: Implement updater with drain, health gate, and rollback

**Files:**
- Complete: `internal/host/updater/*`
- Create: update manifest/signature contracts
- Test: update/rollback/schema compatibility tests

- [ ] Write RED tests for startup-only desktop checks, opt-in update, indefinite drain with progress, staged verification, migration preflight, atomic swap, one health attempt, compatible rollback, incompatible-schema safe mode, old install preservation.
- [ ] Implement updater and external server-manager handoff.
- [ ] Run GREEN with fault injection at download/swap/start/health/migration stages.
- [ ] Commit: `feat(update): add signed atomic updates and rollback`

### Task P5-T07: Complete backup, restore, PITR, retention, and disaster recovery

**Files:**
- Complete: `internal/persistence/postgres/backup/*`
- Create: CLI recovery commands and scheduler jobs
- Create: `tests/recovery/*`

- [ ] Write RED tests for 30 daily backups, server WAL/PITR, desktop snapshots, restore verification, dashboard download, local-CLI-only restore, sensitive permissions, failed-backup critical alert, no application encryption.
- [ ] Implement backup/restore adapters and verification drill.
- [ ] Run GREEN using corrupted/incomplete/old-version fixtures and clean restore environments.
- [ ] Commit: `feat(recovery): add verified backup and restore`

### Task P5-T08: Complete documentation and localization

**Files:**
- Create/complete docs: quickstart, configuration, API, providers, OAuth, deployment, security, backup, troubleshooting, migration/deprecation, contributor, architecture.
- Complete frontend/CLI/installer/user-error locale catalogs for all original locales; English/Indonesian primary documentation.
- Test: link/schema/example/locale coverage tests

- [ ] Write RED doc coverage tests mapping ledger rows and public config/API fields to docs.
- [ ] Write exact commands/examples per installation mode without secrets.
- [ ] Validate snippets in ephemeral environments.
- [ ] Run locale completeness/fallback tests.
- [ ] Commit by documentation domain.

### Task P5-T09: Execute full live provider matrix

**Files:**
- Complete: `tests/providers/live/*`
- Create protected result manifests under release evidence

- [ ] Provision credentials only through protected CI/lab secret store.
- [ ] Run every provider/account/OAuth/model/modality/executor live test with bounded concurrency and sanitized artifacts.
- [ ] Classify failures as credential/environment/product; no provider receives false PASS or silent skip.
- [ ] Fix product defects through TDD and rerun affected automated/live suites.
- [ ] Store final beta evidence; repeat fresh before stable.

### Task P5-T10: Pass security release gates

**Files:**
- Complete: `tests/security`, fuzz targets, workflows, threat-model docs, SBOM/provenance/signing configs

- [ ] Run SAST, dependency/license/SBOM, secret scan, fuzz corpus, race detector, authn/authz/CSRF/trusted-proxy/key-redaction tests, signed artifact verification.
- [ ] Include explicit tests documenting approved unrestricted custom provider URL risk rather than claiming SSRF protection.
- [ ] Fix all release-blocking findings through TDD.
- [ ] Commit evidence and threat-model updates.

### Task P5-T11: Pass accessibility and product-quality gates

**Files:**
- Complete: Playwright/axe/manual checklist fixtures and visual baselines

- [ ] Test every dashboard route/workflow in light/dark, desktop/mobile, keyboard-only, screen reader landmarks, focus/error/loading/empty states.
- [ ] Require WCAG 2.2 AA; fix failures.
- [ ] Verify original content/workflows were not lost in OpenRouter-inspired redesign.
- [ ] Store accessibility report and approved exceptions only if user explicitly decides.

### Task P5-T12: Pass performance gates

**Files:**
- Create/complete: `tests/performance/{lightweight,streaming,router_overhead}.go`
- Create: controlled load-lab manifests

- [ ] Establish reference 4 vCPU/16 GB environment and reproducible provider/fake-upstream latency profiles.
- [ ] Measure p50/p95/p99 router overhead separately from provider latency.
- [ ] Prove p95 overhead <20ms, ≥2,000 concurrent streams, ≥100,000 RPM lightweight endpoints.
- [ ] Validate cancellation, terminal ordering, usage durability, PG pool behavior under load.
- [ ] Fix regressions and store exact evidence.

### Task P5-T13: Pass 72-hour mixed-load soak

**Files:**
- Complete: `tests/soak/*`
- Create: soak scenario/evidence manifests

- [ ] Include mixed formats/providers/modalities/combos/OAuth refresh/jobs/dashboard SSE/CLI operations/backups/restarts/cancellations.
- [ ] Inject transient provider, network, PG, disk, child-process, and update-check failures.
- [ ] Require zero crashes, deadlocks, goroutine leaks, lost accepted usage/state, duplicate side effects, duplicate terminal events.
- [ ] Inspect memory/goroutine/fd/socket/PG growth trends and fix leaks.
- [ ] Store signed 72h evidence.

### Task P5-T14: Publish and validate beta

**Files:**
- Create: release notes, checksums, SBOM, provenance, beta manifests
- Update: all distribution metadata to same SemVer beta

- [ ] Confirm Product Parity Gate and pre-beta provider/security/accessibility/performance evidence.
- [ ] Build/sign/notarize/publish beta across all official channels.
- [ ] Install/update/uninstall/recovery test published artifacts, not local builds.
- [ ] Gather defects; each fix receives regression test and affected gate rerun.
- [ ] Do not rename beta to stable without fresh stable evidence.

### Task P5-T15: Close beta defects and freeze stable candidate

**Files:**
- Update implementation/tests/docs/release evidence as defects demand

- [ ] Triage every beta defect against parity ledger/decision.
- [ ] Ask user on any new behavior choice or upstream conflict.
- [ ] Fix through RED→GREEN, rerun domain and release gates.
- [ ] Freeze exact stable candidate commit and dependencies.
- [ ] Repeat full live provider matrix and required 72h soak on candidate.

### Task P5-T16: Publish stable and verify recovery

**Files:**
- Final stable release manifests, release notes, deprecation notices, channel metadata

- [ ] Verify every ledger row and active decision has final implementation/test/doc/evidence ownership.
- [ ] Build/sign/notarize/publish stable artifacts and package indexes.
- [ ] Verify clean install, update from beta, ordinary uninstall/reinstall, purge, backup restore, compatible rollback, failed-update recovery.
- [ ] Confirm stable docs and support diagnostics match released bits.
- [ ] Record immutable stable release evidence and announce only after all checks pass.

## Mandatory Exit Gate: Stable Release Gate

Stable release is blocked unless all items pass:

- [ ] All parity ledger rows have final evidence; no unapproved omission.
- [ ] All active decisions through the implementation cutoff are mapped to code/tests/docs.
- [ ] Every original provider and specialized executor passes fresh real-account validation.
- [ ] All client formats/modalities/OAuth/routing/combo/mutator/product/host workflows pass automated parity suites.
- [ ] PostgreSQL 16/17/18 and migration/backup/PITR/restore gates pass.
- [ ] All official installation channels pass clean install/update/uninstall/reinstall/purge/recovery tests.
- [ ] Signed artifact, checksum, SBOM, provenance, notarization/package-index verification passes.
- [ ] Security gate passes with no unresolved release blocker.
- [ ] WCAG 2.2 AA gate passes for all dashboard workflows.
- [ ] Performance gate passes on 4 vCPU/16 GB: p95 <20ms, ≥2,000 streams, ≥100,000 RPM lightweight endpoints.
- [ ] Fresh 72-hour mixed-load soak passes with zero prohibited failures.
- [ ] Beta defects are closed or explicitly user-dispositioned without violating parity.
- [ ] Stable documentation/localization is complete and tested.
- [ ] `phase-5.json` contains exact candidate SHA, commands, artifacts, evidence, reviewers, and publication references.
- [ ] User gives explicit stable-release approval.
