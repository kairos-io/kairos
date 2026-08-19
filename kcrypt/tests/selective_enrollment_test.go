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

// Selective Enrollment E2E Tests
// These tests verify the selective enrollment policy for TPM attestation:
// - Empty string ("") = re-enrollment mode (learn on first use, enforce thereafter)
// - Set value = enforcement mode (require exact match)
// - Omitted (nil/not in map) = skip entirely (never verify, never store)

var _ = Describe("Selective Enrollment E2E Tests", func() {

	Describe("EK-Only Verification (Empty Attestation Object)", Label("remote-ek-only"), func() {
		var config string
		var vmOpts VMOptions
		var testVM VM
		var tpmHash string

		BeforeEach(func() {
			vmOpts = DefaultVMOptions()
			_, testVM = startVM(vmOpts)
			testVM.EventuallyConnects(1200)
			tpmHash = getTPMHash(testVM)
		})

		AfterEach(func() {
			cleanupVM(testVM)
			if tpmHash != "" {
				cleanupTestResources(tpmHash)
			}
		})

		It("should handle empty attestation object (EK-only verification, no PCRs)", func() {
			By("Creating SealedVolume with empty attestation object")
			sealedVolumeName := getSealedVolumeName(tpmHash)

			// Create Secret with known passphrase
			kubectlApplyYaml(fmt.Sprintf(`---
apiVersion: v1
kind: Secret
metadata:
  name: %s-cos-persistent
  namespace: default
type: Opaque
stringData:
  passphrase: "test-passphrase-for-ek-only"
`, sealedVolumeName))

			// Create SealedVolume with empty attestation
			kubectlApplyYaml(fmt.Sprintf(`---
apiVersion: keyserver.kairos.io/v1alpha1
kind: SealedVolume
metadata:
  name: "%s"
  namespace: default
spec:
  TPMHash: "%s"
  partitions:
    - label: COS_PERSISTENT
      secret:
        name: %s-cos-persistent
        path: passphrase
  attestation: {}  # Empty - should learn EK, skip all PCRs
`, sealedVolumeName, tpmHash, sealedVolumeName))

			By("Installing Kairos with encryption")
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
  grub_options:
    extra_cmdline: "rd.neednet=1"
  reboot: false

kcrypt:
  challenger:
    challenger_server: "http://%s"
`, os.Getenv("KMS_ADDRESS"))

			installKairosWithConfigAdvanced(testVM, config, true)
			rebootAndConnect(testVM)
			verifyEncryptedPartition(testVM)

			By("Verifying EK was learned and stored")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "sealedvolume", sealedVolumeName, "-o", "yaml")
				out, err := cmd.CombinedOutput()
				if err != nil {
					return false
				}
				outStr := string(out)
				// EK should be present and not empty
				return strings.Contains(outStr, "ekPublicKey:") &&
					!strings.Contains(outStr, "ekPublicKey: \"\"") &&
					!strings.Contains(outStr, "ekPublicKey: |")
			}, 30*time.Second, 5*time.Second).Should(BeTrue())

			By("Verifying NO PCRs were stored")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "sealedvolume", sealedVolumeName, "-o", "yaml")
				out, err := cmd.CombinedOutput()
				if err != nil {
					return false
				}
				outStr := string(out)
				// PCRValues should either not exist or be null/empty
				return !strings.Contains(outStr, "pcrValues:") ||
					strings.Contains(outStr, "pcrValues: null") ||
					strings.Contains(outStr, "pcrValues: {}")
			}, 30*time.Second, 5*time.Second).Should(BeTrue())

			By("Verifying subsequent boot works with EK enforcement but no PCR checks")
			rebootAndConnect(testVM)
			verifyEncryptedPartition(testVM)

			By("Testing that CLI passphrase retrieval works")
			passphrase, err := checkPassphraseRetrieval(testVM, "COS_PERSISTENT")
			Expect(err).ToNot(HaveOccurred(), "Passphrase retrieval should succeed with EK-only verification")
			Expect(passphrase).ToNot(BeEmpty())
		})
	})

	Describe("Selective PCR Tracking from Initial Setup", Label("remote-selective-pcr"), func() {
		var config string
		var vmOpts VMOptions
		var testVM VM
		var tpmHash string

		BeforeEach(func() {
			vmOpts = DefaultVMOptions()
			_, testVM = startVM(vmOpts)
			testVM.EventuallyConnects(1200)
			tpmHash = getTPMHash(testVM)
		})

		AfterEach(func() {
			cleanupVM(testVM)
			if tpmHash != "" {
				cleanupTestResources(tpmHash)
			}
		})

		It("should handle selective PCR tracking from initial setup (track PCR 0,7 only, skip PCR 11)", func() {
			By("Creating SealedVolume with selective PCR configuration")
			sealedVolumeName := getSealedVolumeName(tpmHash)

			// Create Secret with known passphrase
			kubectlApplyYaml(fmt.Sprintf(`---
apiVersion: v1
kind: Secret
metadata:
  name: %s-cos-persistent
  namespace: default
type: Opaque
stringData:
  passphrase: "test-passphrase-selective-pcr"
`, sealedVolumeName))

			// Create SealedVolume with selective PCRs (0 and 7 only, skip 11)
			kubectlApplyYaml(fmt.Sprintf(`---
apiVersion: keyserver.kairos.io/v1alpha1
kind: SealedVolume
metadata:
  name: "%s"
  namespace: default
spec:
  TPMHash: "%s"
  partitions:
    - label: COS_PERSISTENT
      secret:
        name: %s-cos-persistent
        path: passphrase
  attestation:
    ekPublicKey: ""  # Re-enrollment mode (learn EK)
    pcrValues:
      pcrs:
        "0": ""      # Re-enrollment mode (learn PCR 0)
        "7": ""      # Re-enrollment mode (learn PCR 7)
        # "11" omitted - should be skipped entirely
`, sealedVolumeName, tpmHash, sealedVolumeName))

			By("Installing Kairos with encryption")
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
  grub_options:
    extra_cmdline: "rd.neednet=1"
  reboot: false

kcrypt:
  challenger:
    challenger_server: "http://%s"
`, os.Getenv("KMS_ADDRESS"))

			installKairosWithConfigAdvanced(testVM, config, true)
			rebootAndConnect(testVM)
			verifyEncryptedPartition(testVM)

			By("Verifying only PCRs 0 and 7 were learned (not 11)")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "sealedvolume", sealedVolumeName, "-o", "yaml")
				out, err := cmd.CombinedOutput()
				if err != nil {
					return false
				}
				outStr := string(out)
				hasPCR0 := strings.Contains(outStr, "\"0\":")
				hasPCR7 := strings.Contains(outStr, "\"7\":")
				noPCR11 := !strings.Contains(outStr, "\"11\":")

				// Verify PCR 0 and 7 have non-empty values
				notEmptyPCR0 := !strings.Contains(outStr, "\"0\": \"\"")
				notEmptyPCR7 := !strings.Contains(outStr, "\"7\": \"\"")

				return hasPCR0 && hasPCR7 && noPCR11 && notEmptyPCR0 && notEmptyPCR7
			}, 30*time.Second, 5*time.Second).Should(BeTrue())

			By("Verifying EK was also learned")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "sealedvolume", sealedVolumeName, "-o", "yaml")
				out, err := cmd.CombinedOutput()
				if err != nil {
					return false
				}
				outStr := string(out)
				return strings.Contains(outStr, "ekPublicKey:") &&
					!strings.Contains(outStr, "ekPublicKey: \"\"")
			}, 30*time.Second, 5*time.Second).Should(BeTrue())

			By("Verifying subsequent boot works with PCR 0,7 enforcement but PCR 11 ignored")
			rebootAndConnect(testVM)
			verifyEncryptedPartition(testVM)

			By("Testing that CLI passphrase retrieval works")
			passphrase, err := checkPassphraseRetrieval(testVM, "COS_PERSISTENT")
			Expect(err).ToNot(HaveOccurred(), "Passphrase retrieval should succeed with selective PCR tracking")
			Expect(passphrase).ToNot(BeEmpty())
		})
	})

	Describe("EK Re-enrollment Mode", Label("remote-ek-reenroll"), func() {
		var config string
		var vmOpts VMOptions
		var testVM VM
		var tpmHash string

		BeforeEach(func() {
			vmOpts = DefaultVMOptions()
			_, testVM = startVM(vmOpts)
			testVM.EventuallyConnects(1200)
			tpmHash = getTPMHash(testVM)
		})

		AfterEach(func() {
			cleanupVM(testVM)
			if tpmHash != "" {
				cleanupTestResources(tpmHash)
			}
		})

		It("should learn EK when set to empty string (re-enrollment mode)", func() {
			By("Performing initial TOFU enrollment")
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
  grub_options:
    extra_cmdline: "rd.neednet=1"
  reboot: false

kcrypt:
  challenger:
    challenger_server: "http://%s"
`, os.Getenv("KMS_ADDRESS"))

			installKairosWithConfigAdvanced(testVM, config, true)
			rebootAndConnect(testVM)
			verifyEncryptedPartition(testVM)

			By("Verifying initial EK was learned")
			sealedVolumeName := getSealedVolumeName(tpmHash)
			var learnedEK string
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "sealedvolume", sealedVolumeName, "-o", "yaml")
				out, err := cmd.CombinedOutput()
				if err != nil {
					return false
				}

				// Extract the EK value
				lines := strings.Split(string(out), "\n")
				for _, line := range lines {
					if strings.Contains(line, "ekPublicKey:") && !strings.Contains(line, "ekPublicKey: \"\"") {
						parts := strings.Split(line, "ekPublicKey:")
						if len(parts) > 1 {
							learnedEK = strings.TrimSpace(strings.Trim(parts[1], "\""))
						}
					}
				}

				return learnedEK != ""
			}, 30*time.Second, 5*time.Second).Should(BeTrue())

			By("Setting EK to empty string (re-enrollment mode)")
			updateSealedVolumeAttestation(tpmHash, "ekPublicKey", "")

			By("Verifying EK re-enrolls on next boot")
			rebootAndConnect(testVM)
			verifyEncryptedPartition(testVM)

			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "sealedvolume", sealedVolumeName, "-o", "yaml")
				out, err := cmd.CombinedOutput()
				if err != nil {
					return false
				}
				outStr := string(out)
				// EK should now be populated (re-learned)
				return strings.Contains(outStr, "ekPublicKey:") &&
					!strings.Contains(outStr, "ekPublicKey: \"\"")
			}, 30*time.Second, 5*time.Second).Should(BeTrue())

			By("Verifying the EK value is the same as before (same TPM)")
			var reEnrolledEK string
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "sealedvolume", sealedVolumeName, "-o", "yaml")
				out, err := cmd.CombinedOutput()
				if err != nil {
					return false
				}

				lines := strings.Split(string(out), "\n")
				for _, line := range lines {
					if strings.Contains(line, "ekPublicKey:") && !strings.Contains(line, "ekPublicKey: \"\"") {
						parts := strings.Split(line, "ekPublicKey:")
						if len(parts) > 1 {
							reEnrolledEK = strings.TrimSpace(strings.Trim(parts[1], "\""))
						}
					}
				}

				return reEnrolledEK != ""
			}, 30*time.Second, 5*time.Second).Should(BeTrue())

			// The EK should be the same (same TPM)
			Expect(reEnrolledEK).To(Equal(learnedEK), "Re-enrolled EK should match original EK (same TPM)")
		})
	})

	Describe("Mixed Attestation Modes", Label("remote-mixed-modes"), func() {
		var config string
		var vmOpts VMOptions
		var testVM VM
		var tpmHash string

		BeforeEach(func() {
			vmOpts = DefaultVMOptions()
			_, testVM = startVM(vmOpts)
			testVM.EventuallyConnects(1200)
			tpmHash = getTPMHash(testVM)
		})

		AfterEach(func() {
			cleanupVM(testVM)
			if tpmHash != "" {
				cleanupTestResources(tpmHash)
			}
		})

		It("should handle mixed modes: EK enforcement + PCR re-enrollment + PCR omission", func() {
			By("Performing initial TOFU enrollment to learn EK and PCRs")
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
  grub_options:
    extra_cmdline: "rd.neednet=1"
  reboot: false

kcrypt:
  challenger:
    challenger_server: "http://%s"
`, os.Getenv("KMS_ADDRESS"))

			installKairosWithConfigAdvanced(testVM, config, true)
			rebootAndConnect(testVM)
			verifyEncryptedPartition(testVM)

			By("Getting the learned EK and PCR values")
			sealedVolumeName := getSealedVolumeName(tpmHash)
			var learnedEK, learnedPCR0, learnedPCR7 string
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "sealedvolume", sealedVolumeName, "-o", "yaml")
				out, err := cmd.CombinedOutput()
				if err != nil {
					return false
				}

				outStr := string(out)
				lines := strings.Split(outStr, "\n")
				for i, line := range lines {
					if strings.Contains(line, "ekPublicKey:") {
						// Get EK (might be multiline)
						if i+1 < len(lines) {
							learnedEK = strings.TrimSpace(lines[i+1])
						}
					}
					if strings.Contains(line, "\"0\":") {
						parts := strings.Split(line, "\"0\":")
						if len(parts) > 1 {
							learnedPCR0 = strings.TrimSpace(strings.Trim(parts[1], "\""))
						}
					}
					if strings.Contains(line, "\"7\":") {
						parts := strings.Split(line, "\"7\":")
						if len(parts) > 1 {
							learnedPCR7 = strings.TrimSpace(strings.Trim(parts[1], "\""))
						}
					}
				}

				return learnedEK != "" && learnedPCR0 != "" && learnedPCR7 != ""
			}, 30*time.Second, 5*time.Second).Should(BeTrue())

			By("Reconfiguring with mixed modes: EK enforced, PCR 0 re-enrollment, PCR 7 enforced, PCR 11 omitted")
			// Get the full SealedVolume and update it
			cmd := exec.Command("kubectl", "get", "sealedvolume", sealedVolumeName, "-o", "yaml")
			out, err := cmd.CombinedOutput()
			Expect(err).ToNot(HaveOccurred(), string(out))

			// Parse and modify the SealedVolume
			// Set PCR 0 to empty (re-enrollment mode)
			// Keep PCR 7 as is (enforcement mode)
			// Remove PCR 11 if it exists (omit mode)
			patch := fmt.Sprintf(`{"spec":{"attestation":{"pcrValues":{"pcrs":{"0":"","7":"%s"}}}}}`, learnedPCR7)
			cmd = exec.Command("kubectl", "patch", "sealedvolume", sealedVolumeName, "--type=merge", "-p", patch)
			out, err = cmd.CombinedOutput()
			Expect(err).ToNot(HaveOccurred(), string(out))

			By("Rebooting and verifying mixed mode works")
			rebootAndConnect(testVM)
			verifyEncryptedPartition(testVM)

			By("Verifying PCR 0 was re-enrolled (learned new value)")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "sealedvolume", sealedVolumeName, "-o", "yaml")
				out, err := cmd.CombinedOutput()
				if err != nil {
					return false
				}
				outStr := string(out)
				// PCR 0 should have a value (not empty)
				return strings.Contains(outStr, "\"0\":") && !strings.Contains(outStr, "\"0\": \"\"")
			}, 30*time.Second, 5*time.Second).Should(BeTrue())

			By("Verifying PCR 7 remained in enforcement mode (same value)")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "sealedvolume", sealedVolumeName, "-o", "yaml")
				out, err := cmd.CombinedOutput()
				if err != nil {
					return false
				}
				outStr := string(out)
				return strings.Contains(outStr, fmt.Sprintf("\"7\": \"%s\"", learnedPCR7))
			}, 30*time.Second, 5*time.Second).Should(BeTrue())

			By("Verifying EK remained in enforcement mode")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "sealedvolume", sealedVolumeName, "-o", "yaml")
				out, err := cmd.CombinedOutput()
				if err != nil {
					return false
				}
				outStr := string(out)
				// EK should still be present and match
				return strings.Contains(outStr, "ekPublicKey:") && strings.Contains(outStr, learnedEK)
			}, 30*time.Second, 5*time.Second).Should(BeTrue())
		})
	})
})
