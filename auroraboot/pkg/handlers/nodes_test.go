package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"

	"github.com/gorilla/websocket"
	"github.com/kairos-io/AuroraBoot/pkg/auth"
	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/kairos-io/AuroraBoot/pkg/ws"
	"github.com/labstack/echo/v4"
)

var _ = Describe("NodeHandler", func() {
	var (
		e       *echo.Echo
		ns      *fakeNodeStore
		cs      *fakeCommandStore
		gs      *fakeGroupStore
		hub     *ws.Hub
		handler *handlers.NodeHandler
	)

	BeforeEach(func() {
		e = echo.New()
		ns = &fakeNodeStore{}
		cs = &fakeCommandStore{}
		gs = &fakeGroupStore{}
		hub = ws.NewHub()
		handler = handlers.NewNodeHandler(ns, cs, gs, hub, "reg-token", "http://localhost:8080")
	})

	Describe("Register", func() {
		It("should register a new node", func() {
			body := `{"registrationToken":"reg-token","machineID":"machine-1","hostname":"host-1","agentVersion":"1.0"}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/register", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := handler.Register(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Code).To(Equal(http.StatusCreated))

			var resp map[string]interface{}
			Expect(json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["id"]).NotTo(BeEmpty())
			Expect(resp["apiKey"]).NotTo(BeEmpty())
		})

		It("should return existing node if machineID already registered", func() {
			ns.nodes = []*store.ManagedNode{
				{ID: "existing-id", MachineID: "machine-1", APIKey: "existing-key"},
			}
			body := `{"registrationToken":"reg-token","machineID":"machine-1","hostname":"host-1"}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/register", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := handler.Register(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Code).To(Equal(http.StatusOK))

			var resp map[string]interface{}
			Expect(json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["id"]).To(Equal("existing-id"))
			Expect(resp["apiKey"]).To(Equal("existing-key"))
		})

		It("should reject registration without machineID", func() {
			body := `{"registrationToken":"reg-token","hostname":"host-1"}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/register", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := handler.Register(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Code).To(Equal(http.StatusBadRequest))
		})

		It("should persist addresses and bootState and expose them on GET", func() {
			body := `{"registrationToken":"reg-token","machineID":"machine-addr","hostname":"n1",` +
				`"addresses":[{"type":"InternalIP","address":"10.0.10.21"},{"type":"Hostname","address":"n1"}],` +
				`"bootState":"active"}`
			id := registerNode(e, handler, body)

			node := getNode(e, handler, id)
			Expect(node.Addresses).To(Equal([]store.NodeAddress{
				{Type: "InternalIP", Address: "10.0.10.21"},
				{Type: "Hostname", Address: "n1"},
			}))
			Expect(node.BootState).To(Equal("active"))
		})

		It("should register without addresses/bootState (backward compat) and leave them empty", func() {
			body := `{"registrationToken":"reg-token","machineID":"machine-old","hostname":"n2","agentVersion":"v2.29.4"}`
			id := registerNode(e, handler, body)

			node := getNode(e, handler, id)
			Expect(node.Addresses).To(BeEmpty())
			Expect(node.BootState).To(BeEmpty())
		})

		It("puts a new node in the group whose ID it sends", func() {
			gs.groups = []*store.NodeGroup{{ID: "grp-1", Name: "edge"}}

			id := registerNode(e, handler, `{"registrationToken":"reg-token","machineID":"machine-gid","group":"grp-1"}`)

			Expect(getNode(e, handler, id).GroupID).To(HaveValue(Equal("grp-1")))
		})

		It("puts a new node in the group whose name it sends", func() {
			gs.groups = []*store.NodeGroup{{ID: "grp-1", Name: "edge"}}

			id := registerNode(e, handler, `{"registrationToken":"reg-token","machineID":"machine-gname","group":"edge"}`)

			Expect(getNode(e, handler, id).GroupID).To(HaveValue(Equal("grp-1")))
		})

		It("keeps placing nodes by group ID after the group is renamed", func() {
			gs.groups = []*store.NodeGroup{{ID: "grp-1", Name: "edge"}}
			gs.groups[0].Name = "edge-renamed"

			id := registerNode(e, handler, `{"registrationToken":"reg-token","machineID":"machine-renamed","group":"grp-1"}`)

			Expect(getNode(e, handler, id).GroupID).To(HaveValue(Equal("grp-1")))
		})

		It("prefers a group ID match over a name match", func() {
			gs.groups = []*store.NodeGroup{{ID: "grp-1", Name: "grp-2"}, {ID: "grp-2", Name: "other"}}

			id := registerNode(e, handler, `{"registrationToken":"reg-token","machineID":"machine-both","group":"grp-2"}`)

			Expect(getNode(e, handler, id).GroupID).To(HaveValue(Equal("grp-2")))
		})

		It("leaves a new node ungrouped when the group is unknown", func() {
			gs.groups = []*store.NodeGroup{{ID: "grp-1", Name: "edge"}}

			id := registerNode(e, handler, `{"registrationToken":"reg-token","machineID":"machine-nogroup","group":"missing"}`)

			Expect(getNode(e, handler, id).GroupID).To(BeNil())
		})
	})

	Describe("List", func() {
		BeforeEach(func() {
			ns.nodes = []*store.ManagedNode{
				{ID: "node-1", GroupID: new("grp-1"), Labels: map[string]string{"env": "prod"}},
				{ID: "node-2", GroupID: new("grp-2"), Labels: map[string]string{"env": "staging"}},
			}
		})

		It("should list all nodes", func() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := handler.List(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Code).To(Equal(http.StatusOK))

			var nodes []*store.ManagedNode
			Expect(json.Unmarshal(rec.Body.Bytes(), &nodes)).To(Succeed())
			Expect(nodes).To(HaveLen(2))
		})

		It("should filter by group", func() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes?group=grp-1", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := handler.List(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Code).To(Equal(http.StatusOK))

			var nodes []*store.ManagedNode
			Expect(json.Unmarshal(rec.Body.Bytes(), &nodes)).To(Succeed())
			Expect(nodes).To(HaveLen(1))
			Expect(nodes[0].ID).To(Equal("node-1"))
		})

		It("should filter by label", func() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes?label=env:staging", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := handler.List(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Code).To(Equal(http.StatusOK))

			var nodes []*store.ManagedNode
			Expect(json.Unmarshal(rec.Body.Bytes(), &nodes)).To(Succeed())
			Expect(nodes).To(HaveLen(1))
			Expect(nodes[0].ID).To(Equal("node-2"))
		})
	})

	Describe("Get", func() {
		It("should return a node by ID", func() {
			ns.nodes = []*store.ManagedNode{
				{ID: "node-1", Hostname: "host-1"},
			}
			req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/node-1", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("nodeID")
			c.SetParamValues("node-1")

			err := handler.Get(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Code).To(Equal(http.StatusOK))
		})

		It("should return 404 for missing node", func() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/missing", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("nodeID")
			c.SetParamValues("missing")

			err := handler.Get(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Code).To(Equal(http.StatusNotFound))
		})
	})

	Describe("Delete", func() {
		It("should delete a node", func() {
			ns.nodes = []*store.ManagedNode{
				{ID: "node-1"},
			}
			req := httptest.NewRequest(http.MethodDelete, "/api/v1/nodes/node-1", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("nodeID")
			c.SetParamValues("node-1")

			err := handler.Delete(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Code).To(Equal(http.StatusNoContent))
		})

		It("closes the deleted node's agent socket", func() {
			ns.nodes = []*store.ManagedNode{
				{ID: "node-1"},
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if err != nil {
					return
				}
				hub.Register("node-1", conn)
			}))
			defer srv.Close()
			agent, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
			Expect(err).NotTo(HaveOccurred())
			defer agent.Close()
			Eventually(func() bool { return hub.IsOnline("node-1") }, 5*time.Second, 50*time.Millisecond).Should(BeTrue())

			req := httptest.NewRequest(http.MethodDelete, "/api/v1/nodes/node-1", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("nodeID")
			c.SetParamValues("node-1")
			Expect(handler.Delete(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusNoContent))

			Expect(hub.IsOnline("node-1")).To(BeFalse())
			_ = agent.SetReadDeadline(time.Now().Add(5 * time.Second))
			_, _, err = agent.ReadMessage()
			Expect(websocket.IsCloseError(err, websocket.ClosePolicyViolation)).To(BeTrue(), "got %v", err)
		})
	})

	// Decommission is the remote-teardown entry point: it creates an
	// `unregister` NodeCommand and pushes it down the node's live WS
	// connection (if any). It does NOT delete the node record — the UI
	// drives DELETE as a second step once the command finishes.
	Describe("Decommission", func() {
		decommission := func(nodeID string) *httptest.ResponseRecorder {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/"+nodeID+"/decommission", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("nodeID")
			c.SetParamValues(nodeID)
			Expect(handler.Decommission(c)).To(Succeed())
			return rec
		}

		It("returns 404 when the node doesn't exist", func() {
			rec := decommission("missing")
			Expect(rec.Code).To(Equal(http.StatusNotFound))
		})

		It("reports nodeOnline=false and queues no command when the node is offline", func() {
			ns.nodes = []*store.ManagedNode{{ID: "node-offline"}}
			// hub has no registered connection for node-offline, so IsOnline is false

			rec := decommission("node-offline")
			Expect(rec.Code).To(Equal(http.StatusOK))

			var resp map[string]interface{}
			Expect(json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["nodeOnline"]).To(Equal(false))
			Expect(resp["commandID"]).To(Equal(""))
			// Nothing is persisted for the offline path — the operator will
			// force-delete and run the CLI fallback on the box.
			Expect(cs.cmds).To(BeEmpty())
		})

		// Online-path coverage lives in the integration test (needs a real
		// *websocket.Conn inside the hub to satisfy IsOnline). Hub exposes
		// no test hook for synthesizing an online node in unit tests, so
		// asserting the persisted command's shape there would require
		// reaching into unexported hub state — deliberately skipped here.
	})

	Describe("SetLabels", func() {
		It("should set labels on a node", func() {
			ns.nodes = []*store.ManagedNode{
				{ID: "node-1", Labels: map[string]string{}},
			}
			body := `{"labels":{"env":"production","tier":"web"}}`
			req := httptest.NewRequest(http.MethodPut, "/api/v1/nodes/node-1/labels", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("nodeID")
			c.SetParamValues("node-1")

			err := handler.SetLabels(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Code).To(Equal(http.StatusOK))
		})

		It("returns 404 for an unknown node", func() {
			body := `{"labels":{"env":"production"}}`
			req := httptest.NewRequest(http.MethodPut, "/api/v1/nodes/missing/labels", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("nodeID")
			c.SetParamValues("missing")

			Expect(handler.SetLabels(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusNotFound))
		})
	})

	Describe("SetGroup", func() {
		setGroup := func(body string) *httptest.ResponseRecorder {
			req := httptest.NewRequest(http.MethodPut, "/api/v1/nodes/node-1/group", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("nodeID")
			c.SetParamValues("node-1")
			Expect(handler.SetGroup(c)).To(Succeed())
			return rec
		}

		It("should set a group on a node", func() {
			ns.nodes = []*store.ManagedNode{{ID: "node-1"}}
			gs.groups = []*store.NodeGroup{{ID: "grp-1"}}

			rec := setGroup(`{"groupID":"grp-1"}`)

			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(ns.nodes[0].GroupID).To(HaveValue(Equal("grp-1")))
		})

		It("moves a node out of its group on a null group id", func() {
			grp := "grp-1"
			ns.nodes = []*store.ManagedNode{{ID: "node-1", GroupID: &grp}}

			rec := setGroup(`{"groupID":null}`)

			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(ns.nodes[0].GroupID).To(BeNil())
		})

		It("returns 404 for an unknown group and leaves the node alone", func() {
			ns.nodes = []*store.ManagedNode{{ID: "node-1"}}

			rec := setGroup(`{"groupID":"no-such-group"}`)

			Expect(rec.Code).To(Equal(http.StatusNotFound))
			Expect(ns.nodes[0].GroupID).To(BeNil())
		})

		It("returns 404 for an unknown node", func() {
			gs.groups = []*store.NodeGroup{{ID: "grp-1"}}

			rec := setGroup(`{"groupID":"grp-1"}`)

			Expect(rec.Code).To(Equal(http.StatusNotFound))
		})

		It("returns 404 for an unknown node on a null group id", func() {
			rec := setGroup(`{"groupID":null}`)

			Expect(rec.Code).To(Equal(http.StatusNotFound))
		})

		// An empty string names no group. Taking it for "no group" would
		// give the API two spellings of the same request.
		It("rejects an empty group id", func() {
			grp := "grp-1"
			ns.nodes = []*store.ManagedNode{{ID: "node-1", GroupID: &grp}}

			rec := setGroup(`{"groupID":""}`)

			Expect(rec.Code).To(Equal(http.StatusBadRequest))
			Expect(ns.nodes[0].GroupID).To(HaveValue(Equal("grp-1")))
		})
	})

	Describe("Heartbeat", func() {
		It("should update heartbeat and phase", func() {
			ns.nodes = []*store.ManagedNode{
				{ID: "node-1", Phase: store.PhaseRegistered},
			}
			body := `{"agentVersion":"1.1","osRelease":{"ID":"ubuntu"}}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/node-1/heartbeat", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("nodeID")
			c.SetParamValues("node-1")

			err := handler.Heartbeat(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(ns.nodes[0].Phase).To(Equal(store.PhaseOnline))
		})

		It("should update addresses and bootState on heartbeat (e.g. a DHCP change)", func() {
			ns.nodes = []*store.ManagedNode{{
				ID:        "node-1",
				Phase:     store.PhaseRegistered,
				Addresses: []store.NodeAddress{{Type: "InternalIP", Address: "10.0.10.21"}},
				BootState: "active",
			}}
			body := `{"agentVersion":"1.1",` +
				`"addresses":[{"type":"InternalIP","address":"10.0.10.99"},{"type":"Hostname","address":"n1"}],` +
				`"bootState":"passive"}`
			heartbeat(e, handler, "node-1", body)

			node := getNode(e, handler, "node-1")
			Expect(node.Addresses).To(Equal([]store.NodeAddress{
				{Type: "InternalIP", Address: "10.0.10.99"},
				{Type: "Hostname", Address: "n1"},
			}))
			Expect(node.BootState).To(Equal("passive"))
		})

		It("should preserve addresses and bootState when a heartbeat omits them", func() {
			ns.nodes = []*store.ManagedNode{{
				ID:        "node-1",
				Phase:     store.PhaseRegistered,
				Addresses: []store.NodeAddress{{Type: "InternalIP", Address: "10.0.10.21"}},
				BootState: "active",
			}}
			// An older agent sends neither field — must not wipe existing values.
			heartbeat(e, handler, "node-1", `{"agentVersion":"1.1","osRelease":{"ID":"ubuntu"}}`)

			node := getNode(e, handler, "node-1")
			Expect(node.Addresses).To(Equal([]store.NodeAddress{{Type: "InternalIP", Address: "10.0.10.21"}}))
			Expect(node.BootState).To(Equal("active"))
		})

		// kairos-io/kairos#4196: a node registers before cloud-init applies a
		// templated hostname, so it lands as the image default. The heartbeat has
		// to be able to correct it.
		It("should update the hostname on heartbeat", func() {
			ns.nodes = []*store.ManagedNode{{
				ID:       "node-1",
				Phase:    store.PhaseRegistered,
				Hostname: "kairos",
			}}
			heartbeat(e, handler, "node-1", `{"agentVersion":"1.1","hostname":"kairos-a1b2"}`)

			Expect(getNode(e, handler, "node-1").Hostname).To(Equal("kairos-a1b2"))
		})

		It("should preserve the hostname when a heartbeat omits it", func() {
			ns.nodes = []*store.ManagedNode{{
				ID:       "node-1",
				Phase:    store.PhaseRegistered,
				Hostname: "kairos-a1b2",
			}}
			// An older agent sends no hostname, which must not wipe the stored one.
			heartbeat(e, handler, "node-1", `{"agentVersion":"1.1","osRelease":{"ID":"ubuntu"}}`)

			Expect(getNode(e, handler, "node-1").Hostname).To(Equal("kairos-a1b2"))
		})
	})

	Describe("GetCommands", func() {
		It("should return pending commands for agent and mark delivered", func() {
			ns.nodes = []*store.ManagedNode{
				{ID: "node-1"},
			}
			cs.cmds = []*store.NodeCommand{
				{ID: "cmd-1", ManagedNodeID: "node-1", Command: "upgrade", Phase: store.CommandPending},
				{ID: "cmd-2", ManagedNodeID: "node-1", Command: "exec", Phase: store.CommandCompleted},
			}

			req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/node-1/commands", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("nodeID")
			c.SetParamValues("node-1")
			// Simulate agent auth by setting nodeID in context
			c.Set(auth.ContextKeyNodeID, "node-1")

			err := handler.GetCommands(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Code).To(Equal(http.StatusOK))

			var cmds []*store.NodeCommand
			Expect(json.Unmarshal(rec.Body.Bytes(), &cmds)).To(Succeed())
			Expect(cmds).To(HaveLen(1))
			Expect(cmds[0].ID).To(Equal("cmd-1"))
		})

		It("does not re-deliver a command already claimed (e.g. pushed over WS)", func() {
			ns.nodes = []*store.ManagedNode{{ID: "node-1"}}
			// Simulate a command that was already delivered via the WS push path:
			// PushCommand claims it Pending->Delivered before sending.
			cs.cmds = []*store.NodeCommand{
				{ID: "cmd-1", ManagedNodeID: "node-1", Command: "upgrade", Phase: store.CommandDelivered},
			}

			req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/node-1/commands", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("nodeID")
			c.SetParamValues("node-1")
			c.Set(auth.ContextKeyNodeID, "node-1")

			Expect(handler.GetCommands(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusOK))

			var cmds []*store.NodeCommand
			Expect(json.Unmarshal(rec.Body.Bytes(), &cmds)).To(Succeed())
			Expect(cmds).To(BeEmpty())
		})

		It("delivers a command to exactly one of two concurrent agent polls", func() {
			ns.nodes = []*store.ManagedNode{{ID: "node-1"}}
			cs.cmds = []*store.NodeCommand{
				{ID: "cmd-1", ManagedNodeID: "node-1", Command: "upgrade", Phase: store.CommandPending},
			}

			// poll runs on a worker goroutine, so it returns its result instead of
			// asserting (Ginkgo assertions must run on the spec goroutine).
			poll := func() (int, error) {
				req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/node-1/commands", nil)
				rec := httptest.NewRecorder()
				c := e.NewContext(req, rec)
				c.SetParamNames("nodeID")
				c.SetParamValues("node-1")
				c.Set(auth.ContextKeyNodeID, "node-1")
				if err := handler.GetCommands(c); err != nil {
					return 0, err
				}
				var cmds []*store.NodeCommand
				if err := json.Unmarshal(rec.Body.Bytes(), &cmds); err != nil {
					return 0, err
				}
				return len(cmds), nil
			}

			var wg sync.WaitGroup
			counts := make([]int, 2)
			errs := make([]error, 2)
			for i := 0; i < 2; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					counts[i], errs[i] = poll()
				}(i)
			}
			wg.Wait()

			Expect(errs[0]).NotTo(HaveOccurred())
			Expect(errs[1]).NotTo(HaveOccurred())
			Expect(counts[0] + counts[1]).To(Equal(1))
		})

		It("should return all commands for admin", func() {
			cs.cmds = []*store.NodeCommand{
				{ID: "cmd-1", ManagedNodeID: "node-1", Phase: store.CommandPending},
				{ID: "cmd-2", ManagedNodeID: "node-1", Phase: store.CommandCompleted},
			}

			req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/node-1/commands", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("nodeID")
			c.SetParamValues("node-1")
			// No nodeID in context = admin request

			err := handler.GetCommands(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Code).To(Equal(http.StatusOK))

			var cmds []*store.NodeCommand
			Expect(json.Unmarshal(rec.Body.Bytes(), &cmds)).To(Succeed())
			Expect(cmds).To(HaveLen(2))
		})
	})

	Describe("InstallScript", func() {
		It("should return an install shell script", func() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/install-agent", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := handler.InstallScript(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(rec.Body.String()).To(ContainSubstring("#!/bin/sh"))
			Expect(rec.Body.String()).To(ContainSubstring("http://localhost:8080"))
		})

		It("should emit a shell script that passes sh -n and bash -n", func() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/install-agent", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := handler.InstallScript(c)
			Expect(err).NotTo(HaveOccurred())

			tmp, err := os.CreateTemp("", "install-agent-*.sh")
			Expect(err).NotTo(HaveOccurred())
			defer os.Remove(tmp.Name())

			_, err = tmp.Write(rec.Body.Bytes())
			Expect(err).NotTo(HaveOccurred())
			Expect(tmp.Close()).To(Succeed())

			for _, shell := range []string{"sh", "bash"} {
				if _, err := exec.LookPath(shell); err != nil {
					Skip(shell + " not installed")
				}
				cmd := exec.Command(shell, "-n", tmp.Name())
				out, err := cmd.CombinedOutput()
				Expect(err).NotTo(HaveOccurred(), "%s: %s", shell, out)
			}
		})

		// renderPhonehomeYAML runs the script's real allowed_commands builder and
		// config heredoc under /bin/sh and returns the YAML it would write to
		// /oem/phonehome.yaml. It executes the served script verbatim (only
		// redirecting the heredoc to stdout) so the assertions cover the actual
		// shipped rendering, not a re-implementation of it.
		renderPhonehomeYAML := func(allowedCommands string) []byte {
			if _, err := exec.LookPath("sh"); err != nil {
				Skip("sh not installed")
			}

			req := httptest.NewRequest(http.MethodGet, "/api/v1/install-agent", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			Expect(handler.InstallScript(c)).To(Succeed())
			script := rec.Body.String()

			// Take the ALLOWED_RAW builder and the config heredoc as two exact
			// slices, dropping what sits between them: the script echoes progress
			// to stdout and mkdir's /oem there, which would pollute the captured
			// YAML and need root. Everything after the heredoc starts
			// kairos-agent, which we neither can nor want to run here.
			const builderEnd = `IFS="$_old_ifs"`
			const heredocStart = "cat > /oem/phonehome.yaml << EOF"

			builderAt := strings.Index(script, "ALLOWED_RAW=")
			Expect(builderAt).To(BeNumerically(">=", 0), "install script no longer defines ALLOWED_RAW")
			builderEndAt := strings.Index(script[builderAt:], builderEnd)
			Expect(builderEndAt).To(BeNumerically(">=", 0), "install script no longer restores IFS after the loop")
			builder := script[builderAt : builderAt+builderEndAt+len(builderEnd)]

			heredocAt := strings.Index(script, heredocStart)
			Expect(heredocAt).To(BeNumerically(">", builderAt), "install script no longer writes the config heredoc")
			rest := script[heredocAt:]
			end := strings.Index(rest, "\nEOF\n")
			Expect(end).To(BeNumerically(">=", 0), "config heredoc is not terminated")
			heredoc := strings.Replace(rest[:end+len("\nEOF\n")], heredocStart, "cat << EOF", 1)

			fragment := builder + "\n" + heredoc

			cmd := exec.Command("sh", "-c", fragment)
			cmd.Env = append(os.Environ(), "AURORABOOT_ALLOWED_COMMANDS="+allowedCommands)
			out, err := cmd.Output()
			Expect(err).NotTo(HaveOccurred(), "rendering phonehome.yaml: %s", out)
			return out
		}

		// parseAllowed unmarshals the rendered config and returns the list. This
		// is the point of the test: a raw substring check passes even when every
		// entry is concatenated onto one line, which is how the original bug hid.
		parseAllowed := func(out []byte) []string {
			var cfg struct {
				Phonehome struct {
					AllowedCommands []string `yaml:"allowed_commands"`
				} `yaml:"phonehome"`
			}
			Expect(yaml.Unmarshal(out, &cfg)).To(Succeed(), "rendered config is not valid YAML:\n%s", out)
			return cfg.Phonehome.AllowedCommands
		}

		It("should render allowed_commands as a parseable YAML list, one entry per line", func() {
			out := renderPhonehomeYAML("upgrade,reboot,apply-cloud-config,unregister")

			// Regression guard for the $(printf '\n') bug: command substitution
			// strips trailing newlines, so the separator vanished and the list
			// collapsed to a single scalar ("- upgrade    - reboot    - ...").
			Expect(parseAllowed(out)).To(Equal([]string{
				"upgrade", "reboot", "apply-cloud-config", "unregister",
			}), "allowed_commands did not parse into distinct entries:\n%s", out)
		})

		It("should render the safe defaults when AURORABOOT_ALLOWED_COMMANDS is unset", func() {
			out := renderPhonehomeYAML("")

			Expect(parseAllowed(out)).To(Equal([]string{
				"upgrade", "upgrade-recovery", "reboot", "unregister",
			}), "default allowed_commands are wrong:\n%s", out)
		})

		It("should trim whitespace and drop empty entries", func() {
			out := renderPhonehomeYAML("upgrade, reboot ,,unregister")

			Expect(parseAllowed(out)).To(Equal([]string{
				"upgrade", "reboot", "unregister",
			}), "entries were not trimmed/compacted:\n%s", out)
		})
	})
})

// registerNode POSTs a registration body and returns the new node's id.
func registerNode(e *echo.Echo, handler *handlers.NodeHandler, body string) string {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	ExpectWithOffset(1, handler.Register(c)).To(Succeed())
	ExpectWithOffset(1, rec.Code).To(Equal(http.StatusCreated))

	var resp map[string]any
	ExpectWithOffset(1, json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
	id, _ := resp["id"].(string)
	ExpectWithOffset(1, id).NotTo(BeEmpty())
	return id
}

// heartbeat POSTs a heartbeat body for the given node id and asserts 200.
func heartbeat(e *echo.Echo, handler *handlers.NodeHandler, id, body string) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/"+id+"/heartbeat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("nodeID")
	c.SetParamValues(id)

	ExpectWithOffset(1, handler.Heartbeat(c)).To(Succeed())
	ExpectWithOffset(1, rec.Code).To(Equal(http.StatusOK))
}

// getNode GETs /api/v1/nodes/:id and decodes the ManagedNode the API returns.
func getNode(e *echo.Echo, handler *handlers.NodeHandler, id string) store.ManagedNode {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/"+id, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("nodeID")
	c.SetParamValues(id)

	ExpectWithOffset(1, handler.Get(c)).To(Succeed())
	ExpectWithOffset(1, rec.Code).To(Equal(http.StatusOK))

	var node store.ManagedNode
	ExpectWithOffset(1, json.Unmarshal(rec.Body.Bytes(), &node)).To(Succeed())
	return node
}
