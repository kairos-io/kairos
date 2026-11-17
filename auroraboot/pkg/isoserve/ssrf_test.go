package isoserve_test

import (
	"github.com/kairos-io/AuroraBoot/pkg/isoserve"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ValidateMediaURL", func() {
	DescribeTable("rejects unsafe URLs",
		func(raw string) {
			Expect(isoserve.ValidateMediaURL(raw)).To(HaveOccurred())
		},
		Entry("loopback IPv4", "http://127.0.0.1/kairos.iso"),
		Entry("loopback IPv4 range", "http://127.1.2.3/kairos.iso"),
		Entry("cloud metadata", "http://169.254.169.254/latest/meta-data/"),
		Entry("link-local IPv4", "http://169.254.10.5/kairos.iso"),
		Entry("loopback IPv6", "http://[::1]/kairos.iso"),
		Entry("link-local IPv6", "http://[fe80::1]/kairos.iso"),
		Entry("unspecified IPv4", "http://0.0.0.0/kairos.iso"),
		Entry("unspecified IPv6", "http://[::]/kairos.iso"),
		Entry("file scheme", "file:///etc/passwd"),
		Entry("ftp scheme", "ftp://10.0.0.5/kairos.iso"),
		Entry("missing host", "http:///kairos.iso"),
		Entry("localhost name", "http://localhost/kairos.iso"),
	)

	DescribeTable("accepts safe URLs",
		func(raw string) {
			Expect(isoserve.ValidateMediaURL(raw)).To(Succeed())
		},
		Entry("RFC1918 10.x serve host", "http://10.10.0.5:8090/redfish/iso/abc/kairos.iso"),
		Entry("RFC1918 192.168 host", "https://192.168.1.50/kairos.iso"),
		Entry("public-ish IP", "http://93.184.216.34/kairos.iso"),
	)
})

var _ = Describe("URLUsesHTTPS", func() {
	DescribeTable("derives the transfer protocol from the URL scheme",
		func(raw string, wantHTTPS bool) {
			got, err := isoserve.URLUsesHTTPS(raw)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(wantHTTPS))
		},
		Entry("https scheme advertises HTTPS", "https://10.0.0.5/x.iso", true),
		Entry("http scheme advertises HTTP", "http://10.0.0.5/x.iso", false),
		Entry("scheme match is case-insensitive", "HTTPS://10.0.0.5/x.iso", true),
		Entry("surrounding whitespace is ignored", "  https://10.0.0.5/x.iso  ", true),
	)

	It("reports the parse error for a malformed URL", func() {
		_, err := isoserve.URLUsesHTTPS("http://[::1")
		Expect(err).To(HaveOccurred())
	})
})
