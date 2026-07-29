# Model API Contracts

This directory documents the wire-level contract between gorouter and upstream model providers (OpenAI, Anthropic, Google Gemini, etc.).

## Structure

- `openai/` - OpenAI-compatible request/response shapes (chat, embeddings, models)
- `responses/` - Unified response envelope shapes across providers
- `claude/` - Anthropic Claude Messages API compatibility
- `gemini/` - Google Gemini API compatibility
- `codex/` - OpenAI OAuth (Codex/Copilot) contract
- `native/` - Provider-native protocol contracts (non-HTTP-JSON)

## Contract Format

Each contract is defined as a Go struct with JSON tags, annotated with:
- `// +gorouter:upstream` - marks the struct as an upstream contract
- `// +gorouter:phase:N` - marks when this contract becomes active
- `// +gorouter:required` - marks fields required by gorouter internally

## Testing

Contracts should be tested with `tests/parity/contracts_test.go` which uses
fixtures from `tests/fixtures/upstream/` to verify:
1. Request deserialization from upstream fixtures
2. Response serialization matches upstream shape
3. Field presence parity with upstream

## Phase 1

Phase 1 establishes the contract scaffolding. Each subdirectory contains a
Phase 1 scaffold README listing the contract types to be defined in Phase 2.
Actual contract types and Go struct definitions will be added in Phase 2
when model API routes are implemented.
