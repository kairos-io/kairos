package mos_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	. "github.com/spectrocloud/peg/matcher"
)

var stateContains = func(vm VM, query string, expected ...string) {
	or := []types.GomegaMatcher{}
	for _, e := range expected {
		or = append(or, ContainSubstring(e))
	}
	out, err := vm.Sudo(fmt.Sprintf("kairos-agent state get %s", query))
	ExpectWithOffset(1, err).ToNot(HaveOccurred())
	ExpectWithOffset(1, strings.ToLower(out)).To(Or(or...))
}

var _ = Describe("kairos autoinstall test", Label("autoinstall-test"), func() {
	var vm VM

	BeforeEach(func() {
		if os.Getenv("CLOUD_INIT") == "" || !filepath.IsAbs(os.Getenv("CLOUD_INIT")) {
			Fail("CLOUD_INIT must be set and must be pointing to a file as an absolute path")
		}

		_, vm = startVM()
		vm.EventuallyConnects(1200)
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			gatherLogs(vm)
		}

		err := vm.Destroy(nil)
		Expect(err).ToNot(HaveOccurred())
	})

	Context("reboots and passes functional tests", func() {
		BeforeEach(func() {
			expectDefaultService(vm)
			// Flimsy check?
			//expectStartedInstallation(vm)
			expectRebootedToActive(vm)
		})

		It("passes checks", func() {
			By("checking grubenv file", func() {
				out, err := vm.Sudo("cat /oem/grubenv")
				Expect(err).ToNot(HaveOccurred(), out)
				Expect(out).To(ContainSubstring("foobarzz"))
			})

			By("checking custom cmdline", func() {
				out, err := vm.Sudo("cat /proc/cmdline")
				Expect(err).ToNot(HaveOccurred())
				Expect(out).To(ContainSubstring("foobarzz"))
			})

			By("checking the use of dracut immutable module", func() {
				out, err := vm.Sudo("cat /proc/cmdline")
				Expect(err).ToNot(HaveOccurred())
				Expect(out).To(ContainSubstring("cos-img/filename="))
			})

			By("checking Auto assessment", func() {
				// Auto assessment was installed
				out, _ := vm.Sudo("cat /run/initramfs/cos-state/grubcustom")
				Expect(out).To(ContainSubstring("bootfile_loc"))

				out, _ = vm.Sudo("cat /run/initramfs/cos-state/grub_boot_assessment")
				Expect(out).To(ContainSubstring("boot_assessment_blk"))

				cmdline, _ := vm.Sudo("cat /proc/cmdline")
				Expect(cmdline).To(ContainSubstring("rd.emergency=reboot rd.shell=0 panic=5"))
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

			By("checking correct permissions", func() {
				out, err := vm.Sudo(`stat -c "%a" /oem`)
				Expect(err).ToNot(HaveOccurred())
				Expect(out).To(ContainSubstring("770"))

				out, err = vm.Sudo(`stat -c "%a" /usr/local/cloud-config`)
				Expect(err).ToNot(HaveOccurred())
				Expect(out).To(ContainSubstring("770"))
			})

			By("checking grubmenu", func() {
				out, err := vm.Sudo("cat /run/initramfs/cos-state/grubmenu")
				Expect(err).ToNot(HaveOccurred())
				Expect(out).To(ContainSubstring("state reset"))
			})

			By("checking additional mount specified, with no dir in rootfs", func() {
				out, err := vm.Sudo("mount")
				Expect(err).ToNot(HaveOccurred())
				Expect(out).To(ContainSubstring("/var/lib/longhorn"))
			})

			By("checking rootfs shared mount", func() {
				out, err := vm.Sudo(`cat /proc/1/mountinfo | grep ' / / '`)
				Expect(err).ToNot(HaveOccurred(), out)
				Expect(out).To(ContainSubstring("shared"))
			})

			By("checking that it doesn't has grub data into the cloud config", func() {
				out, err := vm.Sudo(`cat /oem/90_custom.yaml`)
				Expect(err).ToNot(HaveOccurred(), out)
				Expect(out).ToNot(ContainSubstring("vga_text"))
				Expect(out).ToNot(ContainSubstring("videotest"))
			})

			By("checking that networking is functional", func() {
				out, err := vm.Sudo(`curl google.it`)
				Expect(err).ToNot(HaveOccurred(), out)
				Expect(out).To(ContainSubstring("Moved"))
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
