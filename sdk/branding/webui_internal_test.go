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

var _ = Describe("the web UI token", func() {
	Describe("TokenMatches", func() {
		It("accepts the token the image set", func() {
			Expect(WebUI{Token: "abc"}.TokenMatches("abc")).To(BeTrue())
		})

		It("refuses anything else", func() {
			Expect(WebUI{Token: "abc"}.TokenMatches("abd")).To(BeFalse())
			Expect(WebUI{Token: "abc"}.TokenMatches("ab")).To(BeFalse())
			Expect(WebUI{Token: "abc"}.TokenMatches("abcd")).To(BeFalse())
			Expect(WebUI{Token: "abc"}.TokenMatches("")).To(BeFalse())
		})

		// A caller that forgot HasToken must not be handed an authorized
		// request because the client sent no token either.
		It("refuses every value when the image set no token", func() {
			Expect(WebUI{}.HasToken()).To(BeFalse())
			Expect(WebUI{}.TokenMatches("")).To(BeFalse())
			Expect(WebUI{}.TokenMatches("anything")).To(BeFalse())
		})
	})

	Describe("URLs", func() {
		It("carries the token, so the printed URL and its QR code let the console user in", func() {
			w := WebUI{Token: "abc"}
			Expect(w.urls(ips("192.168.1.10"))).To(Equal([]string{"http://192.168.1.10:8080/?token=abc"}))
		})

		It("escapes a token that would otherwise break the URL", func() {
			w := WebUI{Token: "a b&c=d/e+f"}
			Expect(w.urls(ips("192.168.1.10"))).
				To(Equal([]string{"http://192.168.1.10:8080/?token=a+b%26c%3Dd%2Fe%2Bf"}))
		})

		It("carries the token on a pinned host too", func() {
			w := WebUI{ListenAddress: "192.168.1.5:9000", Token: "abc"}
			Expect(w.urls(ips())).To(Equal([]string{"http://192.168.1.5:9000/?token=abc"}))
		})

		It("leaves the URL alone when the image set no token", func() {
			Expect(WebUI{}.urls(ips("192.168.1.10"))).To(Equal([]string{"http://192.168.1.10:8080"}))
		})
	})
})
