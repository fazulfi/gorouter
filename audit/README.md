<!-- markdownlint-disable MD013 -->

# Audit Index and Sub-Agent Output Contract

## Purpose

This directory is the durable record for the original 9Router audit. Findings must be written here before they are relied upon in the Go rewrite discussion.

The audit studies source behavior only. It does not make architecture or system-design decisions. Those decisions remain with the user and are recorded separately in `../DECISIONS.md`.

## Audited source

- Repository: `decolua/9router`
- Exact commit: `79918c7830695bbca4a45c9fea4a42c3e9fd73d1`
- Read-only clone: `../upstream-original`

## Current files

| File | Status | Scope |
| --- | --- | --- |
| `00-upstream-audit.md` | Completed | First-pass source-backed architecture, contracts, invariants, risks, and open work |
| `01-http-contracts.md` | Completed | Exact route/method/auth/CORS/request/response inventory |
| `02-provider-matrix.md` | Completed | Exact providers, auth, models, capabilities, transports, executors |
| `03-oauth-token-lifecycle.md` | Completed | Every OAuth/import/token refresh/dedup/callback flow |
| `04-data-persistence.md` | Completed | Schema, repositories, migration, compatibility files, backup/import/export |
| `05-streaming-translation.md` | Completed | Protocol formats, translators, executors, streaming, cancellation, terminal behavior |
| `06-routing-fallback.md` | Completed | Model parsing, aliases, combos, account fallback, cooldowns, retries |
| `07-dashboard-frontend.md` | Completed | Pages, workflows, browser state, polling, SSE, auth, edge cases |
| `08-cli-host-integration.md` | Completed | CLI commands, launcher, tray, update, child processes, packaging |
| `09-security-boundaries.md` | Completed | Trust boundaries, authentication, authorization, local-only operations, credential storage |
| `10-tests-baseline.md` | Completed | Test suites, commands, fixtures, known failures, parity strategy inputs |
| `11-runtime-background-jobs.md` | Completed | Initialization, timers, caches, watchers, process-local state |
| `12-parity-matrix.md` | Completed | Confirmed parity requirements and unresolved questions, without architecture decisions |
| `SUBAGENT-CONTRACT.md` | Active | Mandatory rules for every delegated audit workstream |

## Mandatory workflow

1. Each audit workstream receives one dedicated Markdown output path.
2. The delegated agent must write findings directly to that file.
3. The file must identify the exact upstream commit.
4. Every confirmed claim must include source file paths and symbols or route names.
5. Unverified or ambiguous claims must be labeled clearly.
6. The agent must not edit `DECISIONS.md` or choose rewrite architecture.
7. The orchestrator verifies the file exists and reads it before accepting completion.
8. Findings needed in another file should be linked, not silently duplicated.

## Status labels

Use these labels consistently:

- **Confirmed**: directly supported by source at the audited commit.
- **Observed**: generated inventory/count from the audited tree, but not yet semantically traced.
- **Uncertain**: evidence is incomplete or conflicting.
- **Stale documentation**: documentation contradicts newer source.
- **User decision required**: source exposes a design choice but does not decide the rewrite.
- **Out of scope**: unrelated to original behavior or clean-room parity.

## Completion rule

A delegated workstream is not complete when it only returns chat text. It is complete only when:

- its designated `.md` file exists;
- the file contains source-backed evidence;
- open questions and uncertainty are explicit;
- the orchestrator has read and verified the file;
- the index status is updated.
