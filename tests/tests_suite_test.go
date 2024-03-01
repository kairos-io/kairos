package mos_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"testing"
	"time"

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

var getVersionCmd = ". /etc/os-release; [ ! -z \"$KAIROS_VERSION\" ] && echo $KAIROS_VERSION || echo $VERSION"

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
	u := os.Getenv("SSH_USER")
	if u == "" {
		u = "kairos"
	}
	return u
}

func pass() string {
	p := os.Getenv("SSH_PASS")
	if p == "" {
		p = "kairos"
	}

	return p
}

func gatherLogs(vm VM) {
	vm.Scp("assets/kubernetes_logs.sh", "/tmp/logs.sh", "0770")
	vm.Sudo("sh /tmp/logs.sh > /run/kube_logs")
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

	// zip all files under /var/log/kairos
	vm.Sudo("tar -czf /run/kairos-agent-logs.tar.gz /var/log/kairos")

	vm.GatherAllLogs(
		[]string{
			"edgevpn@kairos",
			"kairos-agent",
			"cos-setup-boot",
			"cos-setup-network",
			"cos-setup-reconcile",
			"kairos",
			"k3s",
			"k3s-agent",
		},
		[]string{
			"/var/log/edgevpn.log",
			"/var/log/kairos/agent.log",
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
			"/run/immucore/immucore.log",
			"/run/immucore/initramfs_stage.log",
			"/run/immucore/rootfs_stage.log",
			"/tmp/ovmf_debug.log",
			"/run/kairos-agent-logs.tar.gz",
		})
}

func startVM() (context.Context, VM) {
	if os.Getenv("ISO") == "" && os.Getenv("CREATE_VM") == "true" {
		fmt.Println("ISO missing")
		os.Exit(1)
	}

	var sshPort, spicePort int

	vmName := uuid.New().String()

	stateDir, err := os.MkdirTemp("", "")
	Expect(err).ToNot(HaveOccurred())
	fmt.Printf("State dir: %s\n", stateDir)

	if os.Getenv("EMULATE_TPM") != "" {
		emulateTPM(stateDir)
	}

	sshPort, err = getFreePort()
	Expect(err).ToNot(HaveOccurred())
	fmt.Printf("Using ssh port: %d\n", sshPort)

	memory := os.Getenv("MEMORY")
	if memory == "" {
		memory = "2096"
	}
	cpus := os.Getenv("CPUS")
	if cpus == "" {
		cpus = "2"
	}

	driveSize := os.Getenv("DRIVE_SIZE")
	if driveSize == "" {
		driveSize = "25000"
	}

	opts := []types.MachineOption{
		types.QEMUEngine,
		types.WithISO(os.Getenv("ISO")),
		types.WithMemory(memory),
		types.WithCPU(cpus),
		types.WithDriveSize(driveSize),
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
		// Serial output to file: https://superuser.com/a/1412150
		func(m *types.MachineConfig) error {
			m.Args = append(m.Args,
				"-chardev", fmt.Sprintf("stdio,mux=on,id=char0,logfile=%s,signal=off", path.Join(stateDir, "serial.log")),
				"-serial", "chardev:char0",
				"-mon", "chardev=char0",
			)
			if os.Getenv("EMULATE_TPM") != "" {
				m.Args = append(m.Args,
					"-chardev", fmt.Sprintf("socket,id=chrtpm,path=%s/swtpm-sock", path.Join(stateDir, "tpm")),
					"-tpmdev", "emulator,id=tpm0,chardev=chrtpm", "-device", "tpm-tis,tpmdev=tpm0",
				)
			}
			return nil
		},
		// Firmware
		func(m *types.MachineConfig) error {
			FW := os.Getenv("FIRMWARE")
			if FW != "" {
				getwd, err := os.Getwd()
				if err != nil {
					return err
				}
				m.Args = append(m.Args, "-drive",
					fmt.Sprintf("file=%s,if=pflash,format=raw,readonly=on", FW),
				)

				// Set custom vars file for efi config so we boot first from disk then from DVD with secureboot on
				UKI := os.Getenv("UKI_TEST")
				if UKI != "" {
					// On uki use an empty efivars.fd so we can test the autoenrollment
					m.Args = append(m.Args, "-drive",
						fmt.Sprintf("file=%s,if=pflash,format=raw", filepath.Join(getwd, "assets/efivars.empty.fd")),
					)
				} else {
					m.Args = append(m.Args, "-drive",
						fmt.Sprintf("file=%s,if=pflash,format=raw", filepath.Join(getwd, "assets/efivars.fd")),
					)
				}
				// Needed to be set for secureboot!
				m.Args = append(m.Args, "-machine", "q35,smm=on")
			}

			return nil
		},
		types.WithDataSource(os.Getenv("DATASOURCE")),
	}
	if os.Getenv("KVM") != "" {
		opts = append(opts, func(m *types.MachineConfig) error {
			m.Args = append(m.Args,
				"-enable-kvm",
			)
			return nil
		})
	}

	if os.Getenv("USE_QEMU") == "true" {
		opts = append(opts, types.QEMUEngine)

		// You can connect to it with "spicy" or other tool.
		// DISPLAY is already taken on Linux X sessions
		if os.Getenv("MACHINE_SPICY") != "" {
			spicePort, _ = getFreePort()
			for spicePort == sshPort { // avoid collision
				spicePort, _ = getFreePort()
			}
			display := fmt.Sprintf("-spice port=%d,addr=127.0.0.1,disable-ticketing=yes", spicePort)
			opts = append(opts, types.WithDisplay(display))

			cmd := exec.Command("spicy",
				"-h", "127.0.0.1",
				"-p", strconv.Itoa(spicePort))
			err = cmd.Start()
			Expect(err).ToNot(HaveOccurred())
		}
	} else {
		opts = append(opts, types.VBoxEngine)
	}
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
			// Our systemd unit is of type "oneoff" like it should be:
			// https://www.digitalocean.com/community/tutorials/understanding-systemd-units-and-unit-files#types-of-units
			// This makes it stay in "Active: activating (start)" state for as long
			// as it runs. The exit code of "systemctl status" on such a service is "3"
			// thus we ignore the error here.
			// https://www.freedesktop.org/software/systemd/man/systemctl.html#Exit%20status
			Eventually(func() string {
				out, _ := vm.Sudo("systemctl status kairos")

				return out
			}, 3*time.Minute, 2*time.Second).Should(
				ContainSubstring("loaded (/etc/systemd/system/kairos.service; enabled;"))
		}
	})
}

func expectStartedInstallation(vm VM) {
	By("checking that installation has started", func() {
		Eventually(func() string {
			out, _ := vm.Sudo("ps aux")
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

	fmt.Println("Payload sent, installation will start on the machine briefly")
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
