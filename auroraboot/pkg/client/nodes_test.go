package client_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/client"
)

var _ = Describe("NodesService", func() {
	var (
		server  *httptest.Server
		gotBody map[string]any
		reply   string
		cli     *client.Client
	)

	BeforeEach(func() {
		gotBody, reply = nil, `{"status":"ok"}`
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &gotBody)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(reply))
		}))
		DeferCleanup(server.Close)
		cli = client.New(server.URL, client.WithAdminPassword("s3cret"))
	})

	Describe("SetGroup", func() {
		It("sends the group to move the node into", func() {
			Expect(cli.Nodes.SetGroup(context.Background(), "n1", new("grp-1"))).To(Succeed())
			Expect(gotBody).To(HaveKeyWithValue("groupID", "grp-1"))
		})

		It("sends a null group to move the node out of its group", func() {
			Expect(cli.Nodes.SetGroup(context.Background(), "n1", nil)).To(Succeed())
			Expect(gotBody).To(HaveKeyWithValue("groupID", BeNil()))
		})
	})

	Describe("Get", func() {
		It("reads a null group as no group", func() {
			reply = `{"id":"n1","machineID":"m1","groupID":null,"phase":"Online"}`
			n, err := cli.Nodes.Get(context.Background(), "n1")
			Expect(err).NotTo(HaveOccurred())
			Expect(n.GroupID).To(BeNil())
		})

		It("reads the group the node is in", func() {
			reply = `{"id":"n1","machineID":"m1","groupID":"grp-1","phase":"Online"}`
			n, err := cli.Nodes.Get(context.Background(), "n1")
			Expect(err).NotTo(HaveOccurred())
			Expect(n.GroupID).To(HaveValue(Equal("grp-1")))
		})
	})
})
