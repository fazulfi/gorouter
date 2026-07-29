# OpenAI Contract (Phase 1 Scaffold)

**Status:** Scaffold — contract types will be defined in Phase 2.

## Scope

OpenAI-compatible request/response shapes:
- Chat completions
- Embeddings
- Models list
- Streaming delta frames

## Upstream Reference

- Baseline: `decolua/9router` @ `79918c7830695bbca4a45c9fea4a42c3e9fd73d1`
- Upstream sources: `cli/src/providers/openai.js` and related files

## Phase 2 Contract Items

- [ ] Chat completion request/response structs
- [ ] Embedding request/response structs
- [ ] Streaming chunk types
- [ ] Error response shapes
- [ ] Field presence parity tests

## Governance

Contracts must be annotated with:
- `// +gorouter:upstream`
- `// +gorouter:phase:2`
- `// +gorouter:required` for mandatory fields
