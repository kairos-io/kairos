GO_VERSION      ?= $(shell grep '^go ' go.mod | awk '{print $$2}' | cut -d. -f1,2)
LUET_VERSION    ?= 0.34.0
OSV_SCANNER_IMG ?= ghcr.io/google/osv-scanner:v2.3.5

.PHONY: test
test:
	$(eval LUET_TMP := $(shell mktemp -d))
	$(eval LUET_CTR := $(shell docker create quay.io/luet/base:$(LUET_VERSION)))
	docker cp $(LUET_CTR):/usr/bin/luet $(LUET_TMP)/luet
	docker rm $(LUET_CTR)
	docker run --rm \
		-v $(CURDIR):/build \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v $(LUET_TMP)/luet:/usr/local/bin/luet \
		-w /build \
		-e CGO_ENABLED=0 \
		golang:$(GO_VERSION) \
		go run github.com/onsi/ginkgo/v2/ginkgo run --fail-fast --covermode=atomic --coverprofile=coverage.out -p -r ./...
	rm -rf $(LUET_TMP)

.PHONY: osv-scan
osv-scan:
	docker run --rm -v $(CURDIR):/src $(OSV_SCANNER_IMG) scan source --lockfile=/src/go.mod
