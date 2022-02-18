package mos_test

import (
	"fmt"
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
			out, _ := machine.SSHCommand("sudo systemctl status c3os-setup")
			Expect(out).Should(ContainSubstring("No network token"))
			out, _ = machine.SSHCommand("sudo systemctl status c3os")
			Expect(out).Should(ContainSubstring("loaded (/etc/systemd/system/c3os.service; enabled; vendor preset: disabled)"))
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
		It("succeeds in setup", func() {
			_, err := machine.SSHCommand("cat /run/cos/live_mode")
			Expect(err).To(HaveOccurred())

			Eventually(func() string {
				out, _ := machine.SSHCommand("sudo systemctl status c3os-setup")
				return out
			}, 900*time.Second, 10*time.Second).Should(
				Or(
					ContainSubstring("Configuring k3s-agent"),
					ContainSubstring("Configuring k3s"),
				))

			Eventually(func() string {
				out, _ := machine.SSHCommand("c3os get-kubeconfig")
				return out
			}, 900*time.Second, 10*time.Second).Should(ContainSubstring("https:"))

			machine.SSHCommand("c3os get-kubeconfig > kubeconfig")

			Eventually(func() string {
				out, _ := machine.SSHCommand("KUBECONFIG=kubeconfig kubectl get nodes -o wide")
				fmt.Println(out)
				return out
			}, 900*time.Second, 10*time.Second).Should(ContainSubstring("Ready"))
		})
	})
})
