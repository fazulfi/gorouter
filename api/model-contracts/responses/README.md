# Responses Contract (Phase 1 Scaffold)

**Status:** Scaffold — contract types will be defined in Phase 2.

## Scope

Unified response envelope shapes shared across model providers:
- Standard response wrapper
- Error response envelope
- Rate-limit headers and body
- Streaming termination signals

## Upstream Reference

- Baseline: `decolua/9router` @ `79918c7830695bbca4a45c9fea4a42c3e9fd73d1`
- Derived from common response patterns across all providers

## Phase 2 Contract Items

- [ ] Standard response struct
- [ ] Error response struct
- [ ] Rate-limit info struct
- [ ] Streaming end-of-stream marker

## Governance

Contracts must be annotated with:
- `// +gorouter:upstream`
- `// +gorouter:phase:2`
- `// +gorouter:required` for mandatory fields
