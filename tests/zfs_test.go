package mos_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
)

var _ = Describe("kairos zfs test", Label("zfs"), func() {
	BeforeEach(func() {
		EventuallyConnects(1200)
	})

	Context("install", func() {
		It("to disk with zfs kernel module loaded", func() {
			err := Machine.SendFile("assets/zfs.yaml", "/tmp/zfs.yaml", "0770")
			Expect(err).ToNot(HaveOccurred())

			out, err := Sudo("kairos-agent manual-install --device auto /tmp/zfs.yaml")
			Expect(err).ToNot(HaveOccurred(), out)
			out, err = Sudo("sync")
			Expect(err).ToNot(HaveOccurred(), out)
			detachAndReboot()
		})
	})

	Context("create pool", func() {
		It("with two disks and mount it at /mnt/pool0", func() {
			out, err := Sudo("dd if=/dev/zero of=/usr/local/disk0.img bs=1M count=1024")
			Expect(err).ToNot(HaveOccurred(), out)
			out, err = Sudo("dd if=/dev/zero of=/usr/local/disk1.img bs=1M count=1024")
			Expect(err).ToNot(HaveOccurred(), out)
			out, err = Sudo("zpool create pool0 -m /usr/local/pool0 /usr/local/disk0.img /usr/local/disk1.img")
			Expect(err).ToNot(HaveOccurred(), out)
		})

		It("to be writable", func() {
			out, err := Sudo("touch /usr/local/pool0/test")
			Expect(err).ToNot(HaveOccurred(), out)
		})

		It("file to persist", func() {
			out, err := Sudo("ls /usr/local/pool0")
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(ContainSubstring("test"))
		})
	})
})
