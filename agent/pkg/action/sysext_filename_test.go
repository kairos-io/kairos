package action

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("extensionFileNameFromURI", Label("sysext"), func() {
	DescribeTable("derives the on-disk filename from the URI path",
		func(uri, expected string) {
			Expect(extensionFileNameFromURI(uri)).To(Equal(expected))
		},
		Entry("plain http URL", "http://host/path/foo.sysext.raw", "foo.sysext.raw"),
		// The query must not end up in the name, or the .raw suffix detection
		// that ListExtensions relies on stops matching.
		Entry("strips a token query string", "http://host/path/foo.sysext.raw?token=abc123", "foo.sysext.raw"),
		Entry("nested https path with several query params", "https://h/a/b/c/bar.confext.raw?x=1&y=2", "bar.confext.raw"),
		Entry("bare filename", "foo.sysext.raw", "foo.sysext.raw"),
		Entry("absolute local path", "/var/lib/x/foo.sysext.raw", "foo.sysext.raw"),
	)
})

var _ = Describe("ExtensionDirFromBootState", Label("sysext"), func() {
	DescribeTable("resolves the persistent scope directory",
		func(bootState, extType, expected string) {
			Expect(ExtensionDirFromBootState(bootState, extType)).To(Equal(expected))
		},
		Entry("sysext active", "active", "sysext", "/var/lib/kairos/extensions/active"),
		Entry("sysext common", "common", "sysext", "/var/lib/kairos/extensions/common"),
		Entry("confext recovery", "recovery", "confext", "/var/lib/kairos/confexts/recovery"),
		Entry("sysext with no boot state falls back to the base dir", "", "sysext", "/var/lib/kairos/extensions/"),
		Entry("unknown extension type yields nothing", "active", "blob", ""),
	)
})
