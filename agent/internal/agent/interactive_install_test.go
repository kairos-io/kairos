package agent

import (
	"os"
	"path/filepath"

	sdkConstants "github.com/kairos-io/kairos/v4/sdk/constants"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkInstall "github.com/kairos-io/kairos/v4/sdk/types/install"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"

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

var _ = Describe("InteractiveInstall", func() {
	var configDir, marker string
	var log sdkLogger.KairosLogger

	BeforeEach(func() {
		dir := GinkgoT().TempDir()

		configDir = filepath.Join(dir, "config")
		Expect(os.MkdirAll(configDir, 0o755)).To(Succeed())

		// Stand in for the installer binary, so a delegation is observable
		// without one of the real installers being present.
		marker = filepath.Join(dir, "installer-ran")
		bin := filepath.Join(dir, "kairos-installer")
		Expect(os.WriteFile(bin, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755)).To(Succeed())
		GinkgoT().Setenv(sdkConstants.InstallerEnvVar, bin)

		log = sdkLogger.NewKairosLogger("test", "debug", true)
	})

	writeConfig := func(body string) {
		Expect(os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(body), 0o644)).To(Succeed())
	}

	It("installs without asking when install.auto is set", func() {
		writeConfig("#cloud-config\ninstall:\n  auto: true\n  device: /dev/null\n")

		err := InteractiveInstall(false, "", log, configDir)

		Expect(marker).ToNot(BeAnExistingFile())
		// The install itself is refused, this config declares no users. What
		// matters here is that the interactive installer was never reached.
		Expect(err).To(HaveOccurred())
	})

	It("delegates to the installer when install.auto is not set", func() {
		writeConfig("#cloud-config\ninstall:\n  device: /dev/null\n")

		Expect(InteractiveInstall(false, "", log, configDir)).To(Succeed())
		Expect(marker).To(BeAnExistingFile())
	})

	It("delegates to the installer when there is no config", func() {
		Expect(InteractiveInstall(false, "", log, configDir)).To(Succeed())
		Expect(marker).To(BeAnExistingFile())
	})
})
