package auth_test

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/auth"
)

var _ = Describe("OriginAllowed", func() {
	request := func(host, origin string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/groups", nil)
		req.Host = host
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		return req
	}

	DescribeTable("decides by the Origin header",
		func(host, origin, publicURL string, allowed bool) {
			Expect(auth.OriginAllowed(request(host, origin), publicURL)).To(Equal(allowed))
		},
		Entry("no Origin header", "aurora.lan:8080", "", "http://aurora.lan:8080", true),
		Entry("Origin matches the request host", "10.0.0.5:8080", "http://10.0.0.5:8080", "http://aurora.lan:8080", true),
		Entry("Origin matches the public URL, Host does not", "127.0.0.1:8080", "https://aurora.example.com", "https://aurora.example.com", true),
		Entry("foreign Origin", "aurora.lan:8080", "https://evil.example", "http://aurora.lan:8080", false),
		Entry("same host, other port", "aurora.lan:8080", "http://aurora.lan:9090", "http://aurora.lan:8080", false),
		Entry("null Origin", "aurora.lan:8080", "null", "http://aurora.lan:8080", false),
		Entry("unparsable Origin", "aurora.lan:8080", "http://%zz", "http://aurora.lan:8080", false),
		Entry("empty public URL still allows the request host", "aurora.lan:8080", "http://aurora.lan:8080", "", true),
		Entry("host comparison ignores case", "Aurora.LAN:8080", "http://aurora.lan:8080", "", true),
	)

	Describe("logging a refusal", func() {
		var buf bytes.Buffer

		BeforeEach(func() {
			buf.Reset()
			log.SetOutput(&buf)
		})

		AfterEach(func() { log.SetOutput(os.Stderr) })

		It("names the Origin, the request Host and the public URL host", func() {
			Expect(auth.OriginAllowed(request("127.0.0.1:8080", "https://aurora.example.com"), "http://aurora.lan:8080")).To(BeFalse())
			out := buf.String()
			Expect(out).To(ContainSubstring("https://aurora.example.com"))
			Expect(out).To(ContainSubstring("127.0.0.1:8080"))
			Expect(out).To(ContainSubstring("aurora.lan:8080"))
			Expect(bytes.Count(buf.Bytes(), []byte("\n"))).To(Equal(1))
		})

		It("logs nothing for an allowed request", func() {
			Expect(auth.OriginAllowed(request("aurora.lan:8080", "http://aurora.lan:8080"), "")).To(BeTrue())
			Expect(buf.String()).To(BeEmpty())
		})
	})
})
