package ws_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/gorilla/websocket"
	gormstore "github.com/kairos-io/AuroraBoot/internal/store/gorm"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/kairos-io/AuroraBoot/pkg/ws"
	"github.com/labstack/echo/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("WebSocket origin and agent credentials", func() {
	var (
		server *httptest.Server
		apiKey string
	)

	BeforeEach(func() {
		n := testCounter.Add(1)
		db, err := gormstore.New(filepath.Join(GinkgoT().TempDir(), fmt.Sprintf("origin_test_%d.db", n)))
		Expect(err).NotTo(HaveOccurred())
		nodes := &gormstore.NodeStoreAdapter{S: db}
		commands := &gormstore.CommandStoreAdapter{S: db}
		node := &store.ManagedNode{MachineID: fmt.Sprintf("origin-machine-%d", n), Hostname: "h", Labels: map[string]string{}}
		Expect(nodes.Register(context.Background(), node)).To(Succeed())
		apiKey = node.APIKey

		hub := ws.NewHub()
		agentHandler := &ws.AgentHandler{Hub: hub, Nodes: nodes, Commands: commands, PublicURL: "https://aurora.example.com"}
		uiHandler := &ws.UIHandler{Hub: hub, PublicURL: "https://aurora.example.com"}
		e := echo.New()
		e.GET("/api/v1/ws", agentHandler.HandleAgentWS)
		e.GET("/api/v1/ws/ui", uiHandler.HandleUIWS)
		server = httptest.NewServer(e)

		DeferCleanup(func() {
			server.Close()
			_ = db.Close()
		})
	})

	dial := func(path string, header http.Header) (*websocket.Conn, *http.Response, error) {
		u := "ws" + strings.TrimPrefix(server.URL, "http") + path
		return websocket.DefaultDialer.Dial(u, header)
	}

	It("refuses a UI upgrade from a foreign origin", func() {
		_, resp, err := dial("/api/v1/ws/ui", http.Header{"Origin": {"https://evil.example"}})
		Expect(err).To(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
	})

	It("accepts a UI upgrade from the public URL's origin and from no origin", func() {
		conn, _, err := dial("/api/v1/ws/ui", http.Header{"Origin": {"https://aurora.example.com"}})
		Expect(err).NotTo(HaveOccurred())
		conn.Close()
		conn, _, err = dial("/api/v1/ws/ui", nil)
		Expect(err).NotTo(HaveOccurred())
		conn.Close()
	})

	It("refuses an agent upgrade from a foreign origin even with a valid key", func() {
		_, resp, err := dial("/api/v1/ws", http.Header{
			"Origin":        {"https://evil.example"},
			"Authorization": {"Bearer " + apiKey},
		})
		Expect(err).To(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
	})

	It("accepts the agent key in the Authorization header", func() {
		conn, _, err := dial("/api/v1/ws", http.Header{"Authorization": {"Bearer " + apiKey}})
		Expect(err).NotTo(HaveOccurred())
		conn.Close()
	})

	It("still accepts the agent key in ?token= for agents already deployed", func() {
		conn, _, err := dial("/api/v1/ws?token="+url.QueryEscape(apiKey), nil)
		Expect(err).NotTo(HaveOccurred())
		conn.Close()
	})

	It("refuses a wrong key in the header without falling back to the query", func() {
		_, resp, err := dial("/api/v1/ws?token="+url.QueryEscape(apiKey), http.Header{"Authorization": {"Bearer wrong"}})
		Expect(err).To(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
	})
})
