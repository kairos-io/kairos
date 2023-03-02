package mos_test

import (
	. "github.com/spectrocloud/peg/matcher"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("kairos netboot test", Label("netboot-test"), func() {
	var vm VM
	BeforeEach(func() {
		_, vm = startVM()
	})

	AfterEach(func() {
		Expect(vm.Destroy(nil)).ToNot(HaveOccurred())
	})

	It("eventually boots", func() {
		vm.EventuallyConnects(1200)
	})
})
