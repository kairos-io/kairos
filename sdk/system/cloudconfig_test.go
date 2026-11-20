package system

import (
	"github.com/kairos-io/kairos/v4/sdk/state"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("persistentCloudConfigTarget", func() {
	oem := state.PartitionState{Name: "/dev/vda2", Label: "COS_OEM"}
	persistent := state.PartitionState{Name: "/dev/vda5", Label: "COS_PERSISTENT"}
	statePart := state.PartitionState{Name: "/dev/vda4", Label: "COS_STATE"}

	It("writes to the OEM partition root when there is one", func() {
		partition, subpath := persistentCloudConfigTarget(state.Runtime{OEM: oem, Persistent: persistent, State: statePart})
		Expect(partition).To(Equal(oem))
		Expect(subpath).To(BeEmpty())
	})

	It("writes to cloud-config on the persistent partition without an OEM partition", func() {
		partition, subpath := persistentCloudConfigTarget(state.Runtime{Persistent: persistent, State: statePart})
		Expect(partition).To(Equal(persistent))
		Expect(subpath).To(Equal("cloud-config"))
	})
})
