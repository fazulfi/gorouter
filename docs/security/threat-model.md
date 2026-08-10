# T10 Threat Model

## Scope and assets
Gorouter accepts client requests, provider credentials, configuration, audit data, and streamed responses. Protected assets are API keys, OAuth tokens, configuration, user data, audit records, signing metadata, and release artifacts.

## Trust boundaries and attack surface
- Internet client → HTTP/API middleware: authentication, authorization, CSRF, request parsing, rate and body limits.
- Reverse proxy → gorouter: forwarded-header trust is restricted to configured CIDRs; untrusted headers are removed.
- Gorouter → configured provider URL: custom URLs are intentionally supported for private/self-hosted providers. This is an approved SSRF-risk boundary; deployment policy must restrict egress where required. No SSRF protection is claimed.
- Gorouter → PostgreSQL: credentials and durable audit/usage state cross a database boundary.
- Build host/CI → release artifacts: provenance, SBOM, and signing metadata must be generated from the exact source revision.

## Risk matrix
| Threat | Asset | CWE family | Mitigations | Residual risk |
|---|---|---|---|---|
| Header spoofing through an untrusted proxy | Client identity/audit data | CWE-441 | CIDR-validated trusted proxy middleware; security tests and fuzz target | Misconfigured proxy CIDR |
| Credential disclosure in logs/errors | API/OAuth keys | CWE-532 | Redaction utilities and redaction tests; gitleaks scan | New logging paths require review |
| Cross-site request forgery | Mutating browser requests | CWE-352 | CSRF middleware/tests in security suite | Browser deployment must preserve cookie policy |
| Unauthorized admin/API access | Provider/configuration data | CWE-862 | Authn/authz tests and fail-closed middleware | Policy changes require security review |
| Malicious provider endpoint | Network and credentials | CWE-918 | Explicitly documented approved custom-provider risk; egress controls are deployment-owned | No application SSRF claim |
| Tampered release artifact | Users and runtime | SLSA provenance | Reproducible SBOM/provenance metadata; signing plan | Actual key use deferred to T14/T16 |

## Release decisions
- Security scans are release gates; unavailable local tools are CI-required, not silently passed.
- Signing keys never enter this repository or local evidence.
- Race testing is CI-required when a CGO-capable Linux runner is available.
