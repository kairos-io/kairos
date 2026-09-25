package branding

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// ips returns a lookup that always reports the given addresses, standing in
// for the machine's own interfaces.
func ips(addrs ...string) func() []string {
	return func() []string { return addrs }
}

var _ = Describe("WebUI.URLs", func() {
	It("offers one URL per local address when the image pins no listen address", func() {
		w := WebUI{}
		Expect(w.urls(ips("192.168.1.10", "10.0.2.15"))).To(Equal([]string{
			"http://192.168.1.10:8080",
			"http://10.0.2.15:8080",
		}))
	})

	It("keeps the branded port", func() {
		w := WebUI{ListenAddress: ":9000"}
		Expect(w.urls(ips("192.168.1.10"))).To(Equal([]string{"http://192.168.1.10:9000"}))
	})

	It("treats a wildcard host the same as no host", func() {
		Expect(WebUI{ListenAddress: "0.0.0.0:8080"}.urls(ips("192.168.1.10"))).
			To(Equal([]string{"http://192.168.1.10:8080"}))
		Expect(WebUI{ListenAddress: "[::]:8080"}.urls(ips("192.168.1.10"))).
			To(Equal([]string{"http://192.168.1.10:8080"}))
	})

	It("answers with the pinned host alone, without asking the interfaces", func() {
		w := WebUI{ListenAddress: "192.168.1.5:9000"}
		Expect(w.urls(func() []string {
			Fail("a pinned host must not need the local addresses")
			return nil
		})).To(Equal([]string{"http://192.168.1.5:9000"}))
	})

	It("brackets an IPv6 address", func() {
		Expect(WebUI{}.urls(ips("2001:db8::1"))).To(Equal([]string{"http://[2001:db8::1]:8080"}))
	})

	It("drops the addresses another machine cannot open", func() {
		Expect(WebUI{}.urls(ips("127.0.0.1", "::1", "169.254.3.4", "fe80::1", "not-an-ip"))).To(BeEmpty())
	})

	It("offers nothing when the image disabled the web UI", func() {
		Expect(WebUI{Disable: true}.urls(ips("192.168.1.10"))).To(BeEmpty())
	})

	It("offers nothing when the host has no usable address", func() {
		Expect(WebUI{}.urls(ips())).To(BeEmpty())
	})

	It("offers nothing when the branded listen address has no port", func() {
		Expect(WebUI{ListenAddress: "192.168.1.5"}.urls(ips("192.168.1.10"))).To(BeEmpty())
	})
})
