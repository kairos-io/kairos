package mos_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
)

var _ = Describe("kairos UKI test", Label("uki"), Ordered, func() {
	var vm VM

	BeforeAll(func() {
		if os.Getenv("FIRMWARE") == "" {
			Fail("FIRMWARE environment variable set to a EFI firmware is needed for UKI test")
		}
	})

	BeforeEach(func() {
		if os.Getenv("DATASOURCE") == "" {
			Fail("DATASOURCE must be set and it should be the absolute path to a datasource iso")
		}
		_, vm = startVM()
		vm.EventuallyConnects(300)
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
		
		err := vm.Destroy(nil)
		Expect(err).ToNot(HaveOccurred())
	})
	It("passes checks", func() {
		By("Checking SecureBoot is enabled", func() {
			out, err := vm.Sudo(`dmesg|grep -i secure| grep -i enabled`)
			Expect(err).ToNot(HaveOccurred(), out)
		})
		By("Checking the boot mode (install)", func() {
			out, err := vm.Sudo("stat /run/cos/uki_install_mode")
			Expect(err).ToNot(HaveOccurred(), out)
		})
		By("Checking OEM/PERSISTENT are not mounted", func() {
			out, err := vm.Sudo("mount")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).ToNot(ContainSubstring("/dev/disk/by-label/COS_OEM"))
			Expect(out).ToNot(ContainSubstring("/dev/disk/by-label/COS_PERSISTENT"))
		})
		By("installing kairos", func() {
			// Install has already started, so we can use Eventually here to track the logs
			Eventually(func() string {
				out, err := vm.Sudo("cat /var/log/kairos/agent*.log")
				Expect(err).ToNot(HaveOccurred())
				return out
			}, 5*time.Minute).Should(And(
				ContainSubstring("Running after-install hook"),
				ContainSubstring("Encrypting COS_OEM"),
				ContainSubstring("Encrypting COS_PERSISTENT"),
				ContainSubstring("Done encrypting COS_OEM"),
				ContainSubstring("Done encrypting COS_PERSISTENT"),
				ContainSubstring("Done executing stage 'kairos-uki-install.after.after'"),
				ContainSubstring("Unmounting disk partitions"),
			))
			vm.Sudo("sync")
			time.Sleep(10 * time.Second)
		})

		By("Ejecting Cdrom", func() {
			vm.DetachCD()
		})

		By("waiting for VM to reboot", func() {
			vm.Reboot()
			vm.EventuallyConnects(1200)
		})
		By("Checking that rootfs is mounted RO", func() {
			out, err := vm.Sudo("findmnt /")
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(ContainSubstring("ro"))
			Expect(out).ToNot(ContainSubstring("rw"))
		})
		By("Checking the boot mode (boot)", func() {
			out, err := vm.Sudo("stat /run/cos/uki_boot_mode")
			Expect(err).ToNot(HaveOccurred(), out)
		})
		By("Checking SecureBoot is enabled", func() {
			out, err := vm.Sudo(`dmesg|grep -i secure| grep -i enabled`)
			Expect(err).ToNot(HaveOccurred(), out)
		})
		By("Checking OEM/PERSISTENT are mounted", func() {
			out, err := vm.Sudo("df -h") // Shows the disk by label which is easier to check
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(ContainSubstring("/dev/disk/by-label/COS_OEM"))
			Expect(out).To(ContainSubstring("/dev/disk/by-label/COS_PERSISTENT"))
		})
		By("Checking OEM/PERSISTENT are encrypted", func() {
			out, err := vm.Sudo("blkid /dev/vda2")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(ContainSubstring("crypto_LUKS"))
			out, err = vm.Sudo("blkid /dev/vda3")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(ContainSubstring("crypto_LUKS"))
		})

		By("checking custom cmdline", func() {
			out, err := vm.Sudo("cat /proc/cmdline")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(ContainSubstring("rd.immucore.uki"))
		})

		By("checking the use of dracut immutable module", func() {
			out, err := vm.Sudo("cat /run/immucore/initramfs_stage.log")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(ContainSubstring("Running stage: initramfs.before"))
			Expect(out).To(ContainSubstring("Running stage: initramfs.after"))
			Expect(out).To(ContainSubstring("Running stage: initramfs"))
		})

		By("checking writeable tmp", func() {
			_, err := vm.Sudo("echo 'foo' > /tmp/bar")
			Expect(err).ToNot(HaveOccurred())

			out, err := vm.Sudo("sudo cat /tmp/bar")
			Expect(err).ToNot(HaveOccurred())

			Expect(out).To(ContainSubstring("foo"))
		})

		By("checking bpf mount", func() {
			out, err := vm.Sudo("mount")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(ContainSubstring("bpf"))
		})

		By("checking rootfs shared mount", func() {
			out, err := vm.Sudo(`cat /proc/1/mountinfo | grep ' / / '`)
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(ContainSubstring("shared"))
		})

		By("checking that networking is functional", func() {
			out, err := vm.Sudo(`curl google.it`)
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(ContainSubstring("Moved"))
		})

		By("checking corresponding state", func() {
			out, err := vm.Sudo("kairos-agent state")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(ContainSubstring("boot: active_boot"))
			currentVersion, err := vm.Sudo(getVersionCmd)
			Expect(err).ToNot(HaveOccurred(), currentVersion)

			stateAssertVM(vm, "kairos.version", strings.ReplaceAll(strings.ReplaceAll(currentVersion, "\r", ""), "\n", ""))
			stateContains(vm, "system.os.name", "alpine", "opensuse", "ubuntu", "debian", "fedora")
			stateContains(vm, "kairos.flavor", "alpine", "opensuse", "ubuntu", "debian", "fedora")
		})

		By("Checking sysext was copied during install", func() {
			out, err := vm.Sudo("ls /.extra/sysext")
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(MatchRegexp("hello-broke.sysext.raw"))
			Expect(out).To(MatchRegexp("work.sysext.raw"))
		})

		By("Checking sysext was copied during boot", func() {
			out, err := vm.Sudo("ls /run/extensions")
			Expect(err).ToNot(HaveOccurred(), out)
			// Should not contain hello-broke.sysext.raw as it didn't pass validation
			Expect(out).ToNot(MatchRegexp("hello-broke.sysext.raw"))
			// Should contain work.sysext.raw as it passed validation
			Expect(out).To(MatchRegexp("work.sysext.raw"))
		})

		By("Checking that sysext was loaded", func() {
			type sysextStatus []struct {
				Hierarchy  string `json:"hierarchy"`
				Extensions any    `json:"extensions"`
			}

			// when calling the status we need to set the hierarchy env variable so it can find them
			env := "SYSTEMD_SYSEXT_HIERARCHIES=\"/usr/local/bin:/usr/local/sbin:/usr/local/include:/usr/local/lib:/usr/local/share:/usr/local/src:/usr/bin:/usr/share:/usr/lib:/usr/include:/usr/src:/usr/sbin\""
			out, err := vm.Sudo(fmt.Sprintf("%s systemd-sysext --json=short", env))
			Expect(err).ToNot(HaveOccurred(), out)
			// marshall output to struct
			var sysexts sysextStatus
			err = json.Unmarshal([]byte(out), &sysexts)
			Expect(err).ToNot(HaveOccurred())
			// check if sysexts are loaded
			for _, sysext := range sysexts {
				if sysext.Hierarchy == "/usr" {
					Expect(sysext.Extensions).To(ContainElement("work"))
				}
			}
		})

		By("Checking that we can run a command from a sysext", func() {
			out, err := vm.Sudo("hello.sh")
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(ContainSubstring("Hello world"))
		})

		By("rebooting to recovery")
		out, err := vm.Sudo("kairos-agent bootentry --select recovery")
		Expect(err).ToNot(HaveOccurred(), out)
		vm.Reboot()
		vm.EventuallyConnects(1200)

		By("Checking the boot mode (recovery)", func() {
			out, err := vm.Sudo("stat /run/cos/recovery_mode")
			Expect(err).ToNot(HaveOccurred(), out)
		})

		By("Checking sysext was not copied during boot", func() {
			out, err := vm.Sudo("stat /.extra/sysext")
			Expect(err).To(HaveOccurred(), out)
		})

		By("resetting")
		out, err = vm.Sudo("kairos-agent --debug reset --unattended")
		Expect(err).ToNot(HaveOccurred(), out)
		vm.Reboot()
		vm.EventuallyConnects(1200)

		By("checking if after-reset was run")
		out, err = vm.Sudo("ls /usr/local/after-reset-file")
		Expect(err).ToNot(HaveOccurred(), out)
		Expect(out).ToNot(MatchRegexp("No such file or directory"))
	})
})
