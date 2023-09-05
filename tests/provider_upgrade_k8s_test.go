// nolint
package mos_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

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
			sshconfig := vm.SshConfig()
			cmd := exec.Command("sshpass", []string{"-p", sshconfig.Pass, "ssh", "-v", "-p", sshconfig.Port, fmt.Sprintf("%s@127.0.0.1", sshconfig.User), "true"}...)
			fmt.Printf("Running command sshpass with args %s\n", []string{"-p", sshconfig.Pass, "ssh", "-v", "-p", sshconfig.Port, fmt.Sprintf("%s@127.0.0.1", sshconfig.User), "true"})
			output, err := cmd.CombinedOutput()
			fmt.Printf("OUTPUT of ssh: %s\n", output)
			gatherLogs(vm)
			file, err := os.ReadFile(filepath.Join(vm.StateDir, "serial.log"))
			if err == nil {
				fmt.Println(string(file))
			}
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
			Expect(out).Should(ContainSubstring("loaded (/etc/systemd/system/kairos.service; enabled; vendor preset: disabled)"))

			out, _ = vm.Sudo("systemctl status logrotate.timer")
			Expect(out).Should(ContainSubstring("active (waiting)"))
		}

		sshconfig := vm.SshConfig()
		fmt.Println(sshconfig)
		cmd := exec.Command("sshpass", []string{"-p", sshconfig.Pass, "ssh", "-v", "-p", sshconfig.Port, fmt.Sprintf("%s@127.0.0.1", sshconfig.User), "true"}...)
		fmt.Printf("Running command sshpass with args %s\n", []string{"-p", sshconfig.Pass, "ssh", "-v", "-p", sshconfig.Port, fmt.Sprintf("%s@127.0.0.1", sshconfig.User), "true"})
		output, err := cmd.CombinedOutput()
		fmt.Println(string(output))
		if err != nil {
			fmt.Println(err.Error())
		}
		By("copy the config")
		err = vm.Scp("assets/single.yaml", "/tmp/config.yaml", "0770")
		Expect(err).ToNot(HaveOccurred())

		cmd = exec.Command("sshpass", []string{"-p", sshconfig.Pass, "ssh", "-v", "-p", sshconfig.Port, fmt.Sprintf("%s@127.0.0.1", sshconfig.User), "true"}...)
		output, err = cmd.CombinedOutput()
		fmt.Printf("OUTPUT of ssh: %s\n", output)
		Expect(err).ToNot(HaveOccurred())
		By("installing")
		cmdremote := "kairos-agent --debug manual-install --device /dev/vda /tmp/config.yaml"
		out, err := vm.Sudo(cmdremote)
		fmt.Printf("OUTPUT of install: %s\n", out)
		Expect(err).ToNot(HaveOccurred(), out)
		Expect(out).Should(ContainSubstring("Running after-install hook"))
		fmt.Println(out)

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
				"loaded (/etc/systemd/system/kairos-agent.service; enabled; vendor preset: disabled)"))

			Eventually(func() string {
				out, _ := vm.Sudo("systemctl status systemd-timesyncd")
				return out
			}, 30*time.Second, 10*time.Second).Should(ContainSubstring(
				"loaded (/usr/lib/systemd/system/systemd-timesyncd.service; enabled; vendor preset: disabled)"))
		}

		By("checking if kairos-agent has started")
		Eventually(func() string {
			var out string
			if isFlavor(vm, "alpine") {
				out, _ = vm.Sudo("rc-service kairos-agent status")
			} else {
				out, _ = vm.Sudo("systemctl status kairos-agent")
			}
			fmt.Println(out)
			return out
		}, 900*time.Second, 10*time.Second).Should(Or(ContainSubstring("One time bootstrap starting"), ContainSubstring("status: started")))
		By("Checking agent provider correct start")
		Eventually(func() string {
			out, _ := vm.Sudo("cat /var/log/kairos/agent-provider.log")
			return out
		}, 900*time.Second, 10*time.Second).Should(Or(ContainSubstring("One time bootstrap starting"), ContainSubstring("Sentinel exists")))

		By("Checking k3s is pointing to https")
		Eventually(func() string {
			out, _ := vm.Sudo("cat /etc/rancher/k3s/k3s.yaml")
			return out
		}, 900*time.Second, 10*time.Second).Should(ContainSubstring("https:"))

		By("checking if logs are rotated")
		out, err = vm.Sudo("logrotate -vf /etc/logrotate.d/kairos")
		Expect(err).ToNot(HaveOccurred())
		Expect(out).To(ContainSubstring("log needs rotating"))
		_, err = vm.Sudo("ls /var/log/kairos/agent-provider.log.1.gz")
		Expect(err).ToNot(HaveOccurred())

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

		By("applying upgrade plan")
		err = vm.Scp("assets/suc.yaml", "./suc.yaml", "0770")
		Expect(err).ToNot(HaveOccurred())

		Eventually(func() string {
			out, _ := kubectl(vm, "apply -f suc.yaml")
			return out
		}, 900*time.Second, 10*time.Second).Should(ContainSubstring("unchanged"))

		Eventually(func() string {
			out, _ = kubectl(vm, "get pods -A")
			return out
		}, 900*time.Second, 10*time.Second).Should(ContainSubstring("apply-os-upgrade-on-"), out)

		expectedVersion := getExpectedVersion()

		Eventually(func() string {
			out, _ = kubectl(vm, "get pods -A")
			version, _ := vm.Sudo(getVersionCmd)
			fmt.Printf("version = %+v\n", version)
			return version
		}, 30*time.Minute, 10*time.Second).Should(ContainSubstring(expectedVersion), out)
	})
})

func getExpectedVersion() string {
	b, err := os.ReadFile("assets/suc.yaml")
	Expect(err).ToNot(HaveOccurred())

	yamlData := make(map[string]interface{})
	err = yaml.Unmarshal(b, &yamlData)

	Expect(err).ToNot(HaveOccurred())
	spec := yamlData["spec"].(map[string]interface{})

	return strings.TrimSuffix(spec["version"].(string), "-k3s1")
}
