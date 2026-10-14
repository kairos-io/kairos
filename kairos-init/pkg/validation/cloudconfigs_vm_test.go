package validation_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// guestAgentChannel is the virtio port qemu-ga talks over. udev symlinks it
// from SUBSYSTEM=="virtio-ports", and qemu-guest-agent.service is BindTo= and
// After= the matching device unit, so starting the service without the channel
// blocks for DefaultTimeoutStartSec and then fails.
const guestAgentChannel = "/dev/virtio-ports/org.qemu.guest_agent.0"

var qemuConditionRe = regexp.MustCompile(`(?m)^\s+\(grep -qiFx "QEMU" /sys/class/dmi/id/sys_vendor 2>/dev/null \|\|\n\s+grep -qiE "qemu\|kvm\|Virtual Machine" /sys/class/dmi/id/product_name 2>/dev/null\) &&\n\s+test -e ` + regexp.QuoteMeta(guestAgentChannel) + `$`)

var _ = Describe("Bundled VM cloudconfig", func() {
	It("starts the QEMU guest agent only on a VM that exposes its channel", func() {
		content, err := os.ReadFile(filepath.Join("..", "bundled", "cloudconfigs", "26_vm.yaml"))
		Expect(err).NotTo(HaveOccurred())

		conditions := qemuConditionRe.FindAllString(string(content), -1)
		Expect(conditions).To(HaveLen(2), "systemd and OpenRC must use the same QEMU detector")

		tests := []struct {
			name        string
			sysVendor   string
			productName string
			channel     bool
			matched     bool
		}{
			{name: "QEMU Q35", sysVendor: "QEMU", productName: "Standard PC (Q35 + ICH9, 2009)", channel: true, matched: true},
			{name: "QEMU i440FX", sysVendor: "qemu", productName: "Standard PC (i440FX + PIIX, 1996)", channel: true, matched: true},
			{name: "KVM product", sysVendor: "Red Hat", productName: "KVM", channel: true, matched: true},
			{name: "libvirt QEMU product", sysVendor: "Red Hat", productName: "QEMU Virtual Machine", channel: true, matched: true},
			{name: "Microsoft Azure", sysVendor: "Microsoft Corporation", productName: "Virtual Machine", channel: true, matched: true},
			{name: "bare metal", sysVendor: "Dell Inc.", productName: "PowerEdge R740", matched: false},
			{name: "generic standard PC", sysVendor: "American Megatrends Inc.", productName: "Standard PC", matched: false},
			{name: "missing DMI files", matched: false},
			// The boot this fixes: a QEMU guest the hypervisor started without
			// a guest agent channel. Before the guard the stage ran
			// `systemctl start qemu-guest-agent` here and held the boot for 90s.
			{name: "QEMU Q35 with no guest agent channel", sysVendor: "QEMU", productName: "Standard PC (Q35 + ICH9, 2009)", channel: false, matched: false},
			{name: "KVM with no guest agent channel", sysVendor: "Red Hat", productName: "KVM", channel: false, matched: false},
			// A channel on hardware that does not say it is a VM is still not
			// a QEMU guest, so the detector must not fire on the channel alone.
			{name: "bare metal with a stray channel", sysVendor: "Dell Inc.", productName: "PowerEdge R740", channel: true, matched: false},
		}

		for _, condition := range conditions {
			for _, test := range tests {
				tmpDir := GinkgoT().TempDir()
				sysVendorPath := filepath.Join(tmpDir, "sys_vendor")
				productNamePath := filepath.Join(tmpDir, "product_name")
				channelPath := filepath.Join(tmpDir, "org.qemu.guest_agent.0")
				if test.sysVendor != "" {
					Expect(os.WriteFile(sysVendorPath, []byte(test.sysVendor+"\n"), 0o600)).To(Succeed())
				}
				if test.productName != "" {
					Expect(os.WriteFile(productNamePath, []byte(test.productName+"\n"), 0o600)).To(Succeed())
				}
				if test.channel {
					Expect(os.WriteFile(channelPath, nil, 0o600)).To(Succeed())
				}

				command := strings.ReplaceAll(condition, "/sys/class/dmi/id/sys_vendor", sysVendorPath)
				command = strings.ReplaceAll(command, "/sys/class/dmi/id/product_name", productNamePath)
				command = strings.ReplaceAll(command, guestAgentChannel, channelPath)
				err := exec.Command("sh", "-c", command).Run()
				if test.matched {
					Expect(err).NotTo(HaveOccurred(), test.name)
				} else {
					Expect(err).To(HaveOccurred(), test.name)
				}
			}
		}
	})
})
