package agent

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("webUIAddresses", func() {
	const listen = ":8080"

	It("keeps the addresses a browser on another machine can reach", func() {
		Expect(webUIAddresses([]string{"192.168.1.50", "10.0.2.15"}, listen)).
			To(Equal([]string{"192.168.1.50:8080", "10.0.2.15:8080"}))
	})

	It("brackets an IPv6 address so the result is a host and a port", func() {
		Expect(webUIAddresses([]string{"2001:db8::1"}, listen)).
			To(Equal([]string{"[2001:db8::1]:8080"}))
	})

	It("drops loopback addresses", func() {
		Expect(webUIAddresses([]string{"127.0.0.1", "::1", "192.168.1.50"}, listen)).
			To(Equal([]string{"192.168.1.50:8080"}))
	})

	It("drops link-local addresses, which need a zone nobody can type here", func() {
		Expect(webUIAddresses([]string{"fe80::5054:ff:fe12:3456", "169.254.10.1", "192.168.1.50"}, listen)).
			To(Equal([]string{"192.168.1.50:8080"}))
	})

	It("drops anything that is not an address", func() {
		Expect(webUIAddresses([]string{"not-an-ip"}, listen)).To(BeEmpty())
	})

	It("says nothing when the listen address carries no port", func() {
		Expect(webUIAddresses([]string{"192.168.1.50"}, "8080")).To(BeEmpty())
	})

	It("says nothing when the listen address has an empty port", func() {
		Expect(webUIAddresses([]string{"192.168.1.50"}, "0.0.0.0:")).To(BeEmpty())
	})
})
