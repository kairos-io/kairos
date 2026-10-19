package phonehome

import (
	"net/url"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("logSafeURL", func() {
	render := func(raw string) string {
		u, err := url.Parse(raw)
		Expect(err).ToNot(HaveOccurred())
		return logSafeURL(u)
	}

	It("keeps the server, the path and the parameter names", func() {
		Expect(render("wss://server.example/api/v1/ws?token=secret-key&group=edge")).
			To(Equal("wss://server.example/api/v1/ws?group=edge&token=REDACTED"))
	})

	It("matches the parameter name whatever its case", func() {
		Expect(render("wss://server.example/api/v1/ws?Token=secret-key")).
			ToNot(ContainSubstring("secret-key"))
	})

	It("leaves a URL with nothing to hide exactly as it is", func() {
		raw := "https://server.example/api/v1/nodes/register"
		Expect(render(raw)).To(Equal(raw))
	})

	// A password in the configured phonehome url reaches the same log line
	// through the same string, so it has to go the same way.
	It("hides a password written into the configured url", func() {
		out := render("wss://node:hunter2@server.example/api/v1/ws?token=secret-key")
		Expect(out).ToNot(ContainSubstring("hunter2"))
		Expect(out).ToNot(ContainSubstring("secret-key"))
		Expect(out).To(ContainSubstring("node:"))
	})
})
