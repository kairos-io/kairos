// nolint
package mos_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
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

var _ = Describe("k3s upgrade test from k8s", Label("provider", "provider-upgrade-latest-k8s-with-kubernetes"), func() {
	var containerImage string
	var vm VM

	BeforeEach(func() {
		containerImage = os.Getenv("CONTAINER_IMAGE")
		_, vm = startVM()
		vm.EventuallyConnects(3600)
	})

	AfterEach(func() {
		if CurrentGinkgoTestDescription().Failed {
			gatherLogs(vm)
		}
		vm.Destroy(nil)
	})

	It("installs to disk with custom config", func() {
		By("checking if it has default service active")
		if containerImage == "" {
			Fail("CONTAINER_IMAGE needs to be set")
		}

		expectDefaultService(vm)

		By("copy the config")
		err := vm.Scp("assets/single.yaml", "/tmp/config.yaml", "0770")
		Expect(err).ToNot(HaveOccurred())

		By("installing")
		out, err := vm.Sudo("kairos-agent manual-install /tmp/config.yaml")
		Expect(err).ToNot(HaveOccurred(), out)
		Expect(out).Should(ContainSubstring("Running after-install hook"))

		out, err = vm.Sudo("sync")
		Expect(err).ToNot(HaveOccurred(), out)

		By("rebooting after install")
		vm.Reboot()

		By("checking default services are on after first boot")
		if isFlavor(vm, "alpine") {
			Eventually(func() string {
				out, _ := vm.Sudo("rc-status")
				return out
			}, 30*time.Second, 10*time.Second).Should(And(
				ContainSubstring("kairos"),
				ContainSubstring("kairos-agent")))
		} else {
			Eventually(func() string {
				out, _ := vm.Sudo("systemctl status kairos-agent")
				return out
			}, 30*time.Second, 10*time.Second).Should(ContainSubstring(
				"loaded (/etc/systemd/system/kairos-agent.service; enabled"))

			Eventually(func() string {
				out, _ := vm.Sudo("systemctl status systemd-timesyncd")
				return out
			}, 30*time.Second, 10*time.Second).Should(ContainSubstring(
				"loaded (/usr/lib/systemd/system/systemd-timesyncd.service; enabled"))
		}

		By("Checking agent provider correct start")
		Eventually(func() string {
			out, _ := vm.Sudo("journalctl -t kairos-provider")
			return out
		}, 900*time.Second, 10*time.Second).Should(Or(ContainSubstring("One time bootstrap starting"), ContainSubstring("Sentinel exists")))

		By("checking kubeconfig")
		Eventually(func() string {
			out, _ := vm.Sudo("cat /etc/rancher/k3s/k3s.yaml")
			return out
		}, 900*time.Second, 10*time.Second).Should(ContainSubstring("https:"))

		By("checking current version")
		currentVersion, err := vm.Sudo(getVersionCmd)
		Expect(err).ToNot(HaveOccurred())
		Expect(currentVersion).To(ContainSubstring("v"))

		By("wait system-upgrade-controller")
		Eventually(func() string {
			out, _ := kubectl(vm, "get pods -A")
			return out
		}, 900*time.Second, 10*time.Second).Should(ContainSubstring("system-upgrade-controller"))

		By("waiting for the plan CRD to be created")
		Eventually(func() string {
			out, _ := kubectl(vm, "get crds")
			return out
		}, 300*time.Second, 10*time.Second).Should(ContainSubstring("plans.upgrade.cattle.io"))

		By("wait for all containers to be in running state")
		Eventually(func() string {
			out, _ := kubectl(vm, "get pods -A")
			return out

		}, 900*time.Second, 10*time.Second).ShouldNot(Or(ContainSubstring("Pending"), ContainSubstring("ContainerCreating")))

		By("triggering an upgrade")
		suc := sucYAML(strings.ReplaceAll(containerImage, ":24h", ""), "24h")

		err = ioutil.WriteFile("assets/generated.yaml", []byte(suc), os.ModePerm)
		Expect(err).ToNot(HaveOccurred())

		err = vm.Scp("assets/generated.yaml", "./suc.yaml", "0770")
		Expect(err).ToNot(HaveOccurred())

		Eventually(func() string {
			out, _ = kubectl(vm, "apply -f suc.yaml")
			return out
		}, 900*time.Second, 10*time.Second).Should(ContainSubstring("created"))

		Eventually(func() string {
			out, _ = kubectl(vm, "get pods -A")
			return out
		}, 900*time.Second, 10*time.Second).Should(ContainSubstring("apply-os-upgrade-on-"))

		By("checking upgraded version")
		Eventually(func() string {
			out, _ = kubectl(vm, "get pods -A")
			version, err := vm.Sudo(getVersionCmd)
			if err != nil || !strings.Contains(version, "v") {
				version, err = vm.Sudo(getVersionCmdOsRelease)
				if err != nil || !strings.Contains(version, "v") {
					// If we met error, keep going with the Eventually
					return currentVersion
				}
			}

			return version
		}, 50*time.Minute, 10*time.Second).ShouldNot(Equal(currentVersion), func() string {
			debugOutput := "DEBUG OUTPUT\n--------------------------------\n"
			out, _ := kubectl(vm, "get pods -A")
			if err != nil {
				return fmt.Sprintf("errored while trying to get debug output: %s", err.Error())
			} else {
				debugOutput += out
			}

			version, err := vm.Sudo(getVersionCmd)
			if err != nil {
				debugOutput += fmt.Sprintf("error getting version from kairos-release: %s\n", err.Error())
			} else {
				debugOutput += fmt.Sprintf("version: %s\n", version)
			}

			version, err = vm.Sudo(getVersionCmdOsRelease)
			if err != nil {
				debugOutput += fmt.Sprintf("error getting version from os-release: %s\n", err.Error())
			} else {
				debugOutput += fmt.Sprintf("version: %s\n", version)
			}

			debugOutput += fmt.Sprintf("current version: %s\n", currentVersion)

			version, err = vm.Sudo("kairos-agent state get boot")
			if err != nil {
				debugOutput += fmt.Sprintf("error getting version from kairos-agent state: %s\n", err.Error())
			} else {
				debugOutput += fmt.Sprintf("kairos-agent state: %s\n", version)
			}

			out, _ = kubectl(vm, "get plans -A")
			if err != nil {
				debugOutput += fmt.Sprintf("error getting plans: %s\n", err.Error())
			} else {
				debugOutput += fmt.Sprintf("plans: %s\n", out)
			}

			return debugOutput
		})
	})
})
