package state

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("parseCustomMount", func() {
	DescribeTable("splits the device from the mountpoint",
		func(entry, device, mountpoint string) {
			gotDevice, gotMountpoint, err := parseCustomMount(entry)
			Expect(err).ToNot(HaveOccurred())
			Expect(gotDevice).To(Equal(device))
			Expect(gotMountpoint).To(Equal(mountpoint))
		},
		Entry("LABEL=", "LABEL=COS_OEM:/oem", "/dev/disk/by-label/COS_OEM", "/oem"),
		Entry("UUID=", "UUID=1234-ABCD:/data", "/dev/disk/by-uuid/1234-ABCD", "/data"),
		Entry("plain device", "/dev/sda5:/plain", "/dev/sda5", "/plain"),
		Entry("by-path device with colons", "/dev/disk/by-path/pci-0000:00:1f.2-ata-1-part5:/srv", "/dev/disk/by-path/pci-0000:00:1f.2-ata-1-part5", "/srv"),
		Entry("by-id device with a colon", "/dev/disk/by-id/usb-SanDisk_Cruzer_1234-0:0-part1:/usb", "/dev/disk/by-id/usb-SanDisk_Cruzer_1234-0:0-part1", "/usb"),
	)

	DescribeTable("refuses an entry it cannot mount",
		func(entry, want string) {
			_, _, err := parseCustomMount(entry)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(want))
		},
		Entry("LABEL: form", "LABEL:FOO:/readme-form", "LABEL=<value>:<mountpoint>"),
		Entry("UUID: form", "UUID:1234-ABCD:/data", "UUID=<value>:<mountpoint>"),
		Entry("no mountpoint", "/dev/sda5", "<device>:<absolute mountpoint>"),
		Entry("relative mountpoint", "/dev/sda5:data", "<device>:<absolute mountpoint>"),
		Entry("no device", ":/data", "<device>:<absolute mountpoint>"),
	)
})
