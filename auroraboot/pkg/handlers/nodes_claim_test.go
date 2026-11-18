package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/kairos-io/AuroraBoot/pkg/ws"
	"github.com/labstack/echo/v4"
)

var _ = Describe("NodeHandler claim/release", func() {
	var (
		e       *echo.Echo
		ns      *fakeNodeStore
		gs      *fakeGroupStore
		handler *handlers.NodeHandler
	)

	BeforeEach(func() {
		e = echo.New()
		ns = &fakeNodeStore{}
		gs = &fakeGroupStore{}
		handler = handlers.NewNodeHandler(ns, &fakeCommandStore{}, gs, ws.NewHub(), "reg-token", "http://localhost:8080")
	})

	// claim drives POST /api/v1/groups/:id/claim and returns the recorder.
	claim := func(groupID, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/groups/"+groupID+"/claim", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues(groupID)
		Expect(handler.Claim(c)).To(Succeed())
		return rec
	}

	// release drives POST /api/v1/nodes/:nodeID/release.
	release := func(nodeID, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/"+nodeID+"/release", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("nodeID")
		c.SetParamValues(nodeID)
		Expect(handler.Release(c)).To(Succeed())
		return rec
	}

	Describe("Claim", func() {
		BeforeEach(func() {
			gs.groups = []*store.NodeGroup{{ID: "g1", Name: "prod"}}
		})

		It("claims an unclaimed node and returns it", func() {
			ns.nodes = []*store.ManagedNode{{ID: "n1", GroupID: new("g1")}}

			rec := claim("g1", `{"claimKey":"machine-a"}`)
			Expect(rec.Code).To(Equal(http.StatusOK))

			var node store.ManagedNode
			Expect(json.Unmarshal(rec.Body.Bytes(), &node)).To(Succeed())
			Expect(node.ID).To(Equal("n1"))
			Expect(node.ClaimKey).NotTo(BeNil())
			Expect(*node.ClaimKey).To(Equal("machine-a"))
		})

		It("is idempotent for the same key", func() {
			ns.nodes = []*store.ManagedNode{{ID: "n1", GroupID: new("g1")}, {ID: "n2", GroupID: new("g1")}}

			first := claim("g1", `{"claimKey":"machine-a"}`)
			second := claim("g1", `{"claimKey":"machine-a"}`)

			var a, b store.ManagedNode
			Expect(json.Unmarshal(first.Body.Bytes(), &a)).To(Succeed())
			Expect(json.Unmarshal(second.Body.Bytes(), &b)).To(Succeed())
			Expect(b.ID).To(Equal(a.ID))
		})

		It("returns 409 NoCapacity when the group is exhausted", func() {
			ns.nodes = []*store.ManagedNode{{ID: "n1", GroupID: new("g1")}}
			claim("g1", `{"claimKey":"machine-a"}`) // takes the only node

			rec := claim("g1", `{"claimKey":"machine-b"}`)
			Expect(rec.Code).To(Equal(http.StatusConflict))

			var apiErr handlers.APIError
			Expect(json.Unmarshal(rec.Body.Bytes(), &apiErr)).To(Succeed())
			Expect(apiErr.Code).To(Equal(handlers.ClaimErrorCodeNoCapacity))
		})

		It("returns 404 for an unknown group", func() {
			rec := claim("nope", `{"claimKey":"machine-a"}`)
			Expect(rec.Code).To(Equal(http.StatusNotFound))
		})

		It("returns 400 when claimKey is missing", func() {
			rec := claim("g1", `{}`)
			Expect(rec.Code).To(Equal(http.StatusBadRequest))
		})
	})

	Describe("Release", func() {
		It("releases a claim held by the caller", func() {
			key := "machine-a"
			ns.nodes = []*store.ManagedNode{{ID: "n1", GroupID: new("g1"), ClaimKey: &key}}

			rec := release("n1", `{"claimKey":"machine-a"}`)
			Expect(rec.Code).To(Equal(http.StatusOK))

			var resp handlers.APIReleaseResponse
			Expect(json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp.Released).To(BeTrue())
			Expect(ns.nodes[0].ClaimKey).To(BeNil())
		})

		It("returns 409 ClaimMismatch when a different key owns the node", func() {
			key := "machine-a"
			ns.nodes = []*store.ManagedNode{{ID: "n1", GroupID: new("g1"), ClaimKey: &key}}

			rec := release("n1", `{"claimKey":"machine-b"}`)
			Expect(rec.Code).To(Equal(http.StatusConflict))

			var apiErr handlers.APIError
			Expect(json.Unmarshal(rec.Body.Bytes(), &apiErr)).To(Succeed())
			Expect(apiErr.Code).To(Equal(handlers.ClaimErrorCodeClaimMismatch))
			Expect(ns.nodes[0].ClaimKey).NotTo(BeNil()) // untouched
		})

		It("is a no-op (released=false, 200) on an unclaimed node", func() {
			ns.nodes = []*store.ManagedNode{{ID: "n1", GroupID: new("g1")}}

			rec := release("n1", `{"claimKey":"machine-a"}`)
			Expect(rec.Code).To(Equal(http.StatusOK))

			var resp handlers.APIReleaseResponse
			Expect(json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp.Released).To(BeFalse())
		})

		It("returns 404 for a missing node", func() {
			rec := release("ghost", `{"claimKey":"machine-a"}`)
			Expect(rec.Code).To(Equal(http.StatusNotFound))
		})

		It("returns 400 when claimKey is missing", func() {
			ns.nodes = []*store.ManagedNode{{ID: "n1", GroupID: new("g1")}}
			rec := release("n1", `{}`)
			Expect(rec.Code).To(Equal(http.StatusBadRequest))
		})
	})
})
