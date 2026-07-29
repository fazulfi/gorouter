# Gemini Contract (Phase 1 Scaffold)

**Status:** Scaffold — contract types will be defined in Phase 2.

## Scope

Google Gemini API compatibility:
- Generate content request/response
- Stream generate content events
- Candidate, content part, safety rating types
- Function calling declarations

## Upstream Reference

- Baseline: `decolua/9router` @ `79918c7830695bbca4a45c9fea4a42c3e9fd73d1`
- Upstream sources: `cli/src/providers/gemini.js` and related files

## Phase 2 Contract Items

- [ ] GenerateContent request struct
- [ ] GenerateContent response struct
- [ ] Streaming chunk types
- [ ] Safety/harm categories
- [ ] Function declaration structs

## Governance

Contracts must be annotated with:
- `// +gorouter:upstream`
- `// +gorouter:phase:2`
- `// +gorouter:required` for mandatory fields
