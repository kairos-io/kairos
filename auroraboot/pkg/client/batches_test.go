package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/client"
)

var _ = Describe("BatchesService", func() {
	const batchJSON = `{"id":"b1","command":"upgrade","selection":"group edge","groupID":"g1","concurrency":2,
		"stopOnFailure":true,"rebootTimeoutSeconds":600,"phase":"stopped","stopReason":"node failed",
		"stoppedByNodeID":"n1","counts":{"failed":1,"canceled":1},
		"nodes":[{"batchID":"b1","nodeID":"n1","position":0,"hostname":"h1","state":"failed","message":"boom"},
		{"batchID":"b1","nodeID":"n2","position":1,"hostname":"h2","state":"canceled"}]}`

	var (
		server    *httptest.Server
		gotMethod string
		gotPath   string
		gotRaw    string
		gotQuery  string
		gotBody   map[string]any
		status    int
		reply     string
		cli       *client.Client
	)

	BeforeEach(func() {
		gotBody, status, reply = nil, http.StatusOK, batchJSON
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotMethod, gotPath, gotRaw, gotQuery = r.Method, r.URL.Path, r.URL.EscapedPath(), r.URL.RawQuery
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &gotBody)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(reply))
		}))
		DeferCleanup(server.Close)
		cli = client.New(server.URL, client.WithAdminPassword("s3cret"))
	})

	Describe("Get", func() {
		It("reads the batch with its nodes", func() {
			b, err := cli.Batches.Get(context.Background(), "b1")
			Expect(err).NotTo(HaveOccurred())
			Expect(gotMethod).To(Equal(http.MethodGet))
			Expect(gotPath).To(Equal("/api/v1/batches/b1"))
			Expect(b.Phase).To(Equal("stopped"))
			Expect(b.GroupID).To(HaveValue(Equal("g1")))
			Expect(b.StoppedByNodeID).To(HaveValue(Equal("n1")))
			Expect(b.Counts).To(HaveKeyWithValue("failed", 1))
			Expect(b.Nodes).To(HaveLen(2))
			Expect(b.Nodes[0].State).To(Equal("failed"))
			Expect(b.Nodes[1].State).To(Equal("canceled"))
		})

		It("escapes the id in the path", func() {
			_, err := cli.Batches.Get(context.Background(), "a/b c")
			Expect(err).NotTo(HaveOccurred())
			Expect(gotRaw).To(Equal("/api/v1/batches/a%2Fb%20c"))
		})

		It("returns a not-found error for an unknown batch", func() {
			status, reply = http.StatusNotFound, `{"error":"batch not found"}`
			_, err := cli.Batches.Get(context.Background(), "nope")
			Expect(client.IsNotFound(err)).To(BeTrue())
		})
	})

	Describe("List", func() {
		It("sends the options in the query string and reads the page", func() {
			reply = `{"batches":[` + batchJSON + `],"total":7}`
			l, err := cli.Batches.List(context.Background(), client.BatchListOptions{GroupID: "g1", NodeID: "n1", Limit: 5, Offset: 10})
			Expect(err).NotTo(HaveOccurred())
			Expect(gotPath).To(Equal("/api/v1/batches"))
			Expect(gotQuery).To(Equal("groupID=g1&limit=5&nodeID=n1&offset=10"))
			Expect(l.Total).To(Equal(7))
			Expect(l.Batches).To(HaveLen(1))
		})

		It("sends no query string for empty options", func() {
			reply = `{"batches":[],"total":0}`
			_, err := cli.Batches.List(context.Background(), client.BatchListOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(gotQuery).To(BeEmpty())
		})
	})

	Describe("Stop", func() {
		It("posts to the stop endpoint and returns the batch", func() {
			b, err := cli.Batches.Stop(context.Background(), "b1")
			Expect(err).NotTo(HaveOccurred())
			Expect(gotMethod).To(Equal(http.MethodPost))
			Expect(gotPath).To(Equal("/api/v1/batches/b1/stop"))
			Expect(b.ID).To(Equal("b1"))
		})

		It("escapes the id in the path", func() {
			_, err := cli.Batches.Stop(context.Background(), "a/b c")
			Expect(err).NotTo(HaveOccurred())
			Expect(gotRaw).To(Equal("/api/v1/batches/a%2Fb%20c/stop"))
		})

		It("returns an APIError on 409 when the batch is not running", func() {
			status, reply = http.StatusConflict, `{"error":"batch is not running"}`
			_, err := cli.Batches.Stop(context.Background(), "b1")
			Expect(client.IsConflict(err)).To(BeTrue())
			var apiErr *client.APIError
			Expect(err).To(MatchError(ContainSubstring("batch is not running")))
			Expect(errors.As(err, &apiErr)).To(BeTrue())
			Expect(apiErr.StatusCode).To(Equal(http.StatusConflict))
		})
	})

	Describe("starting a batch", func() {
		It("sends the rollout options of a group command and returns the batch", func() {
			stop := false
			b, err := cli.Groups.SendCommand(context.Background(), "g1", client.GroupCommandRequest{
				Command:        "upgrade",
				RolloutOptions: client.RolloutOptions{Concurrency: 2, StopOnFailure: &stop, RebootTimeoutSeconds: 120},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(b.ID).To(Equal("b1"))
			Expect(gotBody).To(HaveKeyWithValue("concurrency", BeNumerically("==", 2)))
			Expect(gotBody).To(HaveKeyWithValue("stopOnFailure", false))
			Expect(gotBody).To(HaveKeyWithValue("rebootTimeoutSeconds", BeNumerically("==", 120)))
			Expect(gotBody).NotTo(HaveKey("failFast"))
		})

		It("omits unset rollout options of a bulk command and returns the batch", func() {
			b, err := cli.Commands.CreateBulk(context.Background(), client.BulkCommandRequest{
				Selector: client.CommandSelector{GroupID: "g1"},
				Command:  "reboot",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(b.Phase).To(Equal("stopped"))
			Expect(gotBody).NotTo(HaveKey("concurrency"))
			Expect(gotBody).NotTo(HaveKey("stopOnFailure"))
			Expect(gotBody).NotTo(HaveKey("rebootTimeoutSeconds"))
		})

		It("returns the busy nodes of a 409 in the APIError", func() {
			status, reply = http.StatusConflict, `{"error":"nodes are already in a running batch","nodeIDs":["n1","n2"]}`
			_, err := cli.Groups.SendCommand(context.Background(), "g1", client.GroupCommandRequest{Command: "upgrade"})
			var apiErr *client.APIError
			Expect(errors.As(err, &apiErr)).To(BeTrue())
			Expect(apiErr.StatusCode).To(Equal(http.StatusConflict))
			Expect(apiErr.NodeIDs).To(Equal([]string{"n1", "n2"}))

			_, err = cli.Commands.CreateBulk(context.Background(), client.BulkCommandRequest{
				Selector: client.CommandSelector{GroupID: "g1"},
				Command:  "upgrade",
			})
			Expect(errors.As(err, &apiErr)).To(BeTrue())
			Expect(apiErr.NodeIDs).To(Equal([]string{"n1", "n2"}))
		})
	})
})
