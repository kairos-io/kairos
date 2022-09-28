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

var _ = Describe("kairos reset test", Label("reset-test"), func() {
	BeforeEach(func() {
		if os.Getenv("CLOUD_INIT") == "" || !filepath.IsAbs(os.Getenv("CLOUD_INIT")) {
			Fail("CLOUD_INIT must be set and must be pointing to a file as an absolute path")
		}

		EventuallyConnects()
	})

	Context("live cd", func() {
		It("has default service active", func() {
			if os.Getenv("FLAVOR") == "alpine" {
				out, _ := Machine.Command("sudo rc-status")
				Expect(out).Should(ContainSubstring("kairos"))
				Expect(out).Should(ContainSubstring("kairos-agent"))
				fmt.Println(out)
			} else {
				// Eventually(func() string {
				// 	out, _ := Machine.Command("sudo systemctl status kairosososososos-agent")
				// 	return out
				// }, 30*time.Second, 10*time.Second).Should(ContainSubstring("no network token"))

				out, _ := Machine.Command("sudo systemctl status kairos")
				Expect(out).Should(ContainSubstring("loaded (/etc/systemd/system/kairos.service; enabled;"))
				fmt.Println(out)
			}

			out, _ := Machine.Command("ls -liah /oem")
			fmt.Println(out)
			//	Expect(out).To(ContainSubstring("userdata.yaml"))
			out, _ = Machine.Command("cat /oem/userdata")
			fmt.Println(out)
			out, _ = Machine.Command("sudo ps aux")
			fmt.Println(out)

			out, _ = Machine.Command("sudo lsblk")
			fmt.Println(out)

		})
	})

	Context("auto installs", func() {
		It("to disk with custom config", func() {
			Eventually(func() string {
				out, _ := Machine.Command("sudo ps aux")
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
				out, _ := Machine.Command("sudo cat /oem/grubenv")
				return out
			}, 40*time.Minute, 1*time.Second).Should(
				Or(
					ContainSubstring("foobarzz"),
				))
		})

		It("resets", func() {
			_, err := Sudo("echo 'test' > /usr/local/test")
			Expect(err).ToNot(HaveOccurred())

			_, err = Sudo("echo 'testoem' > /oem/test")
			Expect(err).ToNot(HaveOccurred())

			HasFile("/oem/test")
			HasFile("/usr/local/test")

			_, err = Sudo("grub2-editenv /oem/grubenv set next_entry=statereset")
			Expect(err).ToNot(HaveOccurred())

			Reboot()

			Eventually(func() string {
				out, _ := Sudo("if [ -f /usr/local/test ]; then echo ok; else echo wrong; fi")
				return out
			}, 40*time.Minute, 1*time.Second).Should(
				Or(
					ContainSubstring("wrong"),
				))
			Eventually(func() string {
				out, _ := Sudo("if [ -f /oem/test ]; then echo ok; else echo wrong; fi")
				return out
			}, 40*time.Minute, 1*time.Second).Should(
				Or(
					ContainSubstring("ok"),
				))
		})
	})
})
