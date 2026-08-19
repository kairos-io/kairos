# Base Hadron release OCI image used as the ISO base AND the install source.
# Override on the CLI: make build-test-iso BASE_IMAGE=quay.io/kairos/...
BASE_IMAGE ?= ghcr.io/kairos-io/hadron:v0.3.0
KAIROS_INIT ?= quay.io/kairos/kairos-init:latest
AURORABOOT_IMAGE ?= quay.io/kairos/auroraboot
E2E_DIR := tests/e2e
BUILD_DIR := $(E2E_DIR)/build
INJECTED_IMAGE := kairos-agent-e2e:local

.PHONY: build-agent-e2e build-test-iso run-e2e-tests-with-iso run-e2e-tests

# 1. Build the under-test agent for the guest (linux/amd64) into the e2e dir.
build-agent-e2e:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o $(E2E_DIR)/kairos-agent .

# 2. Build the injected OCI image and turn it into an ISO under tests/e2e/build.
# NOTE: build-iso resolves oci://kairos-agent-e2e:local from the host docker
# daemon via the mounted socket. If AuroraBoot cannot find the local image,
# load it into a reachable registry first or switch the source scheme.
build-test-iso: build-agent-e2e
	docker build \
		--build-arg BASE=$(BASE_IMAGE) \
		--build-arg KAIROS_INIT=$(KAIROS_INIT) \
		-t $(INJECTED_IMAGE) \
		-f $(E2E_DIR)/Dockerfile.inject $(E2E_DIR)
	mkdir -p $(BUILD_DIR)
	docker run --rm --privileged \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v $(PWD)/$(BUILD_DIR):/output \
		$(AURORABOOT_IMAGE) build-iso --output /output oci:$(INJECTED_IMAGE)
	@echo "ISO(s) in $(BUILD_DIR):"; ls -1 $(BUILD_DIR)/*.iso

# 3. Run the suite against a prebuilt ISO. Pass ISO=... or it auto-discovers
#    the newest ISO in tests/e2e/build.
run-e2e-tests-with-iso:
	ISO=$${ISO:-$$(ls -t $(CURDIR)/$(BUILD_DIR)/*.iso 2>/dev/null | head -1)} \
		BASE_IMAGE=$(BASE_IMAGE) \
		go test -tags e2e ./$(E2E_DIR)/ -count=1 -v --ginkgo.label-filter="insecure-registry || partition-validation" --timeout 30m

# 4. Build then run.
run-e2e-tests: build-test-iso run-e2e-tests-with-iso
