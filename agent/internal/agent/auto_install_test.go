package agent

import (
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
		installed, err := AutoInstall("", false, configDir)
		Expect(err).ToNot(HaveOccurred())
		Expect(installed).To(BeFalse())
	})

	It("reports nothing to do for a config without install.auto", func() {
		writeConfig("#cloud-config\ninstall:\n  device: /dev/nonexistent\n")

		installed, err := AutoInstall("", false, configDir)
		Expect(err).ToNot(HaveOccurred())
		Expect(installed).To(BeFalse())
	})

	It("reports nothing to do when install.auto is explicitly false", func() {
		writeConfig("#cloud-config\ninstall:\n  auto: false\n  device: /dev/nonexistent\n")

		installed, err := AutoInstall("", false, configDir)
		Expect(err).ToNot(HaveOccurred())
		Expect(installed).To(BeFalse())
	})

	// install.auto set means AutoInstall owns the boot, so it reports true even
	// when the install itself fails. The error is the user check RunInstall
	// performs first, which is as far as an install gets without a real disk:
	// reaching it is what proves the branch fired.
	It("takes over the boot when install.auto is set", func() {
		writeConfig("#cloud-config\ninstall:\n  auto: true\n  device: /dev/nonexistent\n")

		installed, err := AutoInstall("", false, configDir)
		Expect(installed).To(BeTrue())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("user"))
	})
})
