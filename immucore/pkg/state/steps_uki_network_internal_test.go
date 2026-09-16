package state

import (
	"os"
	"path/filepath"

	internalUtils "github.com/kairos-io/kairos/v4/immucore/internal/utils"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("remoteKMSConfigured", func() {
	var dir string

	write := func(body string) {
		Expect(os.WriteFile(filepath.Join(dir, "kcrypt.yaml"), []byte(body), 0644)).To(Succeed())
	}

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		internalUtils.KLog = logger.NewKairosLogger("immucore-test", "error", true)
	})

	It("says no when there is no configuration at all", func() {
		Expect(remoteKMSConfigured(dir)).To(BeFalse())
	})

	It("says no for a config that has nothing to do with kcrypt", func() {
		write("#cloud-config\ninstall:\n  device: /dev/vda\n")
		Expect(remoteKMSConfigured(dir)).To(BeFalse())
	})

	It("says no for local TPM encryption, which needs no network", func() {
		write("#cloud-config\nkcrypt:\n  tpm_device: /dev/tpmrm0\n")
		Expect(remoteKMSConfigured(dir)).To(BeFalse())
	})

	It("says yes for a challenger server", func() {
		write("#cloud-config\nkcrypt:\n  challenger:\n    challenger_server: \"http://kms.example.org:8082\"\n")
		Expect(remoteKMSConfigured(dir)).To(BeTrue())
	})

	It("says yes for mDNS discovery, which also needs the network", func() {
		write("#cloud-config\nkcrypt:\n  challenger:\n    mdns: true\n")
		Expect(remoteKMSConfigured(dir)).To(BeTrue())
	})

	It("says no when mdns is explicitly off and no server is given", func() {
		write("#cloud-config\nkcrypt:\n  challenger:\n    mdns: false\n")
		Expect(remoteKMSConfigured(dir)).To(BeFalse())
	})
})
