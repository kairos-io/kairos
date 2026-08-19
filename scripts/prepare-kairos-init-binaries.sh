#!/usr/bin/env bash
# Populate kairos-init/pkg/bundled/binaries/ so `go build ./kairos-init`
# has files to //go:embed.
#
# In-tree binaries (kairos-agent, immucore, kcrypt-discovery-challenger)
# come from BIN_SOURCE, which is normally the dist/linux-<arch>/ output
# of `make binaries-early`.
#
# Still-external binaries (provider-kairos, kairos-installer, edgevpn) are
# downloaded from their GitHub Releases at the versions pinned below.
# When those repos are absorbed into the monorepo too, replace the fetch
# steps with cp from BIN_SOURCE, same as the three above.
#
# For VARIANT=fips, also populates kairos-init/pkg/bundled/binaries/fips/
# with the FIPS-boringcrypto builds. FIPS is not supported on riscv64;
# on that arch the script skips the FIPS step.

set -euo pipefail

: "${ARCH:?ARCH must be set (amd64, arm64, or riscv64)}"
: "${VARIANT:=default}"
: "${BIN_SOURCE:?BIN_SOURCE must point at the dist/linux-<arch>/ output}"

if [[ "$VARIANT" != "default" && "$VARIANT" != "fips" ]]; then
    echo "VARIANT must be 'default' or 'fips', got: $VARIANT" >&2
    exit 1
fi

if [[ "$VARIANT" == "fips" && "$ARCH" == "riscv64" ]]; then
    echo "FIPS is not supported on riscv64; nothing to prepare." >&2
    exit 0
fi

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEST_ROOT="$REPO_ROOT/kairos-init/pkg/bundled/binaries"
DEST="$DEST_ROOT"
if [[ "$VARIANT" == "fips" ]]; then
    DEST="$DEST_ROOT/fips"
fi
mkdir -p "$DEST"

# --- External binary versions ---
# Bump these when the corresponding repo cuts a release you want to consume.
# Follow-up: absorb provider-kairos and kairos-installer into the monorepo
# so these fetches are replaced by in-tree cp; only edgevpn (github.com/mudler)
# stays external.
: "${PROVIDER_KAIROS_VERSION:=v2.16.4}"
: "${INSTALLER_VERSION:=v0.1.5}"
: "${EDGEVPN_VERSION:=v0.35.4}"

# --- Copy in-tree binaries ---
# For default variant, copy from BIN_SOURCE (which is our normal build output).
# For FIPS variant, we still need the FIPS versions from the pre-monorepo
# release tarballs until the release pipeline builds FIPS variants in-tree
# and hands them here. Fall through to the external-fetch path below.
if [[ "$VARIANT" == "default" ]]; then
    for b in kairos-agent immucore kcrypt-discovery-challenger; do
        cp "$BIN_SOURCE/$b" "$DEST/$b"
    done
fi

# --- Fetch externals ---
tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

fetch() {
    # $1 = kairos-io project name (used in owner/repo and tarball prefix)
    # $2 = version tag
    # $3 = tarball suffix (empty for default, "-fips" for FIPS)
    # $4 = arch (in tarball URL; may differ from Go GOARCH, e.g. edgevpn uses x86_64)
    # $5 = owner (default: kairos-io)
    local project=$1 version=$2 suffix=${3:-} url_arch=${4:-$ARCH} owner=${5:-kairos-io}
    local url="https://github.com/${owner}/${project}/releases/download/${version}/${project}-${version}-Linux-${url_arch}${suffix}.tar.gz"
    echo "  fetching $project $version${suffix:+ ($suffix)} for $url_arch"
    curl -fsSL --retry 5 --retry-all-errors --retry-delay 2 "$url" | tar -xz -C "$tmpdir"
}

# edgevpn tarballs use x86_64/aarch64 instead of amd64/arm64.
case "$ARCH" in
  amd64) edgevpn_arch=x86_64 ;;
  arm64) edgevpn_arch=aarch64 ;;
  riscv64) edgevpn_arch=riscv64 ;;
esac

if [[ "$VARIANT" == "default" ]]; then
    fetch provider-kairos    "$PROVIDER_KAIROS_VERSION" ""
    fetch kairos-installer   "$INSTALLER_VERSION"       ""
    fetch edgevpn            "$EDGEVPN_VERSION"         "" "$edgevpn_arch" mudler
else
    # FIPS embeds: kairos-agent, immucore, kcrypt-discovery-challenger, provider-kairos
    # are all-FIPS. edgevpn and kairos-installer have no FIPS variant and are not
    # in the FIPS embed list. Until the release pipeline builds FIPS in-tree, fetch
    # the four FIPS binaries from their per-repo releases.
    fetch kairos-agent                 "${AGENT_VERSION:-v2.31.4}"    "-fips"
    fetch immucore                     "${IMMUCORE_VERSION:-v0.20.4}" "-fips"
    fetch kcrypt-discovery-challenger  "${KCRYPT_DISCOVERY_VERSION:-v0.13.4}" "-fips"
    fetch provider-kairos              "$PROVIDER_KAIROS_VERSION"     "-fips"
fi

# Move each expected file from the tar-extracted tmpdir into DEST.
# The tarballs sometimes carry extra files (READMEs, etc.); the tarball
# extraction path preserves them under $tmpdir. We only copy binaries.
for f in "$tmpdir"/*; do
    name=$(basename "$f")
    if [[ -x "$f" && ! -d "$f" ]]; then
        mv "$f" "$DEST/$name"
    fi
done

# --- version-info.yaml (default variant only; kairos-init reads it) ---
if [[ "$VARIANT" == "default" ]]; then
    cat > "$DEST/version-info.yaml" <<YAML
kairos-agent: ${AGENT_VERSION:-in-tree}
immucore: ${IMMUCORE_VERSION:-in-tree}
kcrypt-discovery-challenger: ${KCRYPT_DISCOVERY_VERSION:-in-tree}
provider-kairos: ${PROVIDER_KAIROS_VERSION}
kairos-installer: ${INSTALLER_VERSION}
edgevpn: ${EDGEVPN_VERSION}
YAML
fi

echo "Populated $DEST with:"
ls -1 "$DEST"
