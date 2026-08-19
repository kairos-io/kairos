package phonehome

import "github.com/kairos-io/kairos-sdk/state"

// gatherAddresses collects the node's routable network addresses plus its
// hostname and maps them to the AuroraBoot NodeAddress wire type
// (kairos-io/kairos#4253).
//
// The detection policy — which interfaces count as the node's own, which are
// container/CNI/VPN/virtual and get dropped, plus dedup and deterministic
// ordering — lives in kairos-sdk/state.DetectAddresses so that this agent, a
// CAPI infrastructure provider, and `kairos-agent state` all share exactly one
// implementation and one reviewable exclusion list. See state.DetectAddresses.
//
// Unlike gatherSystemInfo it is NOT cached: addresses change (DHCP, NIC churn)
// and must be re-read on every heartbeat.
func gatherAddresses() []NodeAddress {
	return toNodeAddresses(state.DetectAddresses())
}

// toNodeAddresses maps kairos-sdk MachineAddresses to the phonehome/AuroraBoot
// NodeAddress wire type. Both use the Cluster API address vocabulary
// (InternalIP | ExternalIP | Hostname), so this is a straight field copy; it is
// a pure, testable seam over the host-touching detector.
func toNodeAddresses(addrs []state.MachineAddress) []NodeAddress {
	out := make([]NodeAddress, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, NodeAddress{Type: a.Type, Address: a.Address})
	}
	return out
}
