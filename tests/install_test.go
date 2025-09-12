package mos_test

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	. "github.com/spectrocloud/peg/matcher"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var stateAssertVM = func(vm VM, query, expected string) {
	out, err := vm.Sudo(fmt.Sprintf("kairos-agent state get %s", query))
	ExpectWithOffset(1, err).ToNot(HaveOccurred(), out)
	ExpectWithOffset(1, out).To(ContainSubstring(expected))
}

func testInstall(cloudConfig string, vm VM) string { //, actual interface{}, m types.GomegaMatcher) {
	stateAssertVM(vm, "persistent.found", "false")

	t, err := os.CreateTemp("", "test")
	ExpectWithOffset(1, err).ToNot(HaveOccurred())

	defer os.RemoveAll(t.Name())
	err = os.WriteFile(t.Name(), []byte(cloudConfig), os.ModePerm)
	Expect(err).ToNot(HaveOccurred())

	err = vm.Scp(t.Name(), "/tmp/config.yaml", "0770")
	Expect(err).ToNot(HaveOccurred())

	var out string
	By("installing kairos", func() {
		out, err = vm.Sudo(`kairos-agent --debug manual-install --device "auto" /tmp/config.yaml`)
		Expect(err).ToNot(HaveOccurred(), out)
		Expect(out).Should(ContainSubstring("Running after-install hook"))
		vm.Sudo("sync")
	})

	By("waiting for VM to reboot", func() {
		vm.Reboot()
		vm.EventuallyConnects(1200)
	})

	return out
}

var _ = Describe("kairos install test", Label("install"), func() {

	var vm VM
	BeforeEach(func() {
		_, vm = startVM()
		vm.EventuallyConnects(1200)
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			serial, _ := os.ReadFile(filepath.Join(vm.StateDir, "serial.log"))
			_ = os.MkdirAll("logs", os.ModePerm|os.ModeDir)
			_ = os.WriteFile(filepath.Join("logs", "serial.log"), serial, os.ModePerm)
			fmt.Println(string(serial))
		}

		if CurrentSpecReport().Failed() {
			gatherLogs(vm)
		}
		Expect(vm.Destroy(nil)).ToNot(HaveOccurred())
	})

	Context("install", func() {
		It("cloud-config syntax mixed with extended syntax", func() {

			expectSecureBootEnabled(vm)

			_ = testInstall(`#cloud-config
install:
  bind_mounts:
  - /var/bind1
  - /var/bind2
  ephemeral_mounts:
  - /var/ephemeral
  - /var/ephemeral2
users:
- name: "kairos"
  passwd: "kairos"
stages:
  initramfs:
  - name: "Set user and password"
    users:
      kairos:
         passwd: "kairos"
         groups:
           - "admin"
    commands:
    - echo "bar" > /etc/foo
bundles:
- rootfs_path: "/usr/local/bin"
  targets:
  - container://quay.io/mocaccino/extra:edgevpn-utils-0.15.0
`, vm)

			expectSecureBootEnabled(vm)

			Eventually(func() string {
				out, _ := vm.Sudo("cat /etc/foo")
				return out
			}, 5*time.Minute, 10*time.Second).Should(ContainSubstring("bar"))

			Eventually(func() string {
				out, _ := vm.Sudo("cat /run/cos/cos-layout.env")
				return out
			}, 5*time.Minute, 10*time.Second).Should(ContainSubstring("/var/bind1 /var/bind2"))
			Eventually(func() string {
				out, _ := vm.Sudo("cat /run/cos/cos-layout.env")
				return out
			}, 5*time.Minute, 10*time.Second).Should(ContainSubstring("/var/ephemeral /var/ephemeral2"))

			Eventually(func() string {
				out, _ := vm.Sudo("/usr/local/bin/usr/bin/edgevpn --help | grep peer")
				return out
			}, 5*time.Minute, 10*time.Second).Should(ContainSubstring("peerguard"))

			stateAssertVM(vm, "persistent.found", "true")
			By("Checking install/recovery services are disabled", func() {
				if !isFlavor(vm, "alpine") {
					for _, service := range []string{"kairos-interactive", "kairos-recovery"} {
						By(fmt.Sprintf("Checking that service %s does not exist", service), func() {})
						Eventually(func() string {
							out, _ := vm.Sudo(fmt.Sprintf("systemctl status %s", service))
							return out
						}, 3*time.Minute, 2*time.Second).Should(
							And(
								ContainSubstring(fmt.Sprintf("Unit %s.service could not be found", service)),
							),
						)
					}
				}
			})
		})

		Context("with config_url", func() {
			It("succeeds when config_url is accessible", func() {
				testInstall(`#cloud-config
config_url: "https://gist.githubusercontent.com/Itxaka/c94e42bd52a67e2c9bffd11b8e633e38/raw/255d17fce7ed6857f82e907d261ce4a717662773/testgist.yaml"
users:
- name: "kairos"
  passwd: "kairos"
  groups:
    - "admin"
`, vm)

				Eventually(func() string {
					out, err := vm.Sudo("kairos-agent state")
					Expect(err).ToNot(HaveOccurred())
					return out
				}, 5*time.Minute, 10*time.Second).Should(ContainSubstring("boot: active_boot"))
			})

			It("succeeds when config_url is not accessible (and prints a warning)", func() {
				out := testInstall(`#cloud-config
config_url: "https://thisurldoesntexist.org"
users:
- name: "kairos"
  passwd: "kairos"
  groups:
    - "admin"
`, vm)
				Expect(out).ToNot(ContainSubstring("kairos-agent.service: Failed with result"))

				Eventually(func() string {
					out, err := vm.Sudo("kairos-agent state")
					Expect(err).ToNot(HaveOccurred())
					return out
				}, 5*time.Minute, 10*time.Second).Should(ContainSubstring("boot: active_boot"))
			})
		})
	})
})
