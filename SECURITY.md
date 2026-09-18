# Security Policy

## Supported versions

Security fixes are provided for the latest released minor version. Please
upgrade to the newest release before reporting an issue.

| Version | Supported |
|---|---|
| 1.2.x | :white_check_mark: |
| 1.1.x | :x: |
| < 1.1 | :x: |

## Reporting a vulnerability

Please **do not** open a public GitHub issue for security problems.

Report privately through GitHub's [Security Advisories](https://github.com/Syamchand123/GlassMarble/security/advisories/new)
("Report a vulnerability"), or email the maintainer at the address on the
[GitHub profile](https://github.com/Syamchand123).

Include, where possible:

- the affected version (`gmb version`),
- the platform and architecture,
- a minimal reproduction or proof of concept,
- the impact you believe the issue has.

## What to expect

- Acknowledgement of your report within 72 hours.
- An assessment and a remediation plan within 7 days.
- Credit in the release notes once a fix ships, unless you prefer to remain
  anonymous.

## Scope

GlassMarble is a local-first tool. It reads your source and writes only to
`.glassmarble/`. Relevant areas include, but are not limited to:

- path traversal or writes outside the target repository,
- secret/credential leakage into generated artifacts, logs, or telemetry,
- remote code execution through parsed input or generated documents,
- supply-chain integrity of release artifacts (checksums, Cosign signatures,
  SBOM, provenance).

Release artifacts are signed with Sigstore Cosign and ship with an SBOM and
SLSA provenance; see [docs/getting-started.md](docs/getting-started.md) for
verification instructions.
