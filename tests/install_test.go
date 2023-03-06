package mos_test

import (
	"fmt"
	"os"
	"time"

	. "github.com/spectrocloud/peg/matcher"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
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
		out, err = vm.Sudo(`kairos-agent manual-install --device "auto" /tmp/config.yaml`)
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

func eventuallyAssert(vm VM, cmd string, m types.GomegaMatcher) {
	Eventually(func() string {
		out, _ := vm.Sudo(cmd)
		return out
	}, 5*time.Minute, 10*time.Second).Should(m)
}

var _ = Describe("kairos install test", Label("install-test"), func() {

	var vm VM
	BeforeEach(func() {

		_, vm = startVM()
		vm.EventuallyConnects(1800)
	})

	AfterEach(func() {
		Expect(vm.Destroy(nil)).ToNot(HaveOccurred())
	})

	Context("install", func() {
		It("cloud-config syntax mixed with extended syntax", func() {
			_ = testInstall(`#cloud-config
install:
  bind_mounts:
  - /mnt/bind1
  - /mnt/bind2
  ephemeral_mounts:
  - /mnt/ephemeral
  - /mnt/ephemeral2
users:
- name: "kairos"
  passwd: "kairos"
stages:
  initramfs:
  - name: "Set user and password"
    users:
      kairos:
         passwd: "kairos"
    commands:
    - echo "bar" > /etc/foo
bundles:
- rootfs_path: "/usr/local/bin"
  targets:
  - container://quay.io/mocaccino/extra:edgevpn-utils-0.15.0
`, vm)

			Eventually(func() string {
				out, _ := vm.Sudo("cat /etc/foo")
				return out
			}, 5*time.Minute, 10*time.Second).Should(ContainSubstring("bar"))

			Eventually(func() string {
				out, _ := vm.Sudo("cat /run/cos/cos-layout.env")
				return out
			}, 5*time.Minute, 10*time.Second).Should(ContainSubstring("PERSISTENT_STATE_PATHS=\"/mnt/bind1 /mnt/bind2"))
			Eventually(func() string {
				out, _ := vm.Sudo("cat /run/cos/cos-layout.env")
				return out
			}, 5*time.Minute, 10*time.Second).Should(ContainSubstring("RW_PATHS=\"/mnt/ephemeral /mnt/ephemeral2"))

			Eventually(func() string {
				out, _ := vm.Sudo("/usr/local/bin/usr/bin/edgevpn --help | grep peer")
				return out
			}, 5*time.Minute, 10*time.Second).Should(ContainSubstring("peerguard"))

			stateAssertVM(vm, "persistent.found", "true")
		})

		Context("with config_url", func() {
			It("succeeds when config_url is accessible", func() {
				testInstall(`#cloud-config
config_url: "https://gist.githubusercontent.com/mudler/6db795bad8f9e29ebec14b6ae331e5c0/raw/01137c458ad62cfcdfb201cae2f8814db702c6f9/testgist.yaml"
users:
- name: "kairos"
  passwd: "kairos"
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
`, vm)
				Expect(out).ToNot(ContainSubstring("kairos-agent.service: Failed with result"))
				Expect(out).To(ContainSubstring("WARNING: Couldn't fetch config_url: could not merge configs"))

				Eventually(func() string {
					out, err := vm.Sudo("kairos-agent state")
					Expect(err).ToNot(HaveOccurred())
					return out
				}, 5*time.Minute, 10*time.Second).Should(ContainSubstring("boot: active_boot"))
			})
		})
	})
})
