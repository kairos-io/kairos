package mos_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

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

func detachAndReboot() {
	vbox, ok := Machine.(*machine.VBox)
	if ok {
		vbox.DetachCD()
		vbox.Restart()
	} else {
		Reboot()
	}
}

var tempDir string
var sshPort string

var machineID string = os.Getenv("MACHINE_ID")

var _ = AfterSuite(func() {
	if os.Getenv("CREATE_VM") == "true" {
		if Machine != nil {
			Machine.Stop()
			Machine.Clean()
		}
	}
	if !CurrentSpecReport().Failure.IsZero() {
		gatherLogs()
	}
})

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

var _ = BeforeSuite(func() {

	if machineID == "" {
		machineID = "testvm"
	}

	if os.Getenv("ISO") == "" && os.Getenv("CREATE_VM") == "true" {
		fmt.Println("ISO missing")
		os.Exit(1)
	}

	if os.Getenv("CREATE_VM") == "true" {
		t, err := os.MkdirTemp("", "")
		fmt.Printf("State dir = %+v\n", t)
		Expect(err).ToNot(HaveOccurred())

		sshPort = "2222"
		if os.Getenv("SSH_PORT") != "" {
			sshPort = os.Getenv("SSH_PORT")
		}

		opts := []types.MachineOption{
			types.WithISO(os.Getenv("ISO")),
			types.WithSSHPort(sshPort),
			types.WithID(machineID),
			types.WithCPUType("Cascadelake-Server-v4"),
			types.WithSSHUser(user()),
			types.WithSSHPass(pass()),
			types.OnFailure(func(p *process.Process) {
				defer GinkgoRecover()
				out, _ := os.ReadFile(p.StdoutPath())
				err, _ := os.ReadFile(p.StderrPath())
				status, _ := p.ExitCode()
				// Ginkgo doesn't print?
				fmt.Printf("VM Aborted: %s %s Exit status: %s", out, err, status)
				Fail(fmt.Sprintf("VM Aborted: %s %s Exit status: %s", out, err, status))
			}),
			types.WithStateDir(t),
			types.WithDataSource(os.Getenv("DATASOURCE")),
		}

		if os.Getenv("USE_QEMU") == "true" {
			opts = append(opts, types.QEMUEngine)
		} else {
			opts = append(opts, types.VBoxEngine)
		}

		m, err := machine.New(opts...)
		if err != nil {
			Fail(err.Error())
		}

		Machine = m

		if err := Machine.Create(context.Background()); err != nil {
			Fail(err.Error())
		}
	}
})

func gatherLogs() {
	Sudo("k3s kubectl get pods -A -o json > /run/pods.json")
	Sudo("k3s kubectl get events -A -o json > /run/events.json")
	Sudo("cat /proc/cmdline > /run/cmdline")
	Sudo("chmod 777 /run/events.json")

	Sudo("df -h > /run/disk")
	Sudo("mount > /run/mounts")
	Sudo("blkid > /run/blkid")

	GatherAllLogs(
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
		})
}

func isFlavor(flavor string) bool {
	return strings.Contains(os.Getenv("FLAVOR"), flavor)
}
