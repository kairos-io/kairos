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

var _ = Describe("k3s upgrade test", Label("upgrade-k8s"), func() {
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
				out, _ := machine.Sudo("rc-status")
				Expect(out).Should(ContainSubstring("c3os"))
				Expect(out).Should(ContainSubstring("c3os-agent"))
				Expect(out).Should(ContainSubstring("crond"))
			} else {
				// Eventually(func() string {
				// 	out, _ := machine.SSHCommand("sudo systemctl status c3os-agent")
				// 	return out
				// }, 30*time.Second, 10*time.Second).Should(ContainSubstring("no network token"))

				out, _ := machine.Sudo("systemctl status c3os")
				Expect(out).Should(ContainSubstring("loaded (/etc/systemd/system/c3os.service; enabled; vendor preset: disabled)"))

				out, _ = machine.Sudo("systemctl status logrotate.timer")
				Expect(out).Should(ContainSubstring("active (waiting)"))
			}
		})
	})

	Context("install", func() {
		It("to disk with custom config", func() {
			err := machine.SendFile("assets/single.yaml", "/tmp/config.yaml", "0770")
			Expect(err).ToNot(HaveOccurred())

			out, _ := machine.Sudo("elemental install --cloud-init /tmp/config.yaml /dev/sda")
			Expect(out).Should(ContainSubstring("Running after-install hook"))
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
					out, _ = machine.Sudo("cat /var/log/c3os/agent.log")
				} else {
					out, _ = machine.Sudo("systemctl status c3os-agent")
				}
				return out
			}, 900*time.Second, 10*time.Second).Should(ContainSubstring("One time bootstrap starting"))

			Eventually(func() string {
				out, _ := machine.Sudo("cat /var/log/c3os/agent-provider.log")
				return out
			}, 900*time.Second, 10*time.Second).Should(Or(ContainSubstring("One time bootstrap starting"), ContainSubstring("Sentinel exists")))

			Eventually(func() string {
				out, _ := machine.Sudo("cat /etc/rancher/k3s/k3s.yaml")
				return out
			}, 900*time.Second, 10*time.Second).Should(ContainSubstring("https:"))
		})

		It("rotates logs", func() {
			out, err := machine.Sudo("logrotate -vf /etc/logrotate.d/c3os")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(ContainSubstring("log needs rotating"))
			_, err = machine.Sudo("ls /var/log/c3os/agent-provider.log.1.gz")
			Expect(err).ToNot(HaveOccurred())
		})

		It("upgrades", func() {
			By("installing system-upgrade-controller", func() {
				kubectl := func(s string) (string, error) {
					return machine.Sudo("k3s kubectl " + s)
				}
				temp, err := ioutil.TempFile("", "temp")
				Expect(err).ToNot(HaveOccurred())

				defer os.RemoveAll(temp.Name())

				Eventually(func() string {
					// Re-attempt to download in case it fails
					resp, err := http.Get("https://github.com/rancher/system-upgrade-controller/releases/download/v0.9.1/system-upgrade-controller.yaml")
					Expect(err).ToNot(HaveOccurred())
					defer resp.Body.Close()
					data := bytes.NewBuffer([]byte{})

					_, err = io.Copy(data, resp.Body)
					Expect(err).ToNot(HaveOccurred())

					err = ioutil.WriteFile(temp.Name(), data.Bytes(), os.ModePerm)
					Expect(err).ToNot(HaveOccurred())

					err = machine.SendFile(temp.Name(), "/tmp/kubectl.yaml", "0770")
					Expect(err).ToNot(HaveOccurred())

					kubectl("apply -f /tmp/kubectl.yaml")
					out, _ := kubectl("apply -f /tmp/kubectl.yaml")
					return out
				}, 900*time.Second, 10*time.Second).Should(ContainSubstring("unchanged"))

				err = machine.SendFile("assets/suc.yaml", "./suc.yaml", "0770")
				Expect(err).ToNot(HaveOccurred())

				Eventually(func() string {
					out, _ := kubectl("apply -f suc.yaml")
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
