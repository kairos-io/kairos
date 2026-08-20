#!/usr/bin/env bash
# Populate kairos-init/pkg/bundled/binaries/ so `go build ./kairos-init`
# has files to //go:embed.
#
# kairos-init is a single-flavor binary that embeds BOTH default and
# FIPS versions of the other Kairos binaries; it switches between them
# at install time based on the user's --fips flag. So every kairos-init
# build (except on riscv64) needs both binaries/ and binaries/fips/
# populated regardless of VARIANT. bundled_fips.go's build constraint
# is `!riscv64`, not `fips`, which is what forces this.
#
# What lands where:
#   binaries/kairos-agent, immucore, kcrypt-discovery-challenger
#     Copied from BIN_SOURCE (dist/linux-<arch>/), which the
#     binaries-early Make target populated with in-tree default builds.
#   binaries/provider-kairos, kairos-installer, edgevpn
#     Downloaded from their pre-monorepo GitHub Releases at the
#     versions pinned below. When those repos are absorbed too the
#     fetches become cp-from-BIN_SOURCE.
#   binaries/fips/kairos-agent, immucore, kcrypt-discovery-challenger,
#   provider-kairos
#     Downloaded from FIPS release tarballs of the respective repos.
#     Kairos-init switches to these at install time when --fips is set.
#     Skipped on riscv64 because bundled_fips.go is excluded there.

set -euo pipefail

: "${ARCH:?ARCH must be set (amd64, arm64, or riscv64)}"
: "${BIN_SOURCE:?BIN_SOURCE must point at the dist/linux-<arch>/ output}"

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEST_ROOT="$REPO_ROOT/kairos-init/pkg/bundled/binaries"
DEST_FIPS="$DEST_ROOT/fips"
mkdir -p "$DEST_ROOT"
if [[ "$ARCH" != "riscv64" ]]; then
    mkdir -p "$DEST_FIPS"
fi

# --- External binary versions ---
# Bump these when the corresponding repo cuts a release you want to consume.
# Follow-up: absorb provider-kairos and kairos-installer into the monorepo
# so these fetches are replaced by in-tree cp; only edgevpn (github.com/mudler)
# stays external. FIPS versions of the in-tree binaries stay external until
# the release pipeline builds FIPS variants in-tree and hands them here.
: "${PROVIDER_KAIROS_VERSION:=v2.16.4}"
: "${INSTALLER_VERSION:=v0.1.5}"
: "${EDGEVPN_VERSION:=v0.35.4}"
: "${AGENT_FIPS_VERSION:=v2.31.4}"
: "${IMMUCORE_FIPS_VERSION:=v0.20.4}"
: "${KCRYPT_DISCOVERY_FIPS_VERSION:=v0.13.4}"

# --- Copy in-tree default binaries ---
for b in kairos-agent immucore kcrypt-discovery-challenger; do
    cp "$BIN_SOURCE/$b" "$DEST_ROOT/$b"
done

# --- Fetch defaults from external repos (provider-kairos, kairos-installer, edgevpn) ---
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
    mkdir -p "$staging"
    echo "  fetching $project $version${suffix:+ ($suffix)} for $url_arch -> $dest"
    curl -fsSL --retry 5 --retry-all-errors --retry-delay 2 "$url" | tar -xz -C "$staging"
}

# edgevpn tarballs use x86_64/aarch64 instead of amd64/arm64.
case "$ARCH" in
  amd64) edgevpn_arch=x86_64 ;;
  arm64) edgevpn_arch=aarch64 ;;
  riscv64) edgevpn_arch=riscv64 ;;
esac

# Defaults
fetch provider-kairos  "$PROVIDER_KAIROS_VERSION" default ""  ""             ""
fetch kairos-installer "$INSTALLER_VERSION"       default ""  ""             ""
fetch edgevpn          "$EDGEVPN_VERSION"         default ""  "$edgevpn_arch" mudler

# FIPS embeds (bundled_fips.go is compiled on every non-riscv64 build,
# so these files must exist for `go build` to satisfy //go:embed).
if [[ "$ARCH" != "riscv64" ]]; then
    fetch kairos-agent                 "$AGENT_FIPS_VERSION"           fips "-fips" ""              ""
    fetch immucore                     "$IMMUCORE_FIPS_VERSION"        fips "-fips" ""              ""
    fetch kcrypt-discovery-challenger  "$KCRYPT_DISCOVERY_FIPS_VERSION" fips "-fips" ""              ""
    fetch provider-kairos              "$PROVIDER_KAIROS_VERSION"      fips "-fips" ""              ""
fi

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
provider-kairos: ${PROVIDER_KAIROS_VERSION}
kairos-installer: ${INSTALLER_VERSION}
edgevpn: ${EDGEVPN_VERSION}
YAML

echo "Populated $DEST_ROOT with:"
ls -1 "$DEST_ROOT"
if [[ "$ARCH" != "riscv64" ]]; then
    echo "Populated $DEST_FIPS with:"
    ls -1 "$DEST_FIPS"
fi
