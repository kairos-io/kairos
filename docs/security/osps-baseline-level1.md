# OpenSSF Security Baseline Level 1 self-assessment

Kairos conforms to [OpenSSF Security Baseline](https://baseline.openssf.org/) Level 1 (catalog 2025-10-10, including the 2026-08 additions).

The machine-readable attestation is published in [`security-insights.yml`](../../security-insights.yml) under `security.assessments.self`. This document is the human-readable evidence it points to. The work was tracked in [kairos-io/kairos#3753](https://github.com/kairos-io/kairos/issues/3753).

## Statement

As of 2026-10-09, Kairos meets every OpenSSF Security Baseline Level 1 control. The `pvtr` OSPS Baseline scanner (github-repo plugin v0.31.0, catalog `osps-baseline`) run against `master` reports no failed Level 1 controls: 14 passed and 3 manual-review or transient warnings (BR-01.03 manual sign-off, and LE-02.02 and LE-03.02 license coverage in release archives, the latter a transient GitHub API 504 while fetching the releases page).

## Per-control evidence

| Control | Evidence |
|---|---|
| OSPS-AC-01.01 | Org-level 2FA required (set by an org owner, 2026-10-06) |
| OSPS-AC-02.01 | Org base permission for new members restricted (2026-10-06) |
| OSPS-AC-03.01, AC-03.02 | Default branch protected against direct commits and deletion; `master` bypass limited to pull requests, enforced for admins (2026-10-06) |
| OSPS-BR-01.01, BR-01.02 | zizmor runs on every workflow change; all findings fixed, and the `pr-full.yaml` `pull_request_target` and `upload-cloud-images.yaml` `workflow_run` triggers documented where they are declared |
| OSPS-BR-01.03 | No privileged job runs untrusted code: the `pr-full.yaml` size check runs from the base branch, and the cloud image uploads check out the default branch |
| OSPS-BR-03.01, BR-03.02 | Official and distribution channels served over HTTPS |
| OSPS-BR-07.01 | Secret scanning and push protection enabled on the repository and org-wide (2026-10-06) |
| OSPS-DO-01.01 | User guides at the kairos.io documentation |
| OSPS-DO-02.01 | Defect reporting guide (bug report template) |
| OSPS-GV-02.01 | Public discussion mechanisms (issues and discussions) |
| OSPS-GV-03.01 | Contribution process (`CONTRIBUTING.md`) |
| OSPS-LE-02.01, LE-02.02, LE-03.01, LE-03.02 | OSI license (Apache-2.0) in the repository and in the source archives |
| OSPS-QA-01.01, QA-01.02 | Public repository with full change history |
| OSPS-QA-02.01 | Dependency list (`go.mod`) |
| OSPS-QA-04.01 | Subprojects listed in the README |
| OSPS-QA-05.01, QA-05.02 | Committed binaries, Secure Boot test keys and system extension images replaced with fixtures generated at test time; OVMF variable stores come from the firmware package and the sysext test layer is built in Go |
| OSPS-VM-02.01 | Security contacts (`SECURITY.md`), private vulnerability reporting enabled |

## Keeping this current

Re-run the scanner against `master` when branch protection, the release pipeline, or a workflow that runs with secrets changes, and update the statement date and any affected row. The scanner evaluates only the default branch.
