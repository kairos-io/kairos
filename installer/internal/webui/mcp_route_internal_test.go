package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The MCP endpoint is a route on this server rather than a listener of its
// own, so one address serves the browser and the agent and an operator has one
// thing to reason about. These specs pin that it really is mounted here, and
// that the catch-all asset route does not swallow it.
var _ = Describe("the MCP route", func() {
	var srv *httptest.Server
	var reached []string

	// A stand-in for mcp.Handler: this package must not care what the handler
	// does, only that requests to MCPPath arrive at it.
	mount := func(h http.Handler) {
		srv = httptest.NewServer(newServer(Options{MCP: h}))
		DeferCleanup(srv.Close)
	}

	BeforeEach(func() {
		reached = nil
	})

	spy := func() http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reached = append(reached, r.Method)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("mcp"))
		})
	}

	It("serves the handler on the web UI's own listener", func() {
		mount(spy())

		res, err := srv.Client().Post(srv.URL+MCPPath, "application/json", strings.NewReader("{}"))
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(res.Body.Close)

		Expect(res.StatusCode).To(Equal(http.StatusOK))
		Expect(reached).To(Equal([]string{http.MethodPost}))
	})

	// The streamable HTTP transport opens its event stream with GET and ends a
	// session with DELETE, so a POST-only route would leave a client able to
	// call a tool but unable to be told anything back. GET is also the method
	// the embedded-asset catch-all claims, so it is the one that regresses.
	DescribeTable("reaches the handler with the methods the transport uses", func(method string) {
		mount(spy())

		req, err := http.NewRequest(method, srv.URL+MCPPath, nil)
		Expect(err).ToNot(HaveOccurred())

		res, err := srv.Client().Do(req)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(res.Body.Close)

		Expect(res.StatusCode).To(Equal(http.StatusOK))
		Expect(reached).To(Equal([]string{method}))
	},
		Entry("POST, which calls a tool", http.MethodPost),
		Entry("GET, which opens the event stream", http.MethodGet),
		Entry("DELETE, which ends the session", http.MethodDelete),
	)

	// An image that wants the browser installer without the agent one leaves
	// MCP nil, and then nothing must answer on that path.
	It("is not registered at all when no handler was given", func() {
		mount(nil)

		res, err := srv.Client().Post(srv.URL+MCPPath, "application/json", strings.NewReader("{}"))
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(res.Body.Close)

		Expect(res.StatusCode).ToNot(Equal(http.StatusOK))
		Expect(reached).To(BeEmpty())
	})
})
