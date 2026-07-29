# Claude Contract (Phase 1 Scaffold)

**Status:** Scaffold — contract types will be defined in Phase 2.

## Scope

Anthropic Claude Messages API compatibility:
- Messages request (system, messages, tools, thinking)
- Messages response (content blocks, stop reason, usage)
- Streaming events (content_block_delta, message_stop, etc.)
- Tool use / tool result shapes

## Upstream Reference

- Baseline: `decolua/9router` @ `79918c7830695bbca4a45c9fea4a42c3e9fd73d1`
- Upstream sources: `cli/src/providers/claude.js` and related files

## Phase 2 Contract Items

- [ ] Messages request struct
- [ ] Messages response struct
- [ ] Streaming event types
- [ ] Tool/thinking content block types
- [ ] Usage/metadata structs

## Governance

Contracts must be annotated with:
- `// +gorouter:upstream`
- `// +gorouter:phase:2`
- `// +gorouter:required` for mandatory fields
