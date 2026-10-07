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

// The extension the ISO ships. The generated test extension directory is
// mounted as the auroraboot --overlay-iso directory by the CI workflows on
// non-trusted-boot cells, so the image in it lands at the ISO root and
// therefore under /run/initramfs/live while the installer runs. Its
// work.sysext.raw is verity-only (unsigned) so systemd-sysext can activate
// it on a boot with no test signing key enrolled.
const liveMediaExtension = "work.sysext.raw"

// The hierarchy list the kairos drop-in installs, spelled out so that
// `systemd-sysext status` reports on what the boot merged instead of on its
// own defaults. /usr/local is deliberately not in it: it is the persistent
// partition mount, and a merge would turn it read-only.
const sysextHierarchiesEnv = `SYSTEMD_SYSEXT_HIERARCHIES="/usr/bin:/usr/share:/usr/lib:/usr/include:/usr/src:/usr/sbin"`

// Both test extensions carry their payload at /usr/bin/hello.sh, which is a
// merged hierarchy, so a successful merge puts the script on the host's PATH.
// Running it is the strongest proof the overlay went up: it needs the image to
// be accepted by the boot's image policy, merged, and visible to a fresh
// process. pkg/testartifacts builds both images with that payload.
const (
	mergedExtensionHierarchy = "/usr/bin"
	mergedExtensionCommand   = "hello.sh"
	mergedExtensionOutput    = "Hello world"
)

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
				// defaults rather than on what this boot merged.
				out, err := vm.Sudo(fmt.Sprintf("%s systemd-sysext --json=short", sysextHierarchiesEnv))
				Expect(err).ToNot(HaveOccurred(), out)

				var sysexts sysextStatus
				Expect(json.Unmarshal([]byte(out), &sysexts)).ToNot(HaveOccurred())

				var merged bool
				for _, sysext := range sysexts {
					if sysext.Hierarchy == mergedExtensionHierarchy {
						Expect(sysext.Extensions).To(ContainElement("work"))
						merged = true
					}
				}
				Expect(merged).To(BeTrue(), "no %s hierarchy in %s", mergedExtensionHierarchy, out)
			})

			By("running a command the extension provides", func() {
				out, err := vm.Sudo(mergedExtensionCommand)
				Expect(err).ToNot(HaveOccurred(), out)
				Expect(out).To(ContainSubstring(mergedExtensionOutput))
			})
		})
	})
})
