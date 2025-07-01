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

var _ = Describe("k3s upgrade manual test", Label("upgrade-latest-with-cli"), func() {
	var vm VM
	containerImage := os.Getenv("CONTAINER_IMAGE")
	var installOutput string

	BeforeEach(func() {
		if containerImage == "" {
			Fail("CONTAINER_IMAGE needs to be set")
		}
		_, vm = startVM()
		vm.EventuallyConnects(1200)
	})
	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			fmt.Print(installOutput)
			serial, _ := os.ReadFile(filepath.Join(vm.StateDir, "serial.log"))
			_ = os.MkdirAll("logs", os.ModePerm|os.ModeDir)
			_ = os.WriteFile(filepath.Join("logs", "serial.log"), serial, os.ModePerm)
			fmt.Println(string(serial))
			gatherLogs(vm)
		}
		Expect(vm.Destroy(nil)).ToNot(HaveOccurred())
	})

	Context("upgrades", func() {
		BeforeEach(func() {
			expectDefaultService(vm)
			By("Copying config file")
			err := vm.Scp("assets/config.yaml", "/tmp/config.yaml", "0770")
			Expect(err).ToNot(HaveOccurred())
			By("Manually installing")
			installOutput, err := vm.Sudo("kairos-agent --debug manual-install --device auto /tmp/config.yaml")
			Expect(err).ToNot(HaveOccurred(), installOutput)

			Expect(installOutput).Should(ContainSubstring("Running after-install hook"))
			vm.Sudo("sync")

			By("Rebooting")
			vm.Reboot()
		})

		It("can upgrade to current image", func() {
			var currentVersion string
			currentVersion, err := vm.Sudo(getVersionCmd)
			// Upgrade test uses old version the upgrades to newer so test needs to get the version from os-release for now as
			// fallback
			if err != nil || currentVersion == "" {
				currentVersion, err = vm.Sudo(". /etc/os-release; [ ! -z \"$KAIROS_VERSION\" ] && echo $KAIROS_VERSION")
				Expect(err).ToNot(HaveOccurred())
			}

			By(fmt.Sprintf("Checking current version: %s", currentVersion))
			Expect(currentVersion).To(ContainSubstring("v"))

			By("Getting SSH host key fingerprint before upgrade")
			preFP := HostSSHFingerprint(vm)

			By(fmt.Sprintf("Upgrading to: %s", containerImage))
			out, err := vm.Sudo("kairos-agent upgrade --force --image " + containerImage)
			Expect(err).ToNot(HaveOccurred(), string(out))
			Expect(out).To(ContainSubstring("Upgrade completed"))
			Expect(out).To(ContainSubstring(containerImage))
			fmt.Println(out)

			vm.Reboot()

			Eventually(func() error {
				_, err := vm.Sudo(getVersionCmd)
				return err
			}, 10*time.Minute, 10*time.Second).ShouldNot(HaveOccurred())

			var v string
			Eventually(func() string {
				v, _ = vm.Sudo(getVersionCmd)
				return v
				// TODO: Add regex semver check here
			}, 30*time.Minute, 10*time.Second).ShouldNot(Equal(currentVersion))

			By("Getting SSH host key fingerprint after upgrade")
			postFP := HostSSHFingerprint(vm)

			By("Comparing SSH host key fingerprints")
			Expect(strings.TrimSpace(postFP)).To(Equal(strings.TrimSpace(preFP)),
				"SSH host key fingerprint should remain the same after upgrade")
		})
	})
})
