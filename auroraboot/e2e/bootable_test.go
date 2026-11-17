package e2e_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofrs/uuid"
	"github.com/kairos-io/AuroraBoot/pkg/utils"
	process "github.com/mudler/go-processmanager"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
	"github.com/spectrocloud/peg/pkg/controller"
	"github.com/spectrocloud/peg/pkg/machine"
	"github.com/spectrocloud/peg/pkg/machine/types"
)

// testVM is peg's VM plus the machine it was built from.
//
// peg keeps the machine unexported and reaches it through vm.Sudo, which runs
// `sudo /bin/sh` and feeds the command into its stdin from a goroutine of its
// own. When the SSH session dies while that write is in flight the goroutine
// panics (peg matcher/helpers.go:239), and a panic Ginkgo does not own kills
// the whole test binary: no spec failure, no AfterEach, so no logs and no
// serial console either. Holding the machine lets the suite call Command
// instead, which is session.CombinedOutput and returns the error.
type testVM struct {
	VM
	machine types.Machine
}

// RootCommand runs cmd as root over a fresh SSH session and returns an error,
// never a panic, when the VM is rebooting or already gone. cmd still runs
// under /bin/sh as root, so redirections inside it are performed by the root
// shell exactly as they are with vm.Sudo.
func (v testVM) RootCommand(cmd string) (string, error) {
	return v.machine.Command("sudo /bin/sh -c " + utils.ShellQuote(cmd))
}

// SudoOut runs cmd as root and returns its output with the lines sudo wrote
// about itself removed.
//
// vm.Sudo is peg's session.CombinedOutput, so whatever the outer sudo prints
// on stderr ends up in the same string as the output of cmd. The guest answers
// to the hostname kairos and carries no /etc/hosts entry for it, so every sudo
// looks that name up over DNS; when the lookup times out instead of being
// refused, sudo writes
//
//	sudo: unable to resolve host kairos: Temporary failure in name resolution
//
// and an assertion that compares the output to an expected value fails on a
// message about name resolution. Run 39292052264 reported
// `COS_OEM should be ext4, got "ext4\nsudo: unable to resolve host kairos:
// Temporary failure in name resolution\n"`.
//
// The two streams reach the buffer from separate pipes, so the warning can land
// before or after the output of cmd and a marker to cut at would not be enough.
// It is always a whole line of its own, which is what this drops.
//
// A failed command keeps its output whole: when sudo is the thing that went
// wrong, its own lines are the only report of it there is.
func (v testVM) SudoOut(cmd string) (string, error) {
	out, err := v.Sudo(cmd)
	if err != nil {
		return out, err
	}

	return withoutSudoDiagnostics(out), nil
}

// withoutSudoDiagnostics drops every line sudo wrote about itself and leaves
// the rest of out, including whatever cmd wrote to stderr, untouched.
func withoutSudoDiagnostics(out string) string {
	var kept []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "sudo: ") {
			continue
		}
		kept = append(kept, line)
	}

	return strings.Join(kept, "\n")
}

var _ = Describe("bootable artifacts", Label("bootable"), func() {
	var vm testVM
	var err error

	BeforeEach(func() {
		_, ok := os.Stat(os.Getenv("ISO"))
		Expect(ok).To(BeNil(), "ISO should exist")
		vm, err = startVM()
		Expect(err).ToNot(HaveOccurred())
		vm.EventuallyConnects(1200)
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			saveSerialLog(vm)
			gatherLogs(vm)
		}

		err := vm.Destroy(nil)
		Expect(err).ToNot(HaveOccurred())
	})
	It("Should boot as expected", func() {
		By("Have secureboot enabled", func() {
			output, err := vm.SudoOut("dmesg | grep -i secure")
			Expect(err).ToNot(HaveOccurred(), output)
			Expect(output).To(ContainSubstring("Secure boot enabled"))
		})

		By("Have our custom keys", func() {
			output, err := vm.SudoOut("kairos-agent state get \"kairos.eficerts|tojson\"")
			Expect(err).ToNot(HaveOccurred(), output)
			// Check the test keys we created for this
			Expect(output).To(ContainSubstring("Kairos DB"))
			Expect(output).To(ContainSubstring("Kairos KEK"))
			Expect(output).To(ContainSubstring("Kairos PK"))
		})
	})
})

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

	// qemu exits at once when the socket is not there yet, and a VM started
	// right after this returns can beat swtpm to it.
	Eventually(func() error {
		_, err := os.Stat(path.Join(t, "swtpm-sock"))
		return err
	}, "30s", "100ms").Should(Succeed(), "swtpm creating its socket in %s", t)
}

// stopTPM ends the swtpm process emulateTPM started for stateDir. swtpm keeps
// running when qemu is killed or never connects, so a VM's teardown has to
// stop it, and before the state directory holding its pid file is removed.
func stopTPM(stateDir string) {
	pid, err := os.ReadFile(path.Join(stateDir, "tpm", "pid"))
	if err != nil {
		return
	}
	p, err := strconv.Atoi(strings.TrimSpace(string(pid)))
	if err != nil {
		return
	}
	if proc, err := os.FindProcess(p); err == nil {
		_ = proc.Kill()
		_, _ = proc.Wait()
	}
}

// vmSpec names what a VM boots from and how big it is. Empty fields fall
// back to the MEMORY and CPUS variables and to their current defaults.
type vmSpec struct {
	ISO      string
	RawImage string
	Memory   string
	CPUs     string
}

func startVM() (testVM, error) {
	stateDir, err := os.MkdirTemp("", "")
	Expect(err).ToNot(HaveOccurred())
	fmt.Printf("State dir: %s\n", stateDir)

	return startVMIn(stateDir, defaultVMOpts(stateDir))
}

// startVMWith starts a VM booting spec, with a 25000 MB drive.
func startVMWith(spec vmSpec) (testVM, error) {
	stateDir, err := os.MkdirTemp("", "")
	Expect(err).ToNot(HaveOccurred())
	fmt.Printf("State dir: %s\n", stateDir)

	return startVMIn(stateDir, append(vmOpts(stateDir, spec), types.WithDriveSize("25000")))
}

// startVMIn starts the swtpm the options point qemu at, then the VM. On a
// failed start it stops the swtpm, which nothing else would, and still returns
// the VM: the caller saves its serial log from the state directory and
// destroys it like a VM that started.
func startVMIn(stateDir string, opts []types.MachineOption) (testVM, error) {
	m, err := machine.New(opts...)
	Expect(err).ToNot(HaveOccurred())

	emulateTPM(stateDir)
	vm := NewVM(m, stateDir)
	if _, err = vm.Start(context.Background()); err != nil {
		stopTPM(stateDir)
	}
	return testVM{VM: vm, machine: m}, err
}

func defaultVMOpts(stateDir string) []types.MachineOption {
	return append(defaultVMOptsNoDrives(stateDir), types.WithDriveSize("25000"))
}

func defaultVMOptsNoDrives(stateDir string) []types.MachineOption {
	if os.Getenv("ISO") == "" && os.Getenv("RAW_IMAGE") == "" {
		fmt.Println("ISO or RAW_IMAGE missing")
		os.Exit(1)
	}

	return vmOpts(stateDir, vmSpec{ISO: os.Getenv("ISO"), RawImage: os.Getenv("RAW_IMAGE")})
}

// vmOpts builds the machine options for spec without reading ISO or RAW_IMAGE.
func vmOpts(stateDir string, spec vmSpec) []types.MachineOption {
	var err error

	var sshPort, spicePort int

	uid, _ := uuid.NewV4()
	vmName := uid.String()

	sshPort, err = getFreePort()
	Expect(err).ToNot(HaveOccurred())
	fmt.Printf("Using ssh port: %d\n", sshPort)

	memory := spec.Memory
	if memory == "" {
		memory = os.Getenv("MEMORY")
	}
	if memory == "" {
		memory = "6000"
	}
	cpus := spec.CPUs
	if cpus == "" {
		cpus = os.Getenv("CPUS")
	}
	if cpus == "" {
		cpus = "4"
	}

	opts := []types.MachineOption{
		types.QEMUEngine,
		types.WithMemory(memory),
		types.WithCPU(cpus),
		types.WithSSHPort(strconv.Itoa(sshPort)),
		types.WithID(vmName),
		types.WithSSHUser("kairos"),
		types.WithSSHPass("kairos"),
		types.OnFailure(func(p *process.Process) {
			// peg calls this from the goroutine it starts in
			// machine.monitor, not from a Ginkgo node, so the Fail below
			// needs a GinkgoRecover to be turned into a spec failure
			// instead of an unrecovered panic.
			defer GinkgoRecover()

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
			m.Args = append(m.Args,
				"-chardev", fmt.Sprintf("socket,id=chrtpm,path=%s/swtpm-sock", path.Join(stateDir, "tpm")),
				"-tpmdev", "emulator,id=tpm0,chardev=chrtpm", "-device", "tpm-tis,tpmdev=tpm0",
			)
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

				assetsDir := filepath.Join(getwd, "assets")
				emptyVars := os.Getenv("EFIVARS_EMPTY") == "true"

				// Get the appropriate efivars file based on firmware type
				varsFile, err := getEfivarsFile(FW, assetsDir, emptyVars)
				if err != nil {
					return err
				}

				// Copy the efivars file to state directory to not modify the original
				f, err := os.ReadFile(varsFile)
				if err != nil {
					return fmt.Errorf("failed to read efivars file %s: %w", varsFile, err)
				}

				var varsPath string
				if emptyVars {
					varsPath = filepath.Join(stateDir, "efivars.empty.fd")
				} else {
					varsPath = filepath.Join(stateDir, "efivars.fd")
				}

				err = os.WriteFile(varsPath, f, os.ModePerm)
				if err != nil {
					return fmt.Errorf("failed to write efivars file %s: %w", varsPath, err)
				}

				m.Args = append(m.Args, "-drive",
					fmt.Sprintf("file=%s,if=pflash,format=raw", varsPath),
				)

				// Needed to be set for secureboot!
				m.Args = append(m.Args, "-machine", "q35,smm=on")
			}

			return nil
		},
		types.WithDataSource(os.Getenv("DATASOURCE")),
	}
	if spec.ISO != "" {
		opts = append(opts, types.WithISO(spec.ISO))
	}
	if spec.RawImage != "" {
		opts = append(opts, types.WithDrive(spec.RawImage))
	}
	if os.Getenv("KVM") != "" {
		opts = append(opts, func(m *types.MachineConfig) error {
			m.Args = append(m.Args,
				"-enable-kvm",
			)
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
		opts = append(opts, types.WithDisplay(display))

		cmd := exec.Command("spicy",
			"-h", "127.0.0.1",
			"-p", strconv.Itoa(spicePort))
		err = cmd.Start()
		Expect(err).ToNot(HaveOccurred())
	}

	return opts
}

// getFreePort returns a TCP port that is free on every interface, so it also
// suits a server that listens on all of them.
func getFreePort() (port int, err error) {
	var a *net.TCPAddr
	if a, err = net.ResolveTCPAddr("tcp", ":0"); err == nil {
		var l *net.TCPListener
		if l, err = net.ListenTCP("tcp", a); err == nil {
			defer l.Close()
			return l.Addr().(*net.TCPAddr).Port, nil
		}
	}
	return
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

	is4M := fwInfo.Size() >= 4*1024*1024 ||
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

	return varsFile, nil
}

// vmLogDir is where saveSerialLog and gatherLogs write for vm:
// <base>/<vm ID>, with <base> from VM_LOG_DIR or "logs". The ID keeps two VMs
// of one spec from overwriting each other's files.
func vmLogDir(vm testVM) string {
	base := os.Getenv("VM_LOG_DIR")
	if base == "" {
		base = "logs"
	}
	return filepath.Join(base, vm.machine.Config().ID)
}

// saveSerialLog copies the qemu serial console into vmLogDir(vm) and prints it.
//
// qemu writes that file on the host, so it is readable even when the VM is
// gone, and it is the only log that is. Everything else has to be fetched over
// SSH, so this one comes first.
func saveSerialLog(vm testVM) {
	serial, _ := os.ReadFile(filepath.Join(vm.StateDir, "serial.log"))
	dir := vmLogDir(vm)
	_ = os.MkdirAll(dir, os.ModePerm|os.ModeDir)
	_ = os.WriteFile(filepath.Join(dir, "serial.log"), serial, os.ModePerm)
	fmt.Println(string(serial))
}

// gatherLogs collects what a failed spec left on the VM.
//
// It runs on a machine that has just failed, so an unreachable or rebooting VM
// is the normal case here and not an edge case. Every command goes through
// RootCommand for that reason: vm.Sudo would take the whole suite down with a
// panic before the caller gets to read serial.log, which is the one piece of
// evidence that does not need the VM to be alive.
func gatherLogs(vm testVM) {
	vm.Scp("assets/kubernetes_logs.sh", "/tmp/logs.sh", "0770")
	vm.RootCommand("sh /tmp/logs.sh > /run/kube_logs")
	vm.RootCommand("cat /oem/* > /run/oem.yaml")
	vm.RootCommand("cat /etc/resolv.conf > /run/resolv.conf")
	vm.RootCommand("k3s kubectl get pods -A -o json > /run/pods.json")
	vm.RootCommand("k3s kubectl get events -A -o json > /run/events.json")
	vm.RootCommand("cat /proc/cmdline > /run/cmdline")
	vm.RootCommand("chmod 777 /run/events.json")

	vm.RootCommand("df -h > /run/disk")
	vm.RootCommand("mount > /run/mounts")
	vm.RootCommand("blkid > /run/blkid")
	vm.RootCommand("dmesg > /run/dmesg.log")

	// zip all files under /var/log/kairos
	vm.RootCommand("tar -czf /run/kairos-agent-logs.tar.gz /var/log/kairos")

	gatherAllLogs(vmLogSink(vm), vmLogDir(vm),
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

// logSink is everything gatherAllLogs needs from a VM: run a command as root,
// and copy one file off the machine. Both report failure as an error, which is
// the whole point of the type, and it lets the plan be tested without a VM.
type logSink struct {
	run   func(cmd string) (string, error)
	fetch func(remotePath, localPath string) error
}

// vmLogSink binds a logSink to a running machine.
//
// peg's own vm.GatherAllLogs cannot be used here. It routes every command
// through machineSudo, which runs `sudo /bin/sh` and feeds the command into its
// stdin from a goroutine of its own; when the SSH session has died that write
// fails and the goroutine panics (peg matcher/helpers.go:239). A failed spec is
// precisely when the VM is gone or rebooting, so that is the common case, and a
// panic Ginkgo does not own ends the test binary on the spot: the files the
// steps above just wrote on the VM are never copied off it, and no later spec
// runs. machine.Command and the SCP client both return errors instead.
func vmLogSink(vm testVM) logSink {
	return logSink{
		run: vm.RootCommand,
		fetch: func(remotePath, localPath string) error {
			f, err := os.Create(localPath)
			if err != nil {
				return err
			}
			defer f.Close()

			client := controller.NewSCPClient(vm.machine)
			if err := client.Connect(); err != nil {
				return err
			}
			defer client.Close()

			// A VM that answered the chmod can still stall mid-transfer, and
			// this runs inside AfterEach: without a deadline one wedged file
			// would hold the whole suite until the job timeout.
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			return client.CopyFromRemote(ctx, f, remotePath)
		},
	}
}

// gatherAllLogs collects the services and files peg's GatherAllLogs collects,
// into outDir, without ever taking the process down with it. Every step is
// best effort: a VM that cannot answer costs that one file, not the run.
func gatherAllLogs(s logSink, outDir string, services, logFiles []string) {
	for _, service := range services {
		path := fmt.Sprintf("/run/%s.log", service)
		runForLog(s, fmt.Sprintf("journalctl -u %s -o short-iso >> %s", service, path))
		gatherLog(s, outDir, path)
	}

	for _, file := range logFiles {
		gatherLog(s, outDir, file)
	}

	for _, extra := range []struct {
		cmds []string
		path string
	}{
		{[]string{"dmesg > /run/dmesg"}, "/run/dmesg"},
		{[]string{"journalctl -o short-iso > /run/journal.log"}, "/run/journal.log"},
		{[]string{"uname -a > /run/uname.log"}, "/run/uname.log"},
		{[]string{"lsblk -a >> /run/disks.log", "blkid >> /run/disks.log"}, "/run/disks.log"},
	} {
		for _, cmd := range extra.cmds {
			runForLog(s, cmd)
		}
		gatherLog(s, outDir, extra.path)
	}

	gatherLog(s, outDir, "/etc/passwd")
	gatherLog(s, outDir, "/etc/os-release")
}

// runForLog runs a command whose only job is to produce a log on the VM.
func runForLog(s logSink, cmd string) {
	if out, err := s.run(cmd); err != nil {
		fmt.Printf("Could not run %q on the VM: %s\nOutput: %s\n", cmd, err, out)
	}
}

// gatherLog copies one file off the VM into outDir, under its base name.
func gatherLog(s logSink, outDir, remotePath string) {
	if out, err := s.run("chmod 777 " + remotePath); err != nil {
		fmt.Printf("Could not chmod %s on the VM: %s\nOutput: %s\n", remotePath, err, out)
		return
	}

	fmt.Printf("Trying to get file: %s\n", remotePath)
	if err := os.MkdirAll(outDir, 0755); err != nil {
		fmt.Printf("Could not create %s: %s\n", outDir, err)
		return
	}

	if err := s.fetch(remotePath, filepath.Join(outDir, filepath.Base(remotePath))); err != nil {
		fmt.Printf("Could not copy %s off the VM: %s\n", remotePath, err)
	}
}
