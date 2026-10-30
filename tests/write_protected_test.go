package mos_test

import (
	"fmt"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
)

// Boot test for the write-protected media layout, run on an ordinary writable
// QEMU disk. rd.immucore.write_protected=force applies the layout without the
// read-only probe, so the harness does not need a disk that refuses writes.
// What is under test is the layout itself: the persistent partition is reached
// through the device-mapper snapshot, the snapshot's store is a tmpfs, the
// sentinel the cloud-config stages gate on exists, /oem is read-only, and a
// write to the persistent partition does not survive a reboot.
//
// The install runs from the live ISO, whose cmdline carries no flag, so the
// disk is written normally. The flag goes into the installed GRUB config
// through grub_options and takes effect from the first boot of the installed
// system.
var _ = Describe("kairos write-protected media", Label("write-protected"), func() {
	var vm VM

	BeforeEach(func() {
		_, vm = startVM()
		vm.EventuallyConnects(1200)
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			serial, _ := os.ReadFile(filepath.Join(vm.StateDir, "serial.log"))
			_ = os.MkdirAll("logs", os.ModePerm|os.ModeDir)
			_ = os.WriteFile(filepath.Join("logs", "serial.log"), serial, os.ModePerm)
			fmt.Println(string(serial))
			gatherLogs(vm)
		}
		Expect(vm.Destroy(nil)).ToNot(HaveOccurred())
	})

	It("mounts persistent through a snapshot and forgets writes on reboot", func() {
		testInstall(writeProtectedInstallConfig, vm)
		expectRebootedToActive(vm)

		By("checking the write-protected sentinel is set", func() {
			out, err := vm.Sudo("ls /run/cos")
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(ContainSubstring("write_protected"), out)
			stateAssertVM(vm, "write_protected", "true")
		})

		By("checking the persistent mount is the device-mapper snapshot", func() {
			out, err := vm.Sudo("findmnt -no SOURCE,FSTYPE,OPTIONS /usr/local")
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(ContainSubstring("/dev/mapper/kairos-persistent"), out)
			Expect(out).To(ContainSubstring("ext4"), out)
			Expect(out).To(MatchRegexp(`\brw\b`), out)
		})

		By("checking the snapshot is active and its store is a tmpfs", func() {
			out, err := vm.Sudo("dmsetup status kairos-persistent")
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(ContainSubstring(" snapshot "), out)
			stateAssertVM(vm, "persistent_cow.state", "active")

			out, err = vm.Sudo("findmnt -no FSTYPE /run/immucore/cow")
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(ContainSubstring("tmpfs"), out)
		})

		By("checking /oem is mounted read-only", func() {
			out, err := vm.Sudo("findmnt -no OPTIONS /oem")
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(MatchRegexp(`\bro\b`), out)

			out, err = vm.Sudo("touch /oem/write-protected-probe")
			Expect(err).To(HaveOccurred(), out)
			Expect(out).To(ContainSubstring("Read-only file system"), out)
		})

		By("writing to the persistent partition", func() {
			out, err := vm.Sudo("echo gone-after-reboot > /usr/local/write-protected-probe && sync")
			Expect(err).ToNot(HaveOccurred(), out)

			out, err = vm.Sudo("cat /usr/local/write-protected-probe")
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(ContainSubstring("gone-after-reboot"), out)
		})

		By("rebooting", func() {
			vm.Reboot()
			vm.EventuallyConnects(1200)
			expectRebootedToActive(vm)
		})

		By("checking the write did not survive the reboot", func() {
			out, err := vm.Sudo("ls /usr/local/write-protected-probe")
			Expect(err).To(HaveOccurred(), out)

			out, err = vm.Sudo("findmnt -no SOURCE /usr/local")
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(ContainSubstring("/dev/mapper/kairos-persistent"), out)
		})
	})
})

// writeProtectedInstallConfig installs with the layout forced on and a small
// copy-on-write store, so the test exercises the store size option too and
// the QEMU VM does not need the default store's worth of RAM.
const writeProtectedInstallConfig = `#cloud-config

install:
  grub_options:
    extra_cmdline: "rd.immucore.write_protected=force rd.immucore.write_protected.cow=512M"

users:
  - name: "kairos"
    passwd: "kairos"
    groups:
      - "admin"
`
