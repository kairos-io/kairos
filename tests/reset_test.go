package mos_test

import (
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
)

var _ = Describe("kairos reset test", Label("reset-test"), func() {
	var vm VM
	BeforeEach(func() {
		if os.Getenv("DATASOURCE") == "" {
			Fail("DATASOURCE must be set and it should be the absolute path to a datasource iso")
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
		It("resets", func() {
			Eventually(func() string {
				out, _ := vm.Sudo("cat /oem/grubenv")
				return out
			}, 10*time.Minute, 1*time.Second).Should(
				Or(
					ContainSubstring("foobarzz"),
				))
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
			}, 3*time.Minute, 1*time.Second).Should(
				Or(
					ContainSubstring("wrong"),
				))
			Eventually(func() string {
				out, _ := vm.Sudo("if [ -f /oem/test ]; then echo ok; else echo wrong; fi")
				return out
			}, 3*time.Minute, 1*time.Second).Should(
				Or(
					ContainSubstring("ok"),
				))
		})
	})
})
