package validation_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/values"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	"gopkg.in/yaml.v3"

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

// qemuStartRe pulls the service name out of the command each QEMU stage runs.
// It is read out of the cloud-config rather than retyped so that renaming the
// service in 26_vm.yaml, on either service manager, reaches this spec.
var qemuStartRe = regexp.MustCompile(`^(?:systemctl start|rc-service) ([\w.@-]+)(?: start)?$`)

// vmSystems are the distro, family and architecture triples kairos-init builds
// that can boot as a guest of a hypervisor 26_vm.yaml detects. riscv64 is left
// out on purpose: neither guest agent is published for it (Debian bookworm
// ships no qemu-guest-agent, and open-vm-tools is built for x86_64 and aarch64
// only), which is why BasePackages lists both packages per architecture.
var vmSystems = []values.System{
	{Distro: values.Ubuntu, Family: values.DebianFamily, Version: "24.04", Arch: values.ArchAMD64},
	{Distro: values.Ubuntu, Family: values.DebianFamily, Version: "24.04", Arch: values.ArchARM64},
	{Distro: values.Debian, Family: values.DebianFamily, Version: "12", Arch: values.ArchAMD64},
	{Distro: values.Debian, Family: values.DebianFamily, Version: "12", Arch: values.ArchARM64},
	{Distro: values.Fedora, Family: values.RedHatFamily, Version: "40", Arch: values.ArchAMD64},
	{Distro: values.RockyLinux, Family: values.RedHatFamily, Version: "9", Arch: values.ArchAMD64},
	{Distro: values.AlmaLinux, Family: values.RedHatFamily, Version: "9", Arch: values.ArchARM64},
	{Distro: values.OpenSUSELeap, Family: values.SUSEFamily, Version: "15.6", Arch: values.ArchAMD64},
	{Distro: values.Alpine, Family: values.AlpineFamily, Version: "3.21", Arch: values.ArchAMD64},
	{Distro: values.Alpine, Family: values.AlpineFamily, Version: "3.21", Arch: values.ArchARM64},
}

var _ = Describe("Bundled VM cloudconfig packages", func() {
	// 26_vm.yaml ships on every image, so a service it starts has to come from
	// a package the image installs. The QEMU guest agent is the one a Kairos
	// node most often needs: without it the hypervisor cannot request a guest
	// shutdown, read back the node's addresses, or freeze the filesystem for a
	// snapshot. It used to be absent from the Debian family, so the stage ran
	// `systemctl start qemu-guest-agent` against a unit that was never
	// installed, on the most widely used flavor.
	It("installs the QEMU guest agent on every family whose images can run the stage", func() {
		content, err := os.ReadFile(filepath.Join("..", "bundled", "cloudconfigs", "26_vm.yaml"))
		Expect(err).NotTo(HaveOccurred())

		var cfg guardConfig
		Expect(yaml.Unmarshal(content, &cfg)).To(Succeed())

		// Collect the services started by the stages guarded on the QEMU
		// detector, one per service manager.
		services := map[string]bool{}
		for _, stage := range cfg.Stages["boot"] {
			if !strings.Contains(stage.If, `grep -qiFx "QEMU" /sys/class/dmi/id/sys_vendor`) {
				continue
			}
			Expect(stage.Commands).To(HaveLen(1), "QEMU stage %q should start one service", stage.Name)
			m := qemuStartRe.FindStringSubmatch(strings.TrimSpace(stage.Commands[0]))
			Expect(m).NotTo(BeNil(), "cannot read a service name out of %q", stage.Commands[0])
			services[m[1]] = true
		}
		Expect(services).To(HaveLen(1), "systemd and OpenRC must start the same service")

		var service string
		for s := range services {
			service = s
		}
		// The service and the package share a name on all four families. If
		// that ever stops being true this spec needs a per-family mapping
		// rather than a looser assertion.
		Expect(service).To(Equal("qemu-guest-agent"))

		l := logger.NewKairosLogger("validation", "error", true)
		for _, sys := range vmSystems {
			packages, err := values.GetPackages(sys, l)
			Expect(err).NotTo(HaveOccurred(), "%s/%s", sys.Distro, sys.Arch)
			Expect(packages).To(ContainElement(service),
				"%s/%s installs no %s, but 26_vm.yaml starts it on every QEMU guest",
				sys.Distro, sys.Arch, service)
		}
	})
})

// vmwarePackage provides both names the VMware stages start. OpenRC names the
// service after the package, systemd names it after the daemon the package
// runs, and on the Debian family vmtoolsd.service is an Install alias of
// open-vm-tools.service that the package's own enable step creates.
const vmwarePackage = "open-vm-tools"

var vmwareServiceToPackage = map[string]string{
	"open-vm-tools": vmwarePackage,
	"vmtoolsd":      vmwarePackage,
}

var _ = Describe("Bundled VM cloudconfig VMware packages", func() {
	// The same rule as the QEMU spec above, for the other hypervisor 26_vm.yaml
	// detects. On vSphere the consequence is larger than a missing shutdown
	// hook: vCenter learns a guest's addresses only from the in-guest vmtoolsd,
	// so without the package VSphereMachine.status.addresses stays empty and a
	// Cluster API machine never reaches Ready. That is the failure #4092 was
	// filed for, and it was fixed for Hadron only. The Debian family and the
	// Red Hat family both started the daemon without installing it, so this
	// runs over the same list as the QEMU spec rather than a subset.
	It("installs open-vm-tools on every family whose images can run the stage", func() {
		content, err := os.ReadFile(filepath.Join("..", "bundled", "cloudconfigs", "26_vm.yaml"))
		Expect(err).NotTo(HaveOccurred())

		var cfg guardConfig
		Expect(yaml.Unmarshal(content, &cfg)).To(Succeed())

		// Read the service each VMware stage starts, keyed by service manager,
		// out of the cloud-config rather than retyping it, so a rename in
		// 26_vm.yaml reaches this spec.
		services := map[string]string{}
		for _, stage := range cfg.Stages["boot"] {
			if !strings.Contains(stage.If, `grep -iE "VMware" /sys/class/dmi/id/product_name`) {
				continue
			}
			Expect(stage.OnlyServiceManager).NotTo(BeEmpty(),
				"VMware stage %q must pick a service manager", stage.Name)
			Expect(stage.Commands).To(HaveLen(1), "VMware stage %q should start one service", stage.Name)
			m := qemuStartRe.FindStringSubmatch(strings.TrimSpace(stage.Commands[0]))
			Expect(m).NotTo(BeNil(), "cannot read a service name out of %q", stage.Commands[0])
			services[stage.OnlyServiceManager] = m[1]
		}
		Expect(services).To(HaveLen(2), "VMware needs an OpenRC stage and a systemd stage")

		// Unlike the QEMU agent the two managers start different names, so map
		// each to the package that provides it instead of requiring them equal.
		packages := map[string]bool{}
		for manager, service := range services {
			pkg, known := vmwareServiceToPackage[service]
			Expect(known).To(BeTrue(),
				"%s starts %q, which no known package provides", manager, service)
			packages[pkg] = true
		}
		Expect(packages).To(HaveLen(1), "both stages must come from one package")

		l := logger.NewKairosLogger("validation", "error", true)
		for _, sys := range vmSystems {
			installed, err := values.GetPackages(sys, l)
			Expect(err).NotTo(HaveOccurred(), "%s/%s", sys.Distro, sys.Arch)
			Expect(installed).To(ContainElement(vmwarePackage),
				"%s/%s installs no %s, but 26_vm.yaml starts it on every VMware guest",
				sys.Distro, sys.Arch, vmwarePackage)
		}
	})
})
