package mos_test

import (
	"fmt"
	"os"
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

		By("checking non-writeable /", func() {
			_, err := vm.Sudo("echo 'foo' > /bar")
			Expect(err).To(HaveOccurred())
		})

		By("checking bpf mount", func() {
			out, err := vm.Sudo("mount")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(ContainSubstring("bpf"))
		})

		By("checking rootfs shared mount", func() {
			out, err := vm.Sudo(`findmnt / -o PROPAGATION  -n`)
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(ContainSubstring("shared"))
		})

		By("checking /proc shared mount", func() {
			out, err := vm.Sudo(`findmnt /proc -o PROPAGATION  -n`)
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(ContainSubstring("shared"))
		})

		By("checking that rootfs is mounted as tmpfs", func() {
			out, err := vm.Sudo(`findmnt / -n -o FSTYPE`)
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(ContainSubstring("tmpfs"))
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

		By("Checking the k3s installation", func() {
			By("Cheking that node is ready")
			Eventually(func() string {
				out, err := kubectl(vm, "get nodes")
				Expect(err).ToNot(HaveOccurred())
				return out
			}, 5*time.Minute, 15*time.Second).Should(ContainSubstring("Ready"))
			By("Checking all pods are up and running")
			Eventually(func() string {
				out, _ := kubectl(vm, "get pods -A")
				return out

			}, 900*time.Second, 10*time.Second).ShouldNot(Or(ContainSubstring("Pending"), ContainSubstring("ContainerCreating")))

		})

		By("Installing calico as network plugin", func() {
			err := vm.Scp("assets/calico.yaml", "/tmp/calico.yaml", "0777")
			Expect(err).ToNot(HaveOccurred())
			out, err := kubectl(vm, "apply -f /tmp/calico.yaml")
			Expect(err).ToNot(HaveOccurred(), out)
			Eventually(func() string {
				out, err := kubectl(vm, "get pods -n kube-system -l k8s-app=calico-node")
				Expect(err).ToNot(HaveOccurred())
				fmt.Println(out)
				return out
			}, 5*time.Minute, 15*time.Second).Should(And(
				ContainSubstring("calico-node-"),
				ContainSubstring("Running"),
			))

			Eventually(func() string {
				out, err := kubectl(vm, "get pods -n kube-system -l k8s-app=calico-kube-controllers")
				Expect(err).ToNot(HaveOccurred())
				fmt.Println(out)
				return out
			}, 5*time.Minute, 15*time.Second).Should(And(
				ContainSubstring("calico-kube-controllers-"),
				ContainSubstring("Running"),
			))

			Eventually(func() string {
				out, err := kubectl(vm, "get nodes")
				Expect(err).ToNot(HaveOccurred())
				return out
			}, 5*time.Minute, 15*time.Second).Should(ContainSubstring("Ready"))
		})
	})
})
