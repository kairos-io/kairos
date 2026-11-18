# AuroraBoot Makefile
# Development and build targets for AuroraBoot

.PHONY: help dev ui-install ui-build ui-test build-go build clean clean-ui clean-go run-docker build-docker test openapi fmt lint install-deps

# Default target
help: ## Show this help message
	@echo "AuroraBoot Development Makefile"
	@echo "================================"
	@echo ""
	@echo "Available targets:"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@echo ""
	@echo "Common workflows:"
	@echo "  make build                          # Build React UI + Go binary"
	@echo "  make dev                            # Build and run server in Docker"
	@echo "  make openapi                        # Regenerate Swagger spec from annotations"

# Development target - builds everything and runs the server in Docker
dev: build ## Build everything and run AuroraBoot server in Docker
	@echo "Starting AuroraBoot server in development mode..."
	@mkdir -p data
	docker run --rm \
		--privileged \
		--net host \
		--entrypoint /usr/sbin/auroraboot \
		-v $(PWD)/auroraboot:/usr/sbin/auroraboot \
		-v $(PWD)/data:/data \
		-v /dev:/dev \
		-v /var/run/docker.sock:/var/run/docker.sock \
		quay.io/kairos/auroraboot web --data-dir /data $(ARGS)

# Install React UI dependencies
ui-install: ## Install React UI npm dependencies
	@echo "Installing UI dependencies..."
	cd ui && npm install

# Build React UI bundle. vite.config.ts already writes to internal/ui/dist
# so the Go build can pick it up via go:embed.
ui-build: ui-install ## Build the React UI bundle
	@echo "Building React UI..."
	cd ui && npm run build
	@echo "UI built to internal/ui/dist"

# Run the React UI vitest suite
ui-test: ui-install ## Run the React UI test suite
	@echo "Running UI tests..."
	cd ui && npm test

# Build Go binary
build-go: ## Build the Go binary
	@echo "Building Go binary..."
	go build -tags ui -ldflags "-X main.version=v0.0.0" -o auroraboot
	@echo "Go binary built successfully!"

# Build everything
build: ui-build build-go ## Build React UI and Go binary

# Clean UI build artifacts (keeps node_modules)
clean-ui: ## Clean UI build artifacts (keeps node_modules)
	@echo "Cleaning UI build artifacts..."
	rm -rf internal/ui/dist
	rm -rf ui/dist
	@echo "UI artifacts cleaned!"

# Clean UI dependencies (removes node_modules)
clean-ui-deps: ## Clean UI dependencies (removes node_modules)
	@echo "Cleaning UI dependencies..."
	rm -rf ui/node_modules
	@echo "UI dependencies cleaned!"

# Clean Go build artifacts
clean-go: ## Clean Go build artifacts
	@echo "Cleaning Go build artifacts..."
	rm -f auroraboot
	@echo "Go artifacts cleaned!"

# Clean everything (including node_modules)
clean: clean-ui-deps clean-ui clean-go ## Clean all build artifacts including dependencies

# Build Docker image
build-docker: ## Build the Docker image
	docker build -t auroraboot:local .

# Run tests (UI dist is embedded via go:embed, so it must exist before `go test`)
test: ui-build ## Run Go tests
	go test ./...

# Run the operator-backed builder e2e suite. Requires kind + docker + kubectl.
# The suite is guarded by the operator_e2e build tag so `make test` skips it.
.PHONY: test-operator-e2e
test-operator-e2e: ## Run the kind-based operator builder e2e suite
	go test -tags=operator_e2e ./test/operator/... -v -timeout=30m

# Type-check every build-tagged tree without running it. A tag that no default
# build, vet or lint passes is invisible to all of them, so the tree can stop
# compiling while CI stays green -- which is what happened to test/operator
# between 2026-07-24 and kairos-io/kairos#5402. Add one line here per new
# tagged tree; the CI job calls this target, not the tags.
.PHONY: vet-tagged
vet-tagged: ## Type-check build-tagged trees no default build compiles
	go vet -tags=operator_e2e ./test/operator/...

# Run the phonehome tier: AuroraBoot in a container and two QEMU VMs it
# installs and upgrades. Needs docker, qemu, swtpm, OVMF and KVM; FIRMWARE
# overrides the OVMF code image: the default is Arch's path, Debian and Ubuntu
# use /usr/share/OVMF/OVMF_CODE_4M.fd. The e2e suite builds the auroraboot:test
# image itself.
.PHONY: test-e2e-phonehome
test-e2e-phonehome: ## Run the phonehome e2e tier on two VMs
	KVM=true FIRMWARE=$${FIRMWARE:-/usr/share/edk2/x64/OVMF_CODE.4m.fd} go run github.com/onsi/ginkgo/v2/ginkgo -v --label-filter phonehome-vm --timeout 100m --grace-period 10m ./e2e

# Install development dependencies
install-deps: ## Install development dependencies
	@echo "Installing Go dependencies..."
	go mod download
	@echo "Installing UI dependencies..."
	cd ui && npm install
	@echo "Dependencies installed!"

# Format code
fmt: ## Format Go code
	go fmt ./...

# Lint code
lint: ## Lint Go code
	golangci-lint run

# Generate OpenAPI spec from swag annotations on internal/cmd/web.go
openapi: ## Regenerate Swagger/OpenAPI documentation
	go tool swag init -g internal/cmd/web.go --output docs --parseDependency --parseInternal --parseDepth 2

# Backwards-compat alias
swagger: openapi

# Helm chart targets. The chart lives in deploy/helm/auroraboot and its
# unit tests use the helm-unittest plugin.
HELM_CHART_DIR := deploy/helm/auroraboot

helm-chart-lint: ## Lint the AuroraBoot helm chart
	helm lint $(HELM_CHART_DIR) -f $(HELM_CHART_DIR)/examples/values-min.yaml

helm-chart-template: ## Render the helm chart with example values (for eyeballing)
	helm template ab $(HELM_CHART_DIR) -n kairos-operator -f $(HELM_CHART_DIR)/examples/values-min.yaml

helm-chart-test: ## Run helm-unittest suites against the AuroraBoot chart
	@helm plugin list 2>/dev/null | grep -q '^unittest' || { \
		echo "helm-unittest plugin missing. Install with:"; \
		echo "  helm plugin install https://github.com/helm-unittest/helm-unittest --version 1.2.1 --verify=false"; \
		exit 1; \
	}
	helm unittest $(HELM_CHART_DIR)

helm-chart-check: helm-chart-lint helm-chart-test ## Lint + unit test the helm chart
