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

// The extension the ISO ships. tests/assets/sysext is mounted as the
// auroraboot --overlay-iso directory by reusable-factory.yaml, so both images
// in it land at the ISO root and therefore under /run/initramfs/live while the
// installer runs.
const liveMediaExtension = "work.sysext.raw"

// Coverage for the GRUB half of the live media extension sweep. The UKI half
// is asserted in uki_test.go, where the extension reaches the EFI partition.
// Here it has to reach /var/lib/kairos/extensions on persistent, be enabled
// for the booted state, and be linked into /run/extensions after the reboot,
// under the policy the non UKI drop-in installs.
//
// The merge itself is asserted in uki_test.go and not here. work.sysext.raw
// carries a dm-verity root hash signed with tests/assets/keys/db.key. Trusted
// Boot enrolls that key in the Secure Boot db, so the kernel accepts the
// signature there. A GRUB install enrolls nothing, so dm-verity refuses the
// root hash with ENOKEY and the hierarchies stay empty, however the extension
// reached them. Asserting the merge here would be asserting an enrolled key.
var _ = Describe("kairos live media extensions", Label("sysext"), func() {
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
			gatherLogs(vm)
		}
		Expect(vm.Destroy(nil)).ToNot(HaveOccurred())
	})

	Context("on a GRUB install", func() {
		It("installs the extension shipped on the ISO and offers it under the GRUB policy", func() {
			By("finding the extension on the live media", func() {
				out, err := vm.Sudo("ls /run/initramfs/live")
				Expect(err).ToNot(HaveOccurred(), out)
				Expect(out).To(ContainSubstring(liveMediaExtension))
			})

			_ = testInstall(`#cloud-config
users:
- name: "kairos"
  passwd: "kairos"
  groups:
    - "admin"
`, vm)

			By("keeping the extension on the persistent partition", func() {
				out, err := vm.Sudo("ls /var/lib/kairos/extensions")
				Expect(err).ToNot(HaveOccurred(), out)
				Expect(out).To(ContainSubstring(liveMediaExtension))
			})

			By("enabling the extension for the booted state", func() {
				out, err := vm.Sudo("ls -l /var/lib/kairos/extensions/active")
				Expect(err).ToNot(HaveOccurred(), out)
				Expect(out).To(ContainSubstring(liveMediaExtension))
			})

			By("linking the extension into /run/extensions", func() {
				Eventually(func() string {
					out, _ := vm.Sudo("ls /run/extensions")
					return out
				}, 5*time.Minute, 10*time.Second).Should(ContainSubstring(liveMediaExtension))
			})

			By("enforcing the policy a GRUB install asks for", func() {
				// 99_sysext.yaml writes one of two drop-ins, chosen by
				// whether the boot is a UKI one. Picking the wrong one is
				// silent: the UKI policy wants a signature, so a GRUB
				// install that got it would refuse every extension the
				// hub publishes unsigned.
				out, err := vm.Sudo("cat /etc/systemd/system/systemd-sysext.service.d/kairos.conf")
				Expect(err).ToNot(HaveOccurred(), out)
				Expect(out).To(ContainSubstring(`--image-policy="root=verity+absent:usr=verity+absent"`))
				Expect(out).To(ContainSubstring("SYSTEMD_SYSEXT_HIERARCHIES=/usr/local/bin:"))

				out, err = vm.Sudo("stat /etc/systemd/system/systemd-sysext.service.d/kairos-uki.conf")
				Expect(err).To(HaveOccurred(), out)
			})
		})
	})
})
