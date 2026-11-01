# Security policy

## Reporting a vulnerability

Report privately through GitHub: open this repository's **Security** tab and
choose **Report a vulnerability**. Do not open a public issue.

Include the affected component, impact, reproduction steps and, when safe, a
minimal proof of concept. Never include live credentials, account data, private
keys or game assets.

## Supported code

Fixes target the default branch and the latest release.

The server must be built with the Go release declared in `apps/server/go.mod`
or a newer patched release. CI runs `govulncheck` as a blocking gate; reviewed
exceptions are recorded in
[ACCEPTED_ADVISORIES.md](../apps/server/ops/docs/ACCEPTED_ADVISORIES.md).
