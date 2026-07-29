# Native/Provider-Specific Contract (Phase 1 Scaffold)

**Status:** Scaffold — provider-native protocol contracts will be defined in Phase 2.

## Scope

Provider-native protocol compatibility for non-HTTP-JSON providers:
- Binary protocol shapes (if any)
- Provider-specific transport wrappers
- Streaming format converters
- Non-standard auth flows

## Upstream Reference

- Baseline: `decolua/9router` @ `79918c7830695bbca4a45c9fea4a42c3e9fd73d1`
- Derived from non-standard provider integrations

## Phase 2 Contract Items

- [ ] Provider-native type definitions
- [ ] Streaming adapter contracts
- [ ] Auth flow contracts (OAuth device code, etc.)

## Governance

Contracts must be annotated with:
- `// +gorouter:upstream`
- `// +gorouter:phase:2`
- `// +gorouter:required` for mandatory fields

Do not force native/binary protocols into lossy JSON schemas.
