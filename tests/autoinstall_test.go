package mos_test

import (
	"fmt"
	"os"
	"time"

	"github.com/kairos-io/kairos/tests/machine"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("kairos autoinstall test", Label("autoinstall-test"), func() {
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
				Expect(out).Should(ContainSubstring("kairos"))
				Expect(out).Should(ContainSubstring("kairos-agent"))
				fmt.Println(out)
			} else {
				// Eventually(func() string {
				// 	out, _ := machine.SSHCommand("sudo systemctl status kairosososososos-agent")
				// 	return out
				// }, 30*time.Second, 10*time.Second).Should(ContainSubstring("no network token"))

				out, _ := machine.SSHCommand("sudo systemctl status kairos")
				Expect(out).Should(ContainSubstring("loaded (/etc/systemd/system/kairos.service; enabled;"))
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

	Context("reboots and passes functional tests", func() {
		It("has grubenv file", func() {
			Eventually(func() string {
				out, _ := machine.SSHCommand("sudo cat /oem/grubenv")
				return out
			}, 40*time.Minute, 1*time.Second).Should(
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

		It("has writeable tmp", func() {
			_, err := machine.SSHCommand("sudo echo 'foo' > /tmp/bar")
			Expect(err).ToNot(HaveOccurred())

			out, err := machine.SSHCommand("sudo cat /tmp/bar")
			Expect(err).ToNot(HaveOccurred())

			Expect(out).To(ContainSubstring("foo"))
		})
	})
})
