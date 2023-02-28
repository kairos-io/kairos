package mos_test

import (
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
)

var _ = Describe("kairos reset test", Label("reset-test"), func() {
	var vm VM
	BeforeEach(func() {
		if os.Getenv("CLOUD_INIT") == "" || !filepath.IsAbs(os.Getenv("CLOUD_INIT")) {
			Fail("CLOUD_INIT must be set and must be pointing to a file as an absolute path")
		}

		_, vm = startVM()
		vm.EventuallyConnects(1200)
	})

	AfterEach(func() {
		Expect(vm.Destroy(nil)).ToNot(HaveOccurred())
	})

	Context("auto installs, reboots and passes functional tests", func() {
		BeforeEach(func() {
			expectDefaultService(vm)
			expectStartedInstallation(vm)
			expectRebootedToActive(vm)
		})

		It("has grubenv file", func() {
			Eventually(func() string {
				out, _ := vm.Sudo("cat /oem/grubenv")
				return out
			}, 40*time.Minute, 1*time.Second).Should(
				Or(
					ContainSubstring("foobarzz"),
				))
		})

		It("resets", func() {
			_, err := vm.Sudo("touch /usr/local/test")
			Expect(err).ToNot(HaveOccurred())

			_, err = vm.Sudo("touch /oem/test")
			Expect(err).ToNot(HaveOccurred())

			vm.HasFile("/oem/test")
			vm.HasFile("/usr/local/test")

			_, err = vm.Sudo("grub2-editenv /oem/grubenv set next_entry=statereset")
			Expect(err).ToNot(HaveOccurred())

			vm.Reboot()

			Eventually(func() string {
				out, _ := vm.Sudo("if [ -f /usr/local/test ]; then echo ok; else echo wrong; fi")
				return out
			}, 40*time.Minute, 1*time.Second).Should(
				Or(
					ContainSubstring("wrong"),
				))
			Eventually(func() string {
				out, _ := vm.Sudo("if [ -f /oem/test ]; then echo ok; else echo wrong; fi")
				return out
			}, 40*time.Minute, 1*time.Second).Should(
				Or(
					ContainSubstring("ok"),
				))
		})
	})
})
