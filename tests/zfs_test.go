package mos_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
)

var _ = Describe("kairos zfs test", Label("zfs"), func() {

	var vm VM

	BeforeEach(func() {
		_, vm = startVM()
		vm.EventuallyConnects(1200)
	})

	AfterEach(func() {
		Expect(vm.Destroy(nil)).ToNot(HaveOccurred())
	})

	It("works", func() {
		err := vm.Scp("assets/zfs.yaml", "/tmp/zfs.yaml", "0770")
		Expect(err).ToNot(HaveOccurred())

		By("installing to disk with zfs kernel module loaded", func() {
			out, err := vm.Sudo("kairos-agent manual-install --device auto /tmp/zfs.yaml")
			Expect(err).ToNot(HaveOccurred(), out)
			out, err = vm.Sudo("sync")
			Expect(err).ToNot(HaveOccurred(), out)

			vm.Reboot()
		})

		By("creating a pool with two disks", func() {
			out, err := vm.Sudo("dd if=/dev/zero of=/usr/local/disk0.img bs=1M count=1024")
			Expect(err).ToNot(HaveOccurred(), out)
			out, err = vm.Sudo("dd if=/dev/zero of=/usr/local/disk1.img bs=1M count=1024")
			Expect(err).ToNot(HaveOccurred(), out)
			out, err = vm.Sudo("zpool create pool0 -m /usr/local/pool0 /usr/local/disk0.img /usr/local/disk1.img")
			Expect(err).ToNot(HaveOccurred(), out)
		})

		By("checking if it's writable", func() {
			out, err := vm.Sudo("touch /usr/local/pool0/test")
			Expect(err).ToNot(HaveOccurred(), out)
		})

		By("checking if a file persists", func() {
			out, err := vm.Sudo("ls /usr/local/pool0")
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(ContainSubstring("test"))
		})
	})
})
