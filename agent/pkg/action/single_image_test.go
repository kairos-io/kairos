package action_test

import (
	"github.com/kairos-io/kairos/v4/agent/pkg/action"
	sdkPartitions "github.com/kairos-io/kairos/v4/sdk/types/partitions"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Single image node", Label("single-image"), func() {
	recovery := &sdkPartitions.Partition{FilesystemLabel: "COS_RECOVERY"}

	Describe("IsSingleImageNode", func() {
		It("is true when there is no recovery partition", func() {
			Expect(action.IsSingleImageNode(nil)).To(BeTrue())
		})
		It("is false on a node with a recovery partition", func() {
			Expect(action.IsSingleImageNode(recovery)).To(BeFalse())
		})
	})

	Describe("CheckResetSupported", func() {
		It("refuses reset when there is no recovery partition", func() {
			err := action.CheckResetSupported(nil)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("This node has no recovery partition (it was built with disk.no_recovery), so reset is not supported. Replace the node with a fresh disk."))
		})
		It("allows reset on a node with a recovery partition", func() {
			Expect(action.CheckResetSupported(recovery)).To(Succeed())
		})
	})
})
