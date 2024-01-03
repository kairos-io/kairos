package mos_test

import (
	"fmt"
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
)

var _ = Describe("kairos UKI test", Label("uki"), Ordered, func() {
	var vm VM

	BeforeAll(func() {
		if os.Getenv("FIRMWARE") == "" {
			Fail("FIRMWARE environment variable set to a EFI firmware is needed for UKI test")
		}
	})

	BeforeEach(func() {
		_, vm = startVM()
		vm.EventuallyConnects(300)
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			gatherLogs(vm)
		}

		err := vm.Destroy(nil)
		Expect(err).ToNot(HaveOccurred())
	})
	It("passes checks", func() {
		By("Checking SecureBoot is enabled", func() {
			out, err := vm.Sudo(`dmesg|grep -i secure| grep -i enabled`)
			Expect(err).ToNot(HaveOccurred(), out)
		})
		By("Checking the boot mode (install)", func() {
			out, err := vm.Sudo("stat /run/cos/uki_install_mode")
			Expect(err).ToNot(HaveOccurred(), out)
		})
		By("Checking OEM/PERSISTENT are not mounted", func() {
			out, err := vm.Sudo("mount")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).ToNot(ContainSubstring("/dev/disk/by-label/COS_OEM"))
			Expect(out).ToNot(ContainSubstring("/dev/disk/by-label/COS_PERSISTENT"))
		})
		By("installing kairos", func() {
			out, err := vm.Sudo(`kairos-agent --debug uki install --device /dev/vda`)
			fmt.Println(string(out))
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).Should(ContainSubstring("Running after-install hook"))
			Expect(out).Should(ContainSubstring("Encrypting COS_OEM"))
			Expect(out).Should(ContainSubstring("Encrypting COS_PERSISTENT"))
			Expect(out).Should(ContainSubstring("Done encrypting COS_OEM"))
			Expect(out).Should(ContainSubstring("Done encrypting COS_PERSISTENT"))
			Expect(out).Should(ContainSubstring("New TPM2 token enrolled as key slot 1."))
			vm.Sudo("sync")
		})

		By("Ejecting Cdrom", func() {
			vm.DetachCD()
		})

		By("waiting for VM to reboot", func() {
			vm.Reboot()
			vm.EventuallyConnects(1200)
		})
		By("Checking the boot mode (boot)", func() {
			out, err := vm.Sudo("stat /run/cos/uki_boot_mode")
			Expect(err).ToNot(HaveOccurred(), out)
		})
		By("Checking SecureBoot is enabled", func() {
			out, err := vm.Sudo(`dmesg|grep -i secure| grep -i enabled`)
			Expect(err).ToNot(HaveOccurred(), out)
		})
		By("Checking OEM/PERSISTENT are mounted", func() {
			out, err := vm.Sudo("df -h") // Shows the disk by label which is easier to check
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(ContainSubstring("/dev/disk/by-label/COS_OEM"))
			Expect(out).To(ContainSubstring("/dev/disk/by-label/COS_PERSISTENT"))
		})
		By("Checking OEM/PERSISTENT are encrypted", func() {
			out, err := vm.Sudo("blkid /dev/vda2")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(ContainSubstring("crypto_LUKS"))
			out, err = vm.Sudo("blkid /dev/vda3")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(ContainSubstring("crypto_LUKS"))
		})

		By("checking custom cmdline", func() {
			out, err := vm.Sudo("cat /proc/cmdline")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(ContainSubstring("rd.immucore.uki"))
		})

		By("checking the use of dracut immutable module", func() {
			out, err := vm.Sudo("cat /run/immucore/initramfs_stage.log")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(ContainSubstring("Running stage: initramfs.before"))
			Expect(out).To(ContainSubstring("Running stage: initramfs.after"))
			Expect(out).To(ContainSubstring("Running stage: initramfs"))
		})

		By("checking writeable tmp", func() {
			_, err := vm.Sudo("echo 'foo' > /tmp/bar")
			Expect(err).ToNot(HaveOccurred())

			out, err := vm.Sudo("sudo cat /tmp/bar")
			Expect(err).ToNot(HaveOccurred())

			Expect(out).To(ContainSubstring("foo"))
		})

		By("checking bpf mount", func() {
			out, err := vm.Sudo("mount")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(ContainSubstring("bpf"))
		})

		By("checking rootfs shared mount", func() {
			out, err := vm.Sudo(`cat /proc/1/mountinfo | grep ' / / '`)
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(ContainSubstring("shared"))
		})

		By("checking that networking is functional", func() {
			out, err := vm.Sudo(`curl google.it`)
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(ContainSubstring("Moved"))
		})

		By("checking corresponding state", func() {
			out, err := vm.Sudo("kairos-agent state")
			Expect(err).ToNot(HaveOccurred())
			// TODO: make agent report uki_mode or something?
			Expect(out).To(ContainSubstring("boot: unknown"))
			currentVersion, err := vm.Sudo(getVersionCmd)
			Expect(err).ToNot(HaveOccurred(), currentVersion)

			stateAssertVM(vm, "kairos.version", strings.ReplaceAll(strings.ReplaceAll(currentVersion, "\r", ""), "\n", ""))
			stateContains(vm, "system.os.name", "alpine", "opensuse", "ubuntu", "debian", "fedora")
			stateContains(vm, "kairos.flavor", "alpine", "opensuse", "ubuntu", "debian", "fedora")
		})
	})
})
