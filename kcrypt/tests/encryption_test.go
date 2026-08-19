package e2e_test

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
	"gopkg.in/yaml.v3"

	client "github.com/kairos-io/kairos-challenger/cmd/discovery/client"
)

var installationOutput string
var vm VM
var mdnsVM VM

var _ = Describe("kcrypt encryption", Label("encryption-tests"), func() {
	var config string
	var vmOpts VMOptions
	var expectedInstallationSuccess bool

	BeforeEach(func() {
		expectedInstallationSuccess = true

		vmOpts = DefaultVMOptions()
		RegisterFailHandler(printInstallationOutput)
		_, vm = startVM(vmOpts)
		fmt.Printf("\nvm.StateDir = %+v\n", vm.StateDir)

		vm.EventuallyConnects(1200)
	})

	JustBeforeEach(func() {
		configFile, err := os.CreateTemp("", "")
		Expect(err).ToNot(HaveOccurred())
		defer os.Remove(configFile.Name())

		err = os.WriteFile(configFile.Name(), []byte(config), 0744)
		Expect(err).ToNot(HaveOccurred())

		By("Copying the config in the VM")
		err = vm.Scp(configFile.Name(), "config.yaml", "0744")
		Expect(err).ToNot(HaveOccurred())

		By("starting the installation")
		installationOutput, err = vm.Sudo("/bin/bash -c 'set -o pipefail && kairos-agent manual-install --device auto config.yaml 2>&1 | tee manual-install.txt'")
		if expectedInstallationSuccess {
			Expect(err).ToNot(HaveOccurred(), installationOutput)
		}
	})

	AfterEach(func() {
		vm.GatherLog("/run/immucore/immucore.log")
		err := vm.Destroy(func(vm VM) {
			// Stop TPM emulator
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

	// TODO: Use bridge networking because the default qemu networking won't cut it.
	// The mdns response can't reach the VM.
	XWhen("discovering KMS with mdns", Label("discoverable-kms"), func() {
		var tpmHash string
		var mdnsHostname string

		BeforeEach(func() {
			By("creating the secret in kubernetes")
			tpmHash = createTPMPassphraseSecret(vm)

			mdnsHostname = "discoverable-kms.local"

			By("deploying simple-mdns-server vm")
			mdnsVM = deploySimpleMDNSServer(mdnsHostname)
			By("continuing")

			// Ensure mdnsVM is cleaned up even if test fails
			DeferCleanup(func() {
				if mdnsVM.StateDir != "" {
					err := mdnsVM.Destroy(func(vm VM) {})
					if err != nil {
						fmt.Printf("Warning: Failed to cleanup mdnsVM: %v\n", err)
					}
				}
			})

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
  reboot: false # we will reboot manually

kcrypt:
  challenger:
    mdns: true
    challenger_server: "http://%[1]s"
`, mdnsHostname)
		})

		AfterEach(func() {
			sealedVolumeName := getSealedVolumeName(tpmHash)
			cmd := exec.Command("kubectl", "delete", "sealedvolume", sealedVolumeName)
			out, err := cmd.CombinedOutput()
			Expect(err).ToNot(HaveOccurred(), out)
		})

		It("discovers the KMS using mdns", func() {
			By("rebooting")
			vm.Reboot()
			By("checking that we can connect after installation")
			vm.EventuallyConnects(1200)
			By("checking if we got an encrypted partition")
			out, err := vm.Sudo("blkid")
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(MatchRegexp("TYPE=\"crypto_LUKS\" PARTLABEL=\"persistent\""), out)
		})
	})

	// https://kairos.io/docs/advanced/partition_encryption/#offline-mode
	When("doing local encryption", Label("local-encryption"), func() {
		BeforeEach(func() {
			config = `#cloud-config

install:
  encrypted_partitions:
  - COS_PERSISTENT
  reboot: false # we will reboot manually

hostname: metal-{{ trunc 4 .MachineID }}
users:
- name: kairos
  passwd: kairos
  groups:
    - admin
`
		})

		It("boots and has an encrypted partition", func() {
			vm.Reboot()
			vm.EventuallyConnects(1200)
			out, err := vm.Sudo("blkid")
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(MatchRegexp("TYPE=\"crypto_LUKS\" PARTLABEL=\"persistent\""), out)
		})
	})

	//https://kairos.io/docs/advanced/partition_encryption/#online-mode
	When("using a remote key management server (automated passphrase generation)", Label("remote-auto"), func() {
		var tpmHash string

		BeforeEach(func() {
			tpmHash = createTPMPassphraseSecret(vm)
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
  reboot: false # we will reboot manually

kcrypt:
  challenger:
    challenger_server: "http://%s"
    nv_index: ""
    c_index: ""
    tpm_device: ""
`, os.Getenv("KMS_ADDRESS"))
		})

		AfterEach(func() {
			sealedVolumeName := getSealedVolumeName(tpmHash)
			cmd := exec.Command("kubectl", "delete", "sealedvolume", sealedVolumeName)
			out, err := cmd.CombinedOutput()
			Expect(err).ToNot(HaveOccurred(), out)
		})

		It("creates a passphrase and a key/pair to decrypt it", func() {
			// Expect a LUKS partition
			vm.Reboot(750)
			vm.EventuallyConnects(1200)
			out, err := vm.Sudo("blkid")
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(MatchRegexp("TYPE=\"crypto_LUKS\" PARTLABEL=\"persistent\""), out)

			// Expect a secret to be created
			cmd := exec.Command("kubectl", "get", "secrets",
				fmt.Sprintf("%s-cos-persistent", getSealedVolumeName(tpmHash)),
				"-o=go-template='{{index .metadata.labels \"kcrypt.kairos.io/managed-by\"}}'",
			)

			secretOut, err := cmd.CombinedOutput()
			Expect(err).ToNot(HaveOccurred(), string(secretOut))
			// Verify the secret was created by kcrypt-challenger
			// Trim quotes and whitespace from kubectl output
			managedBy := strings.Trim(string(secretOut), "'\" \n\r\t")
			Expect(managedBy).To(Equal("kcrypt-challenger"))
		})
	})

	// https://kairos.io/docs/advanced/partition_encryption/#scenario-static-keys
	When("using a remote key management server (static keys)", Label("remote-static"), func() {
		var tpmHash string
		var sealedVolumeName string
		var secretName string
		var err error

		BeforeEach(func() {
			tpmHash, err = vm.Sudo("/system/discovery/kcrypt-discovery-challenger")
			Expect(err).ToNot(HaveOccurred(), tpmHash)
			tpmHash = strings.TrimSpace(tpmHash)

			// Use safe Kubernetes names (TPM hash is 64 chars, exceeds 63 char limit)
			sealedVolumeName = getSealedVolumeName(tpmHash)
			secretName = fmt.Sprintf("%s-cos-persistent", sealedVolumeName)

			By(fmt.Sprintf("Creating secret with name: %s", secretName))
			kubectlApplyYaml(fmt.Sprintf(`---
apiVersion: v1
kind: Secret
metadata:
  name: %[1]s
  namespace: default
type: Opaque
stringData:
  passphrase: "awesome-plaintext-passphrase"
`, secretName))

			// Verify secret was created
			By(fmt.Sprintf("Waiting for secret %s to be ready", secretName))
			Eventually(func() bool {
				exists := secretExists(secretName)
				if !exists {
					GinkgoWriter.Printf("Secret %s does not exist yet, waiting...\n", secretName)
				}
				return exists
			}, 10*time.Second, 1*time.Second).Should(BeTrue(), fmt.Sprintf("Secret %s should exist", secretName))

			By(fmt.Sprintf("Secret %s verified to exist", secretName))

			By(fmt.Sprintf("Creating SealedVolume with name: %s, TPM hash: %s", sealedVolumeName, tpmHash[:16]+"..."))
			kubectlApplyYaml(fmt.Sprintf(`---
apiVersion: keyserver.kairos.io/v1alpha1
kind: SealedVolume
metadata:
    name: %[1]s
    namespace: default
spec:
  TPMHash: "%[2]s"
  partitions:
    - label: COS_PERSISTENT
      secret:
       name: %[3]s
       path: passphrase
  quarantined: false
`, sealedVolumeName, tpmHash, secretName))

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
  reboot: false # we will reboot manually

kcrypt:
  challenger:
    challenger_server: "http://%s"
`, os.Getenv("KMS_ADDRESS"))
		})

		AfterEach(func() {
			cmd := exec.Command("kubectl", "delete", "sealedvolume", sealedVolumeName)
			out, err := cmd.CombinedOutput()
			Expect(err).ToNot(HaveOccurred(), out)

			cmd = exec.Command("kubectl", "delete", "secret", secretName)
			out, err = cmd.CombinedOutput()
			Expect(err).ToNot(HaveOccurred(), out)
		})

		It("creates uses the existing passphrase to decrypt it", func() {
			// Expect a LUKS partition
			vm.Reboot()
			vm.EventuallyConnects(1200)
			out, err := vm.Sudo("blkid")
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(MatchRegexp("TYPE=\"crypto_LUKS\" PARTLABEL=\"persistent\""), out)
			Expect(out).To(MatchRegexp("/dev/mapper.*LABEL=\"COS_PERSISTENT\""), out)
		})
	})

	When("the certificate is pinned on the configuration", Label("remote-https-pinned"), func() {
		var tpmHash string

		BeforeEach(func() {
			tpmHash = createTPMPassphraseSecret(vm)
			cert := getChallengerServerCert()
			kcryptConfig := createConfigWithCert(fmt.Sprintf("https://%s", os.Getenv("KMS_ADDRESS")), cert)
			kcryptConfigBytes, err := yaml.Marshal(kcryptConfig)
			Expect(err).ToNot(HaveOccurred())
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
  reboot: false # we will reboot manually

%s

`, string(kcryptConfigBytes))
		})

		It("successfully talks to the server", func() {
			vm.Reboot()
			vm.EventuallyConnects(1200)
			out, err := vm.Sudo("blkid")
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(MatchRegexp("TYPE=\"crypto_LUKS\" PARTLABEL=\"persistent\""), out)
			Expect(out).To(MatchRegexp("/dev/mapper.*LABEL=\"COS_PERSISTENT\""), out)
		})

		AfterEach(func() {
			sealedVolumeName := getSealedVolumeName(tpmHash)
			cmd := exec.Command("kubectl", "delete", "sealedvolume", sealedVolumeName)
			out, err := cmd.CombinedOutput()
			Expect(err).ToNot(HaveOccurred(), out)
		})
	})

	When("the no certificate is set in the configuration", Label("remote-https-bad-cert"), func() {
		var tpmHash string

		BeforeEach(func() {
			tpmHash = createTPMPassphraseSecret(vm)
			expectedInstallationSuccess = false

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
  reboot: false # we will reboot manually

kcrypt:
  challenger:
    challenger_server: "https://%s"
`, os.Getenv("KMS_ADDRESS"))
		})

		It("fails to talk to the server", func() {
			out, err := vm.Sudo("cat manual-install.txt")
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(MatchRegexp("failed to verify certificate: x509: certificate signed by unknown authority"))
		})

		AfterEach(func() {
			sealedVolumeName := getSealedVolumeName(tpmHash)
			cmd := exec.Command("kubectl", "delete", "sealedvolume", sealedVolumeName)
			out, err := cmd.CombinedOutput()
			Expect(err).ToNot(HaveOccurred(), out)
		})
	})
})

func printInstallationOutput(message string, callerSkip ...int) {
	fmt.Printf("This is the installation output in case it's useful:\n%s\n", installationOutput)

	// Ensures the correct line numbers are reported
	Fail(message, callerSkip[0]+1)
}

func getChallengerServerCert() string {
	cmd := exec.Command(
		"kubectl", "get", "secret", "-n", "default", "kms-tls",
		"-o", `go-template={{ index .data "ca.crt" | base64decode }}`)
	out, err := cmd.CombinedOutput()
	Expect(err).ToNot(HaveOccurred(), string(out))

	return string(out)
}

func createConfigWithCert(server, cert string) client.Config {
	c := client.Config{}
	c.Kcrypt.Challenger.Server = server
	c.Kcrypt.Challenger.Certificate = cert

	return c
}

func createTPMPassphraseSecret(vm VM) string {
	tpmHash, err := vm.Sudo("/system/discovery/kcrypt-discovery-challenger")
	Expect(err).ToNot(HaveOccurred(), tpmHash)
	tpmHash = strings.TrimSpace(tpmHash)

	// Use safe Kubernetes name (TPM hash is 64 chars, exceeds 63 char limit)
	sealedVolumeName := getSealedVolumeName(tpmHash)

	kubectlApplyYaml(fmt.Sprintf(`---
apiVersion: keyserver.kairos.io/v1alpha1
kind: SealedVolume
metadata:
  name: "%[1]s"
  namespace: default
spec:
  TPMHash: "%[2]s"
  partitions:
    - label: COS_PERSISTENT
  quarantined: false
`, sealedVolumeName, tpmHash))

	return tpmHash
}

// We run the simple-mdns-server (https://github.com/kairos-io/simple-mdns-server/)
// inside a VM next to the one we test. The server advertises the KMS as running on 10.0.2.2
// (the host machine). This is a "hack" and is needed because of how the default
// networking in qemu works. We need to be within the same network and that
// network is only available withing another VM.
// https://wiki.qemu.org/Documentation/Networking
func deploySimpleMDNSServer(hostname string) VM {
	opts := DefaultVMOptions()
	opts.Memory = "2000"
	opts.CPUS = "1"
	opts.EmulateTPM = false
	By("starting the VM for the mdns server")
	_, vm := startVM(opts)
	vm.EventuallyConnects(1200)

	By("downloading the simple-mdns-server release")
	out, err := vm.Sudo(`curl -s https://api.github.com/repos/kairos-io/simple-mdns-server/releases/latest | jq -r .assets[].browser_download_url | grep $(uname -m) | xargs curl -L -o sms.tar.gz`)
	Expect(err).ToNot(HaveOccurred(), string(out))

	By("extracting the binary")
	out, err = vm.Sudo("tar xvf sms.tar.gz")
	Expect(err).ToNot(HaveOccurred(), string(out))

	// Stop, disable, and mask avahi-daemon to free up port 5353 for simple-mdns-server
	// Masking prevents it from being started even if something tries to enable it
	By("stopping avahi-daemon services")
	out, err = vm.Sudo("systemctl stop avahi-daemon.service avahi-daemon.socket 2>/dev/null || true")
	Expect(err).ToNot(HaveOccurred(), string(out))
	out, err = vm.Sudo("systemctl disable avahi-daemon.service avahi-daemon.socket 2>/dev/null || true")
	Expect(err).ToNot(HaveOccurred(), string(out))
	out, err = vm.Sudo("systemctl mask avahi-daemon.service avahi-daemon.socket 2>/dev/null || true")
	Expect(err).ToNot(HaveOccurred(), string(out))

	// Verify avahi-daemon is stopped and port 5353 is free
	By("cheking if port 5353 is free")
	out, err = vm.Sudo("systemctl is-active avahi-daemon.service avahi-daemon.socket 2>&1 || true")
	Expect(err).ToNot(HaveOccurred(), string(out))
	out, err = vm.Sudo("ss -ulnp | grep :5353 || echo 'Port 5353 is free'")
	Expect(err).ToNot(HaveOccurred(), string(out))

	// Start the simple-mdns-server in the background
	By("starting the simple-mdns-server")
	out, err = vm.Sudo(fmt.Sprintf(
		"nohup ./simple-mdns-server --port 80 --address 10.0.2.2 --serviceType _kcrypt._tcp --hostName %s > /dev/null 2>&1 &", hostname))
	Expect(err).ToNot(HaveOccurred(), string(out))

	return vm
}
