#!/usr/bin/env bash
# Build a local kairos-init image for riscv64 manual testing (openSUSE needs kairos-init#398+).
set -euo pipefail

# shellcheck source=lib/common.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/common.sh"

require_cmd docker
export DOCKER_BUILDKIT=1

if [[ -z "${KAIROS_INIT_SRC:-}" ]]; then
  for candidate in \
    "$(cd "$RISCV64_DIR/../../kairos-init" 2>/dev/null && pwd)" \
    "$(cd "$RISCV64_DIR/../../../kairos-init" 2>/dev/null && pwd)" \
    "/home/mauro/src/kairos-init"; do
    if [[ -f "$candidate/Dockerfile" && -f "$candidate/Makefile" ]]; then
      KAIROS_INIT_SRC="$candidate"
      break
    fi
  done
fi

[[ -n "${KAIROS_INIT_SRC:-}" && -f "${KAIROS_INIT_SRC}/Dockerfile" ]] \
  || die "Set KAIROS_INIT_SRC to a kairos-init checkout (needs Dockerfile and Makefile)."

image="${KAIROS_INIT_IMAGE_OPENSUSE_MIN}"

# Stale host copies of pkg/bundled/binaries/ used to overwrite fresh downloads inside
# the docker build (COPY . . after make all), embedding an old kairos-agent without
# riscv64 shim-skip (needs >= v2.29.4). .dockerignore also excludes this directory.
if [[ -d "${KAIROS_INIT_SRC}/pkg/bundled/binaries" ]]; then
  printf '==> Removing stale %s/pkg/bundled/binaries (re-downloaded in docker build)\n' "$KAIROS_INIT_SRC"
  rm -rf "${KAIROS_INIT_SRC}/pkg/bundled/binaries"
fi

printf '==> Building kairos-init image %s from %s\n' "$image" "$KAIROS_INIT_SRC"
printf '==> Platform: %s\n' "$PLATFORM"

docker build \
  --progress=plain \
  --platform "$PLATFORM" \
  --build-arg "SKIP_UPX=1" \
  --no-cache \
  -f "${KAIROS_INIT_SRC}/Dockerfile" \
  -t "$image" \
  "$KAIROS_INIT_SRC"

printf '\n==> Verifying embedded binary versions\n'
embedded_agent="$(docker run --rm --platform "$PLATFORM" --entrypoint /kairos-init "$image" version 2>&1 \
  | sed -n 's/.*kairos-agent: \(v[0-9.]*\).*/\1/p' | head -1)"
[[ -n "$embedded_agent" ]] || die "Could not read embedded kairos-agent version from $image"

printf '    kairos-agent: %s\n' "$embedded_agent"
if ! printf '%s\n' "$embedded_agent" | awk -F. '
  {
    gsub(/^v/, "", $1)
    if ($1+0 < 2 || ($1+0 == 2 && $2+0 < 29) || ($1+0 == 2 && $2+0 == 29 && $3+0 < 4)) exit 1
  }'; then
  die "Embedded kairos-agent $embedded_agent is too old for riscv64 EFI install (need >= v2.29.4, kairos-agent#1230).

Check ${KAIROS_INIT_SRC}/Makefile AGENT_VERSION and rebuild."
fi

printf '\nDone. Rebuild containers/ISOs so they pick up the new kairos-init:\n'
printf '  KAIROS_INIT_IMAGE=%s ./scripts/riscv64/build-container-opensuse.sh\n' "$image"
printf '  ./scripts/riscv64/build-iso-opensuse.sh\n'
printf '  KAIROS_INIT_IMAGE=%s ./scripts/riscv64/build-container-ubuntu.sh\n' "$image"
printf '  KAIROS_INIT_IMAGE=%s ./scripts/riscv64/build-container-debian.sh\n' "$image"
