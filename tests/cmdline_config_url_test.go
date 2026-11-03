package mos_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	diskfs "github.com/diskfs/go-diskfs"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
	"github.com/spectrocloud/peg/pkg/machine"
	"github.com/spectrocloud/peg/pkg/machine/types"
)

// Kernel arguments that are not Kairos configuration. On their own they must
// not cost the kairos.config_url next to them: `8250` is a dotted key whose
// segment is a number, `a..b` has an empty segment and is skipped.
const foreignCmdlineArgs = "8250.nr_uarts=4 a..b=1"

const configURLInstallConfig = `#cloud-config
install:
  auto: true
  device: auto
  reboot: false
  poweroff: false
users:
- name: kairos
  passwd: kairos
  groups:
  - admin
`

var _ = Describe("kairos config_url on a live cmdline with foreign kernel arguments", Label("cmdline-config-url"), func() {
	var vm VM

	BeforeEach(func() {
		// The guest reaches the host as 10.0.2.2 over QEMU user networking.
		ln, err := net.Listen("tcp", "0.0.0.0:0")
		Expect(err).ToNot(HaveOccurred())
		srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, configURLInstallConfig)
		})}
		go func() { _ = srv.Serve(ln) }()
		DeferCleanup(srv.Close)

		stateDir, err := os.MkdirTemp("", "")
		Expect(err).ToNot(HaveOccurred())

		kernel, initrd, liveArgs := liveBootFiles(os.Getenv("ISO"), stateDir)
		cmdline := fmt.Sprintf("%s %s kairos.config_url=http://10.0.2.2:%d/config.yaml",
			liveArgs, foreignCmdlineArgs, ln.Addr().(*net.TCPAddr).Port)

		// The live media's own boot menu has no way to add kernel arguments
		// unattended, so its kernel and initrd are booted directly.
		opts := append(defaultVMOptsNoTPM(stateDir), func(m *types.MachineConfig) error {
			m.Args = append(m.Args, "-kernel", kernel, "-initrd", initrd, "-append", cmdline)
			return nil
		})
		m, err := machine.New(opts...)
		Expect(err).ToNot(HaveOccurred())
		vm = NewVM(m, stateDir)
		_, err = vm.Start(context.Background())
		Expect(err).ToNot(HaveOccurred())
		vm.EventuallyConnects(1200)
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			gatherLogs(vm)
		}
		Expect(vm.Destroy(nil)).To(Succeed())
	})

	It("installs from the remote config", func() {
		out, err := vm.Sudo("cat /proc/cmdline")
		Expect(err).ToNot(HaveOccurred(), out)
		Expect(out).To(ContainSubstring(foreignCmdlineArgs))

		// The live entry point runs as "kairos-agent interactive-install"
		// with or without a config, so the process list cannot tell an
		// unattended install apart from a prompt. Partitions on the disk can.
		Eventually(func() string {
			out, _ := vm.Sudo("lsblk --output LABEL")
			return out
		}, 15*time.Minute, 5*time.Second).Should(ContainSubstring("COS_OEM"))
	})
})

// liveBootFiles copies the live kernel and initrd out of iso into dir and
// returns them with the arguments of the media's default (install) entry.
func liveBootFiles(iso, dir string) (kernel, initrd, args string) {
	d, err := diskfs.Open(iso, diskfs.WithOpenMode(diskfs.ReadOnly))
	Expect(err).ToNot(HaveOccurred())
	defer d.Close()
	fs, err := d.GetFilesystem(0)
	Expect(err).ToNot(HaveOccurred())

	read := func(p string) []byte {
		f, err := fs.OpenFile(p, os.O_RDONLY)
		Expect(err).ToNot(HaveOccurred(), p)
		b, err := io.ReadAll(f)
		Expect(err).ToNot(HaveOccurred(), p)
		return b
	}

	kernel = filepath.Join(dir, "kernel")
	initrd = filepath.Join(dir, "initrd")
	Expect(os.WriteFile(kernel, read("/boot/kernel"), 0o644)).To(Succeed())
	Expect(os.WriteFile(initrd, read("/boot/initrd"), 0o644)).To(Succeed())

	for _, line := range strings.Split(string(read("/boot/grub2/grub.cfg")), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "linux" || !strings.HasSuffix(fields[1], "/boot/kernel") {
			continue
		}
		var kept []string
		for _, f := range fields[2:] {
			// grub variables such as $thor_options are empty on a VM
			if !strings.HasPrefix(f, "$") {
				kept = append(kept, f)
			}
		}
		return kernel, initrd, strings.Join(kept, " ")
	}
	Fail("no linux line for /boot/kernel in the live grub.cfg")
	return
}
