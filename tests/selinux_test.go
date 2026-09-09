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

// SELinux e2e: install with install.selinux enabled, reboot, assert the
// kernel cmdline, the OEM grubenv, the relabel unit, and the live
// getenforce state. Runs in exactly two jobs, defined in pr.yaml and
// master.yaml: test-core-rocky-9 on rockylinux/rockylinux:9 and
// test-core-leap on opensuse/leap:16.0, because the package stage
// installs selinux-policy-targeted and policycoreutils only on the RHEL
// and SUSE families.
var _ = Describe("kairos selinux test", Label("selinux"), func() {
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
		_ = vm.Destroy(nil)
	})

	It("boots permissive when install.selinux.mode is permissive", func() {
		_ = testInstall(`#cloud-config
install:
  selinux:
    enabled: true
    mode: permissive
users:
- name: "kairos"
  passwd: "kairos"
  groups:
    - "admin"
`, vm)

		By("checking the kernel cmdline", func() {
			Eventually(func() string {
				out, _ := vm.Sudo("cat /proc/cmdline")
				return out
			}, 2*time.Minute, 5*time.Second).Should(And(
				ContainSubstring("selinux=1"),
				ContainSubstring("enforcing=0"),
				ContainSubstring("rd.cos.selinux=permissive"),
			))
		})

		By("checking the OEM grubenv", func() {
			Eventually(func() string {
				out, _ := vm.Sudo("cat /oem/grubenv")
				return out
			}, 2*time.Minute, 5*time.Second).Should(And(
				ContainSubstring("selinux_enabled=true"),
				ContainSubstring("selinux_mode=permissive"),
			))
		})

		By("checking the relabel unit pins permissive", func() {
			out, err := vm.Sudo("cat /etc/systemd/system/kairos-selinux-relabel.service")
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(ContainSubstring("setenforce 0"))
			Expect(out).To(ContainSubstring("restorecon -Riv"))
		})

		By("checking live mode is permissive", func() {
			// The relabel unit runs restorecon over /etc /var /home /oem
			// before multi-user; give it room before reading getenforce.
			Eventually(func() string {
				out, _ := vm.Sudo("getenforce")
				return out
			}, 5*time.Minute, 10*time.Second).Should(ContainSubstring("Permissive"))
		})
	})

	It("boots enforcing when install.selinux.mode is enforcing", func() {
		_ = testInstall(`#cloud-config
install:
  selinux:
    enabled: true
    mode: enforcing
users:
- name: "kairos"
  passwd: "kairos"
  groups:
    - "admin"
`, vm)

		By("checking the kernel cmdline", func() {
			Eventually(func() string {
				out, _ := vm.Sudo("cat /proc/cmdline")
				return out
			}, 2*time.Minute, 5*time.Second).Should(And(
				ContainSubstring("selinux=1"),
				ContainSubstring("enforcing=0"),
				ContainSubstring("rd.cos.selinux=enforcing"),
			))
		})

		By("checking the relabel unit pins enforcing", func() {
			out, err := vm.Sudo("cat /etc/systemd/system/kairos-selinux-relabel.service")
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(ContainSubstring("setenforce 1"))
		})

		By("checking /etc/selinux/config matches the requested mode", func() {
			out, err := vm.Sudo("cat /etc/selinux/config")
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(ContainSubstring("SELINUX=enforcing"))
		})

		By("checking live mode is enforcing after relabel", func() {
			// The kernel boots permissive (selinux=1 enforcing=0) and the
			// relabel unit promotes to enforcing after restorecon -- this
			// Eventually is the actual feature assertion.
			Eventually(func() string {
				out, _ := vm.Sudo("getenforce")
				return out
			}, 5*time.Minute, 10*time.Second).Should(ContainSubstring("Enforcing"))
		})
	})

	It("stays off when install.selinux is not set", func() {
		_ = testInstall(`#cloud-config
install:
  bind_mounts:
  - /var/bind1
users:
- name: "kairos"
  passwd: "kairos"
  groups:
    - "admin"
`, vm)

		By("checking no selinux tokens on the kernel cmdline", func() {
			out, err := vm.Sudo("cat /proc/cmdline")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).ToNot(ContainSubstring("selinux=1"))
			Expect(out).ToNot(ContainSubstring("rd.cos.selinux"))
		})

		By("checking the relabel unit does not exist", func() {
			// Same pattern as install_test: ignore the exit code, assert
			// on the message.
			Eventually(func() string {
				out, _ := vm.Sudo("systemctl status kairos-selinux-relabel")
				return out
			}, 1*time.Minute, 2*time.Second).Should(ContainSubstring("Unit kairos-selinux-relabel.service could not be found"))
		})

		By("checking no selinux vars in the OEM grubenv", func() {
			out, err := vm.Sudo("grep selinux /oem/grubenv 2>/dev/null || true")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(BeEmpty())
		})
	})
})
