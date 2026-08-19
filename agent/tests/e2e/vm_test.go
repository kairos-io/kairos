//go:build e2e

package e2e_test

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"

	"github.com/gofrs/uuid"
	process "github.com/mudler/go-processmanager"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
	"github.com/spectrocloud/peg/pkg/machine"
	"github.com/spectrocloud/peg/pkg/machine/types"
)

// kvmAvailable reports whether /dev/kvm is present and usable. When true, the
// VM is launched with hardware acceleration; boot times drop from minutes
// (TCG) to seconds. CI runners expose /dev/kvm after the "Enable KVM" step.
func kvmAvailable() bool {
	if os.Getenv("KAIROS_E2E_NO_KVM") != "" {
		return false
	}
	if _, err := os.Stat("/dev/kvm"); err != nil {
		return false
	}
	f, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// startVM boots the ISO at $ISO in QEMU (user-mode networking) with a single
// blank 20G drive as the install target, and waits for SSH. Uses KVM when
// available; falls back to TCG otherwise.
func startVM() VM {
	iso := os.Getenv("ISO")
	Expect(iso).ToNot(BeEmpty(), "ISO env var must point to the test ISO")
	_, err := os.Stat(iso)
	Expect(err).ToNot(HaveOccurred(), "ISO file must exist")

	stateDir, err := os.MkdirTemp("", "kairos-e2e-")
	Expect(err).ToNot(HaveOccurred())
	fmt.Printf("State dir: %s\n", stateDir)

	sshPort, err := freePort()
	Expect(err).ToNot(HaveOccurred())

	uid, _ := uuid.NewV4()

	memory := os.Getenv("MEMORY")
	if memory == "" {
		// 2GB is enough for the live ISO + install; keeps the whole suite
		// under the 6GB CI budget even if we later run two VMs in parallel.
		memory = "2048"
	}
	cpus := os.Getenv("CPUS")
	if cpus == "" {
		cpus = "2"
	}

	useKVM := kvmAvailable()
	if useKVM {
		fmt.Println("KVM acceleration: enabled")
	} else {
		fmt.Println("KVM acceleration: disabled (TCG fallback — slow)")
	}

	opts := []types.MachineOption{
		types.QEMUEngine,
		types.WithISO(iso),
		types.WithMemory(memory),
		types.WithCPU(cpus),
		types.WithSSHPort(strconv.Itoa(sshPort)),
		types.WithID(uid.String()),
		types.WithSSHUser("kairos"),
		types.WithSSHPass("kairos"),
		types.WithStateDir(stateDir),
		types.WithDriveSize("20000"),
		types.OnFailure(func(p *process.Process) {
			out, _ := os.ReadFile(p.StdoutPath())
			errOut, _ := os.ReadFile(p.StderrPath())
			serial, _ := os.ReadFile(path.Join(p.StateDir(), "serial.log"))
			status, _ := p.ExitCode()
			Fail(fmt.Sprintf("VM aborted.\nstdout: %s\nstderr: %s\nserial: %s\nexit: %s\n",
				out, errOut, serial, status))
		}),
		func(m *types.MachineConfig) error {
			m.Args = append(m.Args,
				"-chardev", fmt.Sprintf("stdio,mux=on,id=char0,logfile=%s,signal=off", path.Join(stateDir, "serial.log")),
				"-serial", "chardev:char0",
				"-mon", "chardev=char0",
			)
			if useKVM {
				// -cpu host: expose the physical CPU model to the guest so
				// KVM does not need to emulate feature gaps.
				m.Args = append(m.Args, "-enable-kvm", "-cpu", "host")
			}
			return nil
		},
	}

	m, err := machine.New(opts...)
	Expect(err).ToNot(HaveOccurred())

	vm := NewVM(m, stateDir)
	_, err = vm.Start(context.Background())
	Expect(err).ToNot(HaveOccurred())
	return vm
}

// sshTimeout returns the seconds to wait for the guest SSH to come up. With
// KVM a live ISO usually boots in well under a minute; without KVM keep the
// old generous budget so local runs without acceleration still pass.
func sshTimeout() int {
	if v := os.Getenv("SSH_TIMEOUT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	if kvmAvailable() {
		return 240
	}
	return 1200
}

// dumpSerial writes the VM serial log into ./logs on failure.
func dumpSerial(vm VM) {
	serial, _ := os.ReadFile(filepath.Join(vm.StateDir, "serial.log"))
	_ = os.MkdirAll("logs", os.ModePerm|os.ModeDir)
	_ = os.WriteFile(filepath.Join("logs", "serial.log"), serial, os.ModePerm)
	fmt.Println(string(serial))
}
