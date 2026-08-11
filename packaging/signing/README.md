# Release signing preparation

`provenance.json` is a real SLSA v1 attestation populated with genuine digests for the current release tree:

- `buildConfig.sourceRevision`: `7e6a9704c0e17ab6a9ac80bad4835e9f260dd7fb` (release candidate HEAD)
- `subjects`: SHA-256 digests of the five release binaries, produced by the deterministic build tool (`go run ./tools/build`) from that revision and cross-checked against `packaging/beta/v1.2.3-beta.1/checksums.txt`
- `materials`: SHA-256 of `go.mod` and a deterministic hash over `tools/build/`
- `metadata.builtOn`: build timestamp of the local deterministic build

Signing remains intentionally preparation-only for P5-T10. T14/T16 pipeline owners must provision an external key (OIDC/KMS-backed where supported), sign the release archive and checksum/SBOM/provenance bundle, and publish detached signatures plus the verification key through the release channel. Until then, `signingStatus`/`signingKey` are `deferred-to-t14-t16` and the npm launcher uses its embedded verification key, overridable via `GOROUTER_SIGNING_KEY`.

Required verification before publication:

1. Rebuild from the pinned source revision in a clean CI runner.
2. Verify artifact checksum, SBOM subject digest, and provenance subject digest agree.
3. Sign with the protected CI identity; never store private keys in git, fixtures, or evidence.
4. Verify signatures in a separate job using the published public key/fingerprint.
5. Record key identity, artifact digest, source revision, and verifier output in the T14/T16 manifest.

Local T10 does not perform real signing because release publication and signing keys are gated to T14/T16.
