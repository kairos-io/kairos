package mos_test

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
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
			machine.Sudo("k3s kubectl get pods -A -o json > /run/pods.json")
			machine.Sudo("k3s kubectl get events -A -o json > /run/events.json")
			machine.Sudo("cat /proc/cmdline > /run/cmdline")
			machine.Sudo("chmod 777 /run/events.json")

			machine.Sudo("df -h > /run/disk")
			machine.Sudo("mount > /run/mounts")
			machine.Sudo("blkid > /run/blkid")

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
					"/run/cmdline",
				})
		}
	})

	Context("live cd", func() {
		It("has default service active", func() {
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
			err := machine.SendFile("assets/single.yaml", "/tmp/config.yaml", "0770")
			Expect(err).ToNot(HaveOccurred())

			out, _ := machine.Sudo("elemental install --cloud-init /tmp/config.yaml /dev/sda")
			Expect(out).Should(ContainSubstring("COS_ACTIVE"))
			fmt.Println(out)
			machine.Sudo("sync")
			machine.DetachCD()
			machine.Restart()
		})
	})

	Context("first-boot", func() {

		It("has default services on", func() {
			if os.Getenv("FLAVOR") == "alpine" {
				out, _ := machine.Sudo("rc-status")
				Expect(out).Should(ContainSubstring("c3os"))
				Expect(out).Should(ContainSubstring("c3os-agent"))
			} else {
				// Eventually(func() string {
				// 	out, _ := machine.SSHCommand("sudo systemctl status c3os-agent")
				// 	return out
				// }, 30*time.Second, 10*time.Second).Should(ContainSubstring("no network token"))

				out, _ := machine.Sudo("systemctl status c3os-agent")
				Expect(out).Should(ContainSubstring("loaded (/etc/systemd/system/c3os-agent.service; enabled; vendor preset: disabled)"))

				out, _ = machine.Sudo("systemctl status systemd-timesyncd")
				Expect(out).Should(ContainSubstring("loaded (/usr/lib/systemd/system/systemd-timesyncd.service; enabled; vendor preset: disabled)"))
			}
		})

		It("has kubeconfig", func() {
			Eventually(func() string {
				var out string
				if os.Getenv("FLAVOR") == "alpine" {
					out, _ = machine.Sudo("cat /var/log/c3os-agent.log")
				} else {
					out, _ = machine.Sudo("systemctl status c3os-agent")
				}
				return out
			}, 900*time.Second, 10*time.Second).Should(ContainSubstring("One time bootstrap starting"))

			Eventually(func() string {
				out, _ := machine.Sudo("cat /etc/rancher/k3s/k3s.yaml")
				return out
			}, 900*time.Second, 10*time.Second).Should(ContainSubstring("https:"))

			By("installing system-upgrade-controller", func() {

				kubectl := func(s string) (string, error) {
					return machine.Sudo("k3s kubectl " + s)
				}

				resp, err := http.Get("https://github.com/rancher/system-upgrade-controller/releases/download/v0.9.1/system-upgrade-controller.yaml")
				Expect(err).ToNot(HaveOccurred())
				defer resp.Body.Close()
				data := bytes.NewBuffer([]byte{})

				_, err = io.Copy(data, resp.Body)
				Expect(err).ToNot(HaveOccurred())

				temp, err := ioutil.TempFile("", "temp")
				Expect(err).ToNot(HaveOccurred())

				defer os.RemoveAll(temp.Name())
				err = ioutil.WriteFile(temp.Name(), data.Bytes(), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())

				err = machine.SendFile(temp.Name(), "/tmp/kubectl.yaml", "0770")
				Expect(err).ToNot(HaveOccurred())

				Eventually(func() string {
					out, _ := kubectl("apply -f /tmp/kubectl.yaml")
					return out
				}, 900*time.Second, 10*time.Second).Should(ContainSubstring("unchanged"))

				err = machine.SendFile("assets/suc.yaml", "/tmp/suc.yaml", "0770")
				Expect(err).ToNot(HaveOccurred())

				Eventually(func() string {
					out, _ := kubectl("apply -f /tmp/suc.yaml")
					return out
				}, 900*time.Second, 10*time.Second).Should(ContainSubstring("unchanged"))

				Eventually(func() string {
					out, _ := kubectl("get pods -A")
					fmt.Println(out)
					return out
				}, 900*time.Second, 10*time.Second).Should(ContainSubstring("apply-os-upgrade-on-"))

				Eventually(func() string {
					out, _ := kubectl("get pods -A")
					fmt.Println(out)
					version, _ := machine.SSHCommand("source /etc/os-release; echo $VERSION")
					return version
				}, 20*time.Minute, 10*time.Second).Should(ContainSubstring("c3OS44"))
			})
		})

	})
})
