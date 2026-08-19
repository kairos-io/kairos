# Kairos monorepo build entrypoints.
#
# All Go binaries produced by this repo are declared here. CI runs these
# targets; local dev calls them the same way.
#
# Override flags on the command line, e.g.:
#   make GOFLAGS='-race' kairos
#   make LDFLAGS='-s -w -X main.Version=v0.0.1' all

GO      ?= go
GOFLAGS ?= -trimpath
BIN_DIR ?= bin

# VERSION is the human-readable version string every binary reports.
# Format: git describe (e.g. v1.2.3, or v1.2.3-5-gabc1234 between tags, plus
# a "-dirty" suffix if the tree has uncommitted changes). Falls back to "dev"
# outside a git checkout.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

# LDFLAGS default: strip debug/symbol tables and inject the version. Override
# to add your own -X flags; if you override LDFLAGS, remember to include -X
# for the version or every binary will report "dev".
LDFLAGS ?= -s -w -X github.com/kairos-io/kairos/internal/version.Version=$(VERSION)

.PHONY: all
all: kairos kairos-slim kcrypt-challenger

# Multi-call binary. Symlink it (or hard-link) as immucore, kairos-agent,
# or kcrypt-discovery-challenger to invoke a specific sub-tool by argv[0].
# The form `kairos <sub-tool> [args]` also works.
.PHONY: kairos
kairos:
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags='$(LDFLAGS)' -o $(BIN_DIR)/kairos ./cmd/kairos

# Slim variant intended for the initramfs: kairos-agent (and its transitive
# deps) is not linked in. Same binary otherwise; dispatches immucore and
# kcrypt-discovery-challenger.
.PHONY: kairos-slim
kairos-slim:
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -tags initramfs -ldflags='$(LDFLAGS)' -o $(BIN_DIR)/kairos-slim ./cmd/kairos

# In-cluster kcrypt challenger server (kubernetes controller manager).
# Shipped as its own container image; not linked into the multi-call binary.
.PHONY: kcrypt-challenger
kcrypt-challenger:
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags='$(LDFLAGS)' -o $(BIN_DIR)/kcrypt-challenger ./cmd/kcrypt-challenger

# Optional: standalone builds of each sub-tool. Not what a real deployment
# uses (that would use `kairos` plus argv[0] symlinks via `make symlinks`),
# but useful if a consumer wants one specific binary from a specific commit.
.PHONY: standalone
standalone:
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags='$(LDFLAGS)' -o $(BIN_DIR)/immucore ./immucore
	$(GO) build $(GOFLAGS) -ldflags='$(LDFLAGS)' -o $(BIN_DIR)/kairos-agent ./agent
	$(GO) build $(GOFLAGS) -ldflags='$(LDFLAGS)' -o $(BIN_DIR)/kcrypt-discovery-challenger ./kcrypt/cmd/discovery

# Layout a real deployment ships: one kairos binary + argv[0] symlinks.
# Depends on `kairos` (not `all`) so it does not build the slim/challenger
# variants unless you ask for them separately.
.PHONY: symlinks
symlinks: kairos
	ln -sf kairos $(BIN_DIR)/immucore
	ln -sf kairos $(BIN_DIR)/kairos-agent
	ln -sf kairos $(BIN_DIR)/kcrypt-discovery-challenger

.PHONY: test
test:
	$(GO) test ./...

.PHONY: tidy
tidy:
	$(GO) mod tidy

# ============================================================================
# Release targets: cross-compile the full binary set for a specific ARCH and
# optionally a FIPS variant, so CI and local dev can produce the same
# artifacts. Nothing here publishes; publishing is the workflow's job.
#
# Usage:
#   make binaries ARCH=amd64            # default variant for linux/amd64
#   make binaries ARCH=arm64 VARIANT=fips
#   make binaries-all                   # matrix run: default for every ARCH
#   make archives ARCH=amd64            # tar.gz-package the built binaries
#   make image-kcrypt-challenger ARCH=amd64  # docker buildx load (no push)
#   make image-kairos-init ARCH=amd64        # ditto
#   make images ARCH=amd64                    # both container images
# ============================================================================

DIST_DIR ?= dist
DOCKER   ?= docker
ARCH     ?= $(shell go env GOARCH)
VARIANT  ?= default

# Where the cross-compiled binaries land for a given ARCH+VARIANT.
DIST_SUBDIR := linux-$(ARCH)$(if $(filter fips,$(VARIANT)),-fips)
DIST_ARCH_DIR := $(DIST_DIR)/$(DIST_SUBDIR)

# Build env for the current ARCH+VARIANT. FIPS binaries use
# GOEXPERIMENT=boringcrypto; arm64 FIPS additionally needs the aarch64
# cross-linker (installed by the CI job that runs FIPS-arm64 builds).
CROSS_ENV := GOOS=linux GOARCH=$(ARCH) CGO_ENABLED=0
ifeq ($(VARIANT),fips)
  CROSS_ENV += GOEXPERIMENT=boringcrypto
  ifeq ($(ARCH),arm64)
    CROSS_ENV += CC=aarch64-linux-gnu-gcc
  endif
endif

CROSS_BUILD = $(CROSS_ENV) $(GO) build $(GOFLAGS) -ldflags='$(LDFLAGS)'

# The six binaries that build without any cross-binary preparation.
# kairos-init is built separately below because it //go:embeds them.
.PHONY: binaries-early
binaries-early: | $(DIST_ARCH_DIR)
	$(CROSS_BUILD) -o $(DIST_ARCH_DIR)/kairos                      ./cmd/kairos
	$(CROSS_BUILD) -tags initramfs -o $(DIST_ARCH_DIR)/kairos-slim ./cmd/kairos
	$(CROSS_BUILD) -o $(DIST_ARCH_DIR)/kcrypt-challenger           ./cmd/kcrypt-challenger
	$(CROSS_BUILD) -o $(DIST_ARCH_DIR)/immucore                    ./immucore
	$(CROSS_BUILD) -o $(DIST_ARCH_DIR)/kairos-agent                ./agent
	$(CROSS_BUILD) -o $(DIST_ARCH_DIR)/kcrypt-discovery-challenger ./kcrypt/cmd/discovery

# Populate kairos-init/pkg/bundled/binaries/ with what //go:embed needs:
# the three in-tree binaries we just built plus the still-external ones
# (provider-kairos, kairos-installer, edgevpn) curled from their releases.
# Runs BEFORE kairos-init compiles.
.PHONY: kairos-init-embed
kairos-init-embed: binaries-early
	ARCH=$(ARCH) VARIANT=$(VARIANT) BIN_SOURCE=$(CURDIR)/$(DIST_ARCH_DIR) \
	    scripts/prepare-kairos-init-binaries.sh

# kairos-init is built after its embed dir is populated. Version is the same
# monorepo version every other binary reports.
.PHONY: kairos-init-bin
kairos-init-bin: kairos-init-embed
	$(CROSS_BUILD) -o $(DIST_ARCH_DIR)/kairos-init ./kairos-init

# One arch+variant, all seven binaries. This is what a single CI matrix
# cell runs.
.PHONY: binaries
binaries: binaries-early kairos-init-bin

# All default-variant arches. Rarely useful locally; more of a convenience
# to reproduce what release CI does across the default matrix.
ARCHES ?= amd64 arm64 riscv64
.PHONY: binaries-all
binaries-all:
	@set -e; for arch in $(ARCHES); do \
	  echo "=== binaries: linux/$$arch (default) ==="; \
	  $(MAKE) binaries ARCH=$$arch VARIANT=default; \
	done

$(DIST_ARCH_DIR):
	mkdir -p $@

# tar.gz-package each binary produced by `make binaries` into
# dist/archives/<binary>-<version>-linux-<arch>[-fips].tar.gz plus a
# per-arch-variant checksums file.
BINARIES := kairos kairos-slim kcrypt-challenger immucore kairos-agent kcrypt-discovery-challenger kairos-init

.PHONY: archives
archives: binaries
	@mkdir -p $(DIST_DIR)/archives
	@set -e; for b in $(BINARIES); do \
	  tar -czf $(DIST_DIR)/archives/$$b-$(VERSION)-$(DIST_SUBDIR).tar.gz \
	      -C $(DIST_ARCH_DIR) $$b; \
	done
	cd $(DIST_DIR)/archives && \
	  sha256sum $(foreach b,$(BINARIES),$(b)-$(VERSION)-$(DIST_SUBDIR).tar.gz) \
	  > checksums-$(DIST_SUBDIR).txt

# --- container images ---
#
# Local (buildx --load) container image builds. CI overrides with --push
# and multi-platform. Both Dockerfiles pick up their binary from
# dist/linux-<TARGETARCH>/, which `make binaries-early` (kcrypt-challenger)
# or `make kairos-init-bin` (kairos-init) has populated.

IMAGE_REGISTRY ?= ghcr.io/kairos-io/kairos
IMAGE_TAG      ?= dev

.PHONY: image-kcrypt-challenger
image-kcrypt-challenger: binaries-early
	$(DOCKER) buildx build --load \
	  --platform linux/$(ARCH) \
	  --build-arg TARGETARCH=$(ARCH) \
	  -t $(IMAGE_REGISTRY)/kcrypt-challenger:$(IMAGE_TAG) \
	  -f cmd/kcrypt-challenger/Dockerfile .

.PHONY: image-kairos-init
image-kairos-init: kairos-init-bin
	$(DOCKER) buildx build --load \
	  --platform linux/$(ARCH) \
	  --build-arg TARGETARCH=$(ARCH) \
	  -t $(IMAGE_REGISTRY)/kairos-init:$(IMAGE_TAG) \
	  -f kairos-init/Dockerfile .

.PHONY: images
images: image-kcrypt-challenger image-kairos-init

.PHONY: clean
clean:
	rm -rf $(BIN_DIR) $(DIST_DIR)
