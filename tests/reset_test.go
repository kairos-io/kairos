package mos_test

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
)

var _ = Describe("kairos reset test", Label("reset"), func() {
	var vm VM
	var datasource string
	BeforeEach(func() {
		datasource = CreateDatasource("assets/autoinstall.yaml")
		Expect(os.Setenv("DATASOURCE", datasource)).ToNot(HaveOccurred())

		_, vm = startVM()
		vm.EventuallyConnects(1200)
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
		Expect(os.Unsetenv("DATASOURCE")).ToNot(HaveOccurred())
		Expect(os.Remove(datasource)).ToNot(HaveOccurred())
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
			By("Creating files on persistent and oem")
			_, err := vm.Sudo("touch /usr/local/test")
			Expect(err).ToNot(HaveOccurred())

			_, err = vm.Sudo("touch /oem/test")
			Expect(err).ToNot(HaveOccurred())

			vm.HasFile("/oem/test")
			vm.HasFile("/usr/local/test")
			By("Setting the next entry to statereset")
			_, err = vm.Sudo("grub2-editenv /oem/grubenv set next_entry=statereset")
			Expect(err).ToNot(HaveOccurred())
			By("Rebooting")
			vm.Reboot()

			expectRebootedToActive(vm)

			By("Checking that persistent file is gone")
			Eventually(func() string {
				out, _ := vm.Sudo("if [ -f /usr/local/test ]; then echo ok; else echo wrong; fi")
				return out
			}, 3*time.Minute, 1*time.Second).Should(
				Or(
					ContainSubstring("wrong"),
				))
			By("Checking that oem file is still there")
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
