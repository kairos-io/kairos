package debugbundle

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	. "github.com/onsi/gomega"

	ginkgo "github.com/onsi/ginkgo/v2"
)

// fakeDisk describes one entry to write into the sysfs fixture below.
type fakeDisk struct {
	name      string // sda, sdb ...
	devPath   string // where /sys/block/<name> points, relative to /sys/devices
	removable bool
	sectors   int
	parts     map[string]int // partition name -> size in sectors
}

// writeSysfs builds a sysfs and procfs tree that ghw and the sysfs reads in
// usb.go can both be pointed at, and returns its root.
func writeSysfs(disks []fakeDisk, mounts string) string {
	root := ginkgo.GinkgoT().TempDir()
	Expect(os.MkdirAll(filepath.Join(root, "sys", "block"), 0o755)).To(Succeed())
	Expect(os.MkdirAll(filepath.Join(root, "proc", "self"), 0o755)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(root, "proc", "self", "mounts"), []byte(mounts), 0o644)).To(Succeed())

	for _, d := range disks {
		dev := filepath.Join(root, "sys", "devices", d.devPath, "block", d.name)
		Expect(os.MkdirAll(filepath.Join(dev, "queue"), 0o755)).To(Succeed())
		removable := "0"
		if d.removable {
			removable = "1"
		}
		Expect(os.WriteFile(filepath.Join(dev, "removable"), []byte(removable+"\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dev, "size"), []byte(strconv.Itoa(d.sectors)+"\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dev, "queue", "rotational"), []byte("0\n"), 0o644)).To(Succeed())
		for part, sectors := range d.parts {
			Expect(os.MkdirAll(filepath.Join(dev, part), 0o755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(dev, part, "size"), []byte(strconv.Itoa(sectors)+"\n"), 0o644)).To(Succeed())
		}
		// Real sysfs links /sys/block/<name> to the device tree with a
		// relative path, and diskIsUSB walks the resolved path.
		target := filepath.Join("..", "devices", d.devPath, "block", d.name)
		Expect(os.Symlink(target, filepath.Join(root, "sys", "block", d.name))).To(Succeed())
	}
	return root
}

const (
	sataPath = "pci0000:00/0000:00:17.0/ata1/host0/target0:0:0/0:0:0:0"
	usbPath  = "pci0000:00/0000:00:14.0/usb2/2-1/2-1:1.0/host6/target6:0:0/6:0:0:0"
	usbPath2 = "pci0000:00/0000:00:14.0/usb1/1-4/1-4:1.0/host7/target7:0:0/7:0:0:0"
)

func devices(targets []Target) []string {
	out := []string{}
	for _, t := range targets {
		out = append(out, t.Device)
	}
	return out
}

var _ = ginkgo.Describe("CopyTargets", func() {
	var previousRoot string

	ginkgo.BeforeEach(func() {
		previousRoot = sysRoot
	})

	ginkgo.AfterEach(func() {
		sysRoot = previousRoot
	})

	// The reported bug: booted from a USB stick, the user plugs in a second
	// one and the menu still lists only the stick they booted from, because
	// nothing in the live system mounts what you plug in.
	ginkgo.It("offers a USB drive that nothing has mounted", func() {
		sysRoot = writeSysfs([]fakeDisk{
			{name: "sda", devPath: sataPath, sectors: 500118192, parts: map[string]int{"sda1": 1953792}},
			{name: "sdb", devPath: usbPath, removable: true, sectors: 30310400, parts: map[string]int{"sdb1": 30308352}},
			{name: "sdc", devPath: usbPath2, removable: true, sectors: 60620800, parts: map[string]int{"sdc1": 60618752}},
		}, "/dev/sdb1 /run/initramfs/live iso9660 ro,relatime 0 0\n")

		targets, err := CopyTargets()
		Expect(err).ToNot(HaveOccurred())
		Expect(devices(targets)).To(ContainElement("/dev/sdc1"))
	})

	ginkgo.It("reports an unmounted drive with an empty mount point", func() {
		sysRoot = writeSysfs([]fakeDisk{
			{name: "sdc", devPath: usbPath2, removable: true, sectors: 60620800, parts: map[string]int{"sdc1": 60618752}},
		}, "")

		targets, err := CopyTargets()
		Expect(err).ToNot(HaveOccurred())
		Expect(targets).To(HaveLen(1))
		Expect(targets[0].MountPoint).To(BeEmpty())
		Expect(targets[0].SizeBytes).To(Equal(uint64(60618752) * 512))
	})

	// sysfs sets removable to 0 for an SSD or a spinning disk in a USB
	// enclosure, so the removable flag alone hides every USB drive that is
	// not a flash stick.
	ginkgo.It("offers a USB disk whose removable flag is not set", func() {
		sysRoot = writeSysfs([]fakeDisk{
			{name: "sdd", devPath: usbPath2, removable: false, sectors: 976773168, parts: map[string]int{"sdd1": 976771072}},
		}, "")

		targets, err := CopyTargets()
		Expect(err).ToNot(HaveOccurred())
		Expect(devices(targets)).To(ConsistOf("/dev/sdd1"))
	})

	// The live medium is mounted read only, so copying to it always fails.
	// Offering it is worse than offering nothing.
	ginkgo.It("does not offer the read only live medium", func() {
		sysRoot = writeSysfs([]fakeDisk{
			{name: "sdb", devPath: usbPath, removable: true, sectors: 30310400, parts: map[string]int{"sdb1": 30308352}},
		}, "/dev/sdb1 /run/initramfs/live iso9660 ro,relatime 0 0\n")

		targets, err := CopyTargets()
		Expect(err).ToNot(HaveOccurred())
		Expect(targets).To(BeEmpty())
	})

	// The live medium's EFI partition sits beside the read only one and
	// nothing mounts it, so it passes every other test. Writing to the drive
	// the machine booted from is not what the menu is for.
	ginkgo.It("does not offer the rest of the drive the live medium is on", func() {
		sysRoot = writeSysfs([]fakeDisk{
			{name: "sdb", devPath: usbPath, removable: true, sectors: 814336,
				parts: map[string]int{"sdb1": 806912, "sdb2": 8192}},
			{name: "sdd", devPath: usbPath2, removable: true, sectors: 4194304, parts: map[string]int{"sdd1": 4192256}},
		}, "/dev/sdb1 /run/initramfs/live iso9660 ro,relatime 0 0\n")

		targets, err := CopyTargets()
		Expect(err).ToNot(HaveOccurred())
		Expect(devices(targets)).To(ConsistOf("/dev/sdd1"))
	})

	// Both are removable, neither is somewhere a bundle can go, and an empty
	// drive looks the same as a loaded one.
	ginkgo.It("does not offer a floppy or an optical drive", func() {
		sysRoot = writeSysfs([]fakeDisk{
			{name: "fd0", devPath: "platform/floppy.0", removable: true, sectors: 8},
			{name: "sr0", devPath: sataPath, removable: true, sectors: 2097152},
			{name: "sdd", devPath: usbPath2, removable: true, sectors: 4194304, parts: map[string]int{"sdd1": 4192256}},
		}, "")

		targets, err := CopyTargets()
		Expect(err).ToNot(HaveOccurred())
		Expect(devices(targets)).To(ConsistOf("/dev/sdd1"))
	})

	ginkgo.It("keeps the mount point of a drive that is already mounted read write", func() {
		sysRoot = writeSysfs([]fakeDisk{
			{name: "sdc", devPath: usbPath2, removable: true, sectors: 60620800, parts: map[string]int{"sdc1": 60618752}},
		}, "/dev/sdc1 /run/media/usb0 vfat rw,relatime 0 0\n")

		targets, err := CopyTargets()
		Expect(err).ToNot(HaveOccurred())
		Expect(targets).To(HaveLen(1))
		Expect(targets[0].MountPoint).To(Equal("/run/media/usb0"))
	})

	// A stick formatted with no partition table carries its filesystem on the
	// disk itself, and has no partition for the menu to list.
	ginkgo.It("offers a drive that has no partition table", func() {
		sysRoot = writeSysfs([]fakeDisk{
			{name: "sdc", devPath: usbPath2, removable: true, sectors: 2097152},
		}, "")

		targets, err := CopyTargets()
		Expect(err).ToNot(HaveOccurred())
		Expect(targets).To(HaveLen(1))
		Expect(targets[0].Device).To(Equal("/dev/sdc"))
		Expect(targets[0].MountPoint).To(BeEmpty())
		Expect(targets[0].SizeBytes).To(Equal(uint64(2097152) * 512))
	})

	ginkgo.It("keeps the mount point of a partitionless drive that is mounted", func() {
		sysRoot = writeSysfs([]fakeDisk{
			{name: "sdc", devPath: usbPath2, removable: true, sectors: 2097152},
		}, "/dev/sdc /run/media/usb0 vfat rw,relatime 0 0\n")

		targets, err := CopyTargets()
		Expect(err).ToNot(HaveOccurred())
		Expect(targets).To(HaveLen(1))
		Expect(targets[0].MountPoint).To(Equal("/run/media/usb0"))
	})

	ginkgo.It("does not offer a partitionless drive that is mounted read only", func() {
		sysRoot = writeSysfs([]fakeDisk{
			{name: "sdc", devPath: usbPath2, removable: true, sectors: 2097152},
		}, "/dev/sdc /run/initramfs/live iso9660 ro,relatime 0 0\n")

		targets, err := CopyTargets()
		Expect(err).ToNot(HaveOccurred())
		Expect(targets).To(BeEmpty())
	})

	ginkgo.It("does not offer an internal disk", func() {
		sysRoot = writeSysfs([]fakeDisk{
			{name: "sda", devPath: sataPath, sectors: 500118192, parts: map[string]int{"sda1": 1953792, "sda2": 498163712}},
		}, "/dev/sda1 /boot/efi vfat rw,relatime 0 0\n")

		targets, err := CopyTargets()
		Expect(err).ToNot(HaveOccurred())
		Expect(targets).To(BeEmpty())
	})
})

var _ = ginkgo.Describe("CopyBundleTo", func() {
	var previousRun func(string, ...string) ([]byte, error)
	var calls [][]string
	var src string

	ginkgo.BeforeEach(func() {
		previousRun = run
		calls = nil
		run = func(name string, args ...string) ([]byte, error) {
			calls = append(calls, append([]string{name}, args...))
			return nil, nil
		}
		src = filepath.Join(ginkgo.GinkgoT().TempDir(), "bundle.tar.gz")
		Expect(os.WriteFile(src, []byte("data"), 0o644)).To(Succeed())
	})

	ginkgo.AfterEach(func() {
		run = previousRun
	})

	ginkgo.It("mounts a drive nothing has mounted, copies, then unmounts it", func() {
		name, err := CopyBundleTo(src, Target{Device: "/dev/sdc1"})
		Expect(err).ToNot(HaveOccurred())
		Expect(name).To(Equal("bundle.tar.gz"))

		Expect(calls).To(HaveLen(2))
		Expect(calls[0][0]).To(Equal("mount"))
		Expect(calls[0][1]).To(Equal("/dev/sdc1"))
		Expect(calls[1][0]).To(Equal("umount"))
		Expect(calls[1][1]).To(Equal(calls[0][2]))
	})

	ginkgo.It("copies straight to a drive that is already mounted", func() {
		dir := ginkgo.GinkgoT().TempDir()

		name, err := CopyBundleTo(src, Target{Device: "/dev/sdc1", MountPoint: dir})
		Expect(err).ToNot(HaveOccurred())
		Expect(name).To(Equal("bundle.tar.gz"))
		Expect(calls).To(BeEmpty())

		got, err := os.ReadFile(filepath.Join(dir, "bundle.tar.gz"))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(got)).To(Equal("data"))
	})

	ginkgo.It("reports why the mount failed rather than a copy error", func() {
		run = func(string, ...string) ([]byte, error) {
			return []byte("mount: unknown filesystem type 'apfs'"), fmt.Errorf("exit status 32")
		}

		_, err := CopyBundleTo(src, Target{Device: "/dev/sdc1"})
		Expect(err).To(MatchError(ContainSubstring("mounting /dev/sdc1")))
		Expect(err).To(MatchError(ContainSubstring("unknown filesystem type 'apfs'")))
	})
})
