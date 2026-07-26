package phonehome

import (
	"github.com/kairos-io/kairos-sdk/state"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The address detection and interface-exclusion policy is owned and tested by
// kairos-sdk/state.DetectAddresses. Here we only cover the agent's
// responsibility: mapping the SDK's MachineAddress to the AuroraBoot NodeAddress
// wire type without dropping or reordering entries.
var _ = Describe("toNodeAddresses", func() {
	It("maps type and address through, preserving order", func() {
		in := []state.MachineAddress{
			{Type: state.AddressInternalIP, Address: "10.0.10.21"},
			{Type: state.AddressInternalIP, Address: "192.168.50.10"},
			{Type: state.AddressHostname, Address: "node-1"},
		}
		Expect(toNodeAddresses(in)).To(Equal([]NodeAddress{
			{Type: "InternalIP", Address: "10.0.10.21"},
			{Type: "InternalIP", Address: "192.168.50.10"},
			{Type: "Hostname", Address: "node-1"},
		}))
	})

	It("returns an empty (non-nil) slice for no addresses", func() {
		got := toNodeAddresses(nil)
		Expect(got).ToNot(BeNil())
		Expect(got).To(BeEmpty())
	})
})
