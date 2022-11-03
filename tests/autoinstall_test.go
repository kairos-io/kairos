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

var stateAssert = func(query, expected string) {
	out, err := Sudo(fmt.Sprintf("kairos-agent state get %s", query))
	ExpectWithOffset(1, err).ToNot(HaveOccurred())
	ExpectWithOffset(1, out).To(ContainSubstring(expected))
}

var _ = Describe("kairos autoinstall test", Label("autoinstall-test"), func() {

	BeforeEach(func() {
		if os.Getenv("CLOUD_INIT") == "" || !filepath.IsAbs(os.Getenv("CLOUD_INIT")) {
			Fail("CLOUD_INIT must be set and must be pointing to a file as an absolute path")
		}

		EventuallyConnects(1200)
	})

	AfterEach(func() {
		if CurrentGinkgoTestDescription().Failed {
			gatherLogs()
		}
	})

	Context("live cd", func() {
		It("has default service active", func() {
			if os.Getenv("FLAVOR") == "alpine" {
				out, _ := Sudo("rc-status")
				Expect(out).Should(ContainSubstring("kairos"))
				Expect(out).Should(ContainSubstring("kairos-agent"))
				fmt.Println(out)
			} else {
				// Eventually(func() string {
				// 	out, _ := machine.Command("sudo systemctl status kairososososos-agent")
				// 	return out
				// }, 30*time.Second, 10*time.Second).Should(ContainSubstring("no network token"))

				out, _ := Machine.Command("sudo systemctl status kairos")
				Expect(out).Should(ContainSubstring("loaded (/etc/systemd/system/kairos.service; enabled;"))
				fmt.Println(out)
			}

			// Debug output
			out, _ := Sudo("ls -liah /oem")
			fmt.Println(out)
			//	Expect(out).To(ContainSubstring("userdata.yaml"))
			out, _ = Sudo("cat /oem/userdata")
			fmt.Println(out)
			out, _ = Sudo("sudo ps aux")
			fmt.Println(out)

			out, _ = Sudo("sudo lsblk")
			fmt.Println(out)
		})
	})

	Context("auto installs", func() {
		It("to disk with custom config", func() {
			Eventually(func() string {
				out, _ := Sudo("ps aux")
				return out
			}, 30*time.Minute, 1*time.Second).Should(
				Or(
					ContainSubstring("elemental install"),
				))
		})
		It("reboots to active", func() {
			Eventually(func() string {
				out, _ := Sudo("kairos-agent state boot")
				return out
			}, 40*time.Minute, 10*time.Second).Should(
				Or(
					ContainSubstring("active_boot"),
				))
		})
	})

	Context("reboots and passes functional tests", func() {
		It("has grubenv file", func() {
			out, err := Sudo("cat /oem/grubenv")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(ContainSubstring("foobarzz"))

		})

		It("has custom cmdline", func() {
			out, err := Sudo("cat /proc/cmdline")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(ContainSubstring("foobarzz"))
		})

		It("uses the dracut immutable module", func() {
			out, err := Sudo("cat /proc/cmdline")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(ContainSubstring("cos-img/filename="))
		})

		It("installs Auto assessment", func() {
			// Auto assessment was installed
			out, _ := Sudo("cat /run/initramfs/cos-state/grubcustom")
			Expect(out).To(ContainSubstring("bootfile_loc"))

			out, _ = Sudo("cat /run/initramfs/cos-state/grub_boot_assessment")
			Expect(out).To(ContainSubstring("boot_assessment_blk"))

			cmdline, _ := Sudo("cat /proc/cmdline")
			Expect(cmdline).To(ContainSubstring("rd.emergency=reboot rd.shell=0 panic=5"))
		})

		It("has writeable tmp", func() {
			_, err := Sudo("echo 'foo' > /tmp/bar")
			Expect(err).ToNot(HaveOccurred())

			out, err := Machine.Command("sudo cat /tmp/bar")
			Expect(err).ToNot(HaveOccurred())

			Expect(out).To(ContainSubstring("foo"))
		})

		It("has bpf mount", func() {
			out, err := Sudo("mount")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(ContainSubstring("bpf"))
		})

		It("has correct permissions", func() {
			out, err := Sudo(`stat -c "%a" /oem`)
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(ContainSubstring("770"))

			out, err = Sudo(`stat -c "%a" /usr/local/cloud-config`)
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(ContainSubstring("770"))
		})

		It("has grubmenu", func() {
			out, err := Sudo("cat /run/initramfs/cos-state/grubmenu")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(ContainSubstring("state reset"))
		})

		It("has additional mount specified, with no dir in rootfs", func() {
			out, err := Sudo("mount")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(ContainSubstring("/var/lib/longhorn"))
		})

		It("has corresponding state", func() {
			out, err := Sudo("kairos-agent state")
			Expect(err).ToNot(HaveOccurred())
			fmt.Println(out)
			Expect(out).To(ContainSubstring("boot: active_boot"))

			stateAssert("oem.mounted", "true")
			stateAssert("oem.found", "true")
			stateAssert("persistent.mounted", "true")
			stateAssert("state.mounted", "true")
			stateAssert("oem.type", "ext4")
			stateAssert("persistent.type", "ext4")
			stateAssert("state.type", "ext4")
			stateAssert("oem.mount_point", "/oem")
			stateAssert("persistent.mount_point", "/usr/local")
			stateAssert("persistent.name", "/dev/vda")
			stateAssert("state.mount_point", "/run/initramfs/cos-state")
			stateAssert("oem.read_only", "false")
			stateAssert("persistent.read_only", "false")
			stateAssert("state.read_only", "true")
		})
	})
})
