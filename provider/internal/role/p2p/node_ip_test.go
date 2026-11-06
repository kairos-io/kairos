package role

import (
	"net"

	providerConfig "github.com/kairos-io/kairos/v4/provider/internal/provider/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("node IP on the LAN path", func() {
	var original func() ([]systemInterface, error)
	var ifaces []systemInterface

	lanConfig := func() *providerConfig.Config {
		noVPN := false
		return &providerConfig.Config{
			P2P: &providerConfig.P2P{VPN: providerConfig.VPN{Create: &noVPN}},
		}
	}

	BeforeEach(func() {
		original = listInterfaces
		listInterfaces = func() ([]systemInterface, error) { return ifaces, nil }
	})

	AfterEach(func() {
		listInterfaces = original
	})

	Describe("K3sNode WorkerArgs", func() {
		It("errors when the guessed interface has no address", func() {
			ifaces = []systemInterface{
				{Name: "lo", IPs: []net.IP{net.ParseIP("127.0.0.1")}},
				{Name: "eth0"},
			}
			node := &K3sNode{providerConfig: lanConfig(), role: RoleWorker}

			args, err := node.WorkerArgs()
			Expect(err).To(MatchError("node doesn't have an ip yet"))
			Expect(args).To(BeNil())
		})

		It("errors when the guessed interface only has an IPv6 link-local address", func() {
			ifaces = []systemInterface{
				{Name: "lo", IPs: []net.IP{net.ParseIP("127.0.0.1")}},
				{Name: "eth0", IPs: []net.IP{net.ParseIP("fe80::1")}},
			}
			node := &K3sNode{providerConfig: lanConfig(), role: RoleWorker}

			_, err := node.WorkerArgs()
			Expect(err).To(MatchError("node doesn't have an ip yet"))
		})

		It("uses the first interface that has a usable address", func() {
			ifaces = []systemInterface{
				{Name: "lo", IPs: []net.IP{net.ParseIP("127.0.0.1")}},
				{Name: "eth0", IPs: []net.IP{net.ParseIP("fe80::1")}},
				{Name: "eth1", IPs: []net.IP{net.ParseIP("fe80::2"), net.ParseIP("192.168.1.10")}},
			}
			node := &K3sNode{providerConfig: lanConfig(), role: RoleWorker}

			args, err := node.WorkerArgs()
			Expect(err).ToNot(HaveOccurred())
			Expect(args).To(ContainElement("--node-ip 192.168.1.10"))
			Expect(args).ToNot(ContainElement("--node-ip "))
		})
	})

	Describe("guessInterface", func() {
		It("prefers a non-lo interface with a usable address", func() {
			ifaces = []systemInterface{
				{Name: "lo", IPs: []net.IP{net.ParseIP("127.0.0.1")}},
				{Name: "eth0"},
				{Name: "eth1", IPs: []net.IP{net.ParseIP("fe80::1")}},
				{Name: "eth2", IPs: []net.IP{net.ParseIP("10.0.0.5")}},
			}
			Expect(guessInterface(lanConfig())).To(Equal("eth2"))
		})

		It("falls back to the first non-lo interface when none has a usable address", func() {
			ifaces = []systemInterface{
				{Name: "lo", IPs: []net.IP{net.ParseIP("127.0.0.1")}},
				{Name: "eth0"},
				{Name: "eth1", IPs: []net.IP{net.ParseIP("fe80::1")}},
			}
			Expect(guessInterface(lanConfig())).To(Equal("eth0"))
		})

		It("returns the kube-vip interface when one is configured", func() {
			ifaces = nil
			c := lanConfig()
			c.KubeVIP.Interface = "bond0"
			Expect(guessInterface(c)).To(Equal("bond0"))
		})
	})

	Describe("K3sNode GuessInterface", func() {
		It("does not pick a link-local address as the node IP", func() {
			ifaces = []systemInterface{
				{Name: "lo", IPs: []net.IP{net.ParseIP("127.0.0.1")}},
				{Name: "eth0", IPs: []net.IP{net.ParseIP("fe80::1"), net.ParseIP("192.168.1.10")}},
			}
			node := &K3sNode{providerConfig: lanConfig(), role: RoleMaster}

			node.GuessInterface()
			Expect(node.iface).To(Equal("eth0"))
			Expect(node.ifaceIP).To(Equal("192.168.1.10"))
		})
	})
})
