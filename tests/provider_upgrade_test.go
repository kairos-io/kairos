// nolint
package mos_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
)

var _ = Describe("provider upgrade test", Label("provider", "provider-upgrade"), func() {
	var vm VM

	BeforeEach(func() {
		_, vm = startVM()
		vm.EventuallyConnects(1200)
	})

	AfterEach(func() {
		if CurrentGinkgoTestDescription().Failed {
			gatherLogs(vm)
		}
		vm.Destroy(nil)
	})

	Context("kairos-agent upgrade list-releases", func() {
		It("returns at least one option to upgrade to", func() {
			resultStr, _ := vm.Sudo(`kairos-agent upgrade list-releases --all | tail -1`)

			Expect(resultStr).To(ContainSubstring("quay.io/kairos"))
		})
	})
})
