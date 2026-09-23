package mos_test

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
)

// Boot time encryption of pending partitions (kairos-io/kairos#4556): a node
// installed WITHOUT encryption whose OEM carries install.encrypted_partitions
// plus the kcrypt.encrypt_on_boot opt-in encrypts those partitions on the
// first boot that can reach a TPM, before they are mounted. Without a TPM the
// boot halts; there is no plaintext fallback.

// installConfigNoEncryption deliberately omits install.encrypted_partitions:
// the golden install must stay plaintext so the boot time step has work to do.
const installConfigNoEncryption = `#cloud-config

install:
  grub_options:
    extra_cmdline: "rd.immucore.debug"
  reboot: false # we will reboot manually

stages:
  initramfs:
    - name: "Set user and password"
      users:
        kairos:
          passwd: "kairos"
          groups:
            - "admin"
      hostname: kairos-{{ trunc 4 .Random }}
`

// writeEncryptOnBootPolicy drops the boot time encryption policy into the
// installed system's OEM partition, next to the 90_custom.yaml the installer
// wrote. This is the template finalization step of the golden image flow: the
// policy is plain text, carries no key material, and is what the first boot's
// encrypt-pending step acts on.
func writeEncryptOnBootPolicy(vm VM) {
	GinkgoHelper()
	By("Writing the encrypt_on_boot policy into COS_OEM")
	out, err := vm.Sudo(`mkdir -p /tmp/oem && ` +
		`mount /dev/disk/by-label/COS_OEM /tmp/oem && ` +
		`printf '#cloud-config\ninstall:\n  encrypted_partitions:\n    - COS_PERSISTENT\nkcrypt:\n  encrypt_on_boot: true\n' > /tmp/oem/91_encrypt_on_boot.yaml && ` +
		`umount /tmp/oem && sync && echo POLICY_WRITTEN`)
	Expect(err).ToNot(HaveOccurred(), out)
	Expect(out).To(ContainSubstring("POLICY_WRITTEN"), out)
}

var _ = Describe("kcrypt encrypt on boot", func() {
	var bootVM VM
	var bootInstallOutput string
	var bootInstallError error

	installPlaintext := func(vm VM) {
		configFile, err := os.CreateTemp("", "")
		Expect(err).ToNot(HaveOccurred())
		defer os.Remove(configFile.Name())

		err = os.WriteFile(configFile.Name(), []byte(installConfigNoEncryption), 0744)
		Expect(err).ToNot(HaveOccurred())

		err = vm.Scp(configFile.Name(), "/tmp/config.yaml", "0744")
		Expect(err).ToNot(HaveOccurred())
		By("Manually installing without encryption")
		bootInstallOutput, bootInstallError = vm.Sudo("kairos-agent --debug manual-install --device auto /tmp/config.yaml")
		Expect(bootInstallError).ToNot(HaveOccurred(), bootInstallOutput)

		By("Confirming the installed persistent partition is plaintext")
		out, err := vm.Sudo("blkid")
		Expect(err).ToNot(HaveOccurred(), out)
		Expect(out).ToNot(MatchRegexp("crypto_LUKS"), out)

		writeEncryptOnBootPolicy(vm)
	}

	gatherFailureEvidence := func(vm VM) {
		if CurrentSpecReport().Failed() {
			serial, _ := os.ReadFile(filepath.Join(vm.StateDir, "serial.log"))
			_ = os.MkdirAll("logs", os.ModePerm|os.ModeDir)
			_ = os.WriteFile(filepath.Join("logs", "serial.log"), serial, os.ModePerm)
			fmt.Println(string(serial))
		}
	}

	When("a TPM is attached", Label("encryption-on-boot"), func() {
		BeforeEach(func() {
			_, bootVM = startVM()
			bootVM.EventuallyConnects(1200)
			installPlaintext(bootVM)
		})

		AfterEach(func() {
			if CurrentSpecReport().Failed() {
				gatherLogs(bootVM)
			}
			gatherFailureEvidence(bootVM)
			err := bootVM.Destroy(func(vm VM) {
				tpmPID, err := os.ReadFile(path.Join(vm.StateDir, "tpm", "pid"))
				Expect(err).ToNot(HaveOccurred())
				if len(tpmPID) != 0 {
					pid, err := strconv.Atoi(string(tpmPID))
					Expect(err).ToNot(HaveOccurred())
					syscall.Kill(pid, syscall.SIGKILL)
				}
			})
			Expect(err).ToNot(HaveOccurred())
		})

		It("encrypts the pending partition on first boot and is a no-op afterwards", func() {
			By("Rebooting into the installed system")
			bootVM.Reboot()
			bootVM.EventuallyConnects(1200)

			By("Checking the partition was encrypted on boot")
			out, err := bootVM.Sudo("blkid")
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(MatchRegexp("TYPE=\"crypto_LUKS\" PARTLABEL=\"persistent\""), out)
			Expect(out).To(MatchRegexp("/dev/(mapper|dm).*LABEL=\"COS_PERSISTENT\""), out)
			verifyPersistentFstabUsesMapper(bootVM)

			By("Checking immucore ran the encrypt-pending step")
			out, err = bootVM.Sudo("cat /run/immucore/immucore.log")
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(ContainSubstring("encrypting pending partitions before mount"), out)
			Expect(out).To(ContainSubstring("pending partitions encrypted"), out)

			By("Rebooting a second time")
			bootVM.Reboot()
			bootVM.EventuallyConnects(1200)

			By("Checking the second boot was a clean no-op")
			out, err = bootVM.Sudo("cat /run/immucore/immucore.log")
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(ContainSubstring("already LUKS; encrypt-pending is a no-op"), out)
			out, err = bootVM.Sudo("blkid")
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(MatchRegexp("TYPE=\"crypto_LUKS\" PARTLABEL=\"persistent\""), out)
			verifyPersistentFstabUsesMapper(bootVM)
		})
	})

	When("no TPM is attached", Label("encryption-on-boot-no-tpm"), func() {
		BeforeEach(func() {
			_, bootVM = startVMNoTPM()
			bootVM.EventuallyConnects(1200)
			installPlaintext(bootVM)
		})

		AfterEach(func() {
			gatherFailureEvidence(bootVM)
			// No TPM emulator to stop on this variant.
			err := bootVM.Destroy(func(vm VM) {})
			Expect(err).ToNot(HaveOccurred())
		})

		It("halts the boot with the failure screen and leaves the partition plaintext", func() {
			By("Rebooting into the installed system")
			bootVM.Reboot()

			By("Waiting for the fail closed halt screen on the serial console")
			serialLog := filepath.Join(bootVM.StateDir, "serial.log")
			Eventually(func() string {
				b, _ := os.ReadFile(serialLog)
				return string(b)
			}, 10*time.Minute, 10*time.Second).Should(
				And(
					ContainSubstring("KAIROS BOOT FAILED"),
					ContainSubstring("Encrypt on boot: partition encryption failed"),
					ContainSubstring("could not find TPM 2.0 device"),
				))

			By("Confirming the halt happened before any write to the partition")
			b, _ := os.ReadFile(serialLog)
			Expect(string(b)).ToNot(ContainSubstring("Creating LUKS container"), string(b))
		})
	})
})
