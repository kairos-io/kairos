package e2e_test

import (
	"fmt"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"os"
	"strings"
	"time"
)

// NOTE: Once you run a test in 1 raw image, because the image is the installed system, any changes are now permanent
// So you cannot run different tests for 1 raw image that are destructive, like on first boot we will recover and expand
// the system with partitions, so that raw image now has changed.
// All tests in here should be sequential taking into account that the auto-reset is run on teh single raw image
var _ = Describe("raw bootable artifacts", Label("raw-bootable"), func() {
	var vm testVM
	var err error

	BeforeEach(func() {
		_, ok := os.Stat(os.Getenv("RAW_IMAGE"))
		Expect(ok).To(BeNil(), "RAW_IMAGE should exist")
		vm, err = startVM()
		Expect(err).ToNot(HaveOccurred())
		vm.EventuallyConnects(1200)
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			saveSerialLog(vm)
			gatherLogs(vm)
		}

		err := vm.Destroy(nil)
		Expect(err).ToNot(HaveOccurred())
	})
	It("Should boot as expected", func() {
		bootActive := os.Getenv("BOOT_ACTIVE") == "true"
		// Boot-active images ship COS_STATE and must reach active on the very first boot, without a reset reboot
		if bootActive {
			By("Booting straight into active", func() {
				stateAssertVM(vm, "boot", "active_boot")
				boots, err := vm.SudoOut("journalctl --list-boots --no-pager -q | wc -l")
				Expect(err).ToNot(HaveOccurred(), boots)
				Expect(strings.TrimSpace(boots)).To(Equal("1"))
				out, err := vm.SudoOut("ls /oem")
				Expect(err).ToNot(HaveOccurred(), out)
				Expect(out).To(ContainSubstring("01_layout.yaml"))
				Expect(out).ToNot(ContainSubstring("01_reset.yaml"))
			})
			By("Shipping passive.img only when STATE has room for it", func() {
				out, err := vm.Sudo("test -f /run/initramfs/cos-state/cOS/passive.img")
				if os.Getenv("EXPECT_PASSIVE") == "true" {
					Expect(err).ToNot(HaveOccurred(), out)
				} else {
					Expect(err).To(HaveOccurred(), out)
				}
			})
			if os.Getenv("NO_RECOVERY") == "true" {
				By("Having no recovery partition", func() {
					out, err := vm.Sudo("blkid -L COS_RECOVERY")
					Expect(err).To(HaveOccurred(), out)
				})
			}
		}
		// At first raw images boot on recovery and they reset the system and creates the partitions
		// so it can take a while to boot in the active partition
		// lets wait a bit checking
		//
		// The reset ends in a reboot, so this poll spans the moment the SSH
		// connection goes away. RootCommand reports that as an error and the
		// poll carries on; vm.Sudo would panic from a peg goroutine and end
		// the run with no spec failure and no logs.
		By("Waiting for recovery reset to finish", func() {
			Eventually(func() string {
				output, _ := vm.RootCommand("kairos-agent state")
				return output
			}, 5*time.Minute, 1*time.Second).Should(
				Or(
					ContainSubstring("active_boot"),
				))
		})

		// This checks both that the disk is bootable and with secureboot enabled
		if os.Getenv("SECUREBOOT") == "true" {
			By("Have secureboot enabled", func() {
				output, err := vm.SudoOut("dmesg | grep -i secure")
				Expect(err).ToNot(HaveOccurred(), output)
				Expect(output).To(ContainSubstring("Secure boot enabled"))
			})
		}

		By("checking corresponding state", func() {
			currentVersion, err := vm.SudoOut(getVersionCmd)
			Expect(err).ToNot(HaveOccurred(), currentVersion)

			stateAssertVM(vm, "boot", "active_boot")
			stateAssertVM(vm, "oem.mounted", "true")
			stateAssertVM(vm, "oem.found", "true")
			stateAssertVM(vm, "persistent.mounted", "true")
			stateAssertVM(vm, "state.mounted", "true")
			stateAssertVM(vm, "oem.type", "ext4")
			// Persistent is ext4 on every raw image, like an install. State is ext4 only on
			// boot-active images; the reset layout keeps it ext2 until yip can turn off
			// metadata_csum_seed (kairos-io/kairos#5476).
			stateFs := "ext2"
			if bootActive {
				stateFs = "ext4"
			}
			stateAssertVM(vm, "persistent.type", "ext4")
			stateAssertVM(vm, "state.type", stateFs)
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

		// GRUB reads both of these partitions before the kernel exists. The
		// grub.cfg on the ESP does "search --label COS_RECOVERY" and then
		// chainloads its real config from that partition, and the config it
		// chainloads loads grubenv from COS_OEM. AuroraBoot#233 moved every raw
		// partition to ext2 in March 2025 because old GRUB builds died on ext4
		// feature flags instead of ignoring them, so a boot is the only honest
		// proof that the filesystem these two carry is still readable. The VM
		// above has booted, which is that proof; assert the type as well so the
		// coverage cannot pass with the change silently reverted. Images built
		// without a recovery partition only have COS_OEM to check.
		By("creating the GRUB-readable partitions with the filesystem an install gives them", func() {
			partitions := map[string]string{
				"COS_OEM":      "ext4",
				"COS_RECOVERY": "ext4",
			}
			if os.Getenv("NO_RECOVERY") == "true" {
				delete(partitions, "COS_RECOVERY")
			}
			for label, want := range partitions {
				out, err := vm.SudoOut(fmt.Sprintf("blkid -L %s -o device | xargs -r blkid -s TYPE -o value", label))
				Expect(err).ToNot(HaveOccurred(), out)
				Expect(strings.TrimSpace(out)).To(Equal(want), "%s should be %s, got %q", label, want, out)
			}
		})
	})
})
