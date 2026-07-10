#!/usr/bin/env bash
# Shared helpers for manual riscv64 core image + ISO builds.

set -euo pipefail

RISCV64_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
STATE_DIR="$RISCV64_DIR/state"
ARTIFACTS_DIR="$RISCV64_DIR/artifacts"

# kairos-init with riscv64 UPX skip + Fedora GRUB maps (kairos-init#352).
KAIROS_INIT="${KAIROS_INIT:-v0.15.2}"
KAIROS_INIT_IMAGE="${KAIROS_INIT_IMAGE:-quay.io/kairos/kairos-init:${KAIROS_INIT}}"
# openSUSE Tumbleweed riscv64 needs kairos-init#398 (shim scoped to x86/arm + grub2-riscv64-efi).
KAIROS_INIT_IMAGE_OPENSUSE_MIN="${KAIROS_INIT_IMAGE_OPENSUSE_MIN:-kairos-init:riscv64-dev}"
# Last auroraboot release published with a linux/riscv64 image manifest.
AURORABOOT_VERSION="${AURORABOOT_VERSION:-v0.22.0}"
PLATFORM="${PLATFORM:-linux/riscv64}"
MODEL="${MODEL:-generic}"

declare -A DISTRO_BASE_IMAGE=(
  [ubuntu]="ubuntu:24.04"
  [opensuse]="opensuse/tumbleweed"
  [debian]="debian:13"
)

declare -A DISTRO_FLAVOR=(
  [ubuntu]="ubuntu-24.04"
  [opensuse]="opensuse-tumbleweed"
  [debian]="debian-13"
)

die() {
  printf 'Error: %s\n' "$*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "'$1' is required but not installed"
}

bump_version() {
  local distro="$1"
  local state_file="$STATE_DIR/${distro}.version"
  mkdir -p "$STATE_DIR"
  local v=0
  if [[ -f "$state_file" ]]; then
    v="$(<"$state_file")"
  fi
  v=$((v + 1))
  printf '%s\n' "$v" >"$state_file"
  printf '%s' "$v"
}

get_last_container_version() {
  local distro="$1"
  local state_file="$STATE_DIR/${distro}.last-container"
  [[ -f "$state_file" ]] || die "No container build recorded for '$distro'. Run build-container-${distro}.sh first."
  cat "$state_file"
}

set_last_container_version() {
  local distro="$1"
  local version="$2"
  mkdir -p "$STATE_DIR"
  printf '%s\n' "$version" >"$STATE_DIR/${distro}.last-container"
}

container_image_ref() {
  local distro="$1"
  local version="$2"
  printf 'kairos-riscv64-%s-core:v%s' "$distro" "$version"
}

iso_output_dir() {
  local distro="$1"
  printf '%s/%s' "$ARTIFACTS_DIR" "$distro"
}

iso_filename() {
  local distro="$1"
  local version="$2"
  printf 'kairos-%s-core-riscv64-%s-v%s.iso' "${DISTRO_FLAVOR[$distro]}" "$MODEL" "$version"
}

preflight() {
  require_cmd docker
  export DOCKER_BUILDKIT=1

  local host_arch
  host_arch="$(uname -m)"
  if [[ "$host_arch" != "riscv64" && "$PLATFORM" == "linux/riscv64" ]]; then
    printf 'Note: host is %s; building for %s via QEMU binfmt.\n' "$host_arch" "$PLATFORM" >&2
    if ! docker run --rm --platform linux/riscv64 ubuntu:24.04 uname -m 2>/dev/null | grep -qx riscv64; then
      die "QEMU binfmt for riscv64 is not available. Install it with:
  docker run --privileged --rm tonistiigi/binfmt --install all"
    fi
  fi
}

build_core_container() {
  local distro="$1"
  [[ -n "${DISTRO_BASE_IMAGE[$distro]:-}" ]] || die "Unknown distro: $distro"

  preflight

  local dockerfile="$RISCV64_DIR/Dockerfile"
  [[ -f "$dockerfile" ]] || die "Missing $dockerfile"
  if grep -q 'KUBERNETES_DISTRO' "$dockerfile"; then
    die "Refusing to build: $dockerfile still contains Kubernetes logic (wrong file?)"
  fi

  local version
  version="$(bump_version "$distro")"
  local image_ref
  image_ref="$(container_image_ref "$distro" "$version")"
  local base_image="${DISTRO_BASE_IMAGE[$distro]}"
  local kairos_init_image="$KAIROS_INIT_IMAGE"

  if [[ "$distro" == "opensuse" && "$kairos_init_image" == quay.io/kairos/kairos-init:v0.15.2 ]]; then
    die "openSUSE Tumbleweed riscv64 requires a newer kairos-init (installs unavailable 'shim' package).

Build a local kairos-init with the fix from kairos-init#398 first:
  ./scripts/riscv64/build-kairos-init.sh

Then rebuild the container with:
  KAIROS_INIT_IMAGE=${KAIROS_INIT_IMAGE_OPENSUSE_MIN} ./scripts/riscv64/build-container-opensuse.sh"
  fi

  # EFI install on riscv64 needs kairos-agent >= v2.29.4 (skips shim copy). Rebuild
  # kairos-init locally so bundled binaries match Makefile (currently v2.30.1).
  if [[ "$PLATFORM" == "linux/riscv64" && "$kairos_init_image" == quay.io/kairos/kairos-init:v0.15.2 ]]; then
    printf 'Note: for riscv64 EFI install testing, prefer a fresh local kairos-init:\n' >&2
    printf '  ./scripts/riscv64/build-kairos-init.sh\n' >&2
    printf '  KAIROS_INIT_IMAGE=%s ./scripts/riscv64/build-container-%s.sh\n\n' \
      "$KAIROS_INIT_IMAGE_OPENSUSE_MIN" "$distro" >&2
  fi

  printf '==> Building %s core container (base=%s, version=v%s)\n' "$distro" "$base_image" "$version"
  printf '==> Dockerfile: %s\n' "$dockerfile"
  printf '==> kairos-init image: %s\n' "$kairos_init_image"
  printf '==> riscv64 cross-build: services step skipped inside container (uname -m)\n'

  docker build \
    --progress=plain \
    --platform "$PLATFORM" \
    -f "$dockerfile" \
    --build-arg "BASE_IMAGE=${base_image}" \
    --build-arg "KAIROS_INIT_IMAGE=${kairos_init_image}" \
    --build-arg "MODEL=${MODEL}" \
    --build-arg "VERSION=v${version}" \
    --build-arg "DISTRO=${distro}" \
    -t "$image_ref" \
    "$RISCV64_DIR"

  set_last_container_version "$distro" "$version"

  printf '\nDone. Container image: %s\n' "$image_ref"
  printf 'Next: scripts/riscv64/build-iso-%s.sh\n' "$distro"
}

build_core_iso() {
  local distro="$1"
  [[ -n "${DISTRO_BASE_IMAGE[$distro]:-}" ]] || die "Unknown distro: $distro"

  preflight

  local container_version
  container_version="$(get_last_container_version "$distro")"
  local container_ref
  container_ref="$(container_image_ref "$distro" "$container_version")"

  docker image inspect "$container_ref" >/dev/null 2>&1 \
    || die "Container image '$container_ref' not found locally. Re-run build-container-${distro}.sh or rebuild v${container_version}."

  # ISO version matches the container it was built from (do not bump again).
  local version="$container_version"
  local out_dir
  out_dir="$(iso_output_dir "$distro")"
  local iso_name
  iso_name="$(iso_filename "$distro" "$version")"
  mkdir -p "$out_dir"

  printf '==> Building %s core ISO from %s (version=v%s)\n' "$distro" "$container_ref" "$version"

  docker run --rm --privileged \
    -v /var/run/docker.sock:/var/run/docker.sock \
    -v "${out_dir}:/output" \
    "quay.io/kairos/auroraboot:${AURORABOOT_VERSION}" \
    --debug build-iso \
    --output /output/ \
    --arch riscv64 \
    "docker:${container_ref}"

  local generated
  generated="$(find "$out_dir" -maxdepth 1 -name '*.iso' -type f ! -name "$iso_name" | head -1 || true)"
  if [[ -z "$generated" ]]; then
    generated="$(find "$out_dir" -maxdepth 1 -name '*.iso' -type f | head -1 || true)"
  fi
  [[ -n "$generated" ]] || die "auroraboot did not produce an ISO in $out_dir"

  if [[ "$generated" != "$out_dir/$iso_name" ]]; then
    mv -f "$generated" "$out_dir/$iso_name"
  fi

  printf '\nDone. ISO: %s/%s\n' "$out_dir" "$iso_name"
  printf 'Built from container: %s\n' "$container_ref"
}
