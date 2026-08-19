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

var qemuConditionRe = regexp.MustCompile(`(?m)^\s+grep -qiFx "QEMU" /sys/class/dmi/id/sys_vendor 2>/dev/null \|\|\n\s+grep -qiE "qemu\|kvm\|Virtual Machine" /sys/class/dmi/id/product_name 2>/dev/null$`)

var _ = Describe("Bundled VM cloudconfig", func() {
	It("detects supported VMs from DMI for both service managers", func() {
		content, err := os.ReadFile(filepath.Join("..", "bundled", "cloudconfigs", "26_vm.yaml"))
		Expect(err).NotTo(HaveOccurred())

		conditions := qemuConditionRe.FindAllString(string(content), -1)
		Expect(conditions).To(HaveLen(2), "systemd and OpenRC must use the same QEMU detector")

		tests := []struct {
			name        string
			sysVendor   string
			productName string
			matched     bool
		}{
			{name: "QEMU Q35", sysVendor: "QEMU", productName: "Standard PC (Q35 + ICH9, 2009)", matched: true},
			{name: "QEMU i440FX", sysVendor: "qemu", productName: "Standard PC (i440FX + PIIX, 1996)", matched: true},
			{name: "KVM product", sysVendor: "Red Hat", productName: "KVM", matched: true},
			{name: "libvirt QEMU product", sysVendor: "Red Hat", productName: "QEMU Virtual Machine", matched: true},
			{name: "Microsoft Azure", sysVendor: "Microsoft Corporation", productName: "Virtual Machine", matched: true},
			{name: "bare metal", sysVendor: "Dell Inc.", productName: "PowerEdge R740", matched: false},
			{name: "generic standard PC", sysVendor: "American Megatrends Inc.", productName: "Standard PC", matched: false},
			{name: "missing DMI files", matched: false},
		}

		for _, condition := range conditions {
			for _, test := range tests {
				tmpDir := GinkgoT().TempDir()
				sysVendorPath := filepath.Join(tmpDir, "sys_vendor")
				productNamePath := filepath.Join(tmpDir, "product_name")
				if test.sysVendor != "" {
					Expect(os.WriteFile(sysVendorPath, []byte(test.sysVendor+"\n"), 0o600)).To(Succeed())
				}
				if test.productName != "" {
					Expect(os.WriteFile(productNamePath, []byte(test.productName+"\n"), 0o600)).To(Succeed())
				}

				command := strings.ReplaceAll(condition, "/sys/class/dmi/id/sys_vendor", sysVendorPath)
				command = strings.ReplaceAll(command, "/sys/class/dmi/id/product_name", productNamePath)
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
