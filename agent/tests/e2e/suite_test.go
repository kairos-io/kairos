//go:build e2e

package e2e_test

import (
	"os"
	"strconv"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const registryContainerName = "kairos-agent-e2e-registry"

// regPort is the host port the registry is published on;
// sourceRepo is the in-registry repo path (e.g. "kairos/source:test").
var (
	regPort    int
	sourceRepo string
)

// baseImage is the Hadron release OCI image used both as the registry source
// and (separately, at ISO build time) as the ISO base. Override with BASE_IMAGE.
func baseImage() string {
	if v := os.Getenv("BASE_IMAGE"); v != "" {
		return v
	}
	return "ghcr.io/kairos-io/hadron:v0.3.0" // non-kairosified Hadron base; kairos-init runs over it at ISO build time
}

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "kairos-agent e2e suite")
}

var _ = BeforeSuite(func() {
	port, err := freePort()
	Expect(err).ToNot(HaveOccurred())
	repo, err := startRegistry(registryContainerName, baseImage(), port)
	Expect(err).ToNot(HaveOccurred())
	regPort = port
	sourceRepo = repo
})

var _ = AfterSuite(func() {
	_ = stopRegistry(registryContainerName)
})

// sourceURI builds the oci: source the guest uses to reach the host registry.
// 10.0.2.2 is the QEMU user-net host gateway; .sslip.io makes ggcr default to
// HTTPS (not RFC1918 auto-HTTP), so --allow-insecure-registries actually matters.
func sourceURI() string {
	return "oci:10.0.2.2.sslip.io:" + strconv.Itoa(regPort) + "/" + sourceRepo
}
