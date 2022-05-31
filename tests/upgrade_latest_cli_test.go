package mos_test

import (
	"fmt"
	"os"
	"time"

	"github.com/c3os-io/c3os/tests/machine"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("k3s upgrade manual test", Label("upgrade-latest-with-cli"), func() {

	containerImage := os.Getenv("CONTAINER_IMAGE")

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
			if containerImage == "" {
				Fail("CONTAINER_IMAGE needs to be set")
			}

			if os.Getenv("FLAVOR") == "alpine" {
				out, _ := machine.Sudo("rc-status")
				Expect(out).Should(ContainSubstring("c3os"))
				Expect(out).Should(ContainSubstring("c3os-agent"))
			} else {
				// Eventually(func() string {
				// 	out, _ := machine.SSHCommand("sudo systemctl status c3os-agent")
				// 	return out
				// }, 30*time.Second, 10*time.Second).Should(ContainSubstring("no network token"))

				out, _ := machine.Sudo("systemctl status c3os")
				Expect(out).Should(ContainSubstring("loaded (/etc/systemd/system/c3os.service; enabled; vendor preset: disabled)"))
			}
		})
	})

	Context("install", func() {
		It("to disk with custom config", func() {
			err := machine.SendFile("assets/config.yaml", "/tmp/config.yaml", "0770")
			Expect(err).ToNot(HaveOccurred())

			out, _ := machine.Sudo("elemental install --cloud-init /tmp/config.yaml /dev/sda")
			Expect(out).Should(ContainSubstring("Running after-install hook"))
			fmt.Println(out)
			machine.Sudo("sync")
			machine.DetachCD()
			machine.Restart()
		})
	})

	Context("upgrades", func() {
		It("can upgrade to current image", func() {

			currentVersion, err := machine.SSHCommand("source /etc/os-release; echo $VERSION")
			Expect(err).ToNot(HaveOccurred())
			Expect(currentVersion).To(ContainSubstring("c3OS"))
			out, err := machine.Sudo("c3os upgrade --force --image " + containerImage)
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(ContainSubstring("Upgrade completed"))
			Expect(out).To(ContainSubstring(containerImage))

			machine.Sudo("reboot")
			machine.EventuallyConnects(750)

			Eventually(func() error {
				_, err := machine.SSHCommand("source /etc/os-release; echo $VERSION")
				return err
			}, 10*time.Minute, 10*time.Second).ShouldNot(HaveOccurred())

			var v string
			Eventually(func() string {
				v, _ = machine.SSHCommand("source /etc/os-release; echo $VERSION")
				return v
			}, 10*time.Minute, 10*time.Second).Should(ContainSubstring("c3OS"))
			Expect(v).ToNot(Equal(currentVersion))
		})
	})
})
