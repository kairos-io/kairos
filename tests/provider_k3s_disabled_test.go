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

var _ = Describe("kairos k3s disabled test", Label("provider", "provider-k3s-disabled"), func() {
	var vm VM

	BeforeEach(func() {
		_, vm = startVM()
		vm.EventuallyConnects(1200)
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			gatherLogs(vm)
			serial, _ := os.ReadFile(filepath.Join(vm.StateDir, "serial.log"))
			_ = os.MkdirAll("logs", os.ModePerm|os.ModeDir)
			_ = os.WriteFile(filepath.Join("logs", "serial.log"), serial, os.ModePerm)
			fmt.Println(string(serial))
		}
		vm.Destroy(nil)
	})

	It("installs to disk with k3s disabled", func() {
		By("checking if it has default service active")

		expectDefaultService(vm)

		By("copy the config")
		err := vm.Scp("assets/config_k3s_disabled.yaml", "/tmp/config.yaml", "0770")
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

		By("Checking agent provider logs contain 'no P2P or kubernetes configured'")
		if isFlavor(vm, "alpine") {
			// Skip for now as agent doesn't log anymore as it cannot behave both as a one-off and a daemon
		} else {
			Eventually(func() string {
				out, _ := vm.Sudo("journalctl -t kairos-agent")
				return out
			}, 900*time.Second, 10*time.Second).Should(ContainSubstring("no P2P or kubernetes configured"))
		}

		By("Checking k3s service is inactive")
		if isFlavor(vm, "alpine") {
			Eventually(func() string {
				out, _ := vm.Sudo("rc-service k3s status")
				return out
			}, 30*time.Second, 10*time.Second).Should(Or(
				ContainSubstring("status: stopped"),
				ContainSubstring("not running")))
		} else {
			Eventually(func() string {
				out, _ := vm.Sudo("systemctl status k3s")
				return out
			}, 30*time.Second, 10*time.Second).Should(Or(
				ContainSubstring("inactive"),
				ContainSubstring("not-found")))
		}
	})
})
