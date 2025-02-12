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
		time.Sleep(5 * time.Minute)
		if CurrentGinkgoTestDescription().Failed {
			gatherLogs(vm)
		}
		vm.Destroy(nil)
	})

	It("installs to disk with custom config", func() {
		By("checking if it has default service active")
		if isFlavor(vm, "alpine") {
			out, _ := vm.Sudo("rc-status")
			Expect(out).Should(ContainSubstring("kairos"))
			Expect(out).Should(ContainSubstring("kairos-agent"))
			out, _ = vm.Sudo("ps aux")
			Expect(out).Should(ContainSubstring("/usr/sbin/crond"))
		} else {
			out, _ := vm.Sudo("systemctl status kairos")
			Expect(out).Should(ContainSubstring("loaded (/etc/systemd/system/kairos.service; enabled"))

			/* TODO: Add logrotate to kairos-init, check it on acceptance test
			out, _ = vm.Sudo("systemctl status logrotate.timer")
			Expect(out).Should(ContainSubstring("active (waiting)"))

			*/
		}

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
			out, _ := vm.Sudo("cat /var/log/kairos/provider-*.log")
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

		By("wait system-upgrade-controller")
		Eventually(func() string {
			out, _ := kubectl(vm, "get pods -A")
			return out
		}, 900*time.Second, 10*time.Second).Should(ContainSubstring("system-upgrade-controller"))

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
		Expect(resultStr).To(ContainSubstring("quay.io/kairos"))

		By("copy upgrade plan")

		version := "v3.2.1"
		fullArtifact := fmt.Sprintf("leap-15.6-standard-amd64-generic-%s-k3sv1.31.1-k3s1", version)

		tempDir, err := os.MkdirTemp("", "suc-*")
		Expect(err).ToNot(HaveOccurred())
		defer os.RemoveAll(tempDir)

		b, err := os.ReadFile("assets/suc.yaml")
		Expect(err).ToNot(HaveOccurred())

		suc := fmt.Sprintf(string(b), fullArtifact)
		err = os.WriteFile(filepath.Join(tempDir, "suc.yaml"), []byte(suc), 0777)
		Expect(err).ToNot(HaveOccurred())

		err = vm.Scp(filepath.Join(tempDir, "suc.yaml"), "./suc.yaml", "0770")
		Expect(err).ToNot(HaveOccurred())

		By("apply upgrade plan")
		Eventually(func() string {
			out, _ := kubectl(vm, "apply -f suc.yaml")
			return out
		}, 900*time.Second, 10*time.Second).Should(ContainSubstring("unchanged"))

		By("check that plan is being executed")
		Eventually(func() string {
			out, _ = kubectl(vm, "get pods -A")
			return out
		}, 900*time.Second, 10*time.Second).Should(ContainSubstring("apply-os-upgrade-on-"))

		By("wait for plan to finish")
		Eventually(func() string {
			out, _ = kubectl(vm, "get pods -A")
			return out
		}, 30*time.Minute, 10*time.Second).ShouldNot(ContainSubstring("ContainerCreating"))

		By("validate upgraded version")
		Eventually(func() string {
			out, _ = kubectl(vm, "get pods -A")
			version, _ = vm.Sudo(getVersionCmdOsRelease)
			if version == "" {
				version, _ = vm.Sudo(getVersionCmd)
			}
			fmt.Printf("version = %+v\n", version)
			return version
		}, 30*time.Minute, 10*time.Second).Should(ContainSubstring(version), func() string {
			return out
		})
	})
})
