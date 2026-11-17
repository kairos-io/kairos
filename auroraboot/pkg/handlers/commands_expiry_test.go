package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/auth"
	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/kairos-io/AuroraBoot/pkg/ws"
	"github.com/labstack/echo/v4"
)

// A command that carries an ExpiresAt is refused delivery once the deadline
// passes, but nothing used to give it a terminal phase. It stayed Pending
// forever: undeliverable, not collected by Clear History, and with no delete
// button in the dashboard.
var _ = Describe("Command expiry", func() {
	var (
		e           *echo.Echo
		ns          *fakeNodeStore
		cs          *fakeCommandStore
		nodeHandler *handlers.NodeHandler
		cmdHandler  *handlers.CommandHandler
	)

	past := func() *time.Time {
		t := time.Now().Add(-time.Minute)
		return &t
	}
	future := func() *time.Time {
		t := time.Now().Add(time.Hour)
		return &t
	}

	// get runs GET /api/v1/nodes/node-1/commands and decodes the response.
	// asAgent picks the agent branch (node API key) over the admin one.
	get := func(asAgent bool) []*store.NodeCommand {
		GinkgoHelper()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/node-1/commands", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("nodeID")
		c.SetParamValues("node-1")
		if asAgent {
			c.Set(auth.ContextKeyNodeID, "node-1")
		}
		Expect(nodeHandler.GetCommands(c)).To(Succeed())
		Expect(rec.Code).To(Equal(http.StatusOK))
		var cmds []*store.NodeCommand
		Expect(json.Unmarshal(rec.Body.Bytes(), &cmds)).To(Succeed())
		return cmds
	}

	// stored reads a command back out of the store, which is what the next
	// reader (and Clear History) sees.
	stored := func(id string) *store.NodeCommand {
		GinkgoHelper()
		cmd, err := cs.GetByID(GinkgoT().Context(), id)
		Expect(err).NotTo(HaveOccurred())
		return cmd
	}

	BeforeEach(func() {
		e = echo.New()
		ns = &fakeNodeStore{nodes: []*store.ManagedNode{{ID: "node-1"}}}
		cs = &fakeCommandStore{}
		nodeHandler = handlers.NewNodeHandler(ns, cs, &fakeGroupStore{}, ws.NewHub(), "reg-token", "http://localhost:8080")
		cmdHandler = handlers.NewCommandHandler(cs, ns, nil, nil, nil)
	})

	It("reports an overdue Pending command as Expired to the admin", func() {
		cs.cmds = []*store.NodeCommand{
			{ID: "cmd-1", ManagedNodeID: "node-1", Command: "unregister", Phase: store.CommandPending, ExpiresAt: past()},
		}

		cmds := get(false)
		Expect(cmds).To(HaveLen(1))
		Expect(cmds[0].Phase).To(Equal(store.CommandExpired))
		Expect(cmds[0].CompletedAt).NotTo(BeNil())
	})

	It("expires an overdue Delivered command, the phase a WS push leaves behind", func() {
		// Decommission claims Pending->Delivered before writing to the socket,
		// so a node that drops in that window strands the command in Delivered,
		// not Pending.
		cs.cmds = []*store.NodeCommand{
			{ID: "cmd-1", ManagedNodeID: "node-1", Command: "unregister", Phase: store.CommandDelivered, ExpiresAt: past()},
		}

		Expect(get(false)[0].Phase).To(Equal(store.CommandExpired))
	})

	It("expires an overdue command on the agent's own poll, and still does not deliver it", func() {
		cs.cmds = []*store.NodeCommand{
			{ID: "cmd-1", ManagedNodeID: "node-1", Command: "unregister", Phase: store.CommandPending, ExpiresAt: past()},
		}

		Expect(get(true)).To(BeEmpty())
		Expect(stored("cmd-1").Phase).To(Equal(store.CommandExpired))
	})

	It("leaves a command whose deadline has not passed alone, and still delivers it", func() {
		cs.cmds = []*store.NodeCommand{
			{ID: "cmd-1", ManagedNodeID: "node-1", Command: "unregister", Phase: store.CommandPending, ExpiresAt: future()},
		}

		delivered := get(true)
		Expect(delivered).To(HaveLen(1))
		Expect(delivered[0].Phase).To(Equal(store.CommandDelivered))
	})

	It("leaves a command with no deadline alone however old it is", func() {
		cs.cmds = []*store.NodeCommand{
			{ID: "cmd-1", ManagedNodeID: "node-1", Command: "upgrade", Phase: store.CommandPending, CreatedAt: time.Now().Add(-30 * 24 * time.Hour)},
		}

		Expect(get(false)[0].Phase).To(Equal(store.CommandPending))
	})

	It("does not overwrite the real outcome of a command that finished late", func() {
		// The agent may report Completed after the deadline. That outcome is
		// the truth; expiry must not rewrite it.
		cs.cmds = []*store.NodeCommand{
			{ID: "cmd-1", ManagedNodeID: "node-1", Command: "upgrade", Phase: store.CommandCompleted, Result: "ok", ExpiresAt: past()},
			{ID: "cmd-2", ManagedNodeID: "node-1", Command: "reset", Phase: store.CommandFailed, Result: "boom", ExpiresAt: past()},
		}

		Expect(get(false)).To(HaveLen(2))
		Expect(stored("cmd-1").Phase).To(Equal(store.CommandCompleted))
		Expect(stored("cmd-1").Result).To(Equal("ok"))
		Expect(stored("cmd-2").Phase).To(Equal(store.CommandFailed))
	})

	It("does not touch another node's overdue commands", func() {
		cs.cmds = []*store.NodeCommand{
			{ID: "cmd-1", ManagedNodeID: "node-1", Command: "unregister", Phase: store.CommandPending, ExpiresAt: past()},
			{ID: "cmd-2", ManagedNodeID: "node-2", Command: "unregister", Phase: store.CommandPending, ExpiresAt: past()},
		}

		get(false)
		Expect(stored("cmd-2").Phase).To(Equal(store.CommandPending))
	})

	It("lets Clear History collect the expired command", func() {
		cs.cmds = []*store.NodeCommand{
			{ID: "cmd-1", ManagedNodeID: "node-1", Command: "unregister", Phase: store.CommandPending, ExpiresAt: past()},
		}
		Expect(get(false)[0].Phase).To(Equal(store.CommandExpired))

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/nodes/node-1/commands", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("nodeID")
		c.SetParamValues("node-1")
		Expect(cmdHandler.ClearHistory(c)).To(Succeed())
		Expect(rec.Code).To(Equal(http.StatusNoContent))

		Expect(get(false)).To(BeEmpty())
	})
})
