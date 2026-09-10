package webui

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The web UI is a frontend of the installer, so an install driven from the
// browser has to honour the same --source the installer was started with.
// Without it a `--no-tui` boot pointed at a private registry silently
// installs whatever the submitted cloud-config names instead.
var _ = Describe("manualInstallArgs", func() {
	It("forwards the installer's source", func() {
		args := manualInstallArgs("oci://foo:bar",
			&FormData{InstallationDevice: "/dev/sda"}, "/tmp/cfg.yaml")

		Expect(args).To(Equal([]string{
			"manual-install", "--source", "oci://foo:bar",
			"--device", "/dev/sda", "/tmp/cfg.yaml",
		}))
	})

	It("leaves the source out when the installer has none", func() {
		args := manualInstallArgs("",
			&FormData{InstallationDevice: "/dev/sda"}, "/tmp/cfg.yaml")

		Expect(args).To(Equal([]string{
			"manual-install", "--device", "/dev/sda", "/tmp/cfg.yaml",
		}))
	})

	// urfave/cli stops parsing flags at the first positional, so the config
	// path has to stay last.
	It("keeps the finish-action flags ahead of the config path", func() {
		args := manualInstallArgs("oci://foo:bar",
			&FormData{Reboot: "on", PowerOff: "on", InstallationDevice: "/dev/vda"},
			"/tmp/cfg.yaml")

		Expect(args).To(Equal([]string{
			"manual-install", "--source", "oci://foo:bar",
			"--poweroff", "--reboot", "--device", "/dev/vda", "/tmp/cfg.yaml",
		}))
	})
})
