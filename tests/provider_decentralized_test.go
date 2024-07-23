// nolint
package mos_test

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	. "github.com/spectrocloud/peg/matcher"
)

var _ = Describe("kairos decentralized k8s test", Label("provider", "provider-decentralized-k8s"), func() {
	var vms []VM
	var configPath string

	BeforeEach(func() {
		bridge := os.Getenv("BRIDGE_NETWORK")
		if bridge == "" {
			panic("BRIDGE_NETWORK environment variable not set for provider-decentralized-k8s test")
		}
		_, vm1 := startVMWithBridgeNetwork(bridge)
		_, vm2 := startVMWithBridgeNetwork(bridge)
		vms = append(vms, vm1, vm2)

		configPath = cloudConfig()

		vmForEach("waiting until ssh is possible", vms, func(vm VM) {
			vm.EventuallyConnects(1200)
		})
	})

	AfterEach(func() {
		if CurrentGinkgoTestDescription().Failed {
			gatherLogs(vms[0])
		}
		vmForEach("destroying vm", vms, func(vm VM) {
			vm.Destroy(nil)
		})
		os.RemoveAll(configPath)
	})

	It("installs to disk with custom config", func() {
		vmForEach("checking if it has default service active", vms, func(vm VM) {
			if isFlavor(vm, "alpine") {
				out, _ := vm.Sudo("rc-status")
				Expect(out).Should(ContainSubstring("kairos-agent"))
			} else {
				out, _ := vm.Sudo("systemctl status kairos")
				Expect(out).Should(ContainSubstring("loaded (/etc/systemd/system/kairos.service; enabled"))
			}
		})

		vmForEach("installing", vms, func(vm VM) {
			err := vm.Scp(configPath, "/tmp/config.yaml", "0770")
			Expect(err).ToNot(HaveOccurred())

			out, _ := vm.Sudo("kairos-agent manual-install --device auto /tmp/config.yaml")
			Expect(out).Should(ContainSubstring("Running after-install hook"), out)
			vm.Reboot()

			By("waiting until it reboots to installed system")
			Eventually(func() string {
				v, _ := vm.Sudo("kairos-agent state get boot")
				return strings.TrimSpace(v)
			}, 30*time.Minute, 10*time.Second).Should(ContainSubstring("active_boot"))
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
					out, _ = vm.Sudo("cat /var/log/kairos/agent-provider.log")
					return out
				}, 10*time.Minute, 1*time.Second).Should(
					Or(
						ContainSubstring("Configuring k3s-agent"),
						ContainSubstring("Configuring k3s"),
					), out)
			}
		})

		vmForEach("checking if it has a working kubeconfig", vms, func(vm VM) {
			var out string
			Eventually(func() string {
				out, _ = vm.Sudo("kairos get-kubeconfig")
				return out
			}, 1500*time.Second, 10*time.Second).Should(ContainSubstring("https:"), out)

			Eventually(func() string {
				vm.Sudo("kairos get-kubeconfig > kubeconfig")
				out, _ = vm.Sudo("KUBECONFIG=kubeconfig kubectl get nodes -o wide")
				return out
			}, 900*time.Second, 10*time.Second).Should(ContainSubstring("Ready"), out)
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
					var err error
					out, err = vm.Sudo(`curl -X POST http://localhost:8080/api/dns --header "Content-Type: application/json" -d '{ "Regex": "foo.bar", "Records": { "A": "2.2.2.2" } }'`)
					fmt.Println(out)
					Expect(err).ToNot(HaveOccurred(), out)

					out, _ = vm.Sudo("dig +short foo.bar")
					fmt.Println(out)
					return strings.TrimSpace(out)
				}, 900*time.Second, 10*time.Second).Should(Equal("2.2.2.2"), out)
				Eventually(func() string {
					out, _ = vm.Sudo("dig +short google.com")
					return strings.TrimSpace(out)
				}, 900*time.Second, 10*time.Second).ShouldNot(BeEmpty(), out)
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
	for i, vm := range vms {
		By(fmt.Sprintf("%s [%s]", description, strconv.Itoa(i+1)))
		action(vm)
	}
}

func cloudConfig() string {
	token := generateToken()

	configBytes, err := os.ReadFile("assets/config.yaml")
	Expect(err).ToNot(HaveOccurred())

	config := fmt.Sprintf(`%s
RE
p2p:
  network_token: %s
  dns: true
`, string(configBytes), token)

	f, err := os.CreateTemp("", "kairos-config-*.yaml")
	Expect(err).ToNot(HaveOccurred())
	defer f.Close()

	_, err = f.WriteString(config)
	Expect(err).ToNot(HaveOccurred())

	return f.Name()
}
