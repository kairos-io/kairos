package state

import (
	"net"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("filterAddresses", func() {
	ip := func(s string) net.IP { return net.ParseIP(s) }

	It("reports routable NIC addresses and drops loopback, down, link-local and virtual interfaces", func() {
		ifaces := []ifaceInfo{
			{Name: "lo", Up: true, Loopback: true, IPs: []net.IP{ip("127.0.0.1"), ip("::1")}},
			{Name: "eth0", Up: true, IPs: []net.IP{ip("10.0.10.21"), ip("fe80::1")}}, // routable + link-local
			{Name: "eth1", Up: false, IPs: []net.IP{ip("10.0.20.5")}},                // down => skip
			{Name: "docker0", Up: true, IPs: []net.IP{ip("172.17.0.1")}},             // container bridge
			{Name: "cali123", Up: true, IPs: []net.IP{ip("10.244.1.1")}},             // CNI
			{Name: "vethabcd", Up: true, IPs: []net.IP{ip("10.244.1.2")}},            // veth pair
			{Name: "br-9f3a2b", Up: true, IPs: []net.IP{ip("172.18.0.1")}},           // docker/compose bridge
			{Name: "br0", Up: true, IPs: []net.IP{ip("192.168.50.10")}},              // REAL bridge => keep
			{Name: "eth2", Up: true, IPs: []net.IP{ip("169.254.1.1")}},               // link-local only => drop
		}

		got := filterAddresses(ifaces)

		addrs := make([]string, 0, len(got))
		for _, a := range got {
			Expect(a.Type).To(Equal(AddressInternalIP))
			addrs = append(addrs, a.Address)
		}
		// Only the real NIC and the real bridge; deterministic sorted order.
		Expect(addrs).To(Equal([]string{"10.0.10.21", "192.168.50.10"}))
	})

	It("keeps IPv6 global-unicast and drops IPv6 link-local", func() {
		got := filterAddresses([]ifaceInfo{
			{Name: "eth0", Up: true, IPs: []net.IP{ip("2001:db8::5"), ip("fe80::a")}},
		})
		Expect(got).To(HaveLen(1))
		Expect(got[0].Address).To(Equal("2001:db8::5"))
	})

	It("deduplicates the same IP seen on multiple interfaces", func() {
		got := filterAddresses([]ifaceInfo{
			{Name: "eth0", Up: true, IPs: []net.IP{ip("10.0.0.5")}},
			{Name: "eth0.100", Up: true, IPs: []net.IP{ip("10.0.0.5")}}, // VLAN sub-iface, same IP
		})
		Expect(got).To(HaveLen(1))
	})

	It("returns nothing when there are no routable addresses", func() {
		got := filterAddresses([]ifaceInfo{
			{Name: "lo", Up: true, Loopback: true, IPs: []net.IP{ip("127.0.0.1")}},
			{Name: "cni0", Up: true, IPs: []net.IP{ip("10.244.0.1")}},
		})
		Expect(got).To(BeEmpty())
	})
})

var _ = DescribeTable("isExcludedIface",
	func(name string, excluded bool) { Expect(isExcludedIface(name)).To(Equal(excluded)) },
	Entry("docker0", "docker0", true),
	Entry("cni0", "cni0", true),
	Entry("calico veth", "cali1a2b3c", true),
	Entry("veth pair", "vethabcd", true),
	Entry("flannel", "flannel.1", true),
	Entry("compose bridge br-<hex>", "br-9f3a2b", true),
	Entry("wireguard", "wg0", true),
	Entry("tailscale", "tailscale0", true),
	Entry("real NIC eth0", "eth0", false),
	Entry("predictable NIC enp1s0", "enp1s0", false),
	Entry("real bridge br0", "br0", false),
	Entry("bond master bond0", "bond0", false),
	Entry("VLAN sub-interface", "eth0.100", false),
)

var _ = Describe("DetectAddresses", func() {
	It("returns at least the hostname on any host", func() {
		// Cheap smoke test against the real host: hostname is always present.
		got := DetectAddresses()
		var hasHostname bool
		for _, a := range got {
			if a.Type == AddressHostname {
				hasHostname = true
			}
		}
		Expect(hasHostname).To(BeTrue())
	})
})
