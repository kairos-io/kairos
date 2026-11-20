package mos_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
)

var _ = Describe("classic install source minimums", Label("install", "source-sizing"), func() {
	var vm VM

	BeforeEach(func() {
		_, vm = startVM()
		vm.EventuallyConnects(1200)
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			gatherLogs(vm)
		}
		Expect(vm.Destroy(nil)).To(Succeed())
	})

	It("boots when image minimums are smaller than the selected live source", func() {
		// The one MiB requests must grow to hold the selected source. The
		// invalid provisional path must never be used to size or deploy it.
		testInstall(`#cloud-config
install:
  source: file:/missing-provisional-source
  system:
    uri: dir:/run/rootfsbase
    size: 1
  passive:
    size: 1
  recovery-system:
    size: 1
users:
- name: kairos
  passwd: kairos
  groups:
    - admin
`, vm)
		expectRebootedToActive(vm)
		out, err := vm.Sudo("cat /etc/os-release")
		Expect(err).ToNot(HaveOccurred(), out)
		Expect(out).To(ContainSubstring("ID="))
	})
})
