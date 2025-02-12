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

var _ = Describe("kairos bundles test", Label("bundles"), func() {
	var vm VM
	var datasource string

	BeforeEach(func() {
		datasource = CreateDatasource("assets/bundles.yaml")
		Expect(os.Setenv("DATASOURCE", datasource)).ToNot(HaveOccurred())

		_, vm = startVM()
		vm.EventuallyConnects(1200)
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			gatherLogs(vm)
			serial, _ := os.ReadFile(filepath.Join(vm.StateDir, "serial.log"))
			_ = os.MkdirAll("logs", os.ModePerm|os.ModeDir)
			_ = os.WriteFile(filepath.Join("logs", "serial.log"), serial, os.ModePerm)
			fmt.Println(string(serial))
		}

		err := vm.Destroy(nil)
		Expect(err).ToNot(HaveOccurred())

		Expect(os.Unsetenv("DATASOURCE")).ToNot(HaveOccurred())
		Expect(os.Remove(datasource)).ToNot(HaveOccurred())
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
					out, _ := vm.Sudo("cat /etc/kairos-release")
					result = result + fmt.Sprintf("kairos-release:\n%s\n", out)

					out, _ = vm.Sudo("cat /oem/90_custom.yaml")
					result = result + fmt.Sprintf("90_custom.yaml:\n%s\n", out)

					out, _ = vm.Sudo("cat /var/lib/extensions/kubo/usr/lib/extension-release.d/extension-release.kubo")
					result = result + fmt.Sprintf("extension-release.kubo:\n%s\n", out)

					out, _ = vm.Sudo("systemd-sysext status")
					result = result + fmt.Sprintf("systemd-sysext status:\n%s\n", out)

					return result
				})

				ipfsV, err := vm.Sudo("ipfs version")
				Expect(err).ToNot(HaveOccurred(), ipfsV)
				Expect(ipfsV).To(ContainSubstring("0.15.0"))
			})

			By("checking that there are no duplicate entries in the config (issue#2019)", func() {
				out, _ := vm.Sudo("cat /oem/90_custom.yaml")
				// https://pkg.go.dev/regexp/syntax
				// ?s -> "let . match \n (default false)"
				Expect(out).To(MatchRegexp("^(?s)(.*quay\\.io/kairos/ci-temp-images:bundles-test.*){1}$"))
				Expect(out).ToNot(MatchRegexp("(?s)quay.io/kairos/ci-temp-images.*quay.io/kairos/ci-temp-images"))
			})
		})
	})
})
