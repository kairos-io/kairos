package mos_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
)

// The extension the ISO ships. tests/assets/sysext-grub is mounted as the
// auroraboot --overlay-iso directory by _build-iso.yaml on non-trusted-boot
// cells, so its work.sysext.raw lands at the ISO root and therefore under
// /run/initramfs/live while the installer runs. That image is verity-only
// (unsigned) so systemd-sysext can activate it on a boot with no test
// signing key enrolled.
const liveMediaExtension = "work.sysext.raw"

// Coverage for the GRUB half of the live media extension sweep. The UKI half
// is asserted in uki_test.go, where the extension reaches the EFI partition.
// Here it has to reach /var/lib/kairos/extensions on persistent, be enabled
// for the booted state, and be merged by systemd-sysext after the reboot.
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
		It("installs the extension shipped on the ISO and merges it after reboot", func() {
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

			By("merging the extension", func() {
				type sysextStatus []struct {
					Hierarchy  string `json:"hierarchy"`
					Extensions any    `json:"extensions"`
				}

				// The hierarchies have to be spelled out the same way the
				// kairos drop-in does, or systemd-sysext reports on its own
				// defaults and misses the /usr/local ones.
				env := "SYSTEMD_SYSEXT_HIERARCHIES=\"/usr/local/bin:/usr/local/sbin:/usr/local/include:/usr/local/lib:/usr/local/share:/usr/local/src:/usr/bin:/usr/share:/usr/lib:/usr/include:/usr/src:/usr/sbin\""
				out, err := vm.Sudo(fmt.Sprintf("%s systemd-sysext --json=short", env))
				Expect(err).ToNot(HaveOccurred(), out)

				var sysexts sysextStatus
				Expect(json.Unmarshal([]byte(out), &sysexts)).ToNot(HaveOccurred())

				// The extension's payload lives under /usr/local/include so
				// the overlay it creates does not land on /usr/local/bin, which
				// would be read-only after the merge on this systemd version
				// and would break every install path that writes there
				// (bundles with rootfs_path=/usr/local/bin, kairos-agent's
				// fix-home-dir-ownership, ...). See tests/assets/sysext-grub
				// for the constraint. Assert the extension merged into any
				// hierarchy rather than pinning /usr/local/bin.
				var merged bool
				for _, sysext := range sysexts {
					exts, ok := sysext.Extensions.([]interface{})
					if !ok {
						continue
					}
					for _, e := range exts {
						if e == "work" {
							merged = true
							break
						}
					}
					if merged {
						break
					}
				}
				Expect(merged).To(BeTrue(), "no hierarchy has 'work' merged in %s", out)
			})

			By("running a command the extension provides", func() {
				out, err := vm.Sudo("/usr/local/include/kairos-test/hello.sh")
				Expect(err).ToNot(HaveOccurred(), out)
				Expect(out).To(ContainSubstring("Hello world"))
			})
		})
	})
})
