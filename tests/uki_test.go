package mos_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
	"strings"
)

var _ = Describe("kairos UKI test", Label("uki"), func() {
	var vm VM

	BeforeEach(func() {
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
	It("passes checks", func() {

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

		By("checking correct permissions", func() {
			out, err := vm.Sudo(`stat -c "%a" /usr/local/cloud-config`)
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(ContainSubstring("755"))
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
			// TODO: make agetn report uki_mode or something?
			Expect(out).To(ContainSubstring("boot: unknown"))
			currentVersion, err := vm.Sudo(getVersionCmd)
			Expect(err).ToNot(HaveOccurred(), currentVersion)

			stateAssertVM(vm, "kairos.version", strings.ReplaceAll(strings.ReplaceAll(currentVersion, "\r", ""), "\n", ""))
			stateContains(vm, "system.os.name", "alpine", "opensuse", "ubuntu", "debian", "fedora")
			stateContains(vm, "kairos.flavor", "alpine", "opensuse", "ubuntu", "debian", "fedora")
		})
	})
})
