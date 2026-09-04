# Variables
IMMUCORE_VERSION := v0.20.4
AGENT_VERSION := v2.31.4
KCRYPT_DISCOVERY_CHALLENGER_VERSION := v0.13.4
PROVIDER_KAIROS_VERSION := v2.16.4
# Single source of truth for the edgevpn pin. scripts/prepare-kairos-init-binaries.sh
# reads it from the same place. Bumping edgevpn = editing that one line.
EDGEVPN_VERSION := $(shell cat EDGEVPN_VERSION)
ifeq ($(EDGEVPN_VERSION),)
$(error edgevpn version missing from kairos-init/EDGEVPN_VERSION)
endif
INSTALLER_VERSION := v0.1.5
ARCH ?= $(shell uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
BINARY_NAMES := kairos-agent immucore kcrypt-discovery-challenger provider-kairos
OUTPUT_DIR := pkg/bundled/binaries
OUTPUT_DIR_FIPS := pkg/bundled/binaries/fips

# UPX compression: set SKIP_UPX to any non-empty value to disable compression
# and to skip requiring the upx executable in prepare.
# URLs for binaries
define URL_TEMPLATE
https://github.com/kairos-io/$1/releases/download/$2/$1-$2-Linux-$(ARCH)$3.tar.gz
endef

kairos-agent_URL := $(call URL_TEMPLATE,kairos-agent,$(AGENT_VERSION))
immucore_URL := $(call URL_TEMPLATE,immucore,$(IMMUCORE_VERSION))
kcrypt-discovery-challenger_URL := $(call URL_TEMPLATE,kcrypt-discovery-challenger,$(KCRYPT_DISCOVERY_CHALLENGER_VERSION))
provider-kairos_URL := $(call URL_TEMPLATE,provider-kairos,$(PROVIDER_KAIROS_VERSION))

kairos-agent-fips_URL := $(call URL_TEMPLATE,kairos-agent,$(AGENT_VERSION),-fips)
immucore-fips_URL := $(call URL_TEMPLATE,immucore,$(IMMUCORE_VERSION),-fips)
kcrypt-discovery-challenger-fips_URL := $(call URL_TEMPLATE,kcrypt-discovery-challenger,$(KCRYPT_DISCOVERY_CHALLENGER_VERSION),-fips)
provider-kairos-fips_URL := $(call URL_TEMPLATE,provider-kairos,$(PROVIDER_KAIROS_VERSION),-fips)

# A GitHub release exists the moment its tag is pushed, but the assets
# attached to it can take minutes to finish uploading. A version bump that
# lands inside that window gets a 404 from a URL that is perfectly valid a
# few minutes later, so the failure reads as "the bump is wrong" rather than
# "the asset is not there yet" (kairos-io/kairos#4316). This budget used to
# be --retry 5 --retry-delay 2, about 10 seconds, against an observed upload
# window of 3m40s.
CURL_RETRY := --retry 30 --retry-all-errors --retry-delay 10 --retry-max-time 300

# fetch_tarball <url> <destination dir>
#
# Downloads to a temp file before extracting rather than piping into tar.
# curl retries the whole request, so on a mid-transfer reset the retried
# bytes are appended to whatever tar has already consumed and the stream is
# corrupt. A 404 does not hit that, because -f writes no body, but a dropped
# connection does.
define fetch_tarball
	tmp=$$(mktemp); \
	if ! curl -fsSL $(CURL_RETRY) -o "$$tmp" "$(1)"; then \
		rm -f "$$tmp"; \
		echo "Failed to download $(1)"; \
		echo "If that release was published in the last few minutes, its assets may still be uploading."; \
		exit 1; \
	fi; \
	tar -xzf "$$tmp" -C $(2); \
	rm -f "$$tmp"
endef

.PHONY: all prepare download compress cleanup version-info

all: prepare download compress cleanup version-info

# Clean the output directory
prepare:
	@echo "Cleaning up the output directory..."
	@rm -rf $(OUTPUT_DIR)
	@if [ -z "$(SKIP_UPX)" ] && ! command -v upx >/dev/null 2>&1; then \
		echo "Error: upx binary is not available. Please install upx."; \
		exit 1; \
	fi
	@echo "Binary versions:"
	@echo "  kairos-agent: $(AGENT_VERSION)"
	@echo "  immucore: $(IMMUCORE_VERSION)"
	@echo "  kcrypt-discovery-challenger: $(KCRYPT_DISCOVERY_CHALLENGER_VERSION)"
	@echo "  provider-kairos: $(PROVIDER_KAIROS_VERSION)"
	@echo "  edgevpn: $(EDGEVPN_VERSION)"

# Ensure the bundled directory exists
$(OUTPUT_DIR):
	@echo "Creating directory $(OUTPUT_DIR)..."
	@mkdir -p $(OUTPUT_DIR)

download: download-edgevpn

# Set FIPS_BINARIES variable based on ARCH
ifeq ($(ARCH),riscv64)
FIPS_BINARIES :=
else
FIPS_BINARIES := $(addprefix $(OUTPUT_DIR_FIPS)/, $(addsuffix -fips, $(BINARY_NAMES)))
endif

download: $(addprefix $(OUTPUT_DIR)/, $(BINARY_NAMES)) $(FIPS_BINARIES) download-edgevpn download-kairos-installer
ifeq ($(ARCH),riscv64)
	@echo "Skipping FIPS binaries download for riscv64."
endif

# Download edgevpn by itself
download-edgevpn:
	@echo "Downloading and extracting edgevpn for architecture $(ARCH)..."
	@mkdir -p $(OUTPUT_DIR)
	@# Unfortunately edgevpn uses x86_64 instead of amd64 so we need to do some string manipulation here
	@$(call fetch_tarball,https://github.com/mudler/edgevpn/releases/download/$(EDGEVPN_VERSION)/edgevpn-$(EDGEVPN_VERSION)-Linux-$(shell echo $(ARCH) | sed -e 's/amd64/x86_64/').tar.gz,$(OUTPUT_DIR))

# Download kairos-installer (no FIPS variant; same binary for all image flavors)
download-kairos-installer:
	@echo "Downloading and extracting kairos-installer for architecture $(ARCH)..."
	@mkdir -p $(OUTPUT_DIR)
	@$(call fetch_tarball,https://github.com/kairos-io/kairos-installer/releases/download/$(INSTALLER_VERSION)/kairos-installer-$(INSTALLER_VERSION)-Linux-$(ARCH).tar.gz,$(OUTPUT_DIR))

# Download each binary
$(OUTPUT_DIR)/%:
	@echo "Downloading and extracting $* for architecture $(ARCH)..."
	@mkdir -p $(OUTPUT_DIR)
	@$(call fetch_tarball,$($*_URL),$(OUTPUT_DIR))

# Download each FIPS binary
$(OUTPUT_DIR_FIPS)/%-fips:
	@echo "Downloading and extracting $*-fips for architecture $(ARCH)..."
	@mkdir -p $(OUTPUT_DIR_FIPS)
	@$(call fetch_tarball,$($*-fips_URL),$(OUTPUT_DIR_FIPS))


# Run upx to compress binaries unless SKIP_UPX is non-empty

compress:
	@if [ -z "$(SKIP_UPX)" ]; then \
		echo "Running upx compress..."; \
		upx -q --best --lzma $(addprefix $(OUTPUT_DIR)/, $(BINARY_NAMES) edgevpn kairos-installer ); \
		if [ "$(ARCH)" != "riscv64" ]; then upx -q --best --lzma $(addprefix $(OUTPUT_DIR_FIPS)/, $(BINARY_NAMES)); fi; \
	else \
		echo "Skipping upx compression (SKIP_UPX is set)"; \
	fi
# Remove non-binary files from the output directory
cleanup:
	@echo "Cleaning up non-binary files..."
	@find $(OUTPUT_DIR) -type f ! -executable -delete
	@if [ "$(ARCH)" != "riscv64" ]; then find $(OUTPUT_DIR_FIPS) -type f ! -executable -delete; fi

# Add version info config to the bundled binaries dir into a single yaml file
version-info:
	@echo "Adding version info to the bundled binaries directory..."
	@mkdir -p $(OUTPUT_DIR)
	@echo "kairos-agent: $(AGENT_VERSION)" > $(OUTPUT_DIR)/version-info.yaml
	@echo "immucore: $(IMMUCORE_VERSION)" >> $(OUTPUT_DIR)/version-info.yaml
	@echo "kcrypt-discovery-challenger: $(KCRYPT_DISCOVERY_CHALLENGER_VERSION)" >> $(OUTPUT_DIR)/version-info.yaml
	@echo "provider-kairos: $(PROVIDER_KAIROS_VERSION)" >> $(OUTPUT_DIR)/version-info.yaml
	@echo "edgevpn: $(EDGEVPN_VERSION)" >> $(OUTPUT_DIR)/version-info.yaml
	@echo "version-info.yaml created in $(OUTPUT_DIR)"

# Run tests
test:
	@echo "Running tests..."
	@ginkgo -v ./pkg/validation

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	@ginkgo -v -cover ./pkg/validation

# Run linter
lint:
	@echo "Running linter..."
	@golangci-lint run ./...

# Run linter with fix
lint-fix:
	@echo "Running linter with auto-fix..."
	@golangci-lint run --fix ./...
