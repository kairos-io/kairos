package state

import (
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/sdk/constants"
	"github.com/kairos-io/kairos/v4/sdk/ghw/mocks"
	"github.com/kairos-io/kairos/v4/sdk/types/partitions"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The LUKS containers are never mounted themselves, so findmnt finds nothing
// for them. lsblk only knows the unlocked filesystems inside them, both by
// their plain label and by the mapper named after the partition.
const fakeFindmnt = `#!/bin/sh
exit 1
`

const fakeLsblk = `#!/bin/sh
case "$1" in
/dev/disk/by-label/COS_PERSISTENT|/dev/mapper/vda5)
  echo '{"blockdevices":[{"path":"/dev/mapper/vda5","fstype":"ext4","mountpoint":"/usr/local","size":10737418240,"ro":false,"label":"COS_PERSISTENT"}]}'
  ;;
/dev/disk/by-label/COS_OEM|/dev/mapper/vda2)
  echo '{"blockdevices":[{"path":"/dev/mapper/vda2","fstype":"ext4","mountpoint":"/oem","size":67108864,"ro":false,"label":"COS_OEM"}]}'
  ;;
*)
  exit 1
  ;;
esac
`

// Installs made before the LUKS labels existed give the container and the
// filesystem inside it the same label, so the label can point at either.
const fakeLsblkOldLabels = `#!/bin/sh
case "$1" in
/dev/disk/by-label/COS_PERSISTENT)
  echo '{"blockdevices":[{"path":"/dev/vda5","fstype":"crypto_LUKS","size":10737418240,"ro":false,"label":"COS_PERSISTENT"}]}'
  ;;
/dev/mapper/vda5)
  echo '{"blockdevices":[{"path":"/dev/mapper/vda5","fstype":"ext4","mountpoint":"/usr/local","size":10737418240,"ro":false,"label":"COS_PERSISTENT"}]}'
  ;;
*)
  exit 1
  ;;
esac
`

var _ = Describe("detectRuntimeState on an encrypted install", func() {
	var ghwMock mocks.GhwMock
	var oldPath, bin string

	BeforeEach(func() {
		bin = GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(bin, "findmnt"), []byte(fakeFindmnt), 0755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(bin, "lsblk"), []byte(fakeLsblk), 0755)).To(Succeed())
		oldPath = os.Getenv("PATH")
		Expect(os.Setenv("PATH", bin+string(os.PathListSeparator)+oldPath)).To(Succeed())

		ghwMock = mocks.GhwMock{}
		ghwMock.AddDisk(partitions.Disk{
			Name: "vda",
			Partitions: []*partitions.Partition{
				{Name: "vda2", FilesystemLabel: constants.OEMLUKSLabel, FS: constants.LUKSFs},
				{Name: "vda5", FilesystemLabel: constants.PersistentLUKSLabel, FS: constants.LUKSFs},
			},
		})
		ghwMock.CreateDevices()
	})

	AfterEach(func() {
		ghwMock.Clean()
		Expect(os.Setenv("PATH", oldPath)).To(Succeed())
	})

	It("reports persistent and oem as the unlocked, mounted filesystems", func() {
		r := &Runtime{}
		Expect(detectRuntimeState(r)).To(Succeed())

		Expect(r.Persistent.Found).To(BeTrue())
		Expect(r.Persistent.Mounted).To(BeTrue())
		Expect(r.Persistent.MountPoint).To(Equal("/usr/local"))
		Expect(r.Persistent.Name).To(Equal("/dev/mapper/vda5"))
		Expect(r.Persistent.Type).To(Equal("ext4"))
		Expect(r.Persistent.FilesystemLabel).To(Equal(constants.PersistentLabel))

		Expect(r.OEM.Found).To(BeTrue())
		Expect(r.OEM.Mounted).To(BeTrue())
		Expect(r.OEM.MountPoint).To(Equal("/oem"))
		Expect(r.OEM.Name).To(Equal("/dev/mapper/vda2"))
		Expect(r.OEM.Type).To(Equal("ext4"))
		Expect(r.OEM.FilesystemLabel).To(Equal(constants.OEMLabel))
	})

	It("falls back to the mapper when the plain label still names the container", func() {
		Expect(os.WriteFile(filepath.Join(bin, "lsblk"), []byte(fakeLsblkOldLabels), 0755)).To(Succeed())
		r := &Runtime{}
		Expect(detectRuntimeState(r)).To(Succeed())

		Expect(r.Persistent.Mounted).To(BeTrue())
		Expect(r.Persistent.MountPoint).To(Equal("/usr/local"))
		Expect(r.Persistent.Name).To(Equal("/dev/mapper/vda5"))
	})
})
