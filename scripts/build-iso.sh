#!/usr/bin/env bash
# Build a bootable Kairos ISO from this working tree, the same way CI does.
#
# `make binaries` stops at the kairos-init container image. The two steps
# that turn that into something you can boot live only in
# .github/workflows/reusable-factory.yaml, so reproducing a CI ISO locally
# used to mean reading the workflow and reassembling the docker commands by
# hand. This script is those steps, in order:
#
#   1. make binaries        -- the multi-call kairos (default + FIPS), the
#                              installer, and kairos-init with them embedded
#   2. make image-kairos-init -- kairos-init as a container image
#   3. docker build         -- run kairos-init over the base image to produce
#                              the Kairos OS image (images/Dockerfile)
#   4. auroraboot build-iso -- turn that OS image into an ISO
#
# Defaults reproduce the `amd64-standard` cell from master.yaml, which is the
# image the provider and standard qemu test suites run against. Everything is
# overridable by environment variable; nothing takes a flag yet.
#
# Requires docker (with a working socket) and go.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

# --- Build inputs -----------------------------------------------------------
# These mirror the amd64-standard matrix cell in .github/workflows/master.yaml.
: "${ARCH:=amd64}"
: "${BASE_IMAGE:=ghcr.io/kairos-io/hadron:v0.5.1}"
: "${MODEL:=generic}"
: "${KUBERNETES_DISTRO:=k3s}"
: "${TRUSTED_BOOT:=false}"

# Same derivation the factory workflow uses for version: "auto". A bare SHA
# means the tree has no tags reachable, which kairos-init rejects as a
# version, so it gets a v0.0.0- prefix.
if [[ -z "${VERSION:-}" ]]; then
    VERSION="$(git describe --tags --dirty --always 2>/dev/null || echo dev)"
    if [[ "$VERSION" =~ ^[a-f0-9]+$ ]]; then
        VERSION="v0.0.0-$VERSION"
    fi
fi

# kairos-init is consumed by images/Dockerfile as a FROM, so it only has to
# exist in the local docker daemon. Nothing here pushes. The registry and tag
# are separate because `make image-kairos-init` composes the name itself, as
# <registry>/kairos-init:<tag>.
: "${IMAGE_REGISTRY:=kairos-local}"
: "${IMAGE_TAG:=dev}"
KAIROS_INIT_IMAGE="$IMAGE_REGISTRY/kairos-init:$IMAGE_TAG"
: "${OS_IMAGE:=$IMAGE_REGISTRY/kairos-os:$IMAGE_TAG}"
# The auroraboot version is pinned in .github/workflows/_build-iso.yaml, which
# passes auroraboot_version to the factory workflow rather than taking the
# factory's own "latest" default. Read it from there instead of repeating it,
# so a local ISO stays comparable to a CI one and there is only one place to
# bump. scripts/kairos-diff.sh parses pinned versions out of tracked files the
# same way. Failing loudly beats silently falling back to a different version
# than CI uses, which is the whole point of tracking the pin.
if [[ -z "${AURORABOOT_IMAGE:-}" ]]; then
    iso_workflow=".github/workflows/_build-iso.yaml"
    # No `| head -1`: under pipefail, head closing the pipe early can fail the
    # script on sed's SIGPIPE. Take the first match by expansion instead.
    aurora_matches="$(sed -n \
        "s/^[[:space:]]*auroraboot_version:[[:space:]]*['\"]\{0,1\}\([^'\"[:space:]]*\)['\"]\{0,1\}[[:space:]]*$/\1/p" \
        "$iso_workflow")"
    aurora_version="${aurora_matches%%$'\n'*}"
    if [[ -z "$aurora_version" ]]; then
        echo "error: no auroraboot_version found in $iso_workflow" >&2
        echo "       The pin moved or changed shape. Fix the parse above, or set" >&2
        echo "       AURORABOOT_IMAGE to pick the image explicitly." >&2
        exit 1
    fi
    AURORABOOT_IMAGE="quay.io/kairos/auroraboot:$aurora_version"
fi
: "${OUTPUT_DIR:=$REPO_ROOT/build/iso}"

for cmd in docker go; do
    command -v "$cmd" >/dev/null 2>&1 || {
        echo "error: $cmd is required but not on PATH" >&2
        exit 1
    }
done

echo "==> Building ISO"
echo "    arch:       $ARCH"
echo "    base image: $BASE_IMAGE"
echo "    model:      $MODEL"
echo "    k8s:        ${KUBERNETES_DISTRO:-<none>}"
echo "    version:    $VERSION"
echo "    output:     $OUTPUT_DIR"
echo

# --- 1 + 2: binaries, then kairos-init as an image --------------------------
# `binaries` also fetches the external provider-kairos and edgevpn releases
# pinned in scripts/prepare-kairos-init-binaries.sh, so the ISO carries the
# same provider versions CI ships.
echo "==> [1/4] make binaries"
make binaries ARCH="$ARCH"

echo "==> [2/4] make image-kairos-init -> $KAIROS_INIT_IMAGE"
make image-kairos-init \
    ARCH="$ARCH" \
    IMAGE_REGISTRY="$IMAGE_REGISTRY" \
    IMAGE_TAG="$IMAGE_TAG"

# --- 3: OS image ------------------------------------------------------------
echo "==> [3/4] docker build (images/Dockerfile) -> $OS_IMAGE"
docker build -f images/Dockerfile . \
    --build-arg BASE_IMAGE="$BASE_IMAGE" \
    --build-arg KAIROS_INIT_IMAGE="$KAIROS_INIT_IMAGE" \
    --build-arg MODEL="$MODEL" \
    --build-arg TRUSTED_BOOT="$TRUSTED_BOOT" \
    --build-arg KUBERNETES_DISTRO="$KUBERNETES_DISTRO" \
    --build-arg VERSION="$VERSION" \
    -t "$OS_IMAGE"

# --- 4: ISO -----------------------------------------------------------------
# auroraboot reads the OS image straight out of the local docker daemon, which
# is why it needs the socket bind-mounted. It writes into /output.
echo "==> [4/4] auroraboot build-iso -> $OUTPUT_DIR"
mkdir -p "$OUTPUT_DIR"
docker run --rm \
    -v /var/run/docker.sock:/var/run/docker.sock \
    -v "$OUTPUT_DIR:/output" \
    "$AURORABOOT_IMAGE" --debug \
    build-iso --output /output/ "docker:$OS_IMAGE"

# auroraboot runs as root inside its container, so everything it wrote to the
# bind mount is owned by root and the invoking user cannot delete or move it
# without sudo. Hand it back using a throwaway container from the same image,
# which already has the root privileges needed to chown. Doing it in a
# container rather than on the host keeps this script sudo-free.
docker run --rm \
    -v "$OUTPUT_DIR:/output" \
    --entrypoint chown \
    "$AURORABOOT_IMAGE" -R "$(id -u):$(id -g)" /output

# OUTPUT_DIR is reused across runs, so pick the newest ISO rather than the
# first one the filesystem happens to hand back. Taking the first line with a
# parameter expansion rather than `| head -1` keeps `head` from closing the
# pipe early, which under `pipefail` would fail the script on sort's SIGPIPE.
isos="$(find "$OUTPUT_DIR" -maxdepth 1 -name '*.iso' -printf '%T@ %p\n' | sort -rn)"
iso="${isos%%$'\n'*}"
iso="${iso#* }"
if [[ -z "$iso" ]]; then
    echo "error: auroraboot reported success but no ISO landed in $OUTPUT_DIR" >&2
    exit 1
fi

echo
echo "ISO: $iso"
echo
echo "Run a qemu test against it with, for example:"
echo "  export ISO=$iso MEMORY=5000 CPUS=4 DRIVE_SIZE=50000"
echo "  cd tests && go run github.com/onsi/ginkgo/v2/ginkgo -v \\"
echo "    --label-filter \"provider-decentralized-k8s\" --fail-fast -r ./..."
