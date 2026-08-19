package e2e_test

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
)

// Advanced scenarios that test complex operational workflows,
// performance aspects, and edge cases

var _ = Describe("Remote Attestation E2E Tests", Label("remote-complete-workflow"), func() {
	var config string
	var vmOpts VMOptions
	var expectedInstallationSuccess bool
	var testVM VM
	var tpmHash string

	BeforeEach(func() {
		expectedInstallationSuccess = true
		vmOpts = DefaultVMOptions()
		_, testVM = startVM(vmOpts)
		testVM.EventuallyConnects(1200)
	})

	AfterEach(func() {
		cleanupVM(testVM)
		// Clean up test resources if tpmHash was set
		if tpmHash != "" {
			cleanupTestResources(tpmHash)
		}
	})

	installKairosWithConfig := func(config string) {
		installKairosWithConfigAdvanced(testVM, config, expectedInstallationSuccess)
	}

	It("should perform TOFU enrollment, quarantine testing, PCR management, AK management, error handling, secret reuse, and multi-partition support", func() {
		tpmHash = getTPMHash(testVM)

		deleteSealedVolume(tpmHash)

		config = fmt.Sprintf(`#cloud-config

hostname: metal-{{ trunc 4 .MachineID }}
users:
- name: kairos
  passwd: kairos
  groups:
    - admin

install:
  encrypted_partitions:
  - COS_PERSISTENT
  - COS_OEM
  grub_options:
    extra_cmdline: "rd.neednet=1"
  reboot: false

kcrypt:
  challenger:
    challenger_server: "http://%s"
`, os.Getenv("KMS_ADDRESS"))

		installKairosWithConfig(config)

		// BEFORE REBOOT: Check that PCRs are deferred (empty) in livecd mode
		By("Verifying SealedVolume and secrets were created during livecd installation")
		sealedVolumeName := getSealedVolumeName(tpmHash)
		Eventually(func() bool {
			return secretExists(fmt.Sprintf("%s-cos-persistent", sealedVolumeName)) &&
				secretExists(fmt.Sprintf("%s-cos-oem", sealedVolumeName))
		}, 30*time.Second, 5*time.Second).Should(BeTrue(), "Secrets should be created during livecd installation")

		By("Verifying PCRs are empty (deferred) during livecd mode before reboot")
		Eventually(func() bool {
			cmd := exec.Command("kubectl", "get", "sealedvolume", sealedVolumeName, "-o", "yaml")
			out, err := cmd.CombinedOutput()
			if err != nil {
				return false
			}
			outStr := string(out)
			// Check that attestation exists with EK but PCRs are empty strings
			return strings.Contains(outStr, "attestation:") &&
				strings.Contains(outStr, "ekPublicKey:") &&
				strings.Contains(outStr, "pcrValues:") &&
				strings.Contains(outStr, "pcrs:") &&
				strings.Contains(outStr, `"0": ""`) // PCR 0 should be empty string (deferred)
		}, 30*time.Second, 5*time.Second).Should(BeTrue(), "PCRs should be deferred (empty strings) during livecd mode")

		// NOW REBOOT to installed system
		rebootAndConnect(testVM)
		verifyEncryptedPartition(testVM)

		// AFTER REBOOT: Check that PCRs are now enrolled (non-empty)
		By("Verifying PCRs are enrolled (non-empty) after reboot to installed system")
		Eventually(func() bool {
			cmd := exec.Command("kubectl", "get", "sealedvolume", sealedVolumeName, "-o", "yaml")
			out, err := cmd.CombinedOutput()
			if err != nil {
				return false
			}
			outStr := string(out)
			// PCR 0 should now have a non-empty value
			return strings.Contains(outStr, "pcrValues:") &&
				strings.Contains(outStr, `"0":`) &&
				!strings.Contains(outStr, `"0": ""`) // PCR 0 should NOT be empty anymore
		}, 30*time.Second, 5*time.Second).Should(BeTrue(), "PCRs should be enrolled after reboot to installed system")

		// Verify both partitions are encrypted
		By("Verifying both partitions are encrypted")
		out, err := testVM.Sudo("blkid")
		Expect(err).ToNot(HaveOccurred(), out)
		Expect(out).To(MatchRegexp("TYPE=\"crypto_LUKS\" PARTLABEL=\"persistent\""), out)
		Expect(out).To(MatchRegexp("TYPE=\"crypto_LUKS\" PARTLABEL=\"oem\""), out)

		By("Testing subsequent authentication with learned attestation data")
		rebootAndConnect(testVM)
		verifyEncryptedPartition(testVM)

		By("quarantining the TPM")
		quarantineTPM(tpmHash)

		By("Testing that quarantined TPM is rejected via CLI for both partitions")
		expectPassphraseRetrievalWithError(testVM, "COS_PERSISTENT", "quarantined")
		expectPassphraseRetrievalWithError(testVM, "COS_OEM", "quarantined")

		By("Testing recovery by unquarantining TPM")
		unquarantineTPM(tpmHash)

		expectPassphraseRetrieval(testVM, "COS_PERSISTENT", true)
		expectPassphraseRetrieval(testVM, "COS_OEM", true)

		// Continue with PCR and AK Management testing
		By("Testing PCR re-enrollment by setting PCR 0 to wrong value")
		updateSealedVolumeAttestation(tpmHash, "pcrValues.pcrs.0", "wrong-pcr0-value")

		By("checking that the passphrase retrieval fails with wrong PCR for both partitions")
		expectPassphraseRetrievalWithError(testVM, "COS_PERSISTENT", "attestation failed")
		expectPassphraseRetrievalWithError(testVM, "COS_OEM", "attestation failed")

		By("setting PCR 0 to an empty value (re-enrollment mode)")
		updateSealedVolumeAttestation(tpmHash, "pcrValues.pcrs.0", "")

		By("checking that the passphrase retrieval works after PCR re-enrollment for both partitions")
		expectPassphraseRetrieval(testVM, "COS_PERSISTENT", true)
		expectPassphraseRetrieval(testVM, "COS_OEM", true)

		By("Verifying PCR 0 was re-enrolled with current value")
		Eventually(func() bool {
			sealedVolumeName := getSealedVolumeName(tpmHash)
			cmd := exec.Command("kubectl", "get", "sealedvolume", sealedVolumeName, "-o", "yaml")
			out, err := cmd.CombinedOutput()
			if err != nil {
				return false
			}
			// PCR 0 should now have a new non-empty value
			return strings.Contains(string(out), "\"0\":") &&
				!strings.Contains(string(out), "\"0\": \"\"") &&
				!strings.Contains(string(out), "\"0\": \"wrong-pcr0-value\"")
		}, 30*time.Second, 5*time.Second).Should(BeTrue())

		// Continue with EK Management testing (transient AK approach)
		By("Testing EK re-enrollment by setting EK to empty")
		updateSealedVolumeAttestation(tpmHash, "ekPublicKey", "")

		By("Triggering re-enrollment by retrieving passphrase")
		expectPassphraseRetrieval(testVM, "COS_PERSISTENT", true)

		By("Verifying EK was re-enrolled with actual value")
		sealedVolumeName = getSealedVolumeName(tpmHash)
		var learnedEK string
		Eventually(func() bool {
			cmd := exec.Command("kubectl", "get", "sealedvolume", sealedVolumeName, "-o", "jsonpath={.spec.attestation.ekPublicKey}")
			out, err := cmd.CombinedOutput()
			if err != nil {
				return false
			}

			// Extract learned EK for later enforcement test
			// Don't trim! PEM format includes a trailing newline which is significant
			learnedEK = string(out)

			// Check that it's not empty and looks like a valid key (starts with common PEM markers or is substantial length)
			return learnedEK != "" && len(learnedEK) > 50
		}, 30*time.Second, 5*time.Second).Should(BeTrue())

		// Test EK enforcement by setting wrong EK
		By("Testing EK enforcement by setting wrong EK value")
		updateSealedVolumeAttestation(tpmHash, "ekPublicKey", "wrong-ek-value")

		// Verify the wrong value was actually set
		verifyCmd := exec.Command("kubectl", "get", "sealedvolume", sealedVolumeName, "-o", "jsonpath={.spec.attestation.ekPublicKey}")
		verifyOut, verifyErr := verifyCmd.CombinedOutput()
		Expect(verifyErr).ToNot(HaveOccurred())
		Expect(string(verifyOut)).To(Equal("wrong-ek-value"), "Wrong EK should be set")

		// Should fail to retrieve passphrase with wrong EK for both partitions
		expectPassphraseRetrievalWithError(testVM, "COS_PERSISTENT", "attestation failed")
		expectPassphraseRetrievalWithError(testVM, "COS_OEM", "attestation failed")

		// Restore correct EK and verify it works via CLI
		By("Restoring correct EK and verifying authentication works for both partitions")
		updateSealedVolumeAttestation(tpmHash, "ekPublicKey", learnedEK)

		time.Sleep(5 * time.Second)

		// Verify the correct value was actually restored
		restoreCmd := exec.Command("kubectl", "get", "sealedvolume", sealedVolumeName, "-o", "jsonpath={.spec.attestation.ekPublicKey}")
		restoreOut, restoreErr := restoreCmd.CombinedOutput()
		Expect(restoreErr).ToNot(HaveOccurred())
		restoredEK := string(restoreOut)
		Expect(restoredEK).To(Equal(learnedEK), "Restored EK should match learned EK")
		Expect(len(restoredEK)).To(BeNumerically(">", 100), "Restored EK should be a full key, not 'wrong-ek-value'")

		// Should now work with correct EK for both partitions
		expectPassphraseRetrieval(testVM, "COS_PERSISTENT", true)
		expectPassphraseRetrieval(testVM, "COS_OEM", true)

		// Continue with Secret Reuse testing
		By("Testing secret reuse when SealedVolume is recreated for both partitions")
		sealedVolumeName = getSealedVolumeName(tpmHash)
		persistentSecretName := fmt.Sprintf("%s-cos-persistent", sealedVolumeName)
		oemSecretName := fmt.Sprintf("%s-cos-oem", sealedVolumeName)

		// Get secret data for comparison for both partitions
		cmd := exec.Command("kubectl", "get", "secret", persistentSecretName, "-o", "yaml")
		originalPersistentSecretData, err := cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred())

		cmd = exec.Command("kubectl", "get", "secret", oemSecretName, "-o", "yaml")
		originalOemSecretData, err := cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred())

		// Delete SealedVolume but keep secrets
		deleteSealedVolume(tpmHash)

		// Verify secrets still exist
		Expect(secretExists(persistentSecretName)).To(BeTrue())
		Expect(secretExists(oemSecretName)).To(BeTrue())

		// Recreate SealedVolume and verify secret reuse
		By("Recreating SealedVolume and verifying secret reuse for both partitions")
		createSealedVolumeWithAttestation(tpmHash, nil)

		// Should reuse existing secrets
		rebootAndConnect(testVM)
		verifyEncryptedPartition(testVM)

		// Verify the same secrets are being used
		cmd = exec.Command("kubectl", "get", "secret", persistentSecretName, "-o", "yaml")
		newPersistentSecretData, err := cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred())

		cmd = exec.Command("kubectl", "get", "secret", oemSecretName, "-o", "yaml")
		newOemSecretData, err := cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred())

		// The secret data should be identical (reused, not regenerated) for both partitions
		Expect(string(newPersistentSecretData)).To(Equal(string(originalPersistentSecretData)))
		Expect(string(newOemSecretData)).To(Equal(string(originalOemSecretData)))
	})
})
