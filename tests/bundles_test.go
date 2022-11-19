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

var _ = Describe("kairos bundles test", Label("bundles-test"), func() {
	BeforeEach(func() {
		if os.Getenv("CLOUD_INIT") == "" || !filepath.IsAbs(os.Getenv("CLOUD_INIT")) {
			Fail("CLOUD_INIT must be set and must be pointing to a file as an absolute path")
		}

		EventuallyConnects(1200)
	})

	AfterEach(func() {
		if CurrentGinkgoTestDescription().Failed {
			gatherLogs()
		}
	})

	Context("live cd", func() {
		It("has default service active", func() {
			if isFlavor("alpine") {
				out, _ := Sudo("rc-status")
				Expect(out).Should(ContainSubstring("kairos"))
				Expect(out).Should(ContainSubstring("kairos-agent"))
				fmt.Println(out)
			} else {
				// Eventually(func() string {
				// 	out, _ := machine.Command("sudo systemctl status kairososososos-agent")
				// 	return out
				// }, 30*time.Second, 10*time.Second).Should(ContainSubstring("no network token"))

				out, _ := Machine.Command("sudo systemctl status kairos")
				Expect(out).Should(ContainSubstring("loaded (/etc/systemd/system/kairos.service; enabled;"))
				fmt.Println(out)
			}

			// Debug output
			out, _ := Sudo("ls -liah /oem")
			fmt.Println(out)
			//	Expect(out).To(ContainSubstring("userdata.yaml"))
			out, _ = Sudo("cat /oem/userdata")
			fmt.Println(out)
			out, _ = Sudo("sudo ps aux")
			fmt.Println(out)

			out, _ = Sudo("sudo lsblk")
			fmt.Println(out)

		})
	})

	Context("auto installs", func() {
		It("to disk with custom config", func() {
			Eventually(func() string {
				out, _ := Sudo("ps aux")
				return out
			}, 30*time.Minute, 1*time.Second).Should(
				Or(
					ContainSubstring("elemental install"),
				))
		})
	})

	Context("reboots and passes functional tests", func() {

		It("has grubenv file", func() {
			By("checking after-install hook triggered")

			Eventually(func() string {
				out, _ := Sudo("sudo cat /oem/grubenv")
				return out
			}, 40*time.Minute, 1*time.Second).Should(
				Or(
					ContainSubstring("foobarzz"),
				))
		})

		It("has custom cmdline", func() {
			By("waiting reboot and checking cmdline is present")
			Eventually(func() string {
				out, _ := Sudo("sudo cat /proc/cmdline")
				return out
			}, 30*time.Minute, 1*time.Second).Should(
				Or(
					ContainSubstring("foobarzz"),
				))
		})

		It("has kubo extension", func() {
			// Eventually(func() string {
			// 	out, _ := Sudo("systemd-sysext")
			// 	return out
			// }, 40*time.Minute, 1*time.Second).Should(
			// 	Or(
			// 		ContainSubstring("kubo"),
			// 	))
			syset, err := Sudo("systemd-sysext")
			ls, _ := Sudo("ls -liah /usr/local/lib/extensions")
			fmt.Println("LS:", ls)
			Expect(err).ToNot(HaveOccurred())
			Expect(syset).To(ContainSubstring("kubo"))

			ipfsV, err := Sudo("ipfs version")
			Expect(err).ToNot(HaveOccurred())

			Expect(ipfsV).To(ContainSubstring("0.15.0"))
		})
	})
})
