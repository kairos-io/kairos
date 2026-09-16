package utils

import (
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("early-boot networking", func() {
	Describe("ParseNetworkdDNS", func() {
		It("reads the servers off a networkd state file", func() {
			// Shape of a real /run/systemd/netif/state, including the keys
			// that start with DNS but are not the server list.
			state := `OPER_STATE=routable
CARRIER_STATE=carrier
ADDRESS_STATE=routable
DNS=192.168.1.1 1.1.1.1
NTP=192.168.1.1
DOMAINS=lan
DNSSEC=no
DNS_OVER_TLS=no
`
			Expect(ParseNetworkdDNS(state)).To(Equal([]string{"192.168.1.1", "1.1.1.1"}))
		})

		It("does not read DNSSEC or DNS_OVER_TLS as servers", func() {
			Expect(ParseNetworkdDNS("DNSSEC=allow-downgrade\nDNS_OVER_TLS=opportunistic\n")).To(BeEmpty())
		})

		It("strips the server name and the interface suffix", func() {
			Expect(ParseNetworkdDNS("DNS=1.1.1.1#cloudflare-dns.com fe80::1%eth0\n")).
				To(Equal([]string{"1.1.1.1", "fe80::1"}))
		})

		It("drops anything that is not an address", func() {
			Expect(ParseNetworkdDNS("DNS=not-an-ip 9.9.9.9\n")).To(Equal([]string{"9.9.9.9"}))
		})

		It("keeps the first of a repeated server", func() {
			Expect(ParseNetworkdDNS("DNS=8.8.8.8 8.8.4.4\nDNS=8.8.8.8\n")).
				To(Equal([]string{"8.8.8.8", "8.8.4.4"}))
		})

		It("reports nothing when networkd learned nothing", func() {
			Expect(ParseNetworkdDNS("OPER_STATE=degraded\nDNS=\n")).To(BeEmpty())
		})
	})

	Describe("RenderResolvConf", func() {
		It("writes one nameserver line per server, in order", func() {
			Expect(RenderResolvConf([]string{"192.168.1.1", "1.1.1.1"})).To(Equal(
				"# Written by immucore for early-boot networking in UKI mode.\n" +
					"nameserver 192.168.1.1\n" +
					"nameserver 1.1.1.1\n"))
		})
	})

	Describe("ResolvConfTarget", func() {
		var root string

		BeforeEach(func() {
			root = GinkgoT().TempDir()
			Expect(os.MkdirAll(filepath.Join(root, "etc"), 0755)).To(Succeed())
		})

		It("returns the file itself when it is a regular file", func() {
			plain := filepath.Join(root, "etc", "resolv.conf")
			Expect(os.WriteFile(plain, []byte("nameserver 1.1.1.1\n"), 0644)).To(Succeed())
			Expect(ResolvConfTarget(root)).To(Equal(plain))
		})

		It("returns the file itself when it does not exist yet", func() {
			Expect(ResolvConfTarget(root)).To(Equal(filepath.Join(root, "etc", "resolv.conf")))
		})

		It("follows the resolved stub symlink images ship, so the link survives", func() {
			// systemd-resolved images ship /etc/resolv.conf as a symlink into
			// /run. resolved is not running in the initrd, so the target has
			// to be created; replacing the link would be wrong.
			Expect(os.Symlink("/run/systemd/resolve/stub-resolv.conf", filepath.Join(root, "etc", "resolv.conf"))).To(Succeed())
			Expect(ResolvConfTarget(root)).To(Equal(filepath.Join(root, "run", "systemd", "resolve", "stub-resolv.conf")))
		})

		It("resolves a relative symlink against the link's directory", func() {
			Expect(os.Symlink("../run/resolv.conf", filepath.Join(root, "etc", "resolv.conf"))).To(Succeed())
			Expect(ResolvConfTarget(root)).To(Equal(filepath.Join(root, "run", "resolv.conf")))
		})

		It("refuses a symlink that escapes the root", func() {
			Expect(os.Symlink("../../../../../etc/passwd", filepath.Join(root, "etc", "resolv.conf"))).To(Succeed())
			target, err := ResolvConfTarget(root)
			Expect(err).To(HaveOccurred())
			// Still names the link so the caller has somewhere safe to point.
			Expect(target).To(Equal(filepath.Join(root, "etc", "resolv.conf")))
		})
	})

	Describe("hasNetworkConfig", func() {
		var root string

		BeforeEach(func() {
			root = GinkgoT().TempDir()
			Expect(os.MkdirAll(filepath.Join(root, "etc/systemd/network"), 0755)).To(Succeed())
		})

		It("reports nothing when the directory is empty", func() {
			Expect(hasNetworkConfig(root, "/etc/systemd/network")).To(BeFalse())
		})

		It("reports nothing when the directory is missing", func() {
			Expect(hasNetworkConfig(root, "/usr/lib/systemd/network")).To(BeFalse())
		})

		It("finds a .network the image ships", func() {
			Expect(os.WriteFile(filepath.Join(root, "etc/systemd/network/10-wired.network"), []byte("[Match]\n"), 0644)).To(Succeed())
			Expect(hasNetworkConfig(root, "/etc/systemd/network")).To(BeTrue())
		})

		It("finds a .link and a .netdev too", func() {
			Expect(os.WriteFile(filepath.Join(root, "etc/systemd/network/99-default.link"), []byte(""), 0644)).To(Succeed())
			Expect(hasNetworkConfig(root, "/etc/systemd/network")).To(BeTrue())
		})

		It("ignores files that are not networkd configuration", func() {
			Expect(os.WriteFile(filepath.Join(root, "etc/systemd/network/README.md"), []byte(""), 0644)).To(Succeed())
			Expect(os.MkdirAll(filepath.Join(root, "etc/systemd/network/10-wired.network.d"), 0755)).To(Succeed())
			Expect(hasNetworkConfig(root, "/etc/systemd/network")).To(BeFalse())
		})

		It("does not count systemd's own vendor defaults as configuration", func() {
			// The exact layout hadron-trusted v0.5.2 ships: /etc empty, and
			// /usr/lib full of drop-ins that match containers and tunnels,
			// never a wired NIC. Counting these would leave the NIC down.
			Expect(os.MkdirAll(filepath.Join(root, "usr/lib/systemd/network"), 0755)).To(Succeed())
			for _, name := range []string{"80-container-host0.network", "80-wifi-adhoc.network", "99-default.link"} {
				Expect(os.WriteFile(filepath.Join(root, "usr/lib/systemd/network", name), []byte(""), 0644)).To(Succeed())
			}
			Expect(hasNetworkConfig(root, networkConfigDirs...)).To(BeFalse())
			// and the caller must be using exactly that list.
			Expect(networkConfigDirs).NotTo(ContainElement("/usr/lib/systemd/network"))
			Expect(networkConfigDirs).To(ConsistOf("/etc/systemd/network", "/run/systemd/network"))
		})

		It("searches every directory it is given", func() {
			Expect(os.MkdirAll(filepath.Join(root, "run/systemd/network"), 0755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(root, "run/systemd/network/50-dhcp.network"), []byte(""), 0644)).To(Succeed())
			Expect(hasNetworkConfig(root, "/etc/systemd/network", "/run/systemd/network")).To(BeTrue())
		})
	})

	Describe("findNetworkd", func() {
		var root string

		BeforeEach(func() {
			root = GinkgoT().TempDir()
		})

		It("reports nothing when the image ships no networkd", func() {
			Expect(findNetworkd(root)).To(BeEmpty())
		})

		It("finds the binary at the usual path", func() {
			Expect(os.MkdirAll(filepath.Join(root, "usr/lib/systemd"), 0755)).To(Succeed())
			bin := filepath.Join(root, "usr/lib/systemd/systemd-networkd")
			Expect(os.WriteFile(bin, []byte("#!/bin/sh\n"), 0755)).To(Succeed())
			Expect(findNetworkd(root)).To(Equal(bin))
		})

		It("skips a path that is not executable", func() {
			Expect(os.MkdirAll(filepath.Join(root, "usr/lib/systemd"), 0755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(root, "usr/lib/systemd/systemd-networkd"), []byte(""), 0644)).To(Succeed())
			Expect(findNetworkd(root)).To(BeEmpty())
		})

		It("falls back to the other known locations", func() {
			Expect(os.MkdirAll(filepath.Join(root, "lib/systemd"), 0755)).To(Succeed())
			bin := filepath.Join(root, "lib/systemd/systemd-networkd")
			Expect(os.WriteFile(bin, []byte("#!/bin/sh\n"), 0755)).To(Succeed())
			Expect(findNetworkd(root)).To(Equal(bin))
		})
	})

	Describe("waitForAddress", func() {
		var restore func()

		AfterEach(func() { restore() })

		swap := func(fn func() bool) {
			original := hasRoutableAddress
			hasRoutableAddress = fn
			restore = func() { hasRoutableAddress = original }
		}

		It("returns as soon as an interface has an address", func() {
			swap(func() bool { return true })
			Expect(waitForAddress(context.Background(), time.Minute)).To(Succeed())
		})

		It("gives up after the timeout instead of hanging the boot", func(_ SpecContext) {
			swap(func() bool { return false })
			start := time.Now()
			err := waitForAddress(context.Background(), 600*time.Millisecond)
			Expect(err).To(MatchError(ContainSubstring("no interface got an address within")))
			Expect(time.Since(start)).To(BeNumerically("<", 5*time.Second))
		}, SpecTimeout(20*time.Second))

		It("stops waiting when the address turns up late but in time", func(_ SpecContext) {
			calls := 0
			swap(func() bool {
				calls++
				return calls > 3
			})
			Expect(waitForAddress(context.Background(), time.Minute)).To(Succeed())
			Expect(calls).To(Equal(4))
		}, SpecTimeout(20*time.Second))

		It("gives up when the context is canceled", func(_ SpecContext) {
			swap(func() bool { return false })
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			Expect(waitForAddress(ctx, time.Minute)).To(MatchError(context.Canceled))
		}, SpecTimeout(20*time.Second))
	})

	Describe("FallbackNetworkUnit", func() {
		It("asks DHCP for an address and for the DNS servers", func() {
			unit := FallbackNetworkUnit()
			Expect(unit).To(ContainSubstring("DHCP=yes"))
			Expect(unit).To(ContainSubstring("UseDNS=yes"))
			Expect(unit).To(ContainSubstring("Type=ether"))
		})

		It("is numbered last so image configuration wins", func() {
			// networkd applies the first matching file in lexical order, so
			// anything the image ships under /etc must sort before ours.
			Expect(networkdFallbackUnit).To(HavePrefix("99-"))
			Expect(networkdRuntimeUnitDir).To(Equal("/run/systemd/network"))
		})
	})
})
