package mos_test

import (
	"context"
	"crypto/rand"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	diskfs "github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/disk"
	"github.com/diskfs/go-diskfs/filesystem"
	"github.com/diskfs/go-diskfs/filesystem/iso9660"
	"github.com/google/uuid"
	"github.com/kairos-io/go-nodepair"
	qr "github.com/kairos-io/go-nodepair/qrcode"
	"github.com/mudler/edgevpn/pkg/node"
	process "github.com/mudler/go-processmanager"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
	"github.com/spectrocloud/peg/pkg/machine"
	"github.com/spectrocloud/peg/pkg/machine/types"
)

func TestSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "kairos Test Suite")
}

var getVersionCmd = ". /etc/kairos-release; [ ! -z \"$KAIROS_VERSION\" ] && echo $KAIROS_VERSION"
var getVersionCmdOsRelease = ". /etc/os-release; [ ! -z \"$KAIROS_VERSION\" ] && echo $KAIROS_VERSION"

// getUpgradeImage returns the image to upgrade to.
// It first checks if CONTAINER_IMAGE env var is set (used in CI for upgrading to the newly built image).
// If not set, it falls back to using kairos-agent upgrade list-releases to find the latest available upgrade.
func getUpgradeImage(vm VM) (string, error) {
	// First check if CONTAINER_IMAGE is set (CI scenario: upgrade to newly built image)
	if containerImage := os.Getenv("CONTAINER_IMAGE"); containerImage != "" {
		return containerImage, nil
	}

	// Fallback: Get the list of available releases
	out, err := vm.Sudo("kairos-agent upgrade list-releases 2>/dev/null")
	if err != nil {
		return "", fmt.Errorf("failed to list releases: %w", err)
	}

	// Parse the output line by line and find the first valid container image
	// The output may contain informational messages like "Using registry: ..." or sudo warnings
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Valid container images start with quay.io/kairos/ and contain a tag (colon)
		if strings.HasPrefix(line, "quay.io/kairos/") && strings.Contains(line, ":") {
			return line, nil
		}
	}

	return "", fmt.Errorf("no valid upgrade image found in output: %s", out)
}

// CreateDatasource creates a datasource iso from a given user-data file
// And returns the path to the datasource iso
// Its the caller's responsibility to remove the datasource iso afterwards
func CreateDatasource(userDataFile string) string {
	ds, err := os.MkdirTemp("", "datasource-*")
	Expect(err).ToNot(HaveOccurred())
	diskImg := path.Join(ds, "datasource.iso")
	var diskSize int64 = 1 * 1024 * 1024 // 1 MB
	mydisk, err := diskfs.Create(diskImg, diskSize, diskfs.SectorSizeDefault)
	Expect(err).ToNot(HaveOccurred())
	mydisk.LogicalBlocksize = 2048
	fspec := disk.FilesystemSpec{Partition: 0, FSType: filesystem.TypeISO9660, VolumeLabel: "cidata"}
	fs, err := mydisk.CreateFilesystem(fspec)
	Expect(err).ToNot(HaveOccurred())
	rw, err := fs.OpenFile("user-data", os.O_CREATE|os.O_RDWR)
	Expect(err).ToNot(HaveOccurred())
	content, err := os.ReadFile(userDataFile)
	_, err = rw.Write(content)
	Expect(rw.Close()).ToNot(HaveOccurred())
	Expect(err).ToNot(HaveOccurred())
	rw, err = fs.OpenFile("meta-data", os.O_CREATE|os.O_RDWR)
	Expect(err).ToNot(HaveOccurred())
	_, err = rw.Write([]byte(""))
	Expect(rw.Close()).ToNot(HaveOccurred())
	Expect(err).ToNot(HaveOccurred())
	iso, ok := fs.(*iso9660.FileSystem)
	Expect(ok).To(BeTrue())
	err = iso.Finalize(iso9660.FinalizeOptions{RockRidge: true, VolumeIdentifier: "cidata"})
	Expect(err).ToNot(HaveOccurred())
	return diskImg
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

func user() string {
	return getEnvOrDefault("SSH_USER", "kairos")
}

func pass() string {
	return getEnvOrDefault("SSH_PASS", "kairos")
}

func gatherLogs(vm VM) {
	// Use kairos-agent logs command to collect logs
	vm.Sudo("kairos-agent logs --output /run/kairos-logs.tar.gz")

	// Collect additional system information not covered by kairos-agent logs
	vm.Sudo("cat /oem/* > /run/oem.yaml")
	vm.Sudo("cat /etc/resolv.conf > /run/resolv.conf")
	vm.Sudo("k3s kubectl get pods -A -o json > /run/pods.json")
	vm.Sudo("k3s kubectl get events -A -o json > /run/events.json")
	vm.Sudo("cat /proc/cmdline > /run/cmdline")
	vm.Sudo("chmod 777 /run/events.json")

	vm.Sudo("df -h > /run/disk")
	vm.Sudo("mount > /run/mounts")
	vm.Sudo("blkid > /run/blkid")
	vm.Sudo("dmesg > /run/dmesg.log")

	// Collect Kubernetes logs
	vm.Scp("assets/kubernetes_logs.sh", "/tmp/logs.sh", "0770")
	vm.Sudo("sh /tmp/logs.sh > /run/kube_logs")

	vm.GatherAllLogs(
		[]string{
			"edgevpn@kairos",
		},
		[]string{
			"/var/log/edgevpn.log",
			"/run/pods.json",
			"/run/disk",
			"/run/mounts",
			"/run/blkid",
			"/run/events.json",
			"/run/kube_logs",
			"/run/cmdline",
			"/run/oem.yaml",
			"/run/resolv.conf",
			"/run/dmesg.log",
			"/tmp/ovmf_debug.log",
			"/run/kairos-logs.tar.gz",
		})
}

func startVM() (context.Context, VM) {
	stateDir, err := os.MkdirTemp("", "")
	Expect(err).ToNot(HaveOccurred())
	GinkgoLogr.Info("Starting VM", "stateDir", stateDir)

	opts := defaultVMOpts(stateDir)

	m, err := machine.New(opts...)
	Expect(err).ToNot(HaveOccurred())

	vm := NewVM(m, stateDir)

	ctx, err := vm.Start(context.Background())
	Expect(err).ToNot(HaveOccurred())

	return ctx, vm
}

func isFlavor(vm VM, flavor string) bool {
	out, err := vm.Sudo(fmt.Sprintf("cat /etc/os-release | grep ID=%s", flavor))
	return err == nil && out != ""
}

func expectDefaultService(vm VM) {
	By("checking if default service is active in live cd mode", func() {
		if isFlavor(vm, "alpine") {
			out, err := vm.Sudo("rc-status")
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).Should(ContainSubstring("kairos-agent"))
		} else {
			// This is also run in the upgrade latest, so we need to check for both kairos-installer and kairos in case the service name changed
			Eventually(func() string {

				out, _ := vm.Sudo("systemctl status kairos-installer || systemctl status kairos")
				return out
			}, 3*time.Minute, 2*time.Second).Should(
				Or(
					ContainSubstring("loaded (/etc/systemd/system/kairos-installer.service; enabled;"),
					ContainSubstring("loaded (/etc/systemd/system/kairos.service; enabled;"),
				))
		}
	})
}

func expectStartedInstallation(vm VM) {
	By("checking that installation has started", func() {
		Eventually(func() string {
			out, _ := vm.Sudo("ps aux || ps")
			return out
		}, 30*time.Minute, 1*time.Second).Should(ContainSubstring("/usr/bin/kairos-agent install"))
	})
}

func expectRebootedToActive(vm VM) {
	By("checking that vm has rebooted to 'active'", func() {
		Eventually(func() string {
			out, _ := vm.Sudo("kairos-agent state boot")
			return out
		}, 40*time.Minute, 10*time.Second).Should(
			Or(
				ContainSubstring("active_boot"),
			))
	})
}

func expectSecureBootEnabled(vm VM) {
	// Check for secureboot before install, based on firmware env var
	// if we set, then the test suite will load the secureboot firmware
	secureboot := os.Getenv("FIRMWARE")

	if secureboot != "" {
		By("checking that secureboot is enabled", func() {
			out, _ := vm.Sudo("dmesg | grep -i secure")
			Expect(out).To(ContainSubstring("Secure boot enabled"))
		})
	}
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

var kubectl = func(vm VM, s string) (string, error) {
	return vm.Sudo("k3s kubectl " + s)
}

// deployKairosOperator downloads the kairos-operator, deploys it with kubectl, and waits for it and the NodeOpUpgrade CRD to be ready.
func deployKairosOperator(vm VM) {
	By("downloading the kairos-operator kustomization locally (no git available on the node)")
	out, err := vm.Sudo("curl -sL https://github.com/kairos-io/kairos-operator/archive/refs/heads/main.tar.gz | tar -xz -C /tmp")
	Expect(err).ToNot(HaveOccurred(), out)

	By("deploying the kairos-operator")
	out, err = kubectl(vm, "apply -k /tmp/kairos-operator-main/config/default")
	Expect(err).ToNot(HaveOccurred(), out)
	Expect(out).To(Or(ContainSubstring("created"), ContainSubstring("unchanged")))

	By("waiting for kairos-operator to be ready")
	Eventually(func() string {
		out, _ := kubectl(vm, "get pods -n operator-system")
		return out
	}, 900*time.Second, 10*time.Second).Should(ContainSubstring("operator-kairos-operator"))

	By("waiting for the NodeOpUpgrade CRD to be created")
	Eventually(func() string {
		out, _ := kubectl(vm, "get crds")
		return out
	}, 300*time.Second, 10*time.Second).Should(ContainSubstring("nodeopupgrades.operator.kairos.io"))

	By("wait for all containers to be in running state")
	Eventually(func() string {
		out, _ := kubectl(vm, "get pods -A")
		return out
	}, 900*time.Second, 10*time.Second).ShouldNot(Or(ContainSubstring("Pending"), ContainSubstring("ContainerCreating")))
}

// Generates a valid token for provider tests
func generateToken() string {
	l := int(^uint(0) >> 1)
	return node.GenerateNewConnectionData(l).Base64()
}

// register registers a node with a qrfile
func register(loglevel, qrfile, configFile, device string, reboot bool) error {
	b, _ := os.ReadFile(configFile)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if qrfile != "" {
		fileInfo, err := os.Stat(qrfile)
		if err != nil {
			return err
		}
		if fileInfo.IsDir() {
			return fmt.Errorf("cannot register with a directory, please pass a file") //nolint:revive // This is a message printed to the user.
		}

		if !isReadable(qrfile) {
			return fmt.Errorf("cannot register with a file that is not readable") //nolint:revive // This is a message printed to the user.
		}
	}
	// dmesg -D to suppress tty ev
	fmt.Println("Sending registration payload, please wait")

	config := map[string]string{
		"device": device,
		"cc":     string(b),
	}

	if reboot {
		config["reboot"] = "true"
	}

	err := nodepair.Send(
		ctx,
		config,
		nodepair.WithReader(qr.Reader),
		nodepair.WithToken(qrfile),
		nodepair.WithLogLevel(loglevel),
	)
	if err != nil {
		return err
	}

	GinkgoLogr.Info("Registration payload successfully sent")
	return nil
}

func isReadable(fileName string) bool {
	file, err := os.Open(fileName)
	if err != nil {
		if os.IsPermission(err) {
			return false
		}
	}
	file.Close()
	return true
}

// getEfivarsFile returns the appropriate efivars file path based on the firmware being used.
// It checks if 4M firmware is being used and selects the matching VARS file.
// For 4M firmware, it tries the 4M variant first, then falls back to 2M for backward compatibility.
func getEfivarsFile(firmwarePath, assetsDir string, empty bool) (string, error) {
	// Check if we're using 4M firmware (Ubuntu 24.04+)
	// 4M CODE requires 4M VARS, while 2M CODE uses 128KB VARS
	fwInfo, err := os.Stat(firmwarePath)
	if err != nil {
		return "", fmt.Errorf("failed to stat firmware file %s: %w", firmwarePath, err)
	}

	is4M := fwInfo.Size() >= 3*1024*1024 ||
		filepath.Base(firmwarePath) == "OVMF_CODE_4M.fd" ||
		filepath.Base(firmwarePath) == "OVMF_CODE_4M.secboot.fd"

	var baseName string
	if empty {
		baseName = "efivars.empty"
	} else {
		baseName = "efivars"
	}

	var varsFile string
	if is4M {
		// Try 4M version first, fall back to 2M for backward compatibility
		varsFile = filepath.Join(assetsDir, baseName+".4m.fd")
		if _, err := os.Stat(varsFile); os.IsNotExist(err) {
			varsFile = filepath.Join(assetsDir, baseName+".fd")
		}
	} else {
		varsFile = filepath.Join(assetsDir, baseName+".fd")
	}

	GinkgoLogr.Info("reading efivars file", "file", varsFile)

	return varsFile, nil
}

func defaultVMOpts(stateDir string) []types.MachineOption {
	opts := defaultVMOptsNoDrives(stateDir)
	driveSize := getEnvOrDefault("DRIVE_SIZE", "25000")
	opts = append(opts, types.WithDriveSize(driveSize))

	return opts
}

func defaultVMOptsNoDrives(stateDir string) []types.MachineOption {
	var err error

	if os.Getenv("ISO") == "" {
		GinkgoLogr.Error(fmt.Errorf("ISO environment variable missing"), "Failed to set up configuration.")
		os.Exit(1)
	}

	var sshPort, spicePort int

	vmName := uuid.New().String()

	// Always setup a tpm emulator
	emulateTPM(stateDir)

	sshPort, err = getFreePort()
	Expect(err).ToNot(HaveOccurred())
	GinkgoLogr.Info("Got SSH port", "port", sshPort, "vm", vmName)

	memory := getEnvOrDefault("MEMORY", "2048")
	cpus := getEnvOrDefault("CPUS", "2")
	arch := getEnvOrDefault("ARCH", "x86_64")

	opts := []types.MachineOption{
		types.QEMUEngine,
		types.WithISO(os.Getenv("ISO")),
		types.WithMemory(memory),
		types.WithCPU(cpus),
		types.WithSSHPort(strconv.Itoa(sshPort)),
		types.WithID(vmName),
		types.WithSSHUser(user()),
		types.WithSSHPass(pass()),
		types.OnFailure(func(p *process.Process) {
			var serial string

			out, _ := os.ReadFile(p.StdoutPath())
			err, _ := os.ReadFile(p.StderrPath())
			status, _ := p.ExitCode()

			if serialBytes, err := os.ReadFile(path.Join(p.StateDir(), "serial.log")); err != nil {
				serial = fmt.Sprintf("Error reading serial log file: %s\n", err)
			} else {
				serial = string(serialBytes)
			}

			// We are explicitly killing the qemu process. We don't treat that as an error,
			// but we just print the output just in case.
			fmt.Printf("\nVM Aborted.\nstdout: %s\nstderr: %s\nserial: %s\nExit status: %s\n", out, err, serial, status)
			Fail(fmt.Sprintf("\nVM Aborted.\nstdout: %s\nstderr: %s\nserial: %s\nExit status: %s\n",
				out, err, serial, status))
		}),
		types.WithStateDir(stateDir),
		types.WithArch(arch),
		types.WithDataSource(os.Getenv("DATASOURCE")),
		// Set some default extra things for our VMs
		func(m *types.MachineConfig) error {
			// Serial output to file: https://superuser.com/a/1412150
			m.Args = append(m.Args,
				"-chardev", fmt.Sprintf("stdio,mux=on,id=char0,logfile=%s,signal=off", path.Join(stateDir, "serial.log")),
				"-serial", "chardev:char0",
				"-mon", "chardev=char0",
			)
			// Always set a tpm device in the vm
			m.Args = append(m.Args,
				"-chardev", fmt.Sprintf("socket,id=chrtpm,path=%s/swtpm-sock", path.Join(stateDir, "tpm")),
				"-tpmdev", "emulator,id=tpm0,chardev=chrtpm", "-device", "tpm-tis,tpmdev=tpm0",
			)

			// Set boot order to disk -> cdrom
			m.Args = append(m.Args,
				"-boot", "order=dc",
			)

			// Enable kvm
			if m.Arch == "x86_64" {
				m.Args = append(m.Args,
					"-enable-kvm",
				)
			}

			// Add second NIC with bridge networking for VM-to-VM communication.
			// This is useful when running locally where QEMU's user-mode networking (slirp)
			// doesn't allow VMs to see each other (e.g., for edgevpn discovery).
			// The first NIC (user networking with port forwarding) is kept for SSH access.
			// Enable with USE_BRIDGE_NETWORK=1 environment variable.
			if os.Getenv("USE_BRIDGE_NETWORK") != "" {
				if err := ensureBridgeNetwork(); err != nil {
					GinkgoLogr.Info("Warning: failed to ensure bridge network", "error", err)
				} else {
					mac := generateMACAddress()
					GinkgoLogr.Info("Adding bridge network interface", "mac", mac, "vm", vmName)
					m.Args = append(m.Args,
						"-nic", fmt.Sprintf("bridge,br=virbr0,model=virtio-net-pci,mac=%s", mac),
					)
				}
			}

			return nil
		},
	}

	// Now optional settings

	// If FIRMWARE is set, that usually means we are using UEFI to boot
	// This could be normal or UKI so we have a different set of efivars for each
	// UKI_TEST env var is just a flag to use empty efivars so we can test the auto enrollment
	// otherwise we need to use an efivars which contains the secureboot keys already enrolled
	// see tests/assets/efivars.md to know how to update them or regenerate them
	if os.Getenv("FIRMWARE") != "" {
		opts = append(opts, func(m *types.MachineConfig) error {
			FW := os.Getenv("FIRMWARE")
			getwd, err := os.Getwd()
			if err != nil {
				return err
			}
			m.Args = append(m.Args, "-drive",
				fmt.Sprintf("file=%s,if=pflash,format=raw,readonly=on", FW),
			)

			assetsDir := filepath.Join(getwd, "assets")
			UKI := os.Getenv("UKI_TEST")
			emptyVars := UKI != ""

			var varsFile string
			// Get the appropriate efivars file based on firmware type
			if arch == "aarch64" {
				// On aarch64 we always use the efivars-aarch64 file
				varsFile = filepath.Join(assetsDir, "efivars-aarch64.fd")
			} else {
				varsFile, err = getEfivarsFile(FW, assetsDir, emptyVars)
				if err != nil {
					return err
				}
			}

			// Copy the efivars file to state directory to not modify the original
			f, err := os.ReadFile(varsFile)
			if err != nil {
				return fmt.Errorf("failed to read efivars file %s: %w", varsFile, err)
			}

			varsPath := filepath.Join(stateDir, "efivars.fd")
			err = os.WriteFile(varsPath, f, os.ModePerm)
			if err != nil {
				return fmt.Errorf("failed to write efivars file %s: %w", varsPath, err)
			}

			m.Args = append(m.Args, "-drive",
				fmt.Sprintf("file=%s,if=pflash,format=raw", varsPath),
			)

			// Needed to be set for secureboot!
			if arch == "x86_64" {
				m.Args = append(m.Args, "-machine", "q35,smm=on")
			}

			return nil
		})
	}

	// You can connect to it with "spicy" or other tool.
	// DISPLAY is already taken on Linux X sessions
	if os.Getenv("MACHINE_SPICY") != "" {
		spicePort, _ = getFreePort()
		for spicePort == sshPort { // avoid collision
			spicePort, _ = getFreePort()
		}
		display := fmt.Sprintf("-spice port=%d,addr=127.0.0.1,disable-ticketing=yes", spicePort)
		if arch == "aarch64" {
			display += " -device pcie-root-port,port=9,chassis=10,id=pcie.9 -device virtio-gpu-pci,id=video0,max_outputs=1,bus=pcie.9,addr=0x0"
		}
		opts = append(opts, types.WithDisplay(display))

		opts = append(opts, func(m *types.MachineConfig) error {
			m.Args = append(m.Args,
				"-device", "virtio-serial-pci",
				"-chardev", fmt.Sprintf("spicevmc,id=vdagent,name=vdagent,debug=0"),
				"-device", "virtserialport,chardev=vdagent,name=com.redhat.spice.0",
			)
			return nil
		})

		cmd := exec.Command("spicy",
			"-h", "127.0.0.1",
			"-p", strconv.Itoa(spicePort))
		err = cmd.Start()
		Expect(err).ToNot(HaveOccurred())

	}

	return opts
}

func HostSSHFingerprint(vm VM) string {
	By("Getting SSH host key fingerprint")
	fp, err := vm.Sudo("cat /etc/ssh/ssh_host_*.pub 2>/dev/null | ssh-keygen -lf -")
	Expect(err).ToNot(HaveOccurred(), fp)
	Expect(fp).ToNot(BeEmpty(), "SSH host key fingerprint should not be empty")
	return fp
}

func getEnvOrDefault(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// generateMACAddress generates a random MAC address in the QEMU-compatible range (52:54:00:xx:xx:xx).
// This is used for bridge networking where each VM needs a unique MAC address.
func generateMACAddress() string {
	buf := make([]byte, 3)
	_, _ = rand.Read(buf)
	return fmt.Sprintf("52:54:00:%02x:%02x:%02x", buf[0], buf[1], buf[2])
}

// ensureBridgeNetwork ensures the libvirt default network (virbr0) is running.
// This is required for bridge networking to work with VMs.
// It also ensures the bridge is allowed in QEMU's bridge.conf.
func ensureBridgeNetwork() error {
	// Check if virbr0 exists
	cmd := exec.Command("ip", "link", "show", "virbr0")
	if err := cmd.Run(); err != nil {
		// virbr0 doesn't exist, try to start the default network
		GinkgoLogr.Info("virbr0 not found, attempting to start libvirt default network")

		// Try to start the default network
		cmd = exec.Command("sudo", "virsh", "net-start", "--network", "default")
		if out, err := cmd.CombinedOutput(); err != nil {
			// Network might not exist, try to define it first
			cmd = exec.Command("sudo", "virsh", "net-define", "/usr/share/libvirt/networks/default.xml")
			if out2, err2 := cmd.CombinedOutput(); err2 != nil {
				return fmt.Errorf("failed to define default network: %s: %w", string(out2), err2)
			}
			// Now try to start it again
			cmd = exec.Command("sudo", "virsh", "net-start", "--network", "default")
			if out, err = cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("failed to start default network: %s: %w", string(out), err)
			}
		}
	}

	// Ensure virbr0 is in the allowed list for QEMU bridge helper
	bridgeConf := "/etc/qemu/bridge.conf"
	content, err := os.ReadFile(bridgeConf)
	if err != nil || !strings.Contains(string(content), "allow virbr0") {
		GinkgoLogr.Info("Adding virbr0 to QEMU bridge helper allowed list")
		cmd := exec.Command("sudo", "sh", "-c", fmt.Sprintf("mkdir -p /etc/qemu && echo 'allow virbr0' >> %s", bridgeConf))
		if out, err := cmd.CombinedOutput(); err != nil {
			GinkgoLogr.Info("Warning: failed to update bridge.conf (may need manual setup)", "error", err, "output", string(out))
		}
	}

	return nil
}
