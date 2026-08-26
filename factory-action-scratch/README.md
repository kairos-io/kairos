# Kairos Factory Action

A GitHub Actions reusable workflow for building Kairos images and artifacts, with optional security scanning, signing, publishing, and release automation.

## Features

- Build Kairos images for `amd64` and `arm64`
- Optional Kubernetes variants (`k3s` or `k0s`)
- Optional trusted boot (UKI) artifact generation
- Optional artifact generation (`iso`, `raw`)
- Placeholder artifact toggles for `vhd`, `gce`, `tar` (currently not implemented)
- Optional vulnerability scanning with Grype and Trivy
- Configurable security scan policy (`legacy`, `enforce`, `report-only`)
- Optional SARIF generation/upload to GitHub Security
- Optional registry push with SBOM generation
- Optional Cosign signing for pushed images and artifact checksums
- Optional GitHub Release creation and release artifact listing
- Custom image tags, artifact names, and job names
- Optional cloud-config injection (file path or URL)
- Optional runner cleanup to free disk space before build

## Automatic updates

This repository is configured with Renovate to keep the default `kairos_version` up to date in `.github/workflows/reusable-factory.yaml`.

## Quick start

```yaml
jobs:
  build:
    uses: kairos-io/kairos-factory-action/.github/workflows/reusable-factory.yaml@main
    with:
      version: "auto"
      base_image: "ghcr.io/kairos-io/hadron:v0.0.4"
      model: "generic"
      iso: true
      summary_artifacts: true
```

## Inputs

All inputs below come from `.github/workflows/reusable-factory.yaml`.

| Input | Type | Required | Default | Description |
|---|---|---|---|---|
| `dockerfile_path` | string | no | - | Path to Dockerfile. If empty, workflow downloads fallback Dockerfile from Kairos repo. |
| `kairos_version` | string | no | `v4.0.0` | Kairos version used to download fallback Dockerfile files. |
| `base_image` | string | no | `ghcr.io/kairos-io/hadron:v0.0.4` | Base image build argument. |
| `model` | string | no | `generic` | Target model build argument. |
| `arch` | string | no | `amd64` | Target architecture (`amd64` or `arm64`). |
| `kubernetes_distro` | string | no | - | Kubernetes distro to include (`k3s` or `k0s`). |
| `kubernetes_version` | string | no | `auto` | Kubernetes version; `auto` resolves to empty value in build args/tag suffix. |
| `version` | string | yes | - | Build version. Use `auto` for git-based versioning. |
| `trusted_boot` | boolean | no | `false` | Enable UKI/trusted-boot ISO flow. |
| `no_cache` | boolean | no | `false` | Build image with Docker build `--no-cache`. |
| `iso` | boolean | no | `false` | Generate ISO artifact (only when `model == generic`). |
| `raw` | boolean | no | `false` | Generate RAW artifact. |
| `vhd` | boolean | no | `false` | Enable VHD artifact step (currently placeholder, not implemented). |
| `gce` | boolean | no | `false` | Enable GCE artifact step (currently placeholder, not implemented). |
| `tar` | boolean | no | `false` | Enable TAR artifact step (currently placeholder, not implemented). |
| `compute_checksums` | boolean | no | `true` | Compute SHA256 checksums for generated artifacts. |
| `output_format` | string | no | `auto` | Declared input; currently not used by workflow steps. |
| `security_checks` | string | no | `""` | Declared input; currently not used by workflow steps. |
| `security_scan_mode` | string | no | `legacy` | Security gate policy: `legacy` (block on Grype criticals), `enforce` (block on enabled scanners), `report-only` (never block on findings). |
| `cosign` | boolean | no | `false` | Install Cosign and sign pushed images/checksums (requires `registry_domain` for image signing). |
| `grype` | boolean | no | `false` | Run Grype scan against built image. |
| `trivy` | boolean | no | `false` | Run Trivy scan against built image. |
| `grype_sarif` | boolean | no | `false` | Generate, filter, and upload Grype SARIF report. |
| `grype_sarif_fail_build` | boolean | no | `true` | Whether the Grype SARIF scan step fails the job on findings at/above the severity cutoff. Set to `false` to prevent SARIF generation from failing the job; the build may still fail due to `security_scan_mode` when `grype`/`trivy` scans are enabled. |
| `trivy_sarif` | boolean | no | `false` | Generate, filter, and upload Trivy SARIF report. |
| `registry_domain` | string | no | - | Registry domain used for login/push (example: `ghcr.io`). |
| `registry_namespace` | string | no | - | Registry namespace/organization. |
| `registry_repository` | string | no | uses flavor | Registry repository; defaults to derived flavor name. |
| `summary_artifacts` | boolean | no | `false` | Write build summary to GitHub step summary. |
| `auroraboot_version` | string | no | `latest` | Auroraboot container tag used for artifact generation. |
| `custom_tag_format` | string | no | - | Custom image tag format using workflow variables. |
| `custom_artifact_format` | string | no | - | Custom artifact name format using workflow variables. |
| `custom_job_name_format` | string | no | - | Custom GitHub Actions job name format using workflow variables. |
| `image_labels` | string | no | - | Labels passed to Docker image build/push steps. |
| `keys_dir` | string | no | - | Trusted boot keys path (required in practice when `trusted_boot: true`). |
| `sysext_dir` | string | no | - | Optional trusted boot system extensions overlay path. |
| `single_efi_cmdline` | string | no | - | Optional single EFI command line for trusted boot builds. |
| `cloud_config` | string | no | - | Cloud config file path or URL passed to auroraboot. |
| `release` | boolean | no | `false` | Create GitHub Release and upload release files. |
| `list_release_artifacts` | boolean | no | `false` | List release artifacts in GitHub summary. |
| `cleanup` | boolean | no | `false` | Remove preinstalled packages to free runner disk space. |

## Secrets

| Secret | Required | Description |
|---|---|---|
| `registry_username` | no | Registry username used by `docker/login-action` when `registry_domain` is set. |
| `registry_password` | no | Registry password/token used by `docker/login-action` when `registry_domain` is set. |

## Behavior notes

- `version: auto` uses `git describe --tags --dirty --always`; pure SHA values are normalized to `v0.0.0-<sha>`.
- `kubernetes_distro` is validated to `k3s`/`k0s`; `arch` is validated to `amd64`/`arm64`.
- `security_scan_mode` is validated to `legacy`, `enforce`, or `report-only`.
- ISO generation only runs for `model: generic` (`if: inputs.iso && inputs.model == 'generic'`).
- RAW generation can optionally publish an additional `-img` OCI image when `registry_domain` is set.
- VHD/GCE/TAR toggles currently print "not yet implemented" and do not produce artifacts.
- GitHub Release currently uploads only ISO-related files (`*.iso` and matching checksum/signature files).
- Grype/Trivy scanner logs print detailed critical findings when present; GitHub summary includes per-scanner pass/fail with critical counts when `summary_artifacts: true`.

## Custom format variables

These variables are available for `custom_tag_format`, `custom_artifact_format`, and `custom_job_name_format`:

- `$FLAVOR_RELEASE`
- `$VARIANT`
- `$ARCH`
- `$MODEL`
- `$VERSION`
- `$KUBERNETES_DISTRO`
- `$KUBERNETES_VERSION`
- `$UKI`
- `$COMMIT_SHA`

## Examples

### Security scan + SARIF upload

```yaml
jobs:
  build:
    uses: kairos-io/kairos-factory-action/.github/workflows/reusable-factory.yaml@main
    with:
      version: "auto"
      grype: true
      trivy: true
      security_scan_mode: report-only
      grype_sarif: true
      trivy_sarif: true
      summary_artifacts: true
```

### Enforce security gate

```yaml
jobs:
  build:
    uses: kairos-io/kairos-factory-action/.github/workflows/reusable-factory.yaml@main
    with:
      version: "auto"
      grype: true
      trivy: true
      security_scan_mode: enforce
      summary_artifacts: true
```

### Trusted boot ISO

```yaml
jobs:
  build:
    uses: kairos-io/kairos-factory-action/.github/workflows/reusable-factory.yaml@main
    with:
      version: "auto"
      model: "generic"
      iso: true
      trusted_boot: true
      keys_dir: "${{ github.workspace }}/keys"
      sysext_dir: "${{ github.workspace }}/overlay"
      single_efi_cmdline: "console=ttyS0"
```

### Push + sign

```yaml
jobs:
  build:
    uses: kairos-io/kairos-factory-action/.github/workflows/reusable-factory.yaml@main
    secrets:
      registry_username: ${{ secrets.REGISTRY_USERNAME }}
      registry_password: ${{ secrets.REGISTRY_PASSWORD }}
    with:
      version: "auto"
      registry_domain: "ghcr.io"
      registry_namespace: "myorg"
      registry_repository: "kairos"
      cosign: true
```

### RAW artifact + cloud-config

```yaml
jobs:
  build:
    uses: kairos-io/kairos-factory-action/.github/workflows/reusable-factory.yaml@main
    with:
      version: "auto"
      raw: true
      cloud_config: "path/to/cloud-config.yaml"
      compute_checksums: true
```

## Documentation

- https://kairos.io/
- https://github.com/kairos-io/kairos

## License

Apache License 2.0. See `LICENSE`.
