// nolint
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

var _ = Describe("k3s upgrade test", Label("provider", "provider-upgrade-k8s"), func() {
	var vm VM

	BeforeEach(func() {
		_, vm = startVM()
		vm.EventuallyConnects(1200)
	})

	AfterEach(func() {
		if CurrentGinkgoTestDescription().Failed {
			gatherLogs(vm)
		}
		vm.Destroy(nil)
	})

	It("installs to disk with custom config", func() {
		By("checking if it has default service active")

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
			Eventually(func() string {
				var out string
				out, _ = vm.Sudo("rc-service kairos-agent status")
				return out
			}, 900*time.Second, 10*time.Second).Should(ContainSubstring("status: started"))
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

		By("Checking k3s is pointing to https")
		Eventually(func() string {
			out, _ := vm.Sudo("cat /etc/rancher/k3s/k3s.yaml")
			return out
		}, 900*time.Second, 10*time.Second).Should(ContainSubstring("https:"))

		/*
			By("checking if logs are rotated")
			out, err = vm.Sudo("logrotate -vf /etc/logrotate.d/kairos")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(ContainSubstring("log needs rotating"))
			// Check that we have some rotated logs
			out, err = vm.Sudo("[ $(ls /var/log/kairos/agent-*log.1.gz 2>/dev/null | wc -l) -gt 0 ]")
			Expect(err).ToNot(HaveOccurred(), out)
		*/

		By("deploying the kairos-operator")
		// Download and extract the operator repository (git is not available on the node)
		_, err = vm.Sudo("curl -sL https://github.com/kairos-io/kairos-operator/archive/refs/heads/main.tar.gz | tar -xz -C /tmp")
		Expect(err).ToNot(HaveOccurred())

		Eventually(func() string {
			out, _ := kubectl(vm, "apply -k /tmp/kairos-operator-main/config/default")
			return out
		}, 900*time.Second, 10*time.Second).Should(Or(ContainSubstring("created"), ContainSubstring("unchanged")))

		By("waiting for kairos-operator to be ready")
		Eventually(func() string {
			out, _ := kubectl(vm, "get pods -n operator-system")
			return out
		}, 900*time.Second, 10*time.Second).Should(ContainSubstring("operator-kairos-operator"))

		By("waiting for the NodeOpUpgrade CRD to be created")
		Eventually(func() string {
			out, _ := kubectl(vm, "get crds")
			return out
		}, 300*time.Second, 10*time.Second).Should(ContainSubstring("nodeopupgrades.operator.kairos.io"))

		By("wait for all containers to be in running state")
		Eventually(func() string {
			out, _ := kubectl(vm, "get pods -A")
			fmt.Printf("out = %+v\n", out)
			return out

		}, 900*time.Second, 10*time.Second).ShouldNot(Or(ContainSubstring("Pending"), ContainSubstring("ContainerCreating")))

		// Opportunistic feature test here to avoid a full test just
		// for this.
		By("listing upgrade options")
		resultStr, _ := vm.Sudo(`kairos-agent upgrade list-releases --all --pre | tail -1`)
		Expect(resultStr).To(ContainSubstring("quay.io/kairos"), resultStr)

		By("preparing the NodeOpUpgrade resource")

		version := "v3.6.1-beta2"
		fullArtifact := fmt.Sprintf("quay.io/kairos/ubuntu:24.04-standard-amd64-generic-%s-k3s-v1.34.2-k3s1", version)

		tempDir, err := os.MkdirTemp("", "nodeopupgrade-*")
		Expect(err).ToNot(HaveOccurred())
		defer os.RemoveAll(tempDir)

		b, err := os.ReadFile("assets/nodeopupgrade.yaml")
		Expect(err).ToNot(HaveOccurred())

		nodeOpUpgrade := fmt.Sprintf(string(b), fullArtifact)
		err = os.WriteFile(filepath.Join(tempDir, "nodeopupgrade.yaml"), []byte(nodeOpUpgrade), 0777)
		Expect(err).ToNot(HaveOccurred())

		err = vm.Scp(filepath.Join(tempDir, "nodeopupgrade.yaml"), "./nodeopupgrade.yaml", "0770")
		Expect(err).ToNot(HaveOccurred())

		By("applying the NodeOpUpgrade resource")
		Eventually(func() string {
			out, _ := kubectl(vm, "apply -f nodeopupgrade.yaml")
			return out
		}, 900*time.Second, 10*time.Second).Should(Or(ContainSubstring("created"), ContainSubstring("unchanged")))

		By("checking that the upgrade job is created")
		Eventually(func() string {
			out, _ = kubectl(vm, "get pods -n operator-system")
			return out
		}, 900*time.Second, 10*time.Second).Should(ContainSubstring("kairos-upgrade"))

		By("waiting for the upgrade to complete")
		Eventually(func() string {
			out, _ = kubectl(vm, "get nodeopupgrade -n operator-system kairos-upgrade -o jsonpath='{.status.phase}'")
			return out
		}, 30*time.Minute, 10*time.Second).Should(ContainSubstring("Completed"))

		By("validate upgraded version")
		Eventually(func() string {
			out, _ = kubectl(vm, "get pods -A")
			getVersion, _ := vm.Sudo(getVersionCmd)
			fmt.Printf("version = %+v\n", getVersion)
			return getVersion
		}, 30*time.Minute, 10*time.Second).Should(ContainSubstring(version), func() string {
			return out
		})
	})
})
