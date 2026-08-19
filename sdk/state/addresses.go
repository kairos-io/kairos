package state

import (
	"net"
	"os"
	"sort"
	"strings"
)

// MachineAddress is one of a node's network addresses. It mirrors the Cluster
// API MachineAddress shape (type + address) so CAPI-adjacent consumers (e.g. an
// infrastructure provider) can pass it straight through.
type MachineAddress struct {
	Type    string `yaml:"type" json:"type"`
	Address string `yaml:"address" json:"address"`
}

// Address type values, aligned with the Cluster API MachineAddress vocabulary.
const (
	AddressInternalIP = "InternalIP"
	AddressHostname   = "Hostname"
	// AddressExternalIP is reserved for a future externally-supplied address that
	// is not present on any local NIC (e.g. a cloud-assigned public IP). Detection
	// here only reports NIC-local addresses, but the vocabulary leaves room for it.
	AddressExternalIP = "ExternalIP"
)

// excludedIfacePrefixes lists interface-name prefixes whose addresses are NOT the
// node's own routable addresses: container/CNI networking, VM/virtual bridges,
// VPN/overlay tunnels, and dummy devices. Everything else that carries a
// global-unicast IP is reported — including ordinary NICs and real host bridges
// such as "br0" (a real bridge can legitimately hold the node's primary address;
// only the docker-style "br-<hex>" bridges are excluded, via the "br-" prefix,
// not "br").
//
// This list is the reviewable policy knob: adjust it rather than the filtering
// logic. It is deliberately name-based and conservative — when in doubt an
// interface is INCLUDED, since a missing node address is worse than an extra one.
var excludedIfacePrefixes = []string{
	"docker",  // docker0
	"cni",     // cni0
	"veth",    // container veth pairs
	"cali",    // Calico
	"flannel", // Flannel (flannel.1)
	"cilium",  // Cilium
	"kube",    // kube-ipvs0, kube-bridge
	"nodelocaldns",
	"br-",       // docker/compose bridges "br-<hex>" (NOT "br0")
	"virbr",     // libvirt bridges
	"ovs-",      // Open vSwitch
	"lxcbr",     // LXC bridge
	"lxdbr",     // LXD bridge
	"tunl",      // IPIP tunnels
	"wg",        // WireGuard
	"tailscale", // Tailscale
	"zt",        // ZeroTier
	"dummy",     // dummy devices
}

// isExcludedIface reports whether an interface name matches an excluded prefix.
func isExcludedIface(name string) bool {
	for _, p := range excludedIfacePrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// ifaceInfo is the subset of an interface the filter needs. It exists so the
// filtering policy can be unit-tested without real NICs.
type ifaceInfo struct {
	Name     string
	Up       bool
	Loopback bool
	IPs      []net.IP
}

// filterAddresses applies the reporting policy to a list of interfaces and
// returns the node's routable addresses (InternalIP), deduplicated and sorted
// for deterministic output. It is pure: no host I/O.
func filterAddresses(ifaces []ifaceInfo) []MachineAddress {
	seen := map[string]struct{}{}
	var out []MachineAddress
	for _, ifc := range ifaces {
		if !ifc.Up || ifc.Loopback || isExcludedIface(ifc.Name) {
			continue
		}
		for _, ip := range ifc.IPs {
			// Keep global-unicast (this includes RFC1918 private space, which is
			// typically the node's real LAN address); drop loopback, link-local,
			// multicast and unspecified.
			if ip == nil || !ip.IsGlobalUnicast() || ip.IsLinkLocalUnicast() {
				continue
			}
			s := ip.String()
			if _, dup := seen[s]; dup {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, MachineAddress{Type: AddressInternalIP, Address: s})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Address < out[j].Address })
	return out
}

// DetectAddresses returns the node's current routable network addresses plus its
// hostname. It is cheap (reads net.Interfaces and os.Hostname) and is NOT cached:
// addresses change (DHCP, NIC churn) and callers that report them periodically
// must re-read. Best-effort: on error it returns whatever it has.
func DetectAddresses() []MachineAddress {
	var infos []ifaceInfo
	if ifaces, err := net.Interfaces(); err == nil {
		for _, ifc := range ifaces {
			info := ifaceInfo{
				Name:     ifc.Name,
				Up:       ifc.Flags&net.FlagUp != 0,
				Loopback: ifc.Flags&net.FlagLoopback != 0,
			}
			addrs, err := ifc.Addrs()
			if err != nil {
				continue
			}
			for _, a := range addrs {
				if ipnet, ok := a.(*net.IPNet); ok {
					info.IPs = append(info.IPs, ipnet.IP)
				}
			}
			infos = append(infos, info)
		}
	}

	out := filterAddresses(infos)
	if hn, err := os.Hostname(); err == nil && hn != "" {
		out = append(out, MachineAddress{Type: AddressHostname, Address: hn})
	}
	return out
}
