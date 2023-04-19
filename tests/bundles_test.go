package mos_test

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
)

var _ = Describe("kairos bundles test", Label("bundles-test"), func() {
	var vm VM

	BeforeEach(func() {
		if os.Getenv("CLOUD_INIT") == "" || !filepath.IsAbs(os.Getenv("CLOUD_INIT")) {
			Fail("CLOUD_INIT must be set and must be pointing to a file as an absolute path")
		}
		_, vm = startVM()
		vm.EventuallyConnects(1200)
	})

	AfterEach(func() {
		vm.Destroy(nil)
	})

	Context("reboots and passes functional tests", func() {
		BeforeEach(func() {
			expectDefaultService(vm)
			expectStartedInstallation(vm)
			expectRebootedToActive(vm)
		})

		It("passes tests", func() {
			By("checking the grubenv file", func() {
				By("checking after-install hook triggered")

				Eventually(func() string {
					out, _ := vm.Sudo("cat /oem/grubenv")
					return out
				}, 20*time.Minute, 1*time.Second).Should(
					Or(
						ContainSubstring("foobarzz"),
					))
			})

			By("checking if it has custom cmdline", func() {
				By("waiting reboot and checking cmdline is present")
				Eventually(func() string {
					out, _ := vm.Sudo("cat /proc/cmdline")
					return out
				}, 10*time.Minute, 1*time.Second).Should(
					Or(
						ContainSubstring("foobarzz"),
					))
			})

			By("checking if it has kubo extension", func() {
				Eventually(func() string {
					out, _ := vm.Sudo("systemd-sysext")
					return out
				}, 3*time.Minute, 10*time.Second).Should(ContainSubstring("kubo"), func() string {
					// Debug output in case of an error
					result := ""
					out, _ := vm.Sudo("cat /etc/os-release")
					result = result + fmt.Sprintf("os-release:\n%s\n", out)

					out, _ = vm.Sudo("cat /usr/local/lib/extensions/kubo/usr/lib/extension-release.d/extension-release.kubo")
					result = result + fmt.Sprintf("extension-release.kubo:\n%s\n", out)

					out, _ = vm.Sudo("systemd-sysext status")
					result = result + fmt.Sprintf("systemd-sysext status:\n%s\n", out)

					return result
				})

				ipfsV, err := vm.Sudo("ipfs version")
				Expect(err).ToNot(HaveOccurred(), ipfsV)
				Expect(ipfsV).To(ContainSubstring("0.15.0"))
			})
		})
	})
})
