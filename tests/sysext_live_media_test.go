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

// The extension the ISO ships. tests/assets/sysext is mounted as the
// auroraboot --overlay-iso directory by reusable-factory.yaml, so both images
// in it land at the ISO root and therefore under /run/initramfs/live while the
// installer runs.
const liveMediaExtension = "work.sysext.raw"

// The other image in that directory, built with neither verity nor a
// signature. It fails the image policy the non-UKI systemd-sysext drop-in
// enforces, so immucore has to skip it the same way it already does on the UKI
// path (tests/uki_test.go).
const brokenMediaExtension = "hello-broke.sysext.raw"

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

			// The agent refuses to install when no user is in the admin
			// group: "no users found in any stage that are part of the
			// 'admin' group". Every other install spec declares it.
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

			// The media also carries brokenMediaExtension, which has neither
			// verity nor a signature. systemd-sysext refreshes all or nothing,
			// so if immucore links it the extension above does not merge
			// either, and neither does any bundle the node installed.
			By("leaving the extension that cannot pass the policy alone", func() {
				out, err := vm.Sudo("ls /run/extensions")
				Expect(err).ToNot(HaveOccurred(), out)
				Expect(out).ToNot(ContainSubstring(brokenMediaExtension))
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

				var merged bool
				for _, sysext := range sysexts {
					if sysext.Hierarchy == "/usr/local/bin" {
						Expect(sysext.Extensions).To(ContainElement("work"))
						merged = true
					}
				}
				Expect(merged).To(BeTrue(), "no /usr/local/bin hierarchy in %s", out)
			})

			By("running a command the extension provides", func() {
				out, err := vm.Sudo("hello.sh")
				Expect(err).ToNot(HaveOccurred(), out)
				Expect(out).To(ContainSubstring("Hello world"))
			})
		})
	})
})
