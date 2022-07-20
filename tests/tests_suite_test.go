package mos_test

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/c3os-io/c3os/internal/utils"
	"github.com/c3os-io/c3os/tests/machine"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "c3os Test Suite")
}

var tempDir string
var sshPort string

var machineID string = os.Getenv("MACHINE_ID")

var _ = AfterSuite(func() {
	if os.Getenv("CREATE_VM") == "true" {
		machine.Delete()
		if machine.SUT != nil {
			m := &machine.QEMU{}
			m.Stop(machine.SUT)
			m.Clean(machine.SUT)
		}
	}
})

var _ = BeforeSuite(func() {

	if os.Getenv("CLOUD_INIT") == "" || !filepath.IsAbs(os.Getenv("CLOUD_INIT")) {
		Fail("CLOUD_INIT must be set and must be pointing to a file as an absolute path")
	}

	if machineID == "" {
		machineID = "testvm"
	}

	if os.Getenv("ISO") == "" && os.Getenv("CREATE_VM") == "true" {
		fmt.Println("ISO missing")
		os.Exit(1)
	}

	if os.Getenv("CREATE_VM") == "true" {
		t, err := ioutil.TempDir("", "")
		Expect(err).ToNot(HaveOccurred())
		tempDir = t

		machine.ID = machineID
		machine.TempDir = t

		sshPort = "2222"

		if os.Getenv("SSH_PORT") != "" {
			sshPort = os.Getenv("SSH_PORT")
		}

		prepareVM()
	}
})

func download(s string) {
	f2, err := ioutil.TempFile("", "fff")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(f2.Name())

	resp, err := http.Get(s)
	Expect(err).ToNot(HaveOccurred())

	defer resp.Body.Close()
	_, err = io.Copy(f2, resp.Body)
	Expect(err).ToNot(HaveOccurred())

	out, err := utils.SH("tar xvf " + f2.Name())
	fmt.Println(out)
	Expect(err).ToNot(HaveOccurred(), out)
}

func prepareVM() {
	if os.Getenv("CREATE_VM") == "true" {
		machine.Delete()
		machine.Create(sshPort)
	}
}

func gatherLogs() {
	machine.Sudo("k3s kubectl get pods -A -o json > /run/pods.json")
	machine.Sudo("k3s kubectl get events -A -o json > /run/events.json")
	machine.Sudo("cat /proc/cmdline > /run/cmdline")
	machine.Sudo("chmod 777 /run/events.json")

	machine.Sudo("df -h > /run/disk")
	machine.Sudo("mount > /run/mounts")
	machine.Sudo("blkid > /run/blkid")

	machine.GatherAllLogs(
		[]string{
			"edgevpn@c3os",
			"c3os-agent",
			"cos-setup-boot",
			"cos-setup-network",
			"c3os",
			"k3s",
		},
		[]string{
			"/var/log/edgevpn.log",
			"/var/log/c3os-agent.log",
			"/run/pods.json",
			"/run/disk",
			"/run/mounts",
			"/run/blkid",
			"/run/events.json",
			"/run/cmdline",
		})
}
