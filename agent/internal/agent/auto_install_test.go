package agent

import (
	"errors"
	"os"
	"path/filepath"

	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkInstall "github.com/kairos-io/kairos/v4/sdk/types/install"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("autoInstallRequested", func() {
	It("is false without a config", func() {
		Expect(autoInstallRequested(nil)).To(BeFalse())
	})

	It("is false without an install block", func() {
		Expect(autoInstallRequested(&sdkConfig.Config{})).To(BeFalse())
	})

	It("is false when auto is not set", func() {
		Expect(autoInstallRequested(&sdkConfig.Config{Install: &sdkInstall.Install{}})).To(BeFalse())
	})

	It("is true when auto is set", func() {
		Expect(autoInstallRequested(&sdkConfig.Config{Install: &sdkInstall.Install{Auto: true}})).To(BeTrue())
	})
})

var _ = Describe("AutoInstall", func() {
	var configDir string

	BeforeEach(func() {
		configDir = filepath.Join(GinkgoT().TempDir(), "config")
		Expect(os.MkdirAll(configDir, 0o755)).To(Succeed())
	})

	writeConfig := func(body string) {
		Expect(os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(body), 0o644)).To(Succeed())
	}

	It("reports nothing to do when no config was written", func() {
		installed, _, err := AutoInstall("", false, configDir)
		Expect(err).ToNot(HaveOccurred())
		Expect(installed).To(BeFalse())
	})

	It("reports nothing to do for a config without install.auto", func() {
		writeConfig("#cloud-config\ninstall:\n  device: /dev/nonexistent\n")

		installed, _, err := AutoInstall("", false, configDir)
		Expect(err).ToNot(HaveOccurred())
		Expect(installed).To(BeFalse())
	})

	It("reports nothing to do when install.auto is explicitly false", func() {
		writeConfig("#cloud-config\ninstall:\n  auto: false\n  device: /dev/nonexistent\n")

		installed, _, err := AutoInstall("", false, configDir)
		Expect(err).ToNot(HaveOccurred())
		Expect(installed).To(BeFalse())
	})

	// install.auto set means AutoInstall owns the boot, so it reports true even
	// when the install itself fails. Swap the install out rather than letting
	// the spec run a real one: which check RunInstall fails first depends on
	// the host filesystem, e.g. /etc/kairos/.nousers makes the user check pass
	// and the spec fall through into an install against /dev/nonexistent.
	It("takes over the boot when install.auto is set", func() {
		writeConfig("#cloud-config\ninstall:\n  auto: true\n  device: /dev/nonexistent\n")

		sentinel := errors.New("install ran")
		var got *sdkConfig.Config
		original := runInstallFn
		runInstallFn = func(cc *sdkConfig.Config) error {
			got = cc
			return sentinel
		}
		DeferCleanup(func() { runInstallFn = original })

		installed, cc, err := AutoInstall("", false, configDir)
		Expect(installed).To(BeTrue())
		Expect(err).To(MatchError(sentinel))
		Expect(got).ToNot(BeNil())
		Expect(got.Install.Auto).To(BeTrue())
		Expect(got.Install.Device).To(Equal("/dev/nonexistent"))
		Expect(cc).To(BeIdenticalTo(got))
	})

	It("hands the scanned config back when there is nothing to install", func() {
		writeConfig("#cloud-config\ninstall:\n  device: /dev/nonexistent\n")

		installed, cc, err := AutoInstall("", false, configDir)
		Expect(err).ToNot(HaveOccurred())
		Expect(installed).To(BeFalse())
		Expect(cc).ToNot(BeNil())
		Expect(cc.Install.Device).To(Equal("/dev/nonexistent"))
	})
})
