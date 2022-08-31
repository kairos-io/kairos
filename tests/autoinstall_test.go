package mos_test

import (
	"fmt"
	"os"
	"time"

	"github.com/c3os-io/c3os/tests/machine"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("c3os autoinstall test", Label("autoinstall-test"), func() {
	BeforeEach(func() {
		machine.EventuallyConnects()
	})

	AfterEach(func() {
		if CurrentGinkgoTestDescription().Failed {
			gatherLogs()
		}
	})

	Context("live cd", func() {
		It("has default service active", func() {
			if os.Getenv("FLAVOR") == "alpine" {
				out, _ := machine.SSHCommand("sudo rc-status")
				Expect(out).Should(ContainSubstring("c3os"))
				Expect(out).Should(ContainSubstring("c3os-agent"))
				fmt.Println(out)
			} else {
				// Eventually(func() string {
				// 	out, _ := machine.SSHCommand("sudo systemctl status c3os-agent")
				// 	return out
				// }, 30*time.Second, 10*time.Second).Should(ContainSubstring("no network token"))

				out, _ := machine.SSHCommand("sudo systemctl status c3os")
				Expect(out).Should(ContainSubstring("loaded (/etc/systemd/system/c3os.service; enabled; vendor preset: disabled)"))
				fmt.Println(out)
			}

			out, _ := machine.SSHCommand("ls -liah /oem")
			fmt.Println(out)
			//	Expect(out).To(ContainSubstring("userdata.yaml"))
			out, _ = machine.SSHCommand("cat /oem/userdata")
			fmt.Println(out)
			out, _ = machine.SSHCommand("sudo ps aux")
			fmt.Println(out)

			out, _ = machine.SSHCommand("sudo lsblk")
			fmt.Println(out)

		})
	})

	Context("auto installs", func() {
		It("to disk with custom config", func() {
			Eventually(func() string {
				out, _ := machine.SSHCommand("sudo ps aux")
				return out
			}, 30*time.Minute, 1*time.Second).Should(
				Or(
					ContainSubstring("elemental install"),
				))
		})
	})

	Context("reboots", func() {
		It("has grubenv file", func() {
			Eventually(func() string {
				out, _ := machine.SSHCommand("sudo cat /oem/grubenv")
				return out
			}, 30*time.Minute, 1*time.Second).Should(
				Or(
					ContainSubstring("foobarzz"),
				))
		})
		It("has custom cmdline", func() {
			Eventually(func() string {
				out, _ := machine.SSHCommand("sudo cat /proc/cmdline")
				return out
			}, 30*time.Minute, 1*time.Second).Should(
				Or(
					ContainSubstring("foobarzz"),
				))
		})
	})
})
