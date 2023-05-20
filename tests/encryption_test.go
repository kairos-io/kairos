package mos_test

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
	"gopkg.in/yaml.v3"
)

var installationOutput string
var vm VM

var _ = Describe("kcrypt encryption", func() {
	var config string

	BeforeEach(func() {
		RegisterFailHandler(printInstallationOutput)
		_, vm = startVM()
		fmt.Printf("\nvm.StateDir = %+v\n", vm.StateDir)

		vm.EventuallyConnects(1200)
	})

	JustBeforeEach(func() {
		configFile, err := os.CreateTemp("", "")
		Expect(err).ToNot(HaveOccurred())
		defer os.Remove(configFile.Name())

		err = os.WriteFile(configFile.Name(), []byte(config), 0744)
		Expect(err).ToNot(HaveOccurred())

		err = vm.Scp(configFile.Name(), "config.yaml", "0744")
		Expect(err).ToNot(HaveOccurred())

		installationOutput, err = vm.Sudo("/bin/bash -c 'set -o pipefail && kairos-agent manual-install --device auto config.yaml 2>&1 | tee manual-install.txt'")
		Expect(err).ToNot(HaveOccurred(), installationOutput)
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			gatherLogs(vm)
			serial, _ := os.ReadFile(filepath.Join(vm.StateDir, "serial.log"))
			fmt.Println(serial)
		}

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
		var err error

		BeforeEach(func() {
			tpmHash, err = vm.Sudo("/system/discovery/kcrypt-discovery-challenger")
			Expect(err).ToNot(HaveOccurred(), tpmHash)

			kubectlApplyYaml(fmt.Sprintf(`---
apiVersion: keyserver.kairos.io/v1alpha1
kind: SealedVolume
metadata:
  name: "%[1]s"
  namespace: default
spec:
  TPMHash: "%[1]s"
  partitions:
    - label: COS_PERSISTENT
  quarantined: false
`, strings.TrimSpace(tpmHash)))

			config = fmt.Sprintf(`#cloud-config

hostname: metal-{{ trunc 4 .MachineID }}
users:
- name: kairos
  passwd: kairos

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
			cmd := exec.Command("kubectl", "delete", "sealedvolume", tpmHash)
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
				fmt.Sprintf("%s-cos-persistent", tpmHash),
				"-o=go-template='{{.data.generated_by|base64decode}}'",
			)

			secretOut, err := cmd.CombinedOutput()
			Expect(err).ToNot(HaveOccurred(), string(secretOut))
			Expect(string(secretOut)).To(MatchRegexp("tpm"))
		})
	})

	// https://kairos.io/docs/advanced/partition_encryption/#scenario-static-keys
	When("using a remote key management server (static keys)", Label("remote-static"), func() {
		var tpmHash string
		var err error

		BeforeEach(func() {
			tpmHash, err = vm.Sudo("/system/discovery/kcrypt-discovery-challenger")
			Expect(err).ToNot(HaveOccurred(), tpmHash)

			kubectlApplyYaml(fmt.Sprintf(`---
apiVersion: v1
kind: Secret
metadata:
  name: %[1]s
  namespace: default
type: Opaque
stringData:
  pass: "awesome-plaintext-passphrase"
`, tpmHash))

			kubectlApplyYaml(fmt.Sprintf(`---
apiVersion: keyserver.kairos.io/v1alpha1
kind: SealedVolume
metadata:
    name: %[1]s
    namespace: default
spec:
  TPMHash: "%[1]s"
  partitions:
    - label: COS_PERSISTENT
      secret:
       name: %[1]s
       path: pass
  quarantined: false
`, tpmHash))

			config = fmt.Sprintf(`#cloud-config

hostname: metal-{{ trunc 4 .MachineID }}
users:
- name: kairos
  passwd: kairos

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
			cmd := exec.Command("kubectl", "delete", "sealedvolume", tpmHash)
			out, err := cmd.CombinedOutput()
			Expect(err).ToNot(HaveOccurred(), out)

			cmd = exec.Command("kubectl", "delete", "secret", tpmHash)
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

	When("the key management server is listening on https", func() {
		var tpmHash string
		var err error

		BeforeEach(func() {
			tpmHash, err = vm.Sudo("/system/discovery/kcrypt-discovery-challenger")
			Expect(err).ToNot(HaveOccurred(), tpmHash)

			kubectlApplyYaml(fmt.Sprintf(`---
apiVersion: keyserver.kairos.io/v1alpha1
kind: SealedVolume
metadata:
  name: "%[1]s"
  namespace: default
spec:
  TPMHash: "%[1]s"
  partitions:
    - label: COS_PERSISTENT
  quarantined: false
`, strings.TrimSpace(tpmHash)))
		})

		When("the certificate is pinned on the configuration", Label("remote-https-pinned"), func() {
			BeforeEach(func() {
				cert := getChallengerServerCert()
				kcryptConfig := createConfigWithCert(fmt.Sprintf("https://%s", os.Getenv("KMS_ADDRESS")), cert)
				kcryptConfigBytes, err := yaml.Marshal(kcryptConfig)
				Expect(err).ToNot(HaveOccurred())
				config = fmt.Sprintf(`#cloud-config

hostname: metal-{{ trunc 4 .MachineID }}
users:
- name: kairos
  passwd: kairos

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
		})

		When("the no certificate is set in the configuration", Label("remote-https-bad-cert"), func() {
			BeforeEach(func() {
				config = fmt.Sprintf(`#cloud-config

hostname: metal-{{ trunc 4 .MachineID }}
users:
- name: kairos
  passwd: kairos

install:
  encrypted_partitions:
  - COS_PERSISTENT
  grub_options:
    extra_cmdline: "rd.neednet=1"
  reboot: false # we will reboot manually

kcrypt:
  challenger:
    challenger_server: "https://%s"
    nv_index: ""
    c_index: ""
    tpm_device: ""
`, os.Getenv("KMS_ADDRESS"))
			})

			It("fails to talk to the server", func() {
				out, err := vm.Sudo("cat manual-install.txt")
				Expect(err).ToNot(HaveOccurred(), out)
				Expect(out).To(MatchRegexp("could not encrypt partition.*x509: certificate signed by unknown authority"))
			})
		})
	})
})

func printInstallationOutput(message string, callerSkip ...int) {
	fmt.Printf("This is the installation output in case it's useful:\n%s\n", installationOutput)

	// Ensures the correct line numbers are reported
	Fail(message, callerSkip[0]+1)
}

func kubectlApplyYaml(yamlData string) {
	yamlFile, err := os.CreateTemp("", "")
	Expect(err).ToNot(HaveOccurred())
	defer os.Remove(yamlFile.Name())

	err = os.WriteFile(yamlFile.Name(), []byte(yamlData), 0744)
	Expect(err).ToNot(HaveOccurred())

	cmd := exec.Command("kubectl", "apply", "-f", yamlFile.Name())
	out, err := cmd.CombinedOutput()
	Expect(err).ToNot(HaveOccurred(), out)
}

func getChallengerServerCert() string {
	cmd := exec.Command(
		"kubectl", "get", "secret", "-n", "default", "kms-tls",
		"-o", `go-template={{ index .data "ca.crt" | base64decode }}`)
	out, err := cmd.CombinedOutput()
	Expect(err).ToNot(HaveOccurred(), string(out))

	return string(out)
}

type Config struct {
	Kcrypt struct {
		Challenger struct {
			Server string `yaml:"challenger_server,omitempty"`
			// Non-volatile index memory: where we store the encrypted passphrase (offline mode)
			NVIndex string `yaml:"nv_index,omitempty"`
			// Certificate index: this is where the rsa pair that decrypts the passphrase lives
			CIndex      string `yaml:"c_index,omitempty"`
			TPMDevice   string `yaml:"tpm_device,omitempty"`
			Certificate string `yaml:"certificate,omitempty"`
		}
	}
}

func createConfigWithCert(server, cert string) Config {
	return Config{
		Kcrypt: struct {
			Challenger struct {
				Server      string "yaml:\"challenger_server,omitempty\""
				NVIndex     string "yaml:\"nv_index,omitempty\""
				CIndex      string "yaml:\"c_index,omitempty\""
				TPMDevice   string "yaml:\"tpm_device,omitempty\""
				Certificate string "yaml:\"certificate,omitempty\""
			}
		}{
			Challenger: struct {
				Server      string "yaml:\"challenger_server,omitempty\""
				NVIndex     string "yaml:\"nv_index,omitempty\""
				CIndex      string "yaml:\"c_index,omitempty\""
				TPMDevice   string "yaml:\"tpm_device,omitempty\""
				Certificate string "yaml:\"certificate,omitempty\""
			}{
				Server:      server,
				NVIndex:     "",
				CIndex:      "",
				TPMDevice:   "",
				Certificate: cert,
			},
		},
	}
}
