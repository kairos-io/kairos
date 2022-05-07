package mos_test

import (
	"fmt"
	"os"
	"time"

	"github.com/c3os-io/c3os/tests/machine"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("k3s upgrade test", Label("upgrade"), func() {
	BeforeEach(func() {
		machine.EventuallyConnects()
	})

	AfterEach(func() {
		if CurrentGinkgoTestDescription().Failed {
			machine.SSHCommand("sudo k3s kubectl get pods -A -o json > /run/pods.json")
			machine.SSHCommand("sudo k3s kubectl get events -A -o json > /run/events.json")
			machine.SSHCommand("sudo df -h > /run/disk")
			machine.SSHCommand("sudo mount > /run/mounts")
			machine.SSHCommand("sudo blkid > /run/blkid")

			machine.GatherAllLogs(
				[]string{
					"edgevpn@c3os",
					"c3os-agent",
					"cos-setup-boot",
					"cos-setup-network",
					"c3os",
					"k3s",
				},
				[]string{
					"/var/log/edgevpn.log",
					"/var/log/c3os-agent.log",
					"/run/pods.json",
					"/run/disk",
					"/run/mounts",
					"/run/blkid",
					"/run/events.json",
				})
		}
	})

	Context("live cd", func() {
		It("has default service active", func() {
			if os.Getenv("FLAVOR") == "alpine" {
				out, _ := machine.SSHCommand("sudo rc-status")
				Expect(out).Should(ContainSubstring("c3os"))
				Expect(out).Should(ContainSubstring("c3os-agent"))
			} else {
				// Eventually(func() string {
				// 	out, _ := machine.SSHCommand("sudo systemctl status c3os-agent")
				// 	return out
				// }, 30*time.Second, 10*time.Second).Should(ContainSubstring("no network token"))

				out, _ := machine.SSHCommand("sudo systemctl status c3os")
				Expect(out).Should(ContainSubstring("loaded (/etc/systemd/system/c3os.service; enabled; vendor preset: disabled)"))
			}
		})
	})

	Context("install", func() {
		It("to disk with custom config", func() {
			err := machine.SendFile("assets/single.yaml", "/tmp/config.yaml", "0770")
			Expect(err).ToNot(HaveOccurred())

			out, _ := machine.SSHCommand("sudo elemental install --cloud-init /tmp/config.yaml /dev/sda")
			Expect(out).Should(ContainSubstring("COS_ACTIVE"))
			fmt.Println(out)
			machine.SSHCommand("sudo sync")
			machine.DetachCD()
			machine.Restart()
		})
	})

	Context("first-boot", func() {

		It("has default services on", func() {
			if os.Getenv("FLAVOR") == "alpine" {
				out, _ := machine.SSHCommand("sudo rc-status")
				Expect(out).Should(ContainSubstring("c3os"))
				Expect(out).Should(ContainSubstring("c3os-agent"))
			} else {
				// Eventually(func() string {
				// 	out, _ := machine.SSHCommand("sudo systemctl status c3os-agent")
				// 	return out
				// }, 30*time.Second, 10*time.Second).Should(ContainSubstring("no network token"))

				out, _ := machine.SSHCommand("sudo systemctl status c3os-agent")
				Expect(out).Should(ContainSubstring("loaded (/etc/systemd/system/c3os-agent.service; enabled; vendor preset: disabled)"))

				out, _ = machine.SSHCommand("sudo systemctl status systemd-timesyncd")
				Expect(out).Should(ContainSubstring("loaded (/usr/lib/systemd/system/systemd-timesyncd.service; enabled; vendor preset: disabled)"))
			}
		})

		It("has kubeconfig", func() {
			Eventually(func() string {
				var out string
				if os.Getenv("FLAVOR") == "alpine" {
					out, _ = machine.SSHCommand("sudo systemctl status c3os-agent")
				} else {
					out, _ = machine.SSHCommand("sudo systemctl status c3os-agent")
				}
				return out
			}, 900*time.Second, 10*time.Second).Should(ContainSubstring("One time bootstrap starting"))

			Eventually(func() string {
				out, _ := machine.SSHCommand("sudo cat /etc/rancher/k3s/k3s.yaml")
				return out
			}, 900*time.Second, 10*time.Second).Should(ContainSubstring("https:"))
		})

	})
})
