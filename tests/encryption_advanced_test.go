package mos_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
)

// The specs in this file cover the kcrypt-challenger scenarios that were
// tested in the pre-monorepo kcrypt-challenger repo but are not exercised
// by the existing encryption_test.go: the full TOFU + quarantine + PCR/AK
// management + secret-reuse workflow, and the selective enrollment modes
// (EK-only, selective PCR tracking, EK re-enrollment, mixed modes). The
// suite bootstrap (VM, TPM, k3d, kcrypt-challenger install, KMS_ADDRESS)
// is the same one encryption_test.go depends on and is wired by
// .github/encryption-tests.sh.

// getSealedVolumeName is the naming convention the kcrypt-challenger uses
// for TOFU-created SealedVolumes. The input tpmHash is the sha256 hash
// (64 hex chars) the discovery binary prints; the produced name is always
// `tofu-<first 8 lowercase hex chars>` and never exceeds 13 characters, so
// the full SafeKubeName truncation logic upstream would return the input
// unchanged for these inputs.
func getSealedVolumeName(tpmHash string) string {
	return fmt.Sprintf("tofu-%s", strings.ToLower(tpmHash[:8]))
}

// installKairosWithConfigAdvanced runs manual-install with a captured
// tee'd log. When expectSuccess is true the spec fails on install error;
// when false the caller is asserting failure and the output is left for
// downstream expectations. The output is also written to
// /root/manual-install.txt on the guest for post-mortem inspection.
func installKairosWithConfigAdvanced(vm VM, config string, expectSuccess bool) {
	GinkgoHelper()
	configFile, err := os.CreateTemp("", "")
	Expect(err).ToNot(HaveOccurred())
	defer os.Remove(configFile.Name())

	err = os.WriteFile(configFile.Name(), []byte(config), 0744)
	Expect(err).ToNot(HaveOccurred())

	err = vm.Scp(configFile.Name(), "config.yaml", "0744")
	Expect(err).ToNot(HaveOccurred())

	if expectSuccess {
		By("Installing Kairos with config")
		out, err := vm.Sudo("/bin/bash -c 'set -o pipefail && kairos-agent manual-install --device auto config.yaml 2>&1 | tee manual-install.txt'")
		Expect(err).ToNot(HaveOccurred(), out)
	} else {
		By("Installing Kairos with config (expecting failure)")
		_, _ = vm.Sudo("/bin/bash -c 'set -o pipefail && kairos-agent manual-install --device auto config.yaml 2>&1 | tee manual-install.txt'")
	}
}

// rebootAndConnect reboots the guest and waits for SSH to come back.
func rebootAndConnect(vm VM) {
	GinkgoHelper()
	By("Rebooting VM")
	vm.Reboot()
	By("Waiting for VM to be connectable")
	vm.EventuallyConnects(1200)
}

// verifyEncryptedPartition asserts that COS_PERSISTENT is a LUKS volume
// after reboot and that it was unlocked into a mapper device.
func verifyEncryptedPartition(vm VM) {
	GinkgoHelper()
	By("Verifying encrypted partition exists")
	out, err := vm.Sudo("blkid")
	Expect(err).ToNot(HaveOccurred(), out)
	Expect(out).To(MatchRegexp("TYPE=\"crypto_LUKS\" PARTLABEL=\"persistent\""), out)
	Expect(out).To(MatchRegexp("/dev/mapper.*LABEL=\"COS_PERSISTENT\""), out)
}

// checkPassphraseRetrieval invokes the discovery CLI in-guest against the
// challenger server for a partition label, returning the retrieved
// passphrase or an error containing the CLI's stderr.
func checkPassphraseRetrieval(vm VM, partitionLabel string) (string, error) {
	GinkgoHelper()
	By(fmt.Sprintf("Testing passphrase retrieval for partition %s via CLI", partitionLabel))
	cliCmd := fmt.Sprintf(`/system/discovery/kcrypt-discovery-challenger get \
	  --partition-label=%s \
	  --challenger-server="http://%s" \
	  2>&1`, partitionLabel, os.Getenv("KMS_ADDRESS"))

	out, err := vm.Sudo(cliCmd)
	if err != nil {
		return "", fmt.Errorf("%v: %s", err, out)
	}
	passphrase := strings.TrimSpace(out)
	if passphrase == "" {
		return "", fmt.Errorf("empty passphrase response")
	}
	return passphrase, nil
}

// expectPassphraseRetrieval asserts the CLI succeeds (or fails) as
// requested.
func expectPassphraseRetrieval(vm VM, partitionLabel string, shouldSucceed bool) {
	GinkgoHelper()
	passphrase, err := checkPassphraseRetrieval(vm, partitionLabel)
	if shouldSucceed {
		Expect(err).ToNot(HaveOccurred(), "Passphrase retrieval should have succeeded")
		Expect(passphrase).ToNot(BeEmpty(), "Passphrase should not be empty")
	} else {
		Expect(err).To(HaveOccurred(), "Passphrase retrieval should have failed")
	}
}

// expectPassphraseRetrievalWithError asserts that the CLI failed and the
// error output contains the given substring.
func expectPassphraseRetrievalWithError(vm VM, partitionLabel string, expectedError string) {
	GinkgoHelper()
	passphrase, err := checkPassphraseRetrieval(vm, partitionLabel)
	Expect(err).To(MatchError(ContainSubstring(expectedError)),
		"Expected passphrase retrieval to fail with error containing '%s', but got passphrase: %s",
		expectedError, passphrase)
}

// createSealedVolumeWithAttestation creates a SealedVolume with the given
// attestation configuration. Passing nil skips the attestation stanza.
func createSealedVolumeWithAttestation(tpmHash string, attestationConfig map[string]any) {
	sealedVolumeName := getSealedVolumeName(tpmHash)
	sealedVolumeYaml := fmt.Sprintf(`---
apiVersion: keyserver.kairos.io/v1alpha1
kind: SealedVolume
metadata:
  name: "%s"
  namespace: default
spec:
  TPMHash: "%s"
  partitions:
    - label: COS_PERSISTENT
  quarantined: false`, sealedVolumeName, tpmHash)

	if attestationConfig != nil {
		sealedVolumeYaml += "\n  attestation:"
		for key, value := range attestationConfig {
			switch v := value.(type) {
			case string:
				sealedVolumeYaml += fmt.Sprintf("\n    %s: \"%s\"", key, v)
			case map[string]string:
				sealedVolumeYaml += fmt.Sprintf("\n    %s:", key)
				for k, val := range v {
					sealedVolumeYaml += "\n      pcrs:"
					sealedVolumeYaml += fmt.Sprintf("\n        \"%s\": \"%s\"", k, val)
				}
			}
		}
	}

	By(fmt.Sprintf("Creating SealedVolume with attestation config: %+v", attestationConfig))
	kubectlApplyYaml(sealedVolumeYaml)
}

// updateSealedVolumeAttestation patches a field under spec.attestation on
// the SealedVolume for tpmHash. Field paths under pcrValues.pcrs.* are
// expanded to the nested object shape.
func updateSealedVolumeAttestation(tpmHash string, field, value string) {
	GinkgoHelper()
	sealedVolumeName := getSealedVolumeName(tpmHash)
	By(fmt.Sprintf("Updating SealedVolume %s field %s (value length: %d)", sealedVolumeName, field, len(value)))

	valueJSON, err := json.Marshal(value)
	Expect(err).ToNot(HaveOccurred(), "Failed to marshal value to JSON")

	var patch string
	if pcrIndex, hasPrefix := strings.CutPrefix(field, "pcrValues.pcrs."); hasPrefix {
		patch = fmt.Sprintf(`{"spec":{"attestation":{"pcrValues":{"pcrs":{"%s":%s}}}}}`, pcrIndex, valueJSON)
	} else {
		patch = fmt.Sprintf(`{"spec":{"attestation":{"%s":%s}}}`, field, valueJSON)
	}

	cmd := exec.Command("kubectl", "patch", "sealedvolume", sealedVolumeName, "--type=merge", "-p", patch)
	out, err := cmd.CombinedOutput()
	Expect(err).ToNot(HaveOccurred(), "kubectl patch failed: %s", string(out))
}

// quarantineTPM marks a SealedVolume as quarantined; unquarantineTPM
// undoes that. Used to test the challenger's revocation path.
func quarantineTPM(tpmHash string) {
	GinkgoHelper()
	sealedVolumeName := getSealedVolumeName(tpmHash)
	By(fmt.Sprintf("Quarantining TPM %s", sealedVolumeName))
	cmd := exec.Command("kubectl", "patch", "sealedvolume", sealedVolumeName, "--type=merge",
		"-p", `{"spec":{"quarantined":true}}`)
	out, err := cmd.CombinedOutput()
	Expect(err).ToNot(HaveOccurred(), string(out))
}

func unquarantineTPM(tpmHash string) {
	GinkgoHelper()
	sealedVolumeName := getSealedVolumeName(tpmHash)
	By(fmt.Sprintf("Unquarantining TPM %s", sealedVolumeName))
	cmd := exec.Command("kubectl", "patch", "sealedvolume", sealedVolumeName, "--type=merge",
		"-p", `{"spec":{"quarantined":false}}`)
	out, err := cmd.CombinedOutput()
	Expect(err).ToNot(HaveOccurred(), string(out))
}

// deleteSealedVolume deletes the SealedVolume for tpmHash. Safe to call
// on a missing volume.
func deleteSealedVolume(tpmHash string) {
	GinkgoHelper()
	sealedVolumeName := getSealedVolumeName(tpmHash)
	By(fmt.Sprintf("Deleting SealedVolume %s", sealedVolumeName))
	cmd := exec.Command("kubectl", "delete", "sealedvolume", sealedVolumeName, "--ignore-not-found=true")
	out, err := cmd.CombinedOutput()
	Expect(err).ToNot(HaveOccurred(), string(out))
}

// secretExists reports whether a k8s Secret by the given name is present
// in the default namespace.
func secretExists(secretName string) bool {
	cmd := exec.Command("kubectl", "get", "secret", secretName, "--ignore-not-found=true")
	out, err := cmd.CombinedOutput()
	return err == nil && len(out) > 0 && !strings.Contains(string(out), "NotFound")
}

// cleanupTestResources removes the SealedVolume and any labelled Secrets
// the kcrypt-challenger created for a given TPM hash.
func cleanupTestResources(tpmHash string) {
	if tpmHash == "" {
		return
	}
	deleteSealedVolume(tpmHash)
	cmd := exec.Command("kubectl", "delete", "secret",
		"-l", fmt.Sprintf("kcrypt.kairos.io/tpm-hash=%s", tpmHash),
		"--ignore-not-found=true", "--all-namespaces")
	_, _ = cmd.CombinedOutput()
}

var _ = Describe("kcrypt encryption remote complete workflow", Label("encryption-remote-complete-workflow"), func() {
	var config string
	var testVM VM
	var tpmHash string

	BeforeEach(func() {
		RegisterFailHandler(printInstallationOutput)
		_, testVM = startVM()
		testVM.EventuallyConnects(1200)
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			gatherLogs(testVM)
		}
		if tpmHash != "" {
			cleanupTestResources(tpmHash)
		}
		Expect(testVM.Destroy(nil)).ToNot(HaveOccurred())
	})

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

		installKairosWithConfigAdvanced(testVM, config, true)

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
			return strings.Contains(outStr, "attestation:") &&
				strings.Contains(outStr, "ekPublicKey:") &&
				strings.Contains(outStr, "pcrValues:") &&
				strings.Contains(outStr, "pcrs:") &&
				strings.Contains(outStr, `"0": ""`)
		}, 30*time.Second, 5*time.Second).Should(BeTrue(), "PCRs should be deferred (empty strings) during livecd mode")

		rebootAndConnect(testVM)
		verifyEncryptedPartition(testVM)

		By("Verifying PCRs are enrolled (non-empty) after reboot to installed system")
		Eventually(func() bool {
			cmd := exec.Command("kubectl", "get", "sealedvolume", sealedVolumeName, "-o", "yaml")
			out, err := cmd.CombinedOutput()
			if err != nil {
				return false
			}
			outStr := string(out)
			return strings.Contains(outStr, "pcrValues:") &&
				strings.Contains(outStr, `"0":`) &&
				!strings.Contains(outStr, `"0": ""`)
		}, 30*time.Second, 5*time.Second).Should(BeTrue(), "PCRs should be enrolled after reboot to installed system")

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
			cmd := exec.Command("kubectl", "get", "sealedvolume", sealedVolumeName, "-o", "yaml")
			out, err := cmd.CombinedOutput()
			if err != nil {
				return false
			}
			return strings.Contains(string(out), "\"0\":") &&
				!strings.Contains(string(out), "\"0\": \"\"") &&
				!strings.Contains(string(out), "\"0\": \"wrong-pcr0-value\"")
		}, 30*time.Second, 5*time.Second).Should(BeTrue())

		By("Testing EK re-enrollment by setting EK to empty")
		updateSealedVolumeAttestation(tpmHash, "ekPublicKey", "")

		By("Triggering re-enrollment by retrieving passphrase")
		expectPassphraseRetrieval(testVM, "COS_PERSISTENT", true)

		By("Verifying EK was re-enrolled with actual value")
		var learnedEK string
		Eventually(func() bool {
			cmd := exec.Command("kubectl", "get", "sealedvolume", sealedVolumeName, "-o", "jsonpath={.spec.attestation.ekPublicKey}")
			out, err := cmd.CombinedOutput()
			if err != nil {
				return false
			}
			// PEM format includes a trailing newline which is significant, so we do not trim.
			learnedEK = string(out)
			return learnedEK != "" && len(learnedEK) > 50
		}, 30*time.Second, 5*time.Second).Should(BeTrue())

		By("Testing EK enforcement by setting wrong EK value")
		updateSealedVolumeAttestation(tpmHash, "ekPublicKey", "wrong-ek-value")

		verifyCmd := exec.Command("kubectl", "get", "sealedvolume", sealedVolumeName, "-o", "jsonpath={.spec.attestation.ekPublicKey}")
		verifyOut, verifyErr := verifyCmd.CombinedOutput()
		Expect(verifyErr).ToNot(HaveOccurred())
		Expect(string(verifyOut)).To(Equal("wrong-ek-value"), "Wrong EK should be set")

		expectPassphraseRetrievalWithError(testVM, "COS_PERSISTENT", "attestation failed")
		expectPassphraseRetrievalWithError(testVM, "COS_OEM", "attestation failed")

		By("Restoring correct EK and verifying authentication works for both partitions")
		updateSealedVolumeAttestation(tpmHash, "ekPublicKey", learnedEK)

		time.Sleep(5 * time.Second)

		restoreCmd := exec.Command("kubectl", "get", "sealedvolume", sealedVolumeName, "-o", "jsonpath={.spec.attestation.ekPublicKey}")
		restoreOut, restoreErr := restoreCmd.CombinedOutput()
		Expect(restoreErr).ToNot(HaveOccurred())
		restoredEK := string(restoreOut)
		Expect(restoredEK).To(Equal(learnedEK), "Restored EK should match learned EK")
		Expect(len(restoredEK)).To(BeNumerically(">", 100), "Restored EK should be a full key, not 'wrong-ek-value'")

		expectPassphraseRetrieval(testVM, "COS_PERSISTENT", true)
		expectPassphraseRetrieval(testVM, "COS_OEM", true)

		By("Testing secret reuse when SealedVolume is recreated for both partitions")
		persistentSecretName := fmt.Sprintf("%s-cos-persistent", sealedVolumeName)
		oemSecretName := fmt.Sprintf("%s-cos-oem", sealedVolumeName)

		cmd := exec.Command("kubectl", "get", "secret", persistentSecretName, "-o", "yaml")
		originalPersistentSecretData, err := cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred())

		cmd = exec.Command("kubectl", "get", "secret", oemSecretName, "-o", "yaml")
		originalOemSecretData, err := cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred())

		deleteSealedVolume(tpmHash)

		Expect(secretExists(persistentSecretName)).To(BeTrue())
		Expect(secretExists(oemSecretName)).To(BeTrue())

		By("Recreating SealedVolume and verifying secret reuse for both partitions")
		createSealedVolumeWithAttestation(tpmHash, nil)

		rebootAndConnect(testVM)
		verifyEncryptedPartition(testVM)

		cmd = exec.Command("kubectl", "get", "secret", persistentSecretName, "-o", "yaml")
		newPersistentSecretData, err := cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred())

		cmd = exec.Command("kubectl", "get", "secret", oemSecretName, "-o", "yaml")
		newOemSecretData, err := cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred())

		Expect(string(newPersistentSecretData)).To(Equal(string(originalPersistentSecretData)))
		Expect(string(newOemSecretData)).To(Equal(string(originalOemSecretData)))
	})
})

var _ = Describe("kcrypt selective enrollment", func() {
	var testVM VM
	var tpmHash string

	BeforeEach(func() {
		RegisterFailHandler(printInstallationOutput)
		_, testVM = startVM()
		testVM.EventuallyConnects(1200)
		tpmHash = getTPMHash(testVM)
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			gatherLogs(testVM)
		}
		if tpmHash != "" {
			cleanupTestResources(tpmHash)
		}
		Expect(testVM.Destroy(nil)).ToNot(HaveOccurred())
	})

	Describe("EK-Only Verification (Empty Attestation Object)", Label("encryption-remote-ek-only"), func() {
		It("should handle empty attestation object (EK-only verification, no PCRs)", func() {
			By("Creating SealedVolume with empty attestation object")
			sealedVolumeName := getSealedVolumeName(tpmHash)

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
  attestation: {}
`, sealedVolumeName, tpmHash, sealedVolumeName))

			By("Installing Kairos with encryption")
			config := fmt.Sprintf(`#cloud-config

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

			// Fetch the fields directly via jsonpath rather than substring-matching
			// against the full YAML dump: kubectl's YAML encoder wraps multi-line
			// PEM strings in a block scalar (`ekPublicKey: |\n  ...`) and emits
			// hex PCR values unquoted, both of which the pre-monorepo checks
			// were written against but never actually ran, so their assumptions
			// were never validated.

			By("Verifying EK was learned and stored")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "sealedvolume", sealedVolumeName,
					"-o", "jsonpath={.spec.attestation.ekPublicKey}")
				out, err := cmd.CombinedOutput()
				if err != nil {
					return false
				}
				return strings.HasPrefix(strings.TrimSpace(string(out)), "-----BEGIN")
			}, 30*time.Second, 5*time.Second).Should(BeTrue())

			By("Verifying NO PCRs were stored")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "sealedvolume", sealedVolumeName,
					"-o", "jsonpath={.spec.attestation.pcrValues.pcrs}")
				out, err := cmd.CombinedOutput()
				if err != nil {
					return false
				}
				// jsonpath returns an empty string when the field is absent
				// and the literal "map[]" when it is present but empty.
				got := strings.TrimSpace(string(out))
				return got == "" || got == "map[]"
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

	Describe("Selective PCR Tracking from Initial Setup", Label("encryption-remote-selective-pcr"), func() {
		It("should handle selective PCR tracking from initial setup (track PCR 0,7 only, skip PCR 11)", func() {
			By("Creating SealedVolume with selective PCR configuration")
			sealedVolumeName := getSealedVolumeName(tpmHash)

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
    ekPublicKey: ""
    pcrValues:
      pcrs:
        "0": ""
        "7": ""
`, sealedVolumeName, tpmHash, sealedVolumeName))

			By("Installing Kairos with encryption")
			config := fmt.Sprintf(`#cloud-config

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

	Describe("EK Re-enrollment Mode", Label("encryption-remote-ek-reenroll"), func() {
		It("should learn EK when set to empty string (re-enrollment mode)", func() {
			By("Performing initial TOFU enrollment")
			deleteSealedVolume(tpmHash)

			config := fmt.Sprintf(`#cloud-config

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

			Expect(reEnrolledEK).To(Equal(learnedEK), "Re-enrolled EK should match original EK (same TPM)")
		})
	})

	Describe("Mixed Attestation Modes", Label("encryption-remote-mixed-modes"), func() {
		It("should handle mixed modes: EK enforcement + PCR re-enrollment + PCR omission", func() {
			By("Performing initial TOFU enrollment to learn EK and PCRs")
			deleteSealedVolume(tpmHash)

			config := fmt.Sprintf(`#cloud-config

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
			// Use jsonpath to read the fields directly. The pre-monorepo
			// substring-scrape against `kubectl get -o yaml` output was
			// fragile against how kubectl chooses to render (block scalars,
			// unquoted hex, etc.) and had no CI cell to catch drift.
			readSealedVolumeField := func(path string) string {
				cmd := exec.Command("kubectl", "get", "sealedvolume", sealedVolumeName,
					"-o", "jsonpath={"+path+"}")
				out, err := cmd.CombinedOutput()
				if err != nil {
					return ""
				}
				return strings.TrimSpace(string(out))
			}
			var learnedEK, learnedPCR0, learnedPCR7 string
			Eventually(func() bool {
				learnedEK = readSealedVolumeField(".spec.attestation.ekPublicKey")
				learnedPCR0 = readSealedVolumeField(`.spec.attestation.pcrValues.pcrs.0`)
				learnedPCR7 = readSealedVolumeField(`.spec.attestation.pcrValues.pcrs.7`)
				return learnedEK != "" && learnedPCR0 != "" && learnedPCR7 != ""
			}, 30*time.Second, 5*time.Second).Should(BeTrue())

			By("Reconfiguring with mixed modes: EK enforced, PCR 0 re-enrollment, PCR 7 enforced, PCR 11 omitted")
			patch := fmt.Sprintf(`{"spec":{"attestation":{"pcrValues":{"pcrs":{"0":"","7":"%s"}}}}}`, learnedPCR7)
			cmd := exec.Command("kubectl", "patch", "sealedvolume", sealedVolumeName, "--type=merge", "-p", patch)
			out, err := cmd.CombinedOutput()
			Expect(err).ToNot(HaveOccurred(), string(out))

			By("Rebooting and verifying mixed mode works")
			rebootAndConnect(testVM)
			verifyEncryptedPartition(testVM)

			By("Verifying PCR 0 was re-enrolled (learned new value)")
			Eventually(func() bool {
				return readSealedVolumeField(`.spec.attestation.pcrValues.pcrs.0`) != ""
			}, 30*time.Second, 5*time.Second).Should(BeTrue())

			By("Verifying PCR 7 remained in enforcement mode (same value)")
			Eventually(func() string {
				return readSealedVolumeField(`.spec.attestation.pcrValues.pcrs.7`)
			}, 30*time.Second, 5*time.Second).Should(Equal(learnedPCR7))

			By("Verifying EK remained in enforcement mode")
			Eventually(func() string {
				return readSealedVolumeField(".spec.attestation.ekPublicKey")
			}, 30*time.Second, 5*time.Second).Should(Equal(learnedEK))
		})
	})
})
