// nolint
package mos_test

import (
	"encoding/json"
	"github.com/mudler/go-pluggable"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
	"golang.org/x/mod/semver"
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

	Context("agent.available_releases event", func() {
		It("returns the available versions ordered, excluding '.img' tags", func() {
			resultStr, _ := vm.Sudo(`echo '{}' | /system/providers/agent-provider-kairos agent.available_releases`)

			var result pluggable.EventResponse

			err := json.Unmarshal([]byte(resultStr), &result)
			Expect(err).ToNot(HaveOccurred())

			Expect(result.Data).ToNot(BeEmpty())
			var versions []string
			json.Unmarshal([]byte(result.Data), &versions)

			Expect(versions).ToNot(BeEmpty())
			sorted := make([]string, len(versions))
			copy(sorted, versions)

			semver.Sort(sorted)

			for _, t := range sorted {
				Expect(t).ToNot(ContainSubstring(".img"))
			}

			Expect(sorted).To(Equal(versions))
		})
	})
})
