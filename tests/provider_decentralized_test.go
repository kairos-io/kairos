// nolint
package mos_test

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	. "github.com/spectrocloud/peg/matcher"
)

var _ = Describe("kairos decentralized k8s test", Label("provider", "provider-decentralized-k8s"), func() {
	var vms []VM
	var configPath string
	var token string

	BeforeEach(func() {
		_, vm1 := startVM()
		_, vm2 := startVM()
		vms = append(vms, vm1, vm2)

		token = generateToken()

		vmForEach("waiting until ssh is possible", vms, func(vm VM) {
			vm.EventuallyConnects(1200)
		})
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			gatherLogs(vms[0])
		}
		vmForEach("destroying vm", vms, func(vm VM) {
			vm.Destroy(nil)
		})
		os.RemoveAll(configPath)
	})

	It("installs to disk with custom config", func() {
		vmForEach("checking if it has default service active", vms, func(vm VM) {
			expectDefaultService(vm)
		})

		vmForEach("installing", vms, func(vm VM) {
			// Generate a random but fixed hostname for each vm
			// So on reboot the hostname doesnt change and k3s gets restarted and has to build the nodes again
			configPath = cloudConfig(token)
			err := vm.Scp(configPath, "/tmp/config.yaml", "0770")
			Expect(err).ToNot(HaveOccurred())

			out, _ := vm.Sudo("kairos-agent manual-install --device auto /tmp/config.yaml")
			Expect(out).Should(ContainSubstring("Running after-install hook"))
			vm.Reboot()

			By("waiting until it reboots to installed system")
			Eventually(func() string {
				v, _ := vm.Sudo("kairos-agent state get boot")
				return strings.TrimSpace(v)
			}, 30*time.Minute, 10*time.Second).Should(ContainSubstring("active_boot"))
		})

		vmForEach("checking default services are on after first boot", vms, func(vm VM) {
			if isFlavor(vm, "alpine") {
				Eventually(func() string {
					out, _ := vm.Sudo("rc-status")
					return out
				}, 30*time.Second, 10*time.Second).Should(And(
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
		})

		vmForEach("checking if it has correct grub menu entries", vms, func(vm VM) {
			if !isFlavor(vm, "alpine") {
				state, _ := vm.Sudo("blkid -L COS_STATE")
				state = strings.TrimSpace(state)
				out, err := vm.Sudo("blkid")
				Expect(err).ToNot(HaveOccurred(), out)
				out, err = vm.Sudo("mkdir -p /tmp/mnt/STATE")
				Expect(err).ToNot(HaveOccurred(), out)
				out, err = vm.Sudo("mount " + state + " /tmp/mnt/STATE")
				Expect(err).ToNot(HaveOccurred(), out)
				out, err = vm.Sudo("cat /tmp/mnt/STATE/grubmenu")
				Expect(err).ToNot(HaveOccurred(), out)

				Expect(out).To(ContainSubstring("--id remoterecovery"))

				// No longer used. This is created to override the default entry but now the default entry is kairos already
				// TODO: Create a test in acceptance to check for the creation of this file and if it has the correct override entry
				//grub, err := vm.Sudo("cat /tmp/mnt/STATE/grub_oem_env")
				//Expect(err).ToNot(HaveOccurred(), grub)
				//Expect(grub).Should(ContainSubstring("default_menu_entry=Kairos"))

				out, err = vm.Sudo("umount /tmp/mnt/STATE")
				Expect(err).ToNot(HaveOccurred(), out)
			}
		})

		vmForEach("checking if k3s was configured", vms, func(vm VM) {
			out, err := vm.Sudo("cat /run/cos/live_mode")
			Expect(err).To(HaveOccurred(), out)
			if isFlavor(vm, "alpine") {
				// Skip for now as agent doesn't log anymore as it cannot behave both as a one-off and a daemon
				/*
					Eventually(func() string {
						out, _ = vm.Sudo("sudo cat /var/log/kairos/agent.log")
						return out
					}, 20*time.Minute, 1*time.Second).Should(
						Or(
							ContainSubstring("Configuring k3s-agent"),
							ContainSubstring("Configuring k3s"),
						), out)
				*/
			} else {
				Eventually(func() string {
					out, _ = vm.Sudo("journalctl -t kairos-provider")
					return out
				}, 10*time.Minute, 1*time.Second).Should(
					Or(
						ContainSubstring("Configuring k3s worker"),
						ContainSubstring("Configuring k3s"),
					), out)
			}
		})

		vmForEach("checking if it has a working kubeconfig", vms, func(vm VM) {
			var out string
			Eventually(func() string {
				out, _ = vm.Sudo("kairos get-kubeconfig")
				return out
			}, 1500*time.Second, 10*time.Second).Should(ContainSubstring("https:"))

			Eventually(func() string {
				vm.Sudo("kairos get-kubeconfig > kubeconfig")
				out, _ = vm.Sudo("KUBECONFIG=kubeconfig kubectl get nodes -o wide")
				return out
			}, 900*time.Second, 10*time.Second).Should(ContainSubstring("Ready"))
		})

		vmForEach("checking roles", vms, func(vm VM) {
			var out string
			uuid, err := vm.Sudo("kairos-agent uuid")
			Expect(err).ToNot(HaveOccurred(), uuid)
			Expect(uuid).ToNot(Equal(""))
			Eventually(func() string {
				out, _ = vm.Sudo("kairos role list")
				return out
			}, 900*time.Second, 10*time.Second).Should(And(
				ContainSubstring(uuid),
				ContainSubstring("worker"),
				ContainSubstring("master"),
				HaveMinMaxRole("master", 1, 1),
				HaveMinMaxRole("worker", 1, 1),
			), out)
		})

		vmForEach("checking if it has machines with different IPs", vms, func(vm VM) {
			var out string
			Eventually(func() string {
				out, _ = vm.Sudo(`curl http://localhost:8080/api/machines`)
				return out
			}, 900*time.Second, 10*time.Second).Should(And(
				ContainSubstring("10.1.0.1"),
				ContainSubstring("10.1.0.2"),
			), out)
		})

		vmForEach("checking if it can propagate dns and it is functional", vms, func(vm VM) {
			if !isFlavor(vm, "alpine") {
				// FIXUP: DNS needs reboot to take effect
				vm.Reboot(1200)
				out := ""
				Eventually(func() string {
					// First, set up the DNS record via the API
					// Ignore errors here - the API might not be ready yet after reboot
					_, _ = vm.Sudo(`curl -X POST http://localhost:8080/api/dns --header "Content-Type: application/json" -d '{ "Regex": "foo.bar", "Records": { "A": "2.2.2.2" } }'`)

					// Then check if DNS resolution works
					out, _ = vm.Sudo("curl -s -o /dev/null -w '%{remote_ip}' http://foo.bar")
					return strings.TrimSpace(out)
				}, 240*time.Second, 10*time.Second).Should(MatchRegexp("2\\.2\\.2\\.2"), func() string {
					fmt.Printf("DNS is not working: %s", out)
					return out
				})
			}
		})
	})
})

func HaveMinMaxRole(name string, min, max int) types.GomegaMatcher {
	return WithTransform(
		func(actual interface{}) (int, error) {
			switch s := actual.(type) {
			case string:
				return strings.Count(s, name), nil
			default:
				return 0, fmt.Errorf("HaveRoles expects a string, but got %T", actual)
			}
		}, SatisfyAll(
			BeNumerically(">=", min),
			BeNumerically("<=", max)))
}

func vmForEach(description string, vms []VM, action func(vm VM)) {
	var wg sync.WaitGroup
	for i, vm := range vms {
		wg.Add(1)
		go func(i int, vm VM) {
			defer wg.Done()
			defer GinkgoRecover()
			By(fmt.Sprintf("%s [%s]", description, strconv.Itoa(i+1)))
			action(vm)
		}(i, vm)
	}
	wg.Wait()
}

func cloudConfig(token string) string {
	config := fmt.Sprintf(`#cloud-config

install:
  grub_options:
    extra_cmdline: "rd.immucore.debug"

stages:
  initramfs:
    - name: "Set user and password"
      users:
        kairos:
          passwd: "kairos"
          groups:
            - "admin"
      hostname: kairos-{{ trunc 4 .Random }}

k3s:
  enabled: true

p2p:
  network_token: %s
  dns: true
`, token)

	f, err := os.CreateTemp("", "kairos-config-*.yaml")
	Expect(err).ToNot(HaveOccurred())
	defer f.Close()

	_, err = f.WriteString(config)
	Expect(err).ToNot(HaveOccurred())

	return f.Name()
}
