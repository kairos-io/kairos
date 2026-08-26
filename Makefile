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

# VERSION is the human-readable version string every binary reports.
# Format: git describe (e.g. v1.2.3, or v1.2.3-5-gabc1234 between tags, plus
# a "-dirty" suffix if the tree has uncommitted changes). Falls back to "dev"
# outside a git checkout.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

# LDFLAGS default: strip debug/symbol tables and inject the version. Override
# to add your own -X flags; if you override LDFLAGS, remember to include -X
# for the version or every binary will report "dev".
LDFLAGS ?= -s -w -X github.com/kairos-io/kairos/v4/internal/version.Version=$(VERSION)

.PHONY: test
test: kairos-init-embed-stubs
	$(GO) test ./...

# kairos-init/pkg/bundled/bundled.go uses //go:embed binaries/*, and
# bundled_fips.go uses //go:embed binaries/fips/* (excluded on riscv64).
# Compiling the package -- which `go test ./...` does even if the tests
# don't inspect the bytes -- requires those files to exist. For unit
# tests we do not need REAL binaries, only files, so touch zero-byte
# placeholders. Guarded with `test -e || touch` so a preceding real
# `make binaries` is never clobbered.
.PHONY: kairos-init-embed-stubs
kairos-init-embed-stubs:
	@mkdir -p kairos-init/pkg/bundled/binaries/fips
	@for f in kairos provider-kairos kairos-installer edgevpn version-info.yaml; do \
	    [ -e kairos-init/pkg/bundled/binaries/$$f ] || : > kairos-init/pkg/bundled/binaries/$$f; \
	done
	@for f in kairos provider-kairos; do \
	    [ -e kairos-init/pkg/bundled/binaries/fips/$$f ] || : > kairos-init/pkg/bundled/binaries/fips/$$f; \
	done

.PHONY: tidy
tidy:
	$(GO) mod tidy

# ============================================================================
# Local workflow lint. Mirrors the two checks CI runs on every push:
#
#   * lint.yaml calls the kairos-io/linting-composite-action, which under
#     the hood runs cytopia/yamllint against .github/workflows/. Rule set
#     comes from the repo-level .yamllint file. `make lint-workflows-yaml`
#     produces the same output; xargs's exit-code-123 is what CI trips on.
#
#   * actionlint validates step/job-level GitHub Actions semantics that
#     yamllint does not touch (unknown contexts, bad matrix keys, missing
#     inputs). Not currently a CI job on this repo, but the errors it
#     catches manifest as workflow "startup_failure" once GitHub tries to
#     load the file. Adding it locally is the cheapest gate against that.
#
# Both use the same images the CI check uses (or an equivalent), so a green
# `make lint-workflows` is a strong signal the next push will not trip the
# lint job. Requires Docker.
# ============================================================================

.PHONY: lint-workflows lint-workflows-yaml lint-workflows-actions
lint-workflows: lint-workflows-yaml lint-workflows-actions
	@echo 'workflow lint: ok'

# yamllint runs twice: strict (warnings -> errors) over the canonical
# pipeline files (pr.yaml, master.yaml, release.yaml and every
# `_*.yaml` reusable we own) so a warning that tomorrow's yamllint
# image promotes to error cannot slip through, and non-strict over
# the leftover independent workflows (upload-cloud-images.yaml,
# reusable-qemu-test.yaml, scorecards.yaml, etc.) to preserve the CI
# contract without dragging in unrelated cleanup.
lint-workflows-yaml:
	@find .github/workflows -maxdepth 1 \
	    \( -name 'pr.yaml' -o -name 'master.yaml' -o -name 'release.yaml' -o -name '_*.yaml' \) \
	    -print0 \
	    | xargs -0 -r -n1 docker run --rm -v "$$PWD":/work -w /work cytopia/yamllint --strict
	@find .github/workflows -maxdepth 1 \( -name '*.yml' -o -name '*.yaml' \) \
	    ! -name 'pr.yaml' ! -name 'master.yaml' ! -name 'release.yaml' \
	    ! -name '_*.yaml' -print0 \
	    | xargs -0 -r -n1 docker run --rm -v "$$PWD":/work -w /work cytopia/yamllint

# actionlint over the canonical pipeline only. reusable-qemu-test.yaml
# and upload-cloud-images.yaml carry pre-existing shellcheck-through-
# actionlint findings we do not want to fix as drive-by, and CI does
# not currently run actionlint on them either.
lint-workflows-actions:
	@docker run --rm -v "$$PWD":/repo -w /repo rhysd/actionlint -color \
	    $$(find .github/workflows -maxdepth 1 \
	        \( -name 'pr.yaml' -o -name 'master.yaml' -o -name 'release.yaml' -o -name '_*.yaml' \))

# ============================================================================
# Release targets. Nothing here publishes; publishing is the workflow's job.
#
# Pipeline:
#   1. `make kairos`      -- build the multi-call kairos.
#   2. `make kcrypt-challenger` -- build the k8s challenger server.
#   3. `make standalones` -- build the backward-compat entry points
#                            (immucore, kairos-agent,
#                            kcrypt-discovery-challenger). Temporary --
#                            nothing in the pipeline consumes these,
#                            they only exist for tarball backward compat.
#   4. `make kairos-init` -- fetch externals + prep + compile
#                            kairos-init. Needs the multi-call kairos
#                            for BOTH default and FIPS variants already
#                            present under dist/linux-<arch>{,-fips}/.
#
# All build targets honour ARCH (default: host arch) and VARIANT (default:
# `default`, or `fips`). Setting ARCH to something other than the host
# produces a cross-compiled binary; ARCH matching the host is a native
# build. Same target either way -- Go picks the toolchain.
#
# Each step is per (ARCH, VARIANT) except kairos-init which is
# per ARCH (embeds both variants; ships from the default cell only).
#
# `make binaries` is a local convenience that runs the whole pipeline
# in one go. CI drives the steps individually across matrix cells.
# ============================================================================

DIST_DIR ?= dist
DOCKER   ?= docker
ARCH     ?= $(shell go env GOARCH)
VARIANT  ?= default

# Where compiled binaries land for a given ARCH+VARIANT.
DIST_SUBDIR := linux-$(ARCH)$(if $(filter fips,$(VARIANT)),-fips)
DIST_ARCH_DIR := $(DIST_DIR)/$(DIST_SUBDIR)

# Build env for the current ARCH+VARIANT. FIPS binaries use
# GOEXPERIMENT=boringcrypto; on a non-arm64 host, arm64 FIPS also needs
# the aarch64 gcc (installed by the CI job that runs FIPS-arm64 builds).
BUILD_ENV := GOOS=linux GOARCH=$(ARCH) CGO_ENABLED=0
ifeq ($(VARIANT),fips)
  BUILD_ENV += GOEXPERIMENT=boringcrypto
  ifeq ($(ARCH),arm64)
    BUILD_ENV += CC=aarch64-linux-gnu-gcc
  endif
endif

GO_BUILD = $(BUILD_ENV) $(GO) build $(GOFLAGS) -ldflags='$(LDFLAGS)'

# Multi-call binary. Symlink it (or hard-link) as immucore, kairos-agent,
# or kcrypt-discovery-challenger to invoke a specific sub-tool by argv[0].
# The form `kairos <sub-tool> [args]` also works.
.PHONY: kairos
kairos: | $(DIST_ARCH_DIR)
	$(GO_BUILD) -o $(DIST_ARCH_DIR)/kairos ./cmd/kairos

# In-cluster kcrypt challenger server (kubernetes controller manager).
# Shipped as its own container image; not linked into the multi-call binary.
.PHONY: kcrypt-challenger
kcrypt-challenger: | $(DIST_ARCH_DIR)
	$(GO_BUILD) -o $(DIST_ARCH_DIR)/kcrypt-challenger ./cmd/kcrypt-challenger

# Interactive installer (bubbletea TUI). Embedded by kairos-init into
# /system/installer/kairos-installer inside the OS image, invoked by
# `kairos-agent interactive-install`. Not shipped as its own container
# image; only the binary matters.
.PHONY: kairos-installer
kairos-installer: | $(DIST_ARCH_DIR)
	$(GO_BUILD) -o $(DIST_ARCH_DIR)/kairos-installer ./installer

# Layout a real deployment ships: one kairos binary + argv[0] symlinks.
.PHONY: symlinks
symlinks: kairos
	ln -sf kairos $(DIST_ARCH_DIR)/immucore
	ln -sf kairos $(DIST_ARCH_DIR)/kairos-agent
	ln -sf kairos $(DIST_ARCH_DIR)/kcrypt-discovery-challenger

# Backward-compat entry points. Only reachable by explicitly running
# `make standalones`; CI does not build these and the release archives
# do not ship them. Kept as a local escape hatch until the main.go
# files under agent/, immucore/, and kcrypt/cmd/discovery/ are deleted.
.PHONY: standalones
standalones: | $(DIST_ARCH_DIR)
	$(GO_BUILD) -o $(DIST_ARCH_DIR)/immucore                    ./immucore
	$(GO_BUILD) -o $(DIST_ARCH_DIR)/kairos-agent                ./agent
	$(GO_BUILD) -o $(DIST_ARCH_DIR)/kcrypt-discovery-challenger ./kcrypt/cmd/discovery

# Populate kairos-init/pkg/bundled/binaries/. Expects the multi-call
# kairos for BOTH default and FIPS variants to already be present at
# dist/linux-<arch>/kairos and dist/linux-<arch>-fips/kairos.
# FIPS is skipped on riscv64 (bundled_fips.go's build tag excludes it).
.PHONY: kairos-init-embed
kairos-init-embed:
	ARCH=$(ARCH) VARIANT=$(VARIANT) \
	    BIN_SOURCE=$(CURDIR)/$(DIST_DIR)/linux-$(ARCH) \
	    BIN_SOURCE_FIPS=$(CURDIR)/$(DIST_DIR)/linux-$(ARCH)-fips \
	    scripts/prepare-kairos-init-binaries.sh

.PHONY: kairos-init
kairos-init: kairos-init-embed
	$(GO_BUILD) -o $(DIST_DIR)/linux-$(ARCH)/kairos-init ./kairos-init

# Local convenience: build the whole pipeline for one arch. Mirrors CI:
# the multi-call kairos and kcrypt-challenger run per (arch, variant),
# kairos-init runs once per arch (it embeds both variants and ships
# out of the default directory). FIPS is skipped on riscv64.
# Standalones are not part of this -- run `make standalones` explicitly
# if you want them.
.PHONY: binaries
binaries:
	$(MAKE) kairos kcrypt-challenger kairos-installer ARCH=$(ARCH) VARIANT=default
ifneq ($(ARCH),riscv64)
	$(MAKE) kairos kcrypt-challenger ARCH=$(ARCH) VARIANT=fips
endif
	$(MAKE) kairos-init ARCH=$(ARCH) VARIANT=default

# All default-variant arches. Rarely useful locally; more of a convenience
# to reproduce what release CI does across the default matrix.
ARCHES ?= amd64 arm64 riscv64
.PHONY: binaries-all
binaries-all:
	@set -e; for arch in $(ARCHES); do \
	  echo "=== binaries: linux/$$arch (default) ==="; \
	  $(MAKE) binaries ARCH=$$arch; \
	done

$(DIST_ARCH_DIR):
	mkdir -p $@

# tar.gz-package binaries under dist/$(DIST_SUBDIR) into
# dist/archives/<binary>-<version>-linux-<arch>[-fips].tar.gz plus a
# per-arch-variant checksums file. The BINARIES list is derived from
# whatever is actually present under DIST_ARCH_DIR, so `archives` can
# run against the output of any single pipeline step in isolation
# (kairos, standalones, kairos-init, ...) or against the umbrella
# `binaries` build.
.PHONY: archives
archives:
	@mkdir -p $(DIST_DIR)/archives
	@set -e; for f in $(DIST_ARCH_DIR)/*; do \
	  [ -f "$$f" ] || continue; \
	  b=$$(basename $$f); \
	  tar -czf $(DIST_DIR)/archives/$$b-$(VERSION)-$(DIST_SUBDIR).tar.gz \
	      -C $(DIST_ARCH_DIR) $$b; \
	done
	@cd $(DIST_DIR)/archives && \
	  sha256sum *-$(VERSION)-$(DIST_SUBDIR).tar.gz \
	  > checksums-$(DIST_SUBDIR).txt

# --- container images ---
#
# Local (buildx --load) container image builds. CI overrides with --push
# and multi-platform. Both Dockerfiles pick up their binary from
# dist/linux-<TARGETARCH>/, which the corresponding build step
# (`make kcrypt-challenger`, `make kairos-init`) has populated.

IMAGE_REGISTRY ?= ghcr.io/kairos-io/kairos
IMAGE_TAG      ?= dev

.PHONY: image-kcrypt-challenger
image-kcrypt-challenger: kcrypt-challenger
	$(DOCKER) buildx build --load \
	  --platform linux/$(ARCH) \
	  --build-arg TARGETARCH=$(ARCH) \
	  -t $(IMAGE_REGISTRY)/kcrypt-challenger:$(IMAGE_TAG) \
	  -f cmd/kcrypt-challenger/Dockerfile .

.PHONY: image-kairos-init
image-kairos-init: kairos-init
	$(DOCKER) buildx build --load \
	  --platform linux/$(ARCH) \
	  --build-arg TARGETARCH=$(ARCH) \
	  -t $(IMAGE_REGISTRY)/kairos-init:$(IMAGE_TAG) \
	  -f kairos-init/Dockerfile .

.PHONY: images
images: image-kcrypt-challenger image-kairos-init

.PHONY: clean
clean:
	rm -rf $(DIST_DIR)
