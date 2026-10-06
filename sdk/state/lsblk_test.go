package state

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The documents below are the real output of
//
//	lsblk <path> -o PATH,FSTYPE,MOUNTPOINT,SIZE,RO,LABEL,PARTUUID,PARTLABEL -J --bytes
//
// from util-linux 2.42.3, with the device names changed. lsblk writes null,
// not an empty string, for a column it has no value for.
var _ = Describe("reading a partition out of lsblk", func() {
	It("fills every field the findmnt path fills", func() {
		part := partitionStateFromLsblk(`{
   "blockdevices": [
      {
         "path": "/dev/vda3",
         "fstype": "ext4",
         "mountpoint": "/usr/local",
         "size": 21474836480,
         "ro": false,
         "label": "COS_PERSISTENT",
         "partuuid": "8b2e0a1c-6f44-4a0e-9e0a-2f8c7d1b5a33",
         "partlabel": "persistent"
      }
   ]
}`)

		Expect(part.Found).To(BeTrue())
		Expect(part.Name).To(Equal("/dev/vda3"))
		Expect(part.Type).To(Equal("ext4"))
		Expect(part.MountPoint).To(Equal("/usr/local"))
		Expect(part.Mounted).To(BeTrue())
		Expect(part.IsReadOnly).To(BeFalse())
		Expect(part.FilesystemLabel).To(Equal("COS_PERSISTENT"))

		// The three this used to leave at their zero value.
		Expect(part.SizeBytes).To(Equal(uint64(21474836480)))
		Expect(part.UUID).To(Equal("8b2e0a1c-6f44-4a0e-9e0a-2f8c7d1b5a33"))
		Expect(part.Label).To(Equal("persistent"))
	})

	It("reads the size as a byte count, not as the string lsblk prints without --bytes", func() {
		// 320G in bytes. A human readable "320G" would not decode into the
		// uint64 at all, which is the regression this guards.
		part := partitionStateFromLsblk(`{"blockdevices":[{"path":"/dev/vda1","size":343597383680,"ro":false}]}`)

		Expect(part.Found).To(BeTrue())
		Expect(part.SizeBytes).To(Equal(uint64(343597383680)))
	})

	It("reports an unmounted partition as not mounted", func() {
		part := partitionStateFromLsblk(`{
   "blockdevices": [
      {
         "path": "/dev/vda2",
         "fstype": "ext4",
         "mountpoint": null,
         "size": 67108864,
         "ro": true,
         "label": "COS_OEM",
         "partuuid": "1d8f3b20-1f0a-4f3d-9a7e-0c6b2a5e4d11",
         "partlabel": "oem"
      }
   ]
}`)

		Expect(part.Found).To(BeTrue())
		Expect(part.Mounted).To(BeFalse())
		Expect(part.MountPoint).To(BeEmpty())
		Expect(part.IsReadOnly).To(BeTrue())
		Expect(part.SizeBytes).To(Equal(uint64(67108864)))
	})

	It("still finds a device mapper target, which has no partition uuid or label", func() {
		// What detectEncryptedPartitions gets for /dev/mapper/vda2: a LUKS
		// mapping is not a partition, so lsblk has no PARTUUID or PARTLABEL
		// for it. The size is the field that matters here.
		part := partitionStateFromLsblk(`{
   "blockdevices": [
      {
         "path": "/dev/mapper/vda2",
         "fstype": "ext4",
         "mountpoint": "/oem",
         "size": 50331648,
         "ro": false,
         "label": "COS_OEM",
         "partuuid": null,
         "partlabel": null
      }
   ]
}`)

		Expect(part.Found).To(BeTrue())
		Expect(part.Name).To(Equal("/dev/mapper/vda2"))
		Expect(part.FilesystemLabel).To(Equal("COS_OEM"))
		Expect(part.SizeBytes).To(Equal(uint64(50331648)))
		Expect(part.UUID).To(BeEmpty())
		Expect(part.Label).To(BeEmpty())
	})

	It("reports not found when lsblk described no device", func() {
		Expect(partitionStateFromLsblk(`{"blockdevices":[]}`).Found).To(BeFalse())
	})

	It("reports not found when lsblk described a disk and its partitions", func() {
		// A whole disk comes back with its children, and this function
		// describes one partition, so it has nothing to answer with.
		part := partitionStateFromLsblk(`{"blockdevices":[
          {"path":"/dev/vda","size":343597383680,"ro":false},
          {"path":"/dev/vda1","size":1048576,"ro":false}
        ]}`)

		Expect(part.Found).To(BeFalse())
	})

	It("reports not found, without panicking, when the output does not decode", func() {
		// lsblk exits non-zero in the cases where it writes a diagnostic, so
		// the caller skips the decode. Should output that it called good
		// arrive anyway, the partition reads as absent and a log line says so.
		Expect(partitionStateFromLsblk("lsblk: /dev/nope: not a block device").Found).To(BeFalse())
		Expect(partitionStateFromLsblk("").Found).To(BeFalse())
		Expect(partitionStateFromLsblk(`{"blockdevices":[{"size":"320G"}]}`).Found).To(BeFalse())
	})
})
