package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/google/uuid"
	process "github.com/mudler/go-processmanager"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
	machine "github.com/spectrocloud/peg/pkg/machine"
	"github.com/spectrocloud/peg/pkg/machine/types"

	"github.com/kairos-io/kairos-challenger/pkg/kube"
)

// Global VM variable for fail handler access
var globalVM *VM

func TestE2e(t *testing.T) {
	RegisterFailHandler(printChallengerLogsOnFailure)
	//RegisterFailHandler(Fail)
	RunSpecs(t, "kcrypt-challenger e2e test Suite")
}

type VMOptions struct {
	ISO        string
	User       string
	Password   string
	Memory     string
	CPUS       string
	RunSpicy   bool
	UseKVM     bool
	EmulateTPM bool
}

func DefaultVMOptions() VMOptions {
	var err error

	memory := os.Getenv("MEMORY")
	if memory == "" {
		memory = "2096"
	}
	cpus := os.Getenv("CPUS")
	if cpus == "" {
		cpus = "2"
	}

	runSpicy := false
	if s := os.Getenv("MACHINE_SPICY"); s != "" {
		runSpicy, err = strconv.ParseBool(os.Getenv("MACHINE_SPICY"))
		Expect(err).ToNot(HaveOccurred())
	}

	useKVM := false
	if envKVM := os.Getenv("KVM"); envKVM != "" {
		useKVM, err = strconv.ParseBool(os.Getenv("KVM"))
		Expect(err).ToNot(HaveOccurred())
	}

	return VMOptions{
		ISO:        os.Getenv("ISO"),
		User:       user(),
		Password:   pass(),
		Memory:     memory,
		CPUS:       cpus,
		RunSpicy:   runSpicy,
		UseKVM:     useKVM,
		EmulateTPM: true,
	}
}

func user() string {
	user := os.Getenv("SSH_USER")
	if user == "" {
		user = "kairos"
	}
	return user
}

func pass() string {
	pass := os.Getenv("SSH_PASS")
	if pass == "" {
		pass = "kairos"
	}

	return pass
}

func startVM(vmOpts VMOptions) (context.Context, VM) {
	if vmOpts.ISO == "" {
		fmt.Println("ISO missing")
		os.Exit(1)
	}

	vmName := uuid.New().String()

	stateDir, err := os.MkdirTemp("", "")
	Expect(err).ToNot(HaveOccurred())

	if vmOpts.EmulateTPM {
		emulateTPM(stateDir)
	}

	sshPort, err := getFreePort()
	Expect(err).ToNot(HaveOccurred())

	opts := []types.MachineOption{
		types.QEMUEngine,
		types.WithISO(vmOpts.ISO),
		types.WithMemory(vmOpts.Memory),
		types.WithCPU(vmOpts.CPUS),
		types.WithSSHPort(strconv.Itoa(sshPort)),
		types.WithID(vmName),
		types.WithSSHUser(vmOpts.User),
		types.WithSSHPass(vmOpts.Password),
		types.OnFailure(func(p *process.Process) {
			defer GinkgoRecover()

			var stdout, stderr, serial, status string

			if stdoutBytes, err := os.ReadFile(p.StdoutPath()); err != nil {
				stdout = fmt.Sprintf("Error reading stdout file: %s\n", err)
			} else {
				stdout = string(stdoutBytes)
			}

			if stderrBytes, err := os.ReadFile(p.StderrPath()); err != nil {
				stderr = fmt.Sprintf("Error reading stderr file: %s\n", err)
			} else {
				stderr = string(stderrBytes)
			}

			if status, err = p.ExitCode(); err != nil {
				status = fmt.Sprintf("Error reading exit code file: %s\n", err)
			}

			if serialBytes, err := os.ReadFile(path.Join(p.StateDir(), "serial.log")); err != nil {
				serial = fmt.Sprintf("Error reading serial log file: %s\n", err)
			} else {
				serial = string(serialBytes)
			}

			Fail(fmt.Sprintf("\nVM Aborted.\nstdout: %s\nstderr: %s\nserial: %s\nExit status: %s\n",
				stdout, stderr, serial, status))
		}),
		types.WithStateDir(stateDir),
		// Serial output to file: https://superuser.com/a/1412150
		func(m *types.MachineConfig) error {
			if vmOpts.EmulateTPM {
				m.Args = append(m.Args,
					"-chardev", fmt.Sprintf("socket,id=chrtpm,path=%s/swtpm-sock", path.Join(stateDir, "tpm")),
					"-tpmdev", "emulator,id=tpm0,chardev=chrtpm", "-device", "tpm-tis,tpmdev=tpm0")
			}
			m.Args = append(m.Args,
				"-chardev", fmt.Sprintf("stdio,mux=on,id=char0,logfile=%s,signal=off", path.Join(stateDir, "serial.log")),
				"-serial", "chardev:char0",
				"-mon", "chardev=char0",
			)
			return nil
		},
	}

	// Set this to true to debug.
	// You can connect to it with "spicy" or other tool.
	var spicePort int
	if vmOpts.RunSpicy {
		spicePort, err = getFreePort()
		Expect(err).ToNot(HaveOccurred())
		fmt.Printf("Spice port = %d\n", spicePort)
		opts = append(opts, types.WithDisplay(fmt.Sprintf("-spice port=%d,addr=127.0.0.1,disable-ticketing", spicePort)))
	}

	if vmOpts.UseKVM {
		opts = append(opts, func(m *types.MachineConfig) error {
			m.Args = append(m.Args,
				"-enable-kvm",
			)
			return nil
		})
	}

	m, err := machine.New(opts...)
	Expect(err).ToNot(HaveOccurred())

	vm := NewVM(m, stateDir)
	globalVM = &vm // Set global VM for fail handler access

	ctx, err := vm.Start(context.Background())
	Expect(err).ToNot(HaveOccurred())

	if vmOpts.RunSpicy {
		cmd := exec.Command("spicy",
			"-h", "127.0.0.1",
			"-p", strconv.Itoa(spicePort))
		err = cmd.Start()
		Expect(err).ToNot(HaveOccurred())
	}

	return ctx, vm
}

// return the PID of the swtpm (to be killed later) and the state directory
func emulateTPM(stateDir string) {
	t := path.Join(stateDir, "tpm")
	err := os.MkdirAll(t, os.ModePerm)
	Expect(err).ToNot(HaveOccurred())

	cmd := exec.Command("swtpm",
		"socket",
		"--tpmstate", fmt.Sprintf("dir=%s", t),
		"--ctrl", fmt.Sprintf("type=unixio,path=%s/swtpm-sock", t),
		"--tpm2", "--log", "level=20")
	err = cmd.Start()
	Expect(err).ToNot(HaveOccurred())

	err = os.WriteFile(path.Join(t, "pid"), []byte(strconv.Itoa(cmd.Process.Pid)), 0744)
	Expect(err).ToNot(HaveOccurred())
}

// https://gist.github.com/sevkin/96bdae9274465b2d09191384f86ef39d
// GetFreePort asks the kernel for a free open port that is ready to use.
func getFreePort() (port int, err error) {
	var a *net.TCPAddr
	if a, err = net.ResolveTCPAddr("tcp", "localhost:0"); err == nil {
		var l *net.TCPListener
		if l, err = net.ListenTCP("tcp", a); err == nil {
			defer l.Close()
			return l.Addr().(*net.TCPAddr).Port, nil
		}
	}
	return
}

// ========================================
// Common Test Helper Functions
// ========================================

// Helper to install Kairos with given config
func installKairosWithConfig(vm VM, config string) {
	GinkgoHelper()
	configFile, err := os.CreateTemp("", "")
	Expect(err).ToNot(HaveOccurred())
	defer os.Remove(configFile.Name())

	err = os.WriteFile(configFile.Name(), []byte(config), 0744)
	Expect(err).ToNot(HaveOccurred())

	err = vm.Scp(configFile.Name(), "config.yaml", "0744")
	Expect(err).ToNot(HaveOccurred())

	By("Installing Kairos with config")
	installationOutput, err := vm.Sudo("/bin/bash -c 'set -o pipefail && kairos-agent manual-install --device auto config.yaml 2>&1 | tee manual-install.txt'")
	Expect(err).ToNot(HaveOccurred(), installationOutput)
}

// Helper to reboot and wait for connection
func rebootAndConnect(vm VM) {
	GinkgoHelper()
	By("Rebooting VM")
	vm.Reboot()
	By("Waiting for VM to be connectable")
	vm.EventuallyConnects(1200)
}

// Helper to verify encrypted partition exists
func verifyEncryptedPartition(vm VM) {
	GinkgoHelper()
	By("Verifying encrypted partition exists")
	out, err := vm.Sudo("blkid")
	Expect(err).ToNot(HaveOccurred(), out)
	Expect(out).To(MatchRegexp("TYPE=\"crypto_LUKS\" PARTLABEL=\"persistent\""), out)
	Expect(out).To(MatchRegexp("/dev/mapper.*LABEL=\"COS_PERSISTENT\""), out)
}

// Helper to get TPM hash from VM
func getTPMHash(vm VM) string {
	GinkgoHelper()
	By("Getting TPM hash from VM")
	hash, err := vm.Sudo("/system/discovery/kcrypt-discovery-challenger")
	Expect(err).ToNot(HaveOccurred(), hash)
	return strings.TrimSpace(hash)
}

// Helper to test passphrase retrieval via CLI (returns passphrase and error)
func checkPassphraseRetrieval(vm VM, partitionLabel string) (string, error) {
	GinkgoHelper()
	By(fmt.Sprintf("Testing passphrase retrieval for partition %s via CLI", partitionLabel))

	// Configure the CLI to use the challenger server
	// Capture both stdout and stderr by redirecting stderr to stdout
	cliCmd := fmt.Sprintf(`/system/discovery/kcrypt-discovery-challenger get \
	  --partition-label=%s \
	  --challenger-server="http://%s" \
	  2>&1`, partitionLabel, os.Getenv("KMS_ADDRESS"))

	out, err := vm.Sudo(cliCmd)
	if err != nil {
		By(fmt.Sprintf("Passphrase retrieval failed: %v, output: %s", err, out))
		return "", fmt.Errorf("%v: %s", err, out)
	}

	// Check if we got a passphrase (non-empty output)
	passphrase := strings.TrimSpace(out)
	if len(passphrase) > 0 {
		By("Passphrase retrieval successful")
		return passphrase, nil
	}

	By("Passphrase retrieval failed - empty response")
	return "", fmt.Errorf("empty passphrase response")
}

// Helper to test passphrase retrieval with expectation (for cleaner test logic)
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

// Helper to test passphrase retrieval with expected error message
func expectPassphraseRetrievalWithError(vm VM, partitionLabel string, expectedError string) {
	GinkgoHelper()
	passphrase, err := checkPassphraseRetrieval(vm, partitionLabel)
	Expect(err).To(MatchError(ContainSubstring(expectedError)),
		"Expected passphrase retrieval to fail with error containing '%s', but got passphrase: %s",
		expectedError, passphrase)
}

// Helper to get the correct SealedVolume name from TPM hash
// This uses the same logic as pkg/challenger/challenger.go
func getSealedVolumeName(tpmHash string) string {
	return kube.SafeKubeName(fmt.Sprintf("tofu-%s", strings.ToLower(tpmHash[:8])))
}

// Helper to create SealedVolume with specific attestation configuration
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

// Helper to update SealedVolume attestation configuration
func updateSealedVolumeAttestation(tpmHashParam string, field, value string) {
	GinkgoHelper()
	sealedVolumeName := getSealedVolumeName(tpmHashParam)
	By(fmt.Sprintf("Updating SealedVolume %s field %s (value length: %d)", sealedVolumeName, field, len(value)))

	// Properly escape the value for JSON
	valueJSON, err := json.Marshal(value)
	Expect(err).ToNot(HaveOccurred(), "Failed to marshal value to JSON")

	var patch string
	// Handle nested PCR fields specially
	if pcrIndex, hasPrefix := strings.CutPrefix(field, "pcrValues.pcrs."); hasPrefix {
		patch = fmt.Sprintf(`{"spec":{"attestation":{"pcrValues":{"pcrs":{"%s":%s}}}}}`, pcrIndex, valueJSON)
	} else {
		patch = fmt.Sprintf(`{"spec":{"attestation":{"%s":%s}}}`, field, valueJSON)
	}

	cmd := exec.Command("kubectl", "patch", "sealedvolume", sealedVolumeName, "--type=merge", "-p", patch)
	out, err := cmd.CombinedOutput()
	Expect(err).ToNot(HaveOccurred(), "kubectl patch failed: %s", string(out))
}

// Helper to quarantine TPM
func quarantineTPM(tpmHash string) {
	GinkgoHelper()
	sealedVolumeName := getSealedVolumeName(tpmHash)
	By(fmt.Sprintf("Quarantining TPM %s", sealedVolumeName))
	patch := `{"spec":{"quarantined":true}}`
	cmd := exec.Command("kubectl", "patch", "sealedvolume", sealedVolumeName, "--type=merge", "-p", patch)
	out, err := cmd.CombinedOutput()
	Expect(err).ToNot(HaveOccurred(), string(out))
}

// Helper to unquarantine TPM
func unquarantineTPM(tpmHashParam string) {
	GinkgoHelper()
	sealedVolumeName := getSealedVolumeName(tpmHashParam)
	By(fmt.Sprintf("Unquarantining TPM %s", sealedVolumeName))
	patch := `{"spec":{"quarantined":false}}`
	cmd := exec.Command("kubectl", "patch", "sealedvolume", sealedVolumeName, "--type=merge", "-p", patch)
	out, err := cmd.CombinedOutput()
	Expect(err).ToNot(HaveOccurred(), string(out))
}

// Helper to delete SealedVolume
func deleteSealedVolume(tpmHashParam string) {
	GinkgoHelper()
	sealedVolumeName := getSealedVolumeName(tpmHashParam)
	By(fmt.Sprintf("Deleting SealedVolume %s", sealedVolumeName))
	cmd := exec.Command("kubectl", "delete", "sealedvolume", sealedVolumeName, "--ignore-not-found=true")
	out, err := cmd.CombinedOutput()
	Expect(err).ToNot(HaveOccurred(), string(out))
}

// Helper to check if secret exists
func secretExists(secretName string) bool {
	cmd := exec.Command("kubectl", "get", "secret", secretName, "--ignore-not-found=true")
	out, err := cmd.CombinedOutput()
	return err == nil && len(out) > 0 && !strings.Contains(string(out), "NotFound")
}

// Helper to apply YAML to Kubernetes
func kubectlApplyYaml(yamlData string) {
	yamlFile, err := os.CreateTemp("", "")
	Expect(err).ToNot(HaveOccurred())
	defer os.Remove(yamlFile.Name())

	err = os.WriteFile(yamlFile.Name(), []byte(yamlData), 0744)
	Expect(err).ToNot(HaveOccurred())

	cmd := exec.Command("kubectl", "apply", "-f", yamlFile.Name())
	out, err := cmd.CombinedOutput()
	Expect(err).ToNot(HaveOccurred(), string(out))
}

// Helper to cleanup test resources
func cleanupTestResources(tpmHash string) {
	if tpmHash != "" {
		deleteSealedVolume(tpmHash)

		// Cleanup associated secrets using labels
		// This will delete all secrets created by kcrypt-challenger for this TPM hash
		cmd := exec.Command("kubectl", "delete", "secret",
			"-l", fmt.Sprintf("kcrypt.kairos.io/tpm-hash=%s", tpmHash),
			"--ignore-not-found=true", "--all-namespaces")
		cmd.CombinedOutput()
	}
}

// Helper to install Kairos with config (handles both success and failure cases)
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
		installationOutput, err := vm.Sudo("/bin/bash -c 'set -o pipefail && kairos-agent manual-install --device auto config.yaml 2>&1 | tee manual-install.txt'")
		Expect(err).ToNot(HaveOccurred(), installationOutput)
	} else {
		By("Installing Kairos with config (expecting failure)")
		vm.Sudo("/bin/bash -c 'set -o pipefail && kairos-agent manual-install --device auto config.yaml 2>&1 | tee manual-install.txt'")
	}
}

// Helper to cleanup VM and TPM emulator
func cleanupVM(vm VM) {
	GinkgoHelper()
	By("Cleaning up test VM")
	err := vm.Destroy(func(vm VM) {
		// Stop TPM emulator
		tpmPID, err := os.ReadFile(path.Join(vm.StateDir, "tpm", "pid"))
		if err == nil && len(tpmPID) != 0 {
			pid, err := strconv.Atoi(string(tpmPID))
			if err == nil {
				syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
	Expect(err).ToNot(HaveOccurred())
}

// Fail handler that captures challenger logs when any test fails
func printChallengerLogsOnFailure(message string, callerSkip ...int) {
	if globalVM != nil {
		fmt.Printf("\n=== TEST FAILED - CAPTURING CHALLENGER LOGS ===\n")

		// Try to read the challenger log file
		logOutput, err := globalVM.Sudo("cat /var/log/kairos/kcrypt-discovery-challenger.log 2>/dev/null || echo 'Log file not found'")
		if err != nil {
			logOutput = fmt.Sprintf("Error reading challenger log: %v", err)
		}

		// Get additional system information that might be helpful
		processInfo, err := globalVM.Sudo("ps aux | grep kcrypt-discovery-challenger || echo 'No challenger processes found'")
		if err != nil {
			processInfo = fmt.Sprintf("Error getting process info: %v", err)
		}

		// Check if the challenger binary exists and is executable
		binaryInfo, err := globalVM.Sudo("ls -la /system/discovery/kcrypt-discovery-challenger 2>/dev/null || echo 'Challenger binary not found'")
		if err != nil {
			binaryInfo = fmt.Sprintf("Error checking binary: %v", err)
		}

		// Check TPM status
		tpmInfo, err := globalVM.Sudo("ls -la /dev/tpm* 2>/dev/null || echo 'No TPM devices found'")
		if err != nil {
			tpmInfo = fmt.Sprintf("Error checking TPM: %v", err)
		}

		// Print the logs to help with debugging
		fmt.Printf("Challenger log file content:\n%s\n", logOutput)
		fmt.Printf("\nProcess information:\n%s\n", processInfo)
		fmt.Printf("\nBinary information:\n%s\n", binaryInfo)
		fmt.Printf("\nTPM device information:\n%s\n", tpmInfo)
		fmt.Printf("=== END CHALLENGER LOGS ===\n\n")
	} else {
		fmt.Printf("\n=== TEST FAILED - NO VM AVAILABLE FOR LOG CAPTURE ===\n")
	}

	// Capture kcrypt-challenger-server logs from Kubernetes
	fmt.Printf("\n=== CAPTURING KCRYPT-CHALLENGER-SERVER LOGS ===\n")

	// First, let's see what namespaces and pods exist
	allPods, err := exec.Command("kubectl", "get", "pods", "-A").Output()
	if err != nil {
		allPods = []byte(fmt.Sprintf("Error getting all pods: %v", err))
	}
	fmt.Printf("All pods in cluster:\n%s\n", string(allPods))

	// Try to get server logs from both possible namespaces
	// Check system namespace first (based on challenger-patch.yaml)
	serverLogs, err := exec.Command("kubectl", "logs", "-n", "system", "-l", "control-plane=controller-manager", "--tail=500").Output()
	if err != nil {
		serverLogs = []byte(fmt.Sprintf("Error getting server logs from system namespace: %v", err))
	}
	fmt.Printf("Server logs from system namespace (last 500 lines):\n%s\n", string(serverLogs))

	// Also check default namespace (based on kustomization override)
	serverLogsDefault, err := exec.Command("kubectl", "logs", "-n", "default", "-l", "control-plane=controller-manager", "--tail=500").Output()
	if err != nil {
		serverLogsDefault = []byte(fmt.Sprintf("Error getting server logs from default namespace: %v", err))
	}
	fmt.Printf("Server logs from default namespace (last 500 lines):\n%s\n", string(serverLogsDefault))

	// Get logs from the last 10 minutes from both namespaces
	serverLogsAll, err := exec.Command("kubectl", "logs", "-n", "system", "-l", "control-plane=controller-manager", "--since=10m").Output()
	if err != nil {
		serverLogsAll = []byte(fmt.Sprintf("Error getting recent server logs from system namespace: %v", err))
	}
	fmt.Printf("\nServer logs from system namespace (last 10 minutes):\n%s\n", string(serverLogsAll))

	serverLogsAllDefault, err := exec.Command("kubectl", "logs", "-n", "default", "-l", "control-plane=controller-manager", "--since=10m").Output()
	if err != nil {
		serverLogsAllDefault = []byte(fmt.Sprintf("Error getting recent server logs from default namespace: %v", err))
	}
	fmt.Printf("\nServer logs from default namespace (last 10 minutes):\n%s\n", string(serverLogsAllDefault))

	// Check if there are any sealedvolume resources that might be relevant
	sealedVolumeInfo, err := exec.Command("kubectl", "get", "sealedvolume", "-A", "-o", "wide").Output()
	if err != nil {
		sealedVolumeInfo = []byte(fmt.Sprintf("Error getting sealedvolume info: %v", err))
	}
	fmt.Printf("\nSealedVolume resources:\n%s\n", string(sealedVolumeInfo))

	fmt.Printf("=== END KCRYPT-CHALLENGER-SERVER LOGS ===\n\n")

	// Ensures the correct line numbers are reported
	Fail(message, callerSkip[0]+1)
}
