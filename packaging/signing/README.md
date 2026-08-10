# Release signing preparation

Signing is intentionally preparation-only for P5-T10. T14/T16 pipeline owners must provision an external key (OIDC/KMS-backed where supported), sign the release archive and checksum/SBOM/provenance bundle, and publish detached signatures plus the verification key through the release channel.

Required verification before publication:

1. Rebuild from the pinned source revision in a clean CI runner.
2. Verify artifact checksum, SBOM subject digest, and provenance subject digest agree.
3. Sign with the protected CI identity; never store private keys in git, fixtures, or evidence.
4. Verify signatures in a separate job using the published public key/fingerprint.
5. Record key identity, artifact digest, source revision, and verifier output in the T14/T16 manifest.

Local T10 does not perform real signing because release publication and signing keys are gated to T14/T16.
