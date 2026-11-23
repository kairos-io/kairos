# Security Policy

## Supported Versions

Only the latest Kairos release receives fixes, including security fixes. Users
are expected to run the latest release.

Kairos releases from `master` every Monday. When `master` is green and
something landed since the previous release, a new version is cut and
published once a maintainer approves it. A change that adds a feature gives a
minor version, any other change gives a patch version. [RELEASING.md](RELEASING.md)
describes the process.

| Version | Supported |
|---|---|
| Latest release | Yes |
| Older minor or patch releases | No. A maintainer may backport a fix by hand, on a best-effort basis |

Hadron, the minimal base OS Kairos images can be built from, is a rolling
release and has no long-term support versions. It releases twice a week.

Help with older versions is best-effort and comes from the community.
Spectro Cloud offers enterprise support for users who need more.

## Reporting a Vulnerability

Kairos supports responsible disclosure and endeavors to resolve security issues in a reasonable timeframe.

However, as is a community driven project we don't run any bug bounty, but we will make sure credits goes to whom belongs and address the issues as fast as possible, if you can provide also patch and open up a PR that's more than welcome.

To report a security vulnerability, use [GitHub Security Draft Advisory](https://github.com/kairos-io/kairos/security/advisories/new), if you don't have a GitHub account, please email members@kairos.io.

## Security Baseline

Kairos self-assesses against the OpenSSF Security Baseline. The Level 1 statement and per-control evidence are in [docs/security/osps-baseline-level1.md](docs/security/osps-baseline-level1.md).
