package mos_test

import (
	"fmt"
	"github.com/google/uuid"
	. "github.com/spectrocloud/peg/matcher"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("kairos install test different targets", Label("install-test-target"), func() {

	var vm VM
	var label string
	var diskUUID uuid.UUID
	BeforeEach(func() {
		label = "TESTDISK"
		diskUUID = uuid.New()
		_, vm = startVM()
		vm.EventuallyConnects(1200)
		// Format the disk so it gets an uuid and label
		_, err := vm.Sudo(fmt.Sprintf("mkfs.ext4 -L %s -U %s /dev/vda", label, diskUUID.String()))
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		Expect(vm.Destroy(nil)).ToNot(HaveOccurred())
	})

	Context("Selects the disk by uuid/label", func() {
		It("Selects the correct disk if using uuid for target", func() {
			expectSecureBootEnabled(vm)

			err := vm.Scp("assets/config.yaml", "/tmp/config.yaml", "0770")
			Expect(err).ToNot(HaveOccurred())

			var out string
			// Test that install works
			By("installing kairos", func() {
				installCmd := fmt.Sprintf("kairos-agent --debug manual-install --device /dev/disk/by-uuid/%s /tmp/config.yaml", diskUUID.String())
				By(fmt.Sprintf("Running %s", installCmd))
				out, err = vm.Sudo(installCmd)
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

			err := vm.Scp("assets/config.yaml", "/tmp/config.yaml", "0770")
			Expect(err).ToNot(HaveOccurred())

			var out string
			By("installing kairos", func() {
				installCmd := fmt.Sprintf("kairos-agent --debug manual-install --device /dev/disk/by-label/%s /tmp/config.yaml", label)
				By(fmt.Sprintf("Running %s", installCmd))
				out, err = vm.Sudo(installCmd)
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
