package mos_test

import (
	"fmt"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
)

// test ci
var _ = Describe("k3s upgrade manual test", Label("upgrade-with-cli"), func() {

	containerImage := os.Getenv("CONTAINER_IMAGE")

	var vm VM
	BeforeEach(func() {
		_, vm = startVM()
		vm.EventuallyConnects(1200)
	})

	AfterEach(func() {
		Expect(vm.Destroy(nil)).ToNot(HaveOccurred())
	})

	Context("upgrades", func() {
		BeforeEach(func() {
			if containerImage == "" {
				Fail("CONTAINER_IMAGE needs to be set")
			}

			expectDefaultService(vm)
			By("Copying config file")
			err := vm.Scp("assets/config.yaml", "/tmp/config.yaml", "0770")
			Expect(err).ToNot(HaveOccurred())
			By("Manually installing")
			out, err := vm.Sudo("kairos-agent manual-install --device auto /tmp/config.yaml")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).Should(ContainSubstring("Running after-install hook"))
			vm.Sudo("sync")
			By("Rebooting")
			vm.Reboot()
		})

		It("can upgrade to current image", func() {
			currentVersion, err := vm.Sudo(getVersionCmd)
			Expect(err).ToNot(HaveOccurred())
			By(fmt.Sprintf("Checking current version: %s", currentVersion))
			Expect(currentVersion).To(ContainSubstring("v"))
			_, err = vm.Sudo("kairos-agent")
			if err == nil {
				By(fmt.Sprintf("Upgrading to: %s", containerImage))
				out, err := vm.Sudo("kairos-agent upgrade --force --image " + containerImage)
				Expect(err).ToNot(HaveOccurred(), string(out))
				Expect(out).To(ContainSubstring("Upgrade completed"))
				Expect(out).To(ContainSubstring(containerImage))
				fmt.Println(out)
			} else {
				By(fmt.Sprintf("Upgrading to: %s", containerImage))
				out, err := vm.Sudo("kairos upgrade --force --image " + containerImage)
				Expect(err).ToNot(HaveOccurred(), string(out))
				Expect(out).To(ContainSubstring("Upgrade completed"))
				Expect(out).To(ContainSubstring(containerImage))
				fmt.Println(out)
			}

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
			}, 10*time.Minute, 10*time.Second).Should(ContainSubstring("v"), v)
		})
	})
})
