package mos_test

import (
	"fmt"
	. "github.com/spectrocloud/peg/matcher"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("kairos install test different targets", Label("install-test-target"), func() {

	var vm VM
	BeforeEach(func() {

		_, vm = startVM()
		vm.EventuallyConnects(1200)
		// Format the disk so it gets an uuid and label
		_, err := vm.Sudo("mkfs.ext4 -L TESTDISK /dev/vda")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		Expect(vm.Destroy(nil)).ToNot(HaveOccurred())
	})

	Context("Selects the disk by uuid/label", func() {
		It("Selects the correct disk if using uuid for target", func() {
			expectSecureBootEnabled(vm)
			// Get uuid of main disk
			uuid, err := vm.Sudo("lsblk /dev/vda -o UUID -n")

			cc := fmt.Sprintf(`#cloud-config
install:
  device: /dev/disk/by-uuid/%s

stages:
  initramfs:
	- name: "Set user and password"
	  users:
		kairos:
		  passwd: "kairos"
	  hostname: kairos-{{ trunc 4 .Random }}
				`, uuid)

			By("Using the following config")
			fmt.Fprint(GinkgoWriter, cc)

			t, err := os.CreateTemp("", "test")
			ExpectWithOffset(1, err).ToNot(HaveOccurred())

			defer os.RemoveAll(t.Name())
			err = os.WriteFile(t.Name(), []byte(cc), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())

			err = vm.Scp(t.Name(), "/tmp/config.yaml", "0770")
			Expect(err).ToNot(HaveOccurred())

			var out string
			// Test that install works
			By("installing kairos", func() {
				out, err = vm.Sudo(`kairos-agent --debug manual-install /tmp/config.yaml`)
				fmt.Fprint(GinkgoWriter, out)
				Expect(err).ToNot(HaveOccurred(), out)
				Expect(out).Should(ContainSubstring("Running after-install hook"))
				vm.Sudo("sync")
			})

			By("waiting for VM to reboot", func() {
				vm.Reboot()
				vm.EventuallyConnects(1200)
			})

			By("checking that vm has rebooted to 'active'", func() {
				Eventually(func() string {
					out, _ := vm.Sudo("kairos-agent state boot")
					return out
				}, 40*time.Minute, 10*time.Second).Should(
					Or(
						ContainSubstring("active_boot"),
					))
			})

			By("checking corresponding state", func() {
				out, err := vm.Sudo("kairos-agent state")
				Expect(err).ToNot(HaveOccurred())
				Expect(out).To(ContainSubstring("boot: active_boot"))
				currentVersion, err := vm.Sudo(getVersionCmd)
				Expect(err).ToNot(HaveOccurred(), currentVersion)

				stateAssertVM(vm, "oem.mounted", "true")
				stateAssertVM(vm, "oem.found", "true")
				stateAssertVM(vm, "persistent.mounted", "true")
				stateAssertVM(vm, "state.mounted", "true")
				stateAssertVM(vm, "oem.type", "ext4")
				stateAssertVM(vm, "persistent.type", "ext4")
				stateAssertVM(vm, "state.type", "ext4")
				stateAssertVM(vm, "oem.mount_point", "/oem")
				stateAssertVM(vm, "persistent.mount_point", "/usr/local")
				stateAssertVM(vm, "persistent.name", "/dev/vda")
				stateAssertVM(vm, "state.mount_point", "/run/initramfs/cos-state")
				stateAssertVM(vm, "oem.read_only", "false")
				stateAssertVM(vm, "persistent.read_only", "false")
				stateAssertVM(vm, "state.read_only", "true")
				stateAssertVM(vm, "kairos.version", strings.ReplaceAll(strings.ReplaceAll(currentVersion, "\r", ""), "\n", ""))
				stateContains(vm, "system.os.name", "alpine", "opensuse", "ubuntu", "debian")
				stateContains(vm, "kairos.flavor", "alpine", "opensuse", "ubuntu", "debian")
			})
		})
		It("Selects the correct disk if using label for target", func() {
			expectSecureBootEnabled(vm)
			// Get label of main disk
			label, err := vm.Sudo("lsblk /dev/vda -o LABEL -n")

			cc := fmt.Sprintf(`#cloud-config
install:
  device: /dev/disk/by-label/%s

stages:
  initramfs:
	- name: "Set user and password"
	  users:
		kairos:
		  passwd: "kairos"
	  hostname: kairos-{{ trunc 4 .Random }}
				`, label)

			By("Using the following config")
			fmt.Fprintf(GinkgoWriter, cc)

			t, err := os.CreateTemp("", "test")
			ExpectWithOffset(1, err).ToNot(HaveOccurred())

			defer os.RemoveAll(t.Name())
			err = os.WriteFile(t.Name(), []byte(cc), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())

			err = vm.Scp(t.Name(), "/tmp/config.yaml", "0770")
			Expect(err).ToNot(HaveOccurred())

			var out string
			By("installing kairos", func() {
				out, err = vm.Sudo(`kairos-agent --debug manual-install /tmp/config.yaml`)
				Expect(err).ToNot(HaveOccurred(), out)
				fmt.Fprint(GinkgoWriter, out)
				Expect(out).Should(ContainSubstring("Running after-install hook"))
				vm.Sudo("sync")
			})

			By("waiting for VM to reboot", func() {
				vm.Reboot()
				vm.EventuallyConnects(1200)
			})

			By("checking that vm has rebooted to 'active'", func() {
				Eventually(func() string {
					out, _ := vm.Sudo("kairos-agent state boot")
					return out
				}, 40*time.Minute, 10*time.Second).Should(
					Or(
						ContainSubstring("active_boot"),
					))
			})

			By("checking corresponding state", func() {
				out, err := vm.Sudo("kairos-agent state")
				Expect(err).ToNot(HaveOccurred())
				Expect(out).To(ContainSubstring("boot: active_boot"))
				currentVersion, err := vm.Sudo(getVersionCmd)
				Expect(err).ToNot(HaveOccurred(), currentVersion)

				stateAssertVM(vm, "oem.mounted", "true")
				stateAssertVM(vm, "oem.found", "true")
				stateAssertVM(vm, "persistent.mounted", "true")
				stateAssertVM(vm, "state.mounted", "true")
				stateAssertVM(vm, "oem.type", "ext4")
				stateAssertVM(vm, "persistent.type", "ext4")
				stateAssertVM(vm, "state.type", "ext4")
				stateAssertVM(vm, "oem.mount_point", "/oem")
				stateAssertVM(vm, "persistent.mount_point", "/usr/local")
				stateAssertVM(vm, "persistent.name", "/dev/vda")
				stateAssertVM(vm, "state.mount_point", "/run/initramfs/cos-state")
				stateAssertVM(vm, "oem.read_only", "false")
				stateAssertVM(vm, "persistent.read_only", "false")
				stateAssertVM(vm, "state.read_only", "true")
				stateAssertVM(vm, "kairos.version", strings.ReplaceAll(strings.ReplaceAll(currentVersion, "\r", ""), "\n", ""))
				stateContains(vm, "system.os.name", "alpine", "opensuse", "ubuntu", "debian")
				stateContains(vm, "kairos.flavor", "alpine", "opensuse", "ubuntu", "debian")
			})
		})
	})
})
