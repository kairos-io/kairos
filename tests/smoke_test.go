package mos_test

import (
	"os"
	"time"

	"github.com/c3os-io/c3os/tests/machine"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("c3os", func() {
	BeforeEach(func() {
		machine.EventuallyConnects()
	})

	Context("live cd", func() {
		It("has default service active", func() {
			if os.Getenv("FLAVOR") == "alpine" {
				out, _ := machine.SSHCommand("sudo rc-status")
				Expect(out).Should(ContainSubstring("c3os"))
				Expect(out).Should(ContainSubstring("c3os-agent"))
			} else {
				Eventually(func() string {
					out, _ := machine.SSHCommand("sudo journalctl -u c3os-agent")
					return out
				}, 30*time.Second, 10*time.Second).Should(ContainSubstring("no network token"))

				out, _ := machine.SSHCommand("sudo systemctl status c3os")
				Expect(out).Should(ContainSubstring("loaded (/etc/systemd/system/c3os.service; enabled; vendor preset: disabled)"))
			}
		})
	})

	Context("install", func() {
		It("to disk with custom config", func() {
			err := machine.SendFile("assets/config.yaml", "/tmp/config.yaml", "0770")
			Expect(err).ToNot(HaveOccurred())

			out, _ := machine.SSHCommand("sudo cos-installer --cloud-init /tmp/config.yaml /dev/sda")
			Expect(out).Should(ContainSubstring("COS_ACTIVE"))

			machine.SSHCommand("sudo sync")
			machine.DetachCD()
			machine.Restart()
		})
	})

	Context("first-boot", func() {
		It("configure k3s", func() {
			_, err := machine.SSHCommand("cat /run/cos/live_mode")
			Expect(err).To(HaveOccurred())
			if os.Getenv("FLAVOR") == "alpine" {
				Eventually(func() string {
					out, _ := machine.SSHCommand("cat /var/log/c3os-agent.log")
					return out
				}, 900*time.Second, 10*time.Second).Should(
					Or(
						ContainSubstring("Configuring k3s-agent"),
						ContainSubstring("Configuring k3s"),
					))
			} else {
				Eventually(func() string {
					out, _ := machine.SSHCommand("sudo systemctl status c3os-agent")
					return out
				}, 900*time.Second, 10*time.Second).Should(
					Or(
						ContainSubstring("Configuring k3s-agent"),
						ContainSubstring("Configuring k3s"),
					))

			}

		})

		It("propagate kubeconfig", func() {
			Eventually(func() string {
				out, _ := machine.SSHCommand("c3os get-kubeconfig")
				return out
			}, 900*time.Second, 10*time.Second).Should(ContainSubstring("https:"))

			// Eventually(func() string {
			// 	machine.SSHCommand("c3os get-kubeconfig > kubeconfig")
			// 	out, _ := machine.SSHCommand("KUBECONFIG=kubeconfig kubectl get nodes -o wide")
			// 	fmt.Println(out)
			// 	return out
			// }, 900*time.Second, 10*time.Second).Should(ContainSubstring("Ready"))
		})

		It("upgrades to a specific version", func() {
			version, _ := machine.SSHCommand("source /etc/os-release; echo $VERSION")

			machine.SSHCommand("sudo c3os upgrade --image quay.io/mudler/c3os:v1.21.4-19")
			machine.SSHCommand("sudo sync")
			machine.Restart()

			machine.EventuallyConnects(700)

			version2, _ := machine.SSHCommand("source /etc/os-release; echo $VERSION")
			Expect(version).ToNot(Equal(version2))
		})
	})
})
