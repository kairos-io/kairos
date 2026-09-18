package validation_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mudler/yip/pkg/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

// vmwareDaemons are the two names that start VMware's guest daemon. Neither
// belongs on a VirtualBox guest: the daemon talks to the VMware backdoor, and
// open-vm-tools.service carries ConditionVirtualization=vmware, so on systemd
// it is skipped and on OpenRC it starts and fails.
var vmwareDaemons = []string{"open-vm-tools", "vmtoolsd"}

// dmi writes a product_name and sys_vendor pair and rewrites a stage guard to
// read them, so the guard is evaluated the way yip evaluates it, through sh.
func dmi(guard, sysVendor, productName string) *exec.Cmd {
	dir := GinkgoT().TempDir()
	sysVendorPath := filepath.Join(dir, "sys_vendor")
	productNamePath := filepath.Join(dir, "product_name")
	Expect(os.WriteFile(sysVendorPath, []byte(sysVendor+"\n"), 0o600)).To(Succeed())
	Expect(os.WriteFile(productNamePath, []byte(productName+"\n"), 0o600)).To(Succeed())

	command := strings.ReplaceAll(guard, "/sys/class/dmi/id/sys_vendor", sysVendorPath)
	command = strings.ReplaceAll(command, "/sys/class/dmi/id/product_name", productNamePath)
	return exec.Command("sh", "-c", command)
}

var _ = Describe("Bundled VM cloudconfig hypervisor stages", func() {
	var bootStages []schema.Stage

	BeforeEach(func() {
		content, err := os.ReadFile(filepath.Join(cloudConfigsDir, "26_vm.yaml"))
		Expect(err).NotTo(HaveOccurred())

		var config schema.YipConfig
		Expect(yaml.Unmarshal(content, &config)).To(Succeed())

		bootStages = config.Stages["boot"]
		Expect(bootStages).NotTo(BeEmpty())
	})

	// matching returns the commands of every boot stage whose guard passes for
	// the given DMI pair.
	matching := func(sysVendor, productName string) []string {
		var commands []string
		for _, stage := range bootStages {
			if stage.If == "" {
				continue
			}
			if err := dmi(stage.If, sysVendor, productName).Run(); err == nil {
				commands = append(commands, stage.Commands...)
			}
		}
		return commands
	}

	It("starts no VMware daemon on a VirtualBox guest", func() {
		commands := matching("innotek GmbH", "VirtualBox")
		for _, command := range commands {
			for _, daemon := range vmwareDaemons {
				Expect(command).NotTo(ContainSubstring(daemon),
					"a VirtualBox guest must not be told to start VMware's guest daemon")
			}
		}
	})

	It("still starts the VMware daemon on a VMware guest", func() {
		commands := matching("VMware, Inc.", "VMware Virtual Platform")
		Expect(commands).To(ConsistOf(
			"rc-service open-vm-tools start",
			"systemctl start vmtoolsd",
		))
	})

	It("still starts the guest agent on a QEMU guest", func() {
		commands := matching("QEMU", "Standard PC (Q35 + ICH9, 2009)")
		Expect(commands).To(ConsistOf(
			"rc-service qemu-guest-agent start",
			"systemctl start qemu-guest-agent",
		))
	})
})
