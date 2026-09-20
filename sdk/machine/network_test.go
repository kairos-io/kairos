package machine_test

import (
	"net"

	"github.com/kairos-io/kairos/v4/sdk/machine"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The specs read the host's own interface list rather than a fixture, and
// build what Interfaces and LocalIPs are supposed to return from it. Any
// machine that can run the suite has a loopback interface, so the comparison
// is never vacuous: a mask that does not filter loopback puts a name and an
// address in one side and not the other.

func loopbackAndRest() (loopback, rest []string) {
	ifaces, err := net.Interfaces()
	Expect(err).ToNot(HaveOccurred())

	for _, i := range ifaces {
		if i.Flags&net.FlagLoopback != 0 {
			loopback = append(loopback, i.Name)
			continue
		}
		rest = append(rest, i.Name)
	}
	return loopback, rest
}

func addressesOf(names []string) (ips []string) {
	for _, name := range names {
		i, err := net.InterfaceByName(name)
		Expect(err).ToNot(HaveOccurred())

		addrs, err := i.Addrs()
		Expect(err).ToNot(HaveOccurred())
		for _, a := range addrs {
			ip, _, err := net.ParseCIDR(a.String())
			if err != nil {
				continue
			}
			ips = append(ips, ip.String())
		}
	}
	return ips
}

var _ = Describe("network", func() {
	Describe("Interfaces", func() {
		It("reports every interface except the loopback ones", func() {
			loopback, rest := loopbackAndRest()
			Expect(loopback).ToNot(BeEmpty(), "the host has no loopback interface, nothing to assert on")

			Expect(machine.Interfaces()).To(Equal(rest))
		})
	})

	Describe("LocalIPs", func() {
		It("reports every address except the loopback ones", func() {
			loopback, rest := loopbackAndRest()
			Expect(addressesOf(loopback)).ToNot(BeEmpty(), "the host's loopback interface has no address, nothing to assert on")

			Expect(machine.LocalIPs()).To(Equal(addressesOf(rest)))
		})
	})
})
