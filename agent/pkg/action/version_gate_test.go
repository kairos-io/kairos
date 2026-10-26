package action_test

import (
	"github.com/kairos-io/kairos/v4/agent/pkg/action"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("RefuseIfTargetIsDowngrade", func() {
	DescribeTable("allows an upgrade to the same or a newer KAIROS_INIT_VERSION",
		func(current, target string) {
			Expect(action.RefuseIfTargetIsDowngrade(current, target)).To(Succeed())
		},
		Entry("same tag", "v4.4.0", "v4.4.0"),
		Entry("newer tag", "v4.3.0", "v4.4.0"),
		// The master build two commits after the v4.4.0 tag, upgraded to
		// from the v4.4.0 release in the upgrade-latest-with-cli test.
		Entry("commits past the current tag", "v4.4.0", "v4.4.0-2-g8c24a223"),
		Entry("more commits past the same tag", "v4.4.0-2-g8c24a223", "v4.4.0-5-gabc1234"),
		Entry("dirty build past the current tag", "v4.4.0", "v4.4.0-2-g8c24a223-dirty"),
		Entry("commits past a release candidate", "v4.4.0-rc2", "v4.4.0-rc2-3-gabc1234"),
		Entry("next tag after a between-tags build", "v4.4.0-7-gabc1234", "v4.4.1"),
	)

	DescribeTable("refuses an older KAIROS_INIT_VERSION",
		func(current, target string) {
			err := action.RefuseIfTargetIsDowngrade(current, target)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("refusing to upgrade"))
		},
		Entry("older tag", "v4.4.0", "v4.3.0"),
		Entry("the tag a between-tags build came from", "v4.4.0-2-g8c24a223", "v4.4.0"),
		Entry("fewer commits past the same tag", "v4.4.0-5-gabc1234", "v4.4.0-2-g8c24a223"),
		Entry("a build past an older tag", "v4.4.0", "v4.3.0-9-gabc1234"),
	)

	It("refuses a version it cannot parse", func() {
		Expect(action.RefuseIfTargetIsDowngrade("v4.4.0", "8c24a223")).ToNot(Succeed())
	})
})
