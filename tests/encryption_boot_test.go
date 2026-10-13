package mos_test

import (
	"fmt"
	"os"
	"os/exec"
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

// installConfigNoEncryptionFmt deliberately omits
// install.encrypted_partitions: the golden install must stay plaintext so the
// boot time step has work to do. The verb takes the extra cmdline, because
// the challenger variant needs rd.neednet=1 baked into the installed grub.
const installConfigNoEncryptionFmt = `#cloud-config

install:
  grub_options:
    extra_cmdline: "%s"
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

// encryptOnBootLocalPolicy is the policy the golden image flow bakes into
// OEM when the passphrase lives in the local TPM.
const encryptOnBootLocalPolicy = `#cloud-config

install:
  encrypted_partitions:
    - COS_PERSISTENT

kcrypt:
  encrypt_on_boot: true
`

// writeEncryptOnBootPolicy drops the boot time encryption policy into the
// installed system's OEM partition, next to the 90_custom.yaml the installer
// wrote. This is the template finalization step of the golden image flow: the
// policy is plain text, carries no key material, and is what the first boot's
// encrypt-pending step acts on.
func writeEncryptOnBootPolicy(vm VM, policy string) {
	GinkgoHelper()
	By("Writing the encrypt_on_boot policy into COS_OEM")

	policyFile, err := os.CreateTemp("", "")
	Expect(err).ToNot(HaveOccurred())
	defer os.Remove(policyFile.Name())
	Expect(os.WriteFile(policyFile.Name(), []byte(policy), 0644)).To(Succeed())
	Expect(vm.Scp(policyFile.Name(), "/tmp/91_encrypt_on_boot.yaml", "0644")).To(Succeed())

	out, err := vm.Sudo(`mkdir -p /tmp/oem && ` +
		`mount /dev/disk/by-label/COS_OEM /tmp/oem && ` +
		`cp /tmp/91_encrypt_on_boot.yaml /tmp/oem/91_encrypt_on_boot.yaml && ` +
		`umount /tmp/oem && sync && echo POLICY_WRITTEN`)
	Expect(err).ToNot(HaveOccurred(), out)
	Expect(out).To(ContainSubstring("POLICY_WRITTEN"), out)
}

var _ = Describe("kcrypt encrypt on boot", func() {
	var bootVM VM
	var bootInstallOutput string
	var bootInstallError error

	installPlaintext := func(vm VM, extraCmdline, policy string) {
		configFile, err := os.CreateTemp("", "")
		Expect(err).ToNot(HaveOccurred())
		defer os.Remove(configFile.Name())

		err = os.WriteFile(configFile.Name(), []byte(fmt.Sprintf(installConfigNoEncryptionFmt, extraCmdline)), 0744)
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

		writeEncryptOnBootPolicy(vm, policy)
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
			installPlaintext(bootVM, "rd.immucore.debug", encryptOnBootLocalPolicy)
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
			installPlaintext(bootVM, "rd.immucore.debug", encryptOnBootLocalPolicy)
		})

		AfterEach(func() {
			gatherFailureEvidence(bootVM)
			// No TPM emulator to stop on this variant.
			err := bootVM.Destroy(func(vm VM) {})
			Expect(err).ToNot(HaveOccurred())
		})

		It("halts the boot with the failure screen and leaves the partition plaintext", func() {
			By("Rebooting into the installed system")
			// peg's Reboot() asserts the machine becomes reachable again,
			// which is exactly what must not happen here: the halt screen
			// keeps the boot stopped. Issue the reboot and watch the serial
			// console instead.
			bootVM.Sudo("reboot") //nolint:errcheck

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

	// The challenger variant of the golden image flow: the policy baked into
	// OEM carries a kcrypt.challenger block, so the first boot fetches its
	// passphrase from the KMS (TOFU enrollment) instead of sealing it in the
	// local TPM NV index. Runs against the same challenger cluster as the
	// encryption-remote-* suites, hence the separate label; rd.neednet=1 is
	// baked into the installed grub so the initramfs has network by the time
	// the encrypt-pending step needs the KMS.
	When("a remote key management server manages the passphrase", Label("encryption-on-boot-remote"), func() {
		var tpmHash string

		BeforeEach(func() {
			tpmHash = ""
			_, bootVM = startVM()
			bootVM.EventuallyConnects(1200)
			policy := fmt.Sprintf(`#cloud-config

install:
  encrypted_partitions:
    - COS_PERSISTENT

kcrypt:
  encrypt_on_boot: true
  challenger:
    challenger_server: "http://%s"
    nv_index: ""
    c_index: ""
    tpm_device: ""
`, os.Getenv("KMS_ADDRESS"))
			installPlaintext(bootVM, "rd.immucore.debug rd.neednet=1", policy)
		})

		AfterEach(func() {
			if CurrentSpecReport().Failed() {
				gatherLogs(bootVM)
			}
			gatherFailureEvidence(bootVM)
			if tpmHash != "" {
				// Clean up the TOFU-created SealedVolume, same shape as the
				// encryption-remote-auto suite.
				cmd := exec.Command("kubectl", "delete", "sealedvolume",
					fmt.Sprintf("tofu-%s-cos-persistent-luks", tpmHash[:8]), "--ignore-not-found")
				out, err := cmd.CombinedOutput()
				Expect(err).ToNot(HaveOccurred(), string(out))
			}
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

		It("fetches the passphrase from the challenger and encrypts on first boot", func() {
			By("Rebooting into the installed system")
			bootVM.Reboot(750)
			bootVM.EventuallyConnects(1200)

			By("Checking the partition was encrypted on boot")
			out, err := bootVM.Sudo("blkid")
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(MatchRegexp("TYPE=\"crypto_LUKS\" PARTLABEL=\"persistent\""), out)
			Expect(out).To(MatchRegexp("/dev/(mapper|dm).*LABEL=\"COS_PERSISTENT\""), out)
			verifyPersistentFstabUsesMapper(bootVM)

			By("Expecting the TOFU secret to exist on the cluster")
			tpmHash = getTPMHash(bootVM)
			cmd := exec.Command("kubectl", "get", "secrets",
				fmt.Sprintf("tofu-%s-cos-persistent-luks", tpmHash[:8]))
			secretOut, err := cmd.CombinedOutput()
			Expect(err).ToNot(HaveOccurred(), string(secretOut))

			By("Rebooting a second time")
			bootVM.Reboot(750)
			bootVM.EventuallyConnects(1200)

			By("Checking the second boot was a clean no-op unlocked via the KMS")
			out, err = bootVM.Sudo("cat /run/immucore/immucore.log")
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(ContainSubstring("already LUKS; encrypt-pending is a no-op"), out)
			out, err = bootVM.Sudo("blkid")
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(MatchRegexp("TYPE=\"crypto_LUKS\" PARTLABEL=\"persistent\""), out)
			verifyPersistentFstabUsesMapper(bootVM)
		})
	})
})
