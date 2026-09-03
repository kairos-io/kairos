package mos_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
)

const (
	bundleImagePlaceholder = "__BUNDLE_IMAGE__"
	legacyBundleImage      = "quay.io/kairos/ci-temp-images:bundles-test"
)

func createBundleDatasource(bundleImage string) string {
	content, err := os.ReadFile("assets/bundles.yaml")
	ExpectWithOffset(1, err).ToNot(HaveOccurred())

	template := string(content)
	ExpectWithOffset(1, strings.Count(template, bundleImagePlaceholder)).To(
		Equal(1), "bundle datasource must contain exactly one image placeholder",
	)

	config, err := os.CreateTemp("", "bundles-*.yaml")
	ExpectWithOffset(1, err).ToNot(HaveOccurred())
	defer func() { _ = os.Remove(config.Name()) }()

	_, err = config.WriteString(strings.Replace(template, bundleImagePlaceholder, bundleImage, 1))
	ExpectWithOffset(1, err).ToNot(HaveOccurred())
	ExpectWithOffset(1, config.Close()).To(Succeed())

	return CreateDatasource(config.Name())
}

var _ = Describe("kairos bundles test", Label("bundles"), func() {
	var vm VM
	var datasource string
	var bundleImage string

	BeforeEach(func() {
		// pull_request_target resolves reusable workflows from the base branch.
		// The fallback keeps this PR and local runs compatible with the old
		// workflow; after merge CI always supplies the digest-pinned reference.
		bundleImage = getEnvOrDefault("BUNDLE_IMAGE", legacyBundleImage)
		datasource = createBundleDatasource(bundleImage)
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
					// This seems to not load the /etc/profile.d/systemd-sysext.sh sot it cant use the
					// SYSTEMD_SYSEXT_HIERARCHIES that we set
					// So we run the command with login so it loads the envs
					// Without the SYSTEMD_SYSEXT_HIERARCHIES set to the proper paths it will not
					// find any extensions
					out, _ := vm.Sudo("bash -l -c \"systemd-sysext\"")
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
				Expect(ipfsV).To(ContainSubstring("0.41.0"))
			})

			By("checking that there are no duplicate entries in the config (issue#2019)", func() {
				out, _ := vm.Sudo("cat /oem/90_custom.yaml")
				Expect(strings.Count(out, bundleImage)).To(Equal(1))
			})
		})
	})
})
