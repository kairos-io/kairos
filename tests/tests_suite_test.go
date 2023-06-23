package mos_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	process "github.com/mudler/go-processmanager"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
	machine "github.com/spectrocloud/peg/pkg/machine"
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
	vm.Sudo("k3s kubectl get pods -A -o json > /run/pods.json")
	vm.Sudo("k3s kubectl get events -A -o json > /run/events.json")
	vm.Sudo("cat /proc/cmdline > /run/cmdline")
	vm.Sudo("chmod 777 /run/events.json")

	vm.Sudo("df -h > /run/disk")
	vm.Sudo("mount > /run/mounts")
	vm.Sudo("blkid > /run/blkid")

	vm.GatherAllLogs(
		[]string{
			"edgevpn@kairos",
			"kairos-agent",
			"cos-setup-boot",
			"cos-setup-network",
			"kairos",
			"k3s",
		},
		[]string{
			"/var/log/edgevpn.log",
			"/var/log/kairos/agent.log",
			"/run/pods.json",
			"/run/disk",
			"/run/mounts",
			"/run/blkid",
			"/run/events.json",
			"/run/cmdline",
			"/run/immucore/immucore.log",
			"/run/immucore/initramfs_stage.log",
			"/run/immucore/rootfs_stage.log",
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

func isFlavor(flavor string) bool {
	return strings.Contains(os.Getenv("FLAVOR"), flavor)
}

func expectDefaultService(vm VM) {
	By("checking if default service is active in live cd mode", func() {
		if isFlavor("alpine") {
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
