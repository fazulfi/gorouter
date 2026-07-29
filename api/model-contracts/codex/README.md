# Codex (OpenAI OAuth) Contract (Phase 1 Scaffold)

**Status:** Scaffold — contract types will be defined in Phase 2.

## Scope

OpenAI OAuth-based model API (Codex/Copilot) compatibility:
- Chat completion request/response
- OAuth token exchange and refresh
- Copilot API-specific headers and metadata
- IDE-originated request shaping

## Upstream Reference

- Baseline: `decolua/9router` @ `79918c7830695bbca4a45c9fea4a42c3e9fd73d1`
- Upstream sources: `cli/src/providers/codex.js`, `cli/src/cli/api/client.js`

## Phase 2 Contract Items

- [ ] Codex chat request struct
- [ ] Codex chat response struct
- [ ] OAuth token structures
- [ ] IDE metadata headers

## Governance

Contracts must be annotated with:
- `// +gorouter:upstream`
- `// +gorouter:phase:2`
- `// +gorouter:required` for mandatory fields
