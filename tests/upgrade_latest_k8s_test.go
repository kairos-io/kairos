package mos_test

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/c3os-io/c3os/tests/machine"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func sucYAML(image, version string) string {
	return `
---
apiVersion: upgrade.cattle.io/v1
kind: Plan
metadata:
  name: os-upgrade
  namespace: system-upgrade
  labels:
    k3s-upgrade: server
spec:
  concurrency: 1
  version: "` + version + `"
  nodeSelector:
    matchExpressions:
      - {key: kubernetes.io/hostname, operator: Exists}
  serviceAccountName: system-upgrade
  cordon: false
  upgrade:
    image: "` + image + `"
    command:
    - "/usr/sbin/suc-upgrade"
`

}

var _ = Describe("k3s upgrade test from k8s", Label("upgrade-latest-with-kubernetes"), func() {
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

		It("upgrades from kubernetes", func() {
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
				out, _ := machine.Sudo("cat /etc/rancher/k3s/k3s.yaml")
				return out
			}, 900*time.Second, 10*time.Second).Should(ContainSubstring("https:"))

			kubectl := func(s string) (string, error) {
				return machine.Sudo("k3s kubectl " + s)
			}

			currentVersion, err := machine.SSHCommand("source /etc/os-release; echo $VERSION")
			Expect(err).ToNot(HaveOccurred())
			Expect(currentVersion).To(ContainSubstring("c3OS"))

			By("installing system-upgrade-controller", func() {
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
			})

			By("triggering an upgrade", func() {
				suc := sucYAML(strings.ReplaceAll(containerImage, ":8h", ""), "8h")

				err := ioutil.WriteFile("assets/generated.yaml", []byte(suc), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())

				err = machine.SendFile("assets/generated.yaml", "./suc.yaml", "0770")
				Expect(err).ToNot(HaveOccurred())
				fmt.Println(suc)

				Eventually(func() string {
					out, _ := kubectl("apply -f suc.yaml")
					fmt.Println(out)
					return out
				}, 900*time.Second, 10*time.Second).Should(ContainSubstring("created"))

				Eventually(func() string {
					out, _ := kubectl("get pods -A")
					fmt.Println(out)
					return out
				}, 900*time.Second, 10*time.Second).Should(ContainSubstring("apply-os-upgrade-on-"))

				Eventually(func() string {
					out, _ := kubectl("get pods -A")
					fmt.Println(out)
					version, err := machine.SSHCommand("source /etc/os-release; echo $VERSION")
					if err != nil || !strings.Contains(version, "v0") {
						// If we met error, keep going with the Eventually
						return currentVersion
					}
					return version
				}, 20*time.Minute, 10*time.Second).ShouldNot(Equal(currentVersion))
			})
		})
	})
})
