package client_test

import (
	"context"
	"net/http"
	"net/http/httptest"

	"github.com/gorilla/websocket"
	"github.com/kairos-io/AuroraBoot/pkg/client"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("WebSocket dialing", func() {
	var (
		gotHeader http.Header
		gotQuery  string
		server    *httptest.Server
	)

	BeforeEach(func() {
		up := websocket.Upgrader{}
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotHeader = r.Header.Clone()
			gotQuery = r.URL.RawQuery
			conn, err := up.Upgrade(w, r, nil)
			if err == nil {
				conn.Close()
			}
		}))
		DeferCleanup(server.Close)
	})

	It("sends the admin password for the UI socket in the Authorization header, not the URL", func() {
		c := client.New(server.URL, client.WithAdminPassword("admin-pass"))
		conn, err := c.DialUIWS(context.Background())
		Expect(err).NotTo(HaveOccurred())
		conn.Close()
		Expect(gotHeader.Get("Authorization")).To(Equal("Bearer admin-pass"))
		Expect(gotQuery).To(BeEmpty())
	})

	It("sends the node key for the agent socket in the Authorization header, not the URL", func() {
		c := client.New(server.URL, client.WithNodeAPIKey("node-key"))
		conn, err := c.DialAgentWS(context.Background())
		Expect(err).NotTo(HaveOccurred())
		conn.Close()
		Expect(gotHeader.Get("Authorization")).To(Equal("Bearer node-key"))
		Expect(gotQuery).To(BeEmpty())
	})
})
