package action

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ExtensionDirFromBootState", Label("sysext"), func() {
	DescribeTable("resolves the persistent scope directory",
		func(bootState, extType, expected string) {
			Expect(ExtensionDirFromBootState(bootState, extType)).To(Equal(expected))
		},
		Entry("sysext active", "active", "sysext", "/var/lib/kairos/extensions/active"),
		Entry("sysext common", "common", "sysext", "/var/lib/kairos/extensions/common"),
		Entry("confext recovery", "recovery", "confext", "/var/lib/kairos/confexts/recovery"),
		Entry("sysext with no boot state falls back to the base dir", "", "sysext", "/var/lib/kairos/extensions"),
		Entry("unknown extension type yields nothing", "active", "blob", ""),
	)
})
