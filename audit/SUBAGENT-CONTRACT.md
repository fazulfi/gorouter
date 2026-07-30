<!-- markdownlint-disable MD013 -->

# Mandatory Sub-Agent Audit Contract

Every future sub-agent working on the original 9Router audit must follow this contract.

## Task boundary

The task is source investigation and behavior documentation only.

The sub-agent must not:

- choose or modify rewrite architecture;
- choose technologies;
- edit `DECISIONS.md`;
- implement Go rewrite code;
- build, run, install, or deploy the original application;
- modify the read-only upstream clone;
- access or expose production credentials;
- replace source evidence with assumptions.

## Required output file

The delegation prompt must specify one absolute Markdown path under:

`C:\Users\faizz\9router-go\audit`

The sub-agent must write its full report directly to that path. Returning findings only in the task response is insufficient.

## Required report structure

Every report must include:

1. **Scope**
2. **Audited repository and exact commit**
3. **Method**
4. **Confirmed findings**
5. **Source evidence** with file paths and symbols/routes
6. **Behavioral invariants**
7. **Process-local or concurrency assumptions**
8. **Error and edge-case behavior**
9. **Security implications**, if relevant
10. **Uncertainties and conflicting evidence**
11. **Parity requirements** stated as observed behavior, not design choices
12. **Questions requiring user decisions**
13. **Coverage gaps**

## Evidence requirements

A confirmed claim must cite at least one of:

- source file path and exported symbol/function/class;
- route path and HTTP method;
- test file and test name;
- configuration key and source location;
- generated inventory command and reproducible count.

Documentation-only claims must be labeled as documentation and cross-checked against source. Stale documentation must be called out explicitly.

## Prohibited language

Do not state these as facts unless the source proves them:

- “the rewrite should use ...”
- “the best architecture is ...”
- “Redis/PostgreSQL/Go must ...”
- “this feature is unnecessary”
- “all endpoints/providers/tests ...” when inventory is incomplete

Use neutral wording:

- “The original implements ...”
- “Parity would require preserving or consciously replacing ...”
- “This creates a user decision about ...”
- “No implementation was found in the inspected path ...”

## Formatting policy

Formatting is not an audit workstream or completion gate.

The sub-agent must not spend time on:

- line-length limits or wrapping text to 80 characters;
- markdown linting or automatic style fixes;
- counting lines, headings, tables, or formatting changes;
- rewriting valid evidence solely for cosmetic consistency.

Markdown only needs to remain readable and structurally valid. Source accuracy, coverage, invariants, edge cases, risks, and unresolved questions take priority over formatting.

## Completion response

The final task response must contain only a concise completion summary:

- output file path;
- audited commit;
- sections completed;
- count of confirmed findings;
- unresolved gaps.

The Markdown file is the authoritative deliverable.
