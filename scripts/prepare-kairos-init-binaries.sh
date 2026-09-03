#!/usr/bin/env bash
# Populate kairos-init/pkg/bundled/binaries/ so `go build ./kairos-init`
# has files to //go:embed.
#
# kairos-init embeds default and FIPS binaries side by side and picks
# between them at install time based on the user's --fips flag. Because
# bundled_fips.go's build constraint is `!riscv64` (not `fips`), every
# non-riscv64 build needs both binaries/ and binaries/fips/ populated
# regardless of VARIANT.

set -euo pipefail

: "${ARCH:?ARCH must be set (amd64, arm64, or riscv64)}"
: "${BIN_SOURCE:?BIN_SOURCE must point at the dist/linux-<arch>/ output}"
if [[ "$ARCH" != "riscv64" ]]; then
    : "${BIN_SOURCE_FIPS:?BIN_SOURCE_FIPS must point at the dist/linux-<arch>-fips/ output}"
fi

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEST_ROOT="$REPO_ROOT/kairos-init/pkg/bundled/binaries"
DEST_FIPS="$DEST_ROOT/fips"
mkdir -p "$DEST_ROOT"
if [[ "$ARCH" != "riscv64" ]]; then
    mkdir -p "$DEST_FIPS"
fi

# --- External binary versions ---
# Single source of truth for the edgevpn pin: kairos-init/EDGEVPN_VERSION.
# kairos-init/Makefile reads it from the same place. Bumping edgevpn =
# editing that one line. An EDGEVPN_VERSION env var still overrides it for
# one-off local runs.
EDGEVPN_VERSION_FILE="$REPO_ROOT/kairos-init/EDGEVPN_VERSION"
: "${EDGEVPN_VERSION:=$(cat "$EDGEVPN_VERSION_FILE" 2>/dev/null)}"
if [ -z "$EDGEVPN_VERSION" ]; then
    echo "edgevpn version missing from $EDGEVPN_VERSION_FILE and not passed via EDGEVPN_VERSION" >&2
    exit 1
fi

cp "$BIN_SOURCE/kairos" "$DEST_ROOT/kairos"
cp "$BIN_SOURCE/kairos-installer" "$DEST_ROOT/kairos-installer"
cp "$BIN_SOURCE/provider-kairos" "$DEST_ROOT/provider-kairos"
if [[ "$ARCH" != "riscv64" ]]; then
    cp "$BIN_SOURCE_FIPS/kairos" "$DEST_FIPS/kairos"
    cp "$BIN_SOURCE_FIPS/kairos-installer" "$DEST_FIPS/kairos-installer"
    cp "$BIN_SOURCE_FIPS/provider-kairos" "$DEST_FIPS/provider-kairos"
fi

# --- Fetch defaults from external repos (edgevpn only) ---
tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

fetch() {
    # $1 = project name (used in owner/repo and tarball prefix)
    # $2 = version tag
    # $3 = destination dir under kairos-init/pkg/bundled/binaries
    # $4 = tarball suffix (empty for default, "-fips" for FIPS)
    # $5 = arch (in tarball URL; may differ from Go GOARCH, e.g. edgevpn uses x86_64)
    # $6 = owner (default: kairos-io)
    local project=$1 version=$2 dest=$3 suffix=${4:-} url_arch=${5:-$ARCH} owner=${6:-kairos-io}
    local url="https://github.com/${owner}/${project}/releases/download/${version}/${project}-${version}-Linux-${url_arch}${suffix}.tar.gz"
    local staging="$tmpdir/$dest"
    local tarball="$tmpdir/${dest}-${project}${suffix}.tar.gz"
    mkdir -p "$staging"
    echo "  fetching $project $version${suffix:+ ($suffix)} for $url_arch -> $dest"

    # A GitHub release exists the moment its tag is pushed, but the assets
    # attached to it can take minutes to finish uploading. A version bump
    # that lands inside that window gets a 404 from a URL that is perfectly
    # valid a few minutes later, so the failure reads as "the bump is wrong"
    # rather than "the asset is not there yet" (kairos-io/kairos#4316). The
    # budget here used to be --retry 5 --retry-delay 2, about 10 seconds,
    # against an observed upload window of 3m40s.
    #
    # Download to a file instead of piping straight into tar. curl retries
    # the whole request, so on a mid-transfer reset the retried bytes are
    # appended to what tar has already consumed and the stream is corrupt.
    # A 404 does not hit that, because -f writes no body, but a dropped
    # connection does.
    if ! curl -fsSL --retry 30 --retry-all-errors --retry-delay 10 \
        --retry-max-time 300 -o "$tarball" "$url"; then
        echo "  failed to download $url" >&2
        echo "  if $owner/$project $version was published in the last few minutes," >&2
        echo "  its release assets may still be uploading; retry once they are up" >&2
        return 1
    fi

    tar -xzf "$tarball" -C "$staging"
    rm -f "$tarball"
}

# edgevpn amd64 tarball is named x86_64; arm64 and riscv64 match GOARCH.
# (Do not "fix" arm64 to aarch64 -- that asset does not exist in the
# edgevpn release, and curl -fsSL will 404.)
case "$ARCH" in
  amd64) edgevpn_arch=x86_64 ;;
  *)     edgevpn_arch=$ARCH ;;
esac

# Defaults (external repos only; the in-tree kairos multi-call, its FIPS
# twin, kairos-installer and provider-kairos were cp'd from BIN_SOURCE /
# BIN_SOURCE_FIPS above).
fetch edgevpn "$EDGEVPN_VERSION" default "" "$edgevpn_arch" mudler

# No FIPS variant of edgevpn.

# Move each fetched executable into the right destination dir.
for src in "$tmpdir/default" "$tmpdir/fips"; do
    [ -d "$src" ] || continue
    dest="$DEST_ROOT"
    [ "$(basename "$src")" = "fips" ] && dest="$DEST_FIPS"
    for f in "$src"/*; do
        [ -f "$f" ] || continue
        if [[ -x "$f" && ! -d "$f" ]]; then
            mv "$f" "$dest/$(basename "$f")"
        fi
    done
done

# --- version-info.yaml (read by kairos-init at runtime) ---
cat > "$DEST_ROOT/version-info.yaml" <<YAML
kairos-agent: ${AGENT_VERSION:-in-tree}
immucore: ${IMMUCORE_VERSION:-in-tree}
kcrypt-discovery-challenger: ${KCRYPT_DISCOVERY_VERSION:-in-tree}
kairos-installer: ${INSTALLER_VERSION:-in-tree}
provider-kairos: ${PROVIDER_KAIROS_VERSION:-in-tree}
edgevpn: ${EDGEVPN_VERSION}
YAML

echo "Populated $DEST_ROOT with:"
ls -1 "$DEST_ROOT"
if [[ "$ARCH" != "riscv64" ]]; then
    echo "Populated $DEST_FIPS with:"
    ls -1 "$DEST_FIPS"
fi
