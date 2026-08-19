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

.PHONY: clean
clean:
	rm -rf $(BIN_DIR)
