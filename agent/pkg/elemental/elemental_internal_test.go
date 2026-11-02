package elemental

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("checkPartitionOnDisk (kairos-io/kairos#5731)", Label("partition"), func() {
	var root, sysClassBlock, devDir string

	// link creates /sys/class/block/<part> pointing into the directory of
	// the disk that holds it, the way the kernel lays it out.
	link := func(disk, part string) {
		target := filepath.Join(root, "sys/devices/pci0000:00", disk, part)
		Expect(os.MkdirAll(target, 0o755)).To(Succeed())
		Expect(os.Symlink(target, filepath.Join(sysClassBlock, part))).To(Succeed())
	}
	device := func(name string) string {
		p := filepath.Join(devDir, name)
		Expect(os.WriteFile(p, nil, 0o644)).To(Succeed())
		return p
	}

	BeforeEach(func() {
		root = GinkgoT().TempDir()
		sysClassBlock = filepath.Join(root, "sys/class/block")
		devDir = filepath.Join(root, "dev")
		Expect(os.MkdirAll(sysClassBlock, 0o755)).To(Succeed())
		Expect(os.MkdirAll(devDir, 0o755)).To(Succeed())
		link("sda", "sda2")
		link("sdb", "sdb2")
		link("nvme0n1", "nvme0n1p2")
	})

	It("accepts a partition of the target", func() {
		Expect(checkPartitionOnDisk(sysClassBlock, device("sda2"), device("sda"))).To(Succeed())
		Expect(checkPartitionOnDisk(sysClassBlock, device("nvme0n1p2"), device("nvme0n1"))).To(Succeed())
	})

	It("accepts a target given through a symlink", func() {
		byID := filepath.Join(devDir, "by-id-disk")
		Expect(os.Symlink(device("sda"), byID)).To(Succeed())
		Expect(checkPartitionOnDisk(sysClassBlock, device("sda2"), byID)).To(Succeed())
	})

	It("rejects a partition of another disk with the same name", func() {
		err := checkPartitionOnDisk(sysClassBlock, device("sdb2"), device("sda"))
		Expect(err).To(MatchError(ContainSubstring("is on /dev/sdb, not on the install target")))
	})

	It("rejects a device with no sysfs entry", func() {
		err := checkPartitionOnDisk(sysClassBlock, device("sdc2"), device("sda"))
		Expect(err).To(MatchError(ContainSubstring("reading the sysfs entry")))
	})
})
