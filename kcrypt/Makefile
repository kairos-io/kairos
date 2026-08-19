# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= 0.0.1

# CHANNELS define the bundle channels used in the bundle.
# Add a new line here if you would like to change its default config. (E.g CHANNELS = "candidate,fast,stable")
# To re-generate a bundle for other specific channels without changing the standard setup, you can:
# - use the CHANNELS as arg of the bundle target (e.g make bundle CHANNELS=candidate,fast,stable)
# - use environment variables to overwrite this value (e.g export CHANNELS="candidate,fast,stable")
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif

# DEFAULT_CHANNEL defines the default channel used in the bundle.
# Add a new line here if you would like to change its default config. (E.g DEFAULT_CHANNEL = "stable")
# To re-generate a bundle for any other default channel without changing the default setup, you can:
# - use the DEFAULT_CHANNEL as arg of the bundle target (e.g make bundle DEFAULT_CHANNEL=stable)
# - use environment variables to overwrite this value (e.g export DEFAULT_CHANNEL="stable")
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

# IMAGE_TAG_BASE defines the docker.io namespace and part of the image name for remote images.
# This variable is used to construct full image tags for bundle and catalog images.
#
# For example, running 'make bundle-build bundle-push catalog-build catalog-push' will build and push both
# kairos.io/kcrypt-controller-bundle:$VERSION and kairos.io/kcrypt-controller-catalog:$VERSION.
IMAGE_TAG_BASE ?= quay.io/kairos/kcrypt-controller

# BUNDLE_IMG defines the image:tag used for the bundle.
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= $(IMAGE_TAG_BASE)-bundle:v$(VERSION)

# BUNDLE_GEN_FLAGS are the flags passed to the operator-sdk generate bundle command
BUNDLE_GEN_FLAGS ?= -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)

# USE_IMAGE_DIGESTS defines if images are resolved via tags or digests
# You can enable this value if you would like to use SHA Based Digests
# To enable set flag to true
USE_IMAGE_DIGESTS ?= false
ifeq ($(USE_IMAGE_DIGESTS), true)
	BUNDLE_GEN_FLAGS += --use-image-digests
endif

# Image URL to use all building/pushing image targets
IMG ?= controller:latest
# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.24.2

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" go test ./pkg/...

##@ Build

.PHONY: build
build: generate fmt vet ## Build manager binary.
	go build -o bin/manager main.go

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./main.go

.PHONY: docker-build
docker-build: test ## Build docker image with the manager.
	docker build -t ${IMG} .

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	docker push ${IMG}

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = true
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | kubectl apply -f -

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/default | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest

## Tool Versions
KUSTOMIZE_VERSION ?= v3.8.7
CONTROLLER_TOOLS_VERSION ?= v0.16.0

KUSTOMIZE_INSTALL_SCRIPT ?= "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	test -s $(LOCALBIN)/kustomize || { curl -s $(KUSTOMIZE_INSTALL_SCRIPT) | bash -s -- $(subst v,,$(KUSTOMIZE_VERSION)) $(LOCALBIN); }

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen || curl -L -v -Sso $(LOCALBIN)/controller-gen https://github.com/kubernetes-sigs/controller-tools/releases/download/$(CONTROLLER_TOOLS_VERSION)/controller-gen-linux-amd64
	chmod +x $(LOCALBIN)/controller-gen

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	test -s $(LOCALBIN)/setup-envtest || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

.PHONY: bundle
bundle: manifests kustomize ## Generate bundle manifests and metadata, then validate generated files.
	operator-sdk generate kustomize manifests -q
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/manifests | operator-sdk generate bundle $(BUNDLE_GEN_FLAGS)
	operator-sdk bundle validate ./bundle

.PHONY: bundle-build
bundle-build: ## Build the bundle image.
	docker build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

.PHONY: bundle-push
bundle-push: ## Push the bundle image.
	$(MAKE) docker-push IMG=$(BUNDLE_IMG)

.PHONY: opm
OPM = ./bin/opm
opm: ## Download opm locally if necessary.
ifeq (,$(wildcard $(OPM)))
ifeq (,$(shell which opm 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPM)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/v1.23.0/$${OS}-$${ARCH}-opm ;\
	chmod +x $(OPM) ;\
	}
else
OPM = $(shell which opm)
endif
endif

# A comma-separated list of bundle images (e.g. make catalog-build BUNDLE_IMGS=example.com/operator-bundle:v0.1.0,example.com/operator-bundle:v0.2.0).
# These images MUST exist in a registry and be pull-able.
BUNDLE_IMGS ?= $(BUNDLE_IMG)

# The image tag given to the resulting catalog image (e.g. make catalog-build CATALOG_IMG=example.com/operator-catalog:v0.2.0).
CATALOG_IMG ?= $(IMAGE_TAG_BASE)-catalog:v$(VERSION)

# Set CATALOG_BASE_IMG to an existing catalog image tag to add $BUNDLE_IMGS to that image.
ifneq ($(origin CATALOG_BASE_IMG), undefined)
FROM_INDEX_OPT := --from-index $(CATALOG_BASE_IMG)
endif

# Build a catalog image by adding bundle images to an empty catalog using the operator package manager tool, 'opm'.
# This recipe invokes 'opm' in 'semver' bundle add mode. For more information on add modes, see:
# https://github.com/operator-framework/community-operators/blob/7f1438c/docs/packaging-operator.md#updating-your-existing-operator
.PHONY: catalog-build
catalog-build: opm ## Build a catalog image.
	$(OPM) index add --container-tool docker --mode semver --tag $(CATALOG_IMG) --bundles $(BUNDLE_IMGS) $(FROM_INDEX_OPT)

# Push the catalog image.
.PHONY: catalog-push
catalog-push: ## Push a catalog image.
	$(MAKE) docker-push IMG=$(CATALOG_IMG)

CLUSTER_NAME?="kairos-challenger-e2e"

kind-setup:
	kind create cluster --name ${CLUSTER_NAME} || true
	$(MAKE) kind-setup-image

kind-setup-image: docker-build
	kind load docker-image --name $(CLUSTER_NAME) $(IMG)

kind-prepare-tests: kind-setup install undeploy-dev deploy-dev

.PHONY: deploy-dev
deploy-dev: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/dev | kubectl apply -f -

.PHONY: undeploy-dev
undeploy-dev: ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/dev | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

kubesplit: manifests kustomize
	rm -rf helm-chart
	mkdir helm-chart
	$(KUSTOMIZE) build config/default | kubesplit -helm helm-chart

##@ Kairos Image Build

# Kairos image build variables
KAIROS_IMAGE_TAG ?= kcrypt-challenger:latest
AURORABOOT_IMAGE ?= quay.io/kairos/auroraboot
ISO_NAME ?= challenger
BUILD_DIR = build
KEYS_DIR ?= $(HOME)/tmp/keys

# Optional build args for custom binaries (empty = use package versions)
# Set REF to build custom binaries from kairos-io repos (ref can be branch or commit)
IMMUCORE_REF ?=
KAIROS_AGENT_REF ?=
KAIROS_INIT_VERSION ?= v0.5.20
NO_CACHE ?=

.PHONY: kairos-image
kairos-image: ## Build Kairos image with challenger
	@echo "Building Kairos image..."
	@echo "Optional: Set IMMUCORE_REF or KAIROS_AGENT_REF to build custom binaries"
	docker build --progress=plain $(if $(NO_CACHE),--no-cache) -f Dockerfile.kairos-image -t $(KAIROS_IMAGE_TAG) \
		$(if $(IMMUCORE_REF),--build-arg IMMUCORE_REF=$(IMMUCORE_REF)) \
		$(if $(KAIROS_AGENT_REF),--build-arg KAIROS_AGENT_REF=$(KAIROS_AGENT_REF)) \
		--build-arg KAIROS_INIT_VERSION=$(KAIROS_INIT_VERSION) \
		.

.PHONY: genkeys
genkeys: ## Generate secure boot keys for UKI signing
	@echo "Generating keys in $(KEYS_DIR)..."
	@mkdir -p $(KEYS_DIR)
	@docker run --rm \
		-v "$(KEYS_DIR):/work/keys" \
		$(AURORABOOT_IMAGE) \
		genkey --skip-microsoft-certs-I-KNOW-WHAT-IM-DOING --expiration-in-days 365 -o /work/keys "$(KEYS_ORG)"
	@echo "Keys generated successfully in $(KEYS_DIR)"

.PHONY: kairos-iso
kairos-iso: kairos-image ## Build normal Kairos ISO using auroraboot
	@echo "Building ISO using auroraboot..."
	@mkdir -p $(BUILD_DIR)
	@docker run --rm \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v "$(CURDIR)/$(BUILD_DIR):/tmp/auroraboot" \
		$(AURORABOOT_IMAGE) \
		build-iso $(KAIROS_IMAGE_TAG)
	@echo "ISO built successfully in $(BUILD_DIR)/"
	@ls -lh $(BUILD_DIR)/*.iso 2>/dev/null || echo "Note: ISO files may have different naming"

.PHONY: kairos-iso-uki
kairos-iso-uki: kairos-image ## Build UKI (Unified Kernel Image) ISO with secure boot signing
	@echo "Building UKI ISO using auroraboot..."
	@mkdir -p $(BUILD_DIR)
	@if [ ! -d "$(KEYS_DIR)" ] || [ ! -f "$(KEYS_DIR)/db.key" ]; then \
		echo "Error: Keys directory not found or missing keys at $(KEYS_DIR)"; \
		echo "Keys are required for UKI ISO build. Run 'make genkeys' to generate them."; \
		exit 1; \
	fi
	@echo "Building UKI with secure boot signing..."; \
	docker run --rm \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v "$(CURDIR)/$(BUILD_DIR):/result" \
		-v "$(KEYS_DIR):/keys" \
		$(AURORABOOT_IMAGE) \
		build-uki -t iso -d /result/ \
		--public-keys /keys \
		--tpm-pcr-private-key /keys/tpm2-pcr-private.pem \
		--sb-key /keys/db.key \
		--sb-cert /keys/db.pem \
		--extend-cmdline "console=tty0 console=ttyS0,115200n8 rd.immucore.debug" \
		$(KAIROS_IMAGE_TAG)
	@echo "UKI ISO built successfully in $(BUILD_DIR)/"
	@ls -lh $(BUILD_DIR)/*.iso 2>/dev/null || echo "Note: ISO files may have different naming"

		#--extend-cmdline "console=tty0 console=ttyS0,115200n8 rd.immucore.debug kcrypt.challenger.challenger_server=http://192.168.122.1.challenger.sslip.io" \

##@ Cloud Init Datasource ISO

# Build cloud-init datasource ISO (for cloud-init NoCloud datasource)
# Usage: make datasource-iso CLOUD_CONFIG=tests/assets/qrcode.yaml
.PHONY: datasource-iso
datasource-iso: ## Build cloud-init datasource ISO for NoCloud
	@if [ -z "$(CLOUD_CONFIG)" ]; then \
		echo "Error: CLOUD_CONFIG is required. Usage: make datasource-iso CLOUD_CONFIG=path/to/config.yaml"; \
		exit 1; \
	fi
	@mkdir -p $(BUILD_DIR)
	@docker run --rm \
		-v "$(CURDIR)/$(BUILD_DIR):/build" \
		-v "$(CURDIR)/$(CLOUD_CONFIG):/tmp/user-data:ro" \
		quay.io/kairos/osbuilder-tools \
		sh -c "zypper in -y mkisofs && \
		       touch /build/meta-data && \
		       cp /tmp/user-data /build/user-data && \
		       mkisofs -output /build/ci.iso -volid cidata -joliet -rock /build/user-data /build/meta-data"
	@echo "Datasource ISO built: $(BUILD_DIR)/ci.iso"
