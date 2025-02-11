package mos_test

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	. "github.com/spectrocloud/peg/matcher"
	"github.com/spectrocloud/peg/pkg/machine"
	"github.com/spectrocloud/peg/pkg/machine/types"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("kairos install test different targets", Label("install-target"), func() {

	var vm VM
	var label string
	var diskUUID uuid.UUID
	BeforeEach(func() {
		label = "TESTDISK"
		diskUUID = uuid.New()
		stateDir, err := os.MkdirTemp("", "")
		Expect(err).ToNot(HaveOccurred())
		fmt.Printf("State dir: %s\n", stateDir)

		opts := defaultVMOptsNoDrives(stateDir)
		opts = append(opts, types.WithDriveSize("40000"))
		opts = append(opts, types.WithDriveSize("30000"))

		m, err := machine.New(opts...)
		Expect(err).ToNot(HaveOccurred())
		vm = NewVM(m, stateDir)
		_, err = vm.Start(context.Background())
		Expect(err).ToNot(HaveOccurred())

		vm.EventuallyConnects(1200)
		// Format the first disk so it gets an uuid and label
		_, err = vm.Sudo(fmt.Sprintf("mkfs.ext4 -L %s -U %s /dev/vda", label, diskUUID.String()))
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			serial, _ := os.ReadFile(filepath.Join(vm.StateDir, "serial.log"))
			_ = os.MkdirAll("logs", os.ModePerm|os.ModeDir)
			_ = os.WriteFile(filepath.Join("logs", "serial.log"), serial, os.ModePerm)
			fmt.Println(string(serial))
		}

		if CurrentSpecReport().Failed() {
			gatherLogs(vm)
		}

		Expect(vm.Destroy(nil)).ToNot(HaveOccurred())

	})

	// TODO: Install on second disk instead of first and check that it worked.
	// Missing the bootindex check, it will only try to boot from the first disk it seems
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
				_, _ = vm.Sudo("reboot")
				vm.EventuallyConnects(1200)
			})

			By("checking that vm has rebooted to 'active'", func() {
				var out string
				Eventually(func() string {
					out, err = vm.Sudo("kairos-agent state boot")
					if err != nil {
						fmt.Println(err.Error())
						return ""
					}
					return out
				}, 5*time.Minute, 10*time.Second).Should(
					Or(
						ContainSubstring("active_boot"),
					), out)
			})

			By("checking corresponding state", func() {
				currentVersion, err := vm.Sudo(getVersionCmd)
				Expect(err).ToNot(HaveOccurred(), currentVersion)

				stateAssertVM(vm, "boot", "active_boot")
				stateAssertVM(vm, "oem.mounted", "true")
				stateAssertVM(vm, "oem.found", "true")
				stateAssertVM(vm, "persistent.mounted", "true")
				stateAssertVM(vm, "state.mounted", "true")
				stateAssertVM(vm, "oem.type", "ext4")
				stateAssertVM(vm, "persistent.type", "ext4")
				stateAssertVM(vm, "state.type", "ext4")
				stateAssertVM(vm, "oem.mount_point", "/oem")
				stateAssertVM(vm, "persistent.mount_point", "/usr/local")
				stateAssertVM(vm, "persistent.name", "/dev/vda5")
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
				currentVersion, err := vm.Sudo(getVersionCmd)
				Expect(err).ToNot(HaveOccurred(), currentVersion)

				stateAssertVM(vm, "boot", "active_boot")
				stateAssertVM(vm, "oem.mounted", "true")
				stateAssertVM(vm, "oem.found", "true")
				stateAssertVM(vm, "persistent.mounted", "true")
				stateAssertVM(vm, "state.mounted", "true")
				stateAssertVM(vm, "oem.type", "ext4")
				stateAssertVM(vm, "persistent.type", "ext4")
				stateAssertVM(vm, "state.type", "ext4")
				stateAssertVM(vm, "oem.mount_point", "/oem")
				stateAssertVM(vm, "persistent.mount_point", "/usr/local")
				stateAssertVM(vm, "persistent.name", "/dev/vda5")
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
