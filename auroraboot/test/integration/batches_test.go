package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Rolling batches", func() {
	type agent struct {
		id, apiKey, machineID string
		conn                  *websocket.Conn
	}

	var (
		suffix  string
		groupID string
		agents  []*agent
	)

	nodePhase := func(id string) func() any {
		return func() any {
			resp := adminGet(testServerURL, "/api/v1/nodes/"+id, testAdminPassword)
			var n map[string]any
			decodeJSON(resp, &n)
			return n["phase"]
		}
	}

	// online registers n nodes in a new group and connects each one's agent
	// socket, waiting until the server reports every node online.
	online := func(n int) {
		suffix = fmt.Sprintf("%d", time.Now().UnixNano())
		resp := adminPost(testServerURL, "/api/v1/groups", testAdminPassword, map[string]any{"name": "batch-" + suffix})
		Expect(resp.StatusCode).To(Equal(http.StatusCreated))
		var group map[string]any
		decodeJSON(resp, &group)
		groupID = group["id"].(string)

		agents = nil
		for i := range n {
			a := &agent{machineID: fmt.Sprintf("machine-batch-%d-%s", i, suffix)}
			a.id, a.apiKey = registerNode(testServerURL, testRegToken, a.machineID, fmt.Sprintf("host-batch-%d", i))
			resp := adminPut(testServerURL, "/api/v1/nodes/"+a.id+"/group", testAdminPassword, map[string]string{"groupID": groupID})
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			resp.Body.Close()
			a.conn = connectWS(testServerURL, a.apiKey)
			DeferCleanup(a.conn.Close)
			Eventually(nodePhase(a.id), 5*time.Second, 50*time.Millisecond).Should(Equal("Online"))
			agents = append(agents, a)
		}
	}

	startGroup := func(body map[string]any) map[string]any {
		resp := adminPost(testServerURL, "/api/v1/groups/"+groupID+"/commands", testAdminPassword, body)
		ExpectWithOffset(1, resp.StatusCode).To(Equal(http.StatusCreated))
		var batch map[string]any
		decodeJSON(resp, &batch)
		return batch
	}

	// nextCommand returns the ID of the next command pushed to a's socket, or
	// "" when none arrives within wait. A read that times out leaves the
	// connection unusable, so a spec only waits for nothing at its end.
	nextCommand := func(a *agent, wait time.Duration) string {
		Expect(a.conn.SetReadDeadline(time.Now().Add(wait))).To(Succeed())
		for {
			_, raw, err := a.conn.ReadMessage()
			if err != nil {
				return ""
			}
			var msg struct {
				Type string `json:"type"`
				Data struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			Expect(json.Unmarshal(raw, &msg)).To(Succeed())
			if msg.Type == "command" {
				return msg.Data.ID
			}
		}
	}

	send := func(a *agent, typ string, data any) {
		payload, err := json.Marshal(data)
		Expect(err).NotTo(HaveOccurred())
		msg, err := json.Marshal(map[string]any{"type": typ, "data": json.RawMessage(payload)})
		Expect(err).NotTo(HaveOccurred())
		Expect(a.conn.WriteMessage(websocket.TextMessage, msg)).To(Succeed())
	}

	report := func(a *agent, cmdID, phase, result string) {
		send(a, "command_status", map[string]string{"id": cmdID, "phase": phase, "result": result})
	}

	heartbeat := func(a *agent, uptime time.Duration) {
		send(a, "heartbeat", map[string]any{
			"agentVersion": "v4.4.0",
			"bootState":    "active",
			"metrics":      map[string]any{"uptimeSeconds": uint64(uptime.Seconds())},
		})
	}

	commandsOf := func(a *agent) []map[string]any {
		resp := adminGet(testServerURL, "/api/v1/nodes/"+a.id+"/commands", testAdminPassword)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var cmds []map[string]any
		decodeJSON(resp, &cmds)
		return cmds
	}

	It("delivers only as many commands as the concurrency allows", func() {
		online(3)

		batch := startGroup(map[string]any{"command": "exec", "args": map[string]string{"cmd": "true"}, "concurrency": 1})

		Expect(batch["phase"]).To(Equal("running"))
		Expect(batch["counts"]).To(Equal(map[string]any{"running": float64(1), "waiting": float64(2)}))
		Expect(batch["selection"]).To(Equal("group batch-" + suffix))
		Expect(nextCommand(agents[0], 5*time.Second)).NotTo(BeEmpty())
		Expect(nextCommand(agents[1], time.Second)).To(BeEmpty())
		Expect(nextCommand(agents[2], 10*time.Millisecond)).To(BeEmpty())
	})

	It("starts the next node when the previous one reports completion over the WebSocket", func() {
		online(2)
		startGroup(map[string]any{"command": "exec", "args": map[string]string{"cmd": "true"}, "concurrency": 1})
		first := nextCommand(agents[0], 5*time.Second)
		Expect(first).NotTo(BeEmpty())

		report(agents[0], first, "Completed", "ok")

		Expect(nextCommand(agents[1], 5*time.Second)).NotTo(BeEmpty())
	})

	It("stops the batch when a node reports failure", func() {
		online(2)
		startGroup(map[string]any{"command": "exec", "args": map[string]string{"cmd": "false"}, "concurrency": 1})
		first := nextCommand(agents[0], 5*time.Second)
		Expect(first).NotTo(BeEmpty())

		report(agents[0], first, "Failed", "exit status 1")

		Expect(nextCommand(agents[1], time.Second)).To(BeEmpty())
		Expect(commandsOf(agents[1])).To(BeEmpty())
	})

	It("rejects a selection that matches no nodes", func() {
		resp := adminPost(testServerURL, "/api/v1/nodes/commands", testAdminPassword, map[string]any{
			"selector": map[string]any{"labels": map[string]string{"none": "x"}},
			"command":  "reboot",
		})
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		var body map[string]any
		decodeJSON(resp, &body)
		Expect(body["error"]).To(Equal("no nodes match the selection"))
	})

	It("rejects negative settings", func() {
		online(1)
		for _, body := range []map[string]any{
			{"command": "reboot", "concurrency": -1},
			{"command": "reboot", "rebootTimeoutSeconds": -1},
		} {
			resp := adminPost(testServerURL, "/api/v1/groups/"+groupID+"/commands", testAdminPassword, body)
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			var got map[string]any
			decodeJSON(resp, &got)
			Expect(got["error"]).To(Equal("concurrency and rebootTimeoutSeconds must not be negative"))
		}
	})

	It("rejects a reboot timeout above the maximum", func() {
		online(1)
		for _, timeout := range []int{store.MaxRebootTimeoutSeconds + 1, 10000000000} {
			resp := adminPost(testServerURL, "/api/v1/groups/"+groupID+"/commands", testAdminPassword,
				map[string]any{"command": "reboot", "rebootTimeoutSeconds": timeout})
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			var got map[string]any
			decodeJSON(resp, &got)
			Expect(got["error"]).To(Equal("rebootTimeoutSeconds must not exceed 604800 (7 days)"))
		}
	})

	It("accepts a reboot timeout at the maximum", func() {
		online(1)
		batch := startGroup(map[string]any{"command": "reboot", "rebootTimeoutSeconds": store.MaxRebootTimeoutSeconds})
		Expect(batch["rebootTimeoutSeconds"]).To(Equal(float64(store.MaxRebootTimeoutSeconds)))
	})

	It("rejects a single-node upgrade for a node in a running batch", func() {
		online(2)
		startGroup(map[string]any{"command": "exec", "args": map[string]string{"cmd": "true"}, "concurrency": 1})

		resp := adminPost(testServerURL, "/api/v1/nodes/"+agents[1].id+"/commands", testAdminPassword,
			map[string]any{"command": "upgrade", "args": map[string]string{"source": "oci:example"}})

		Expect(resp.StatusCode).To(Equal(http.StatusConflict))
		var body map[string]any
		decodeJSON(resp, &body)
		Expect(body["error"]).To(Equal("node is in a running batch"))
		Expect(body["nodeIDs"]).To(ConsistOf(agents[1].id))
	})

	It("rejects a batch whose nodes are already in a running batch", func() {
		online(2)
		startGroup(map[string]any{"command": "exec", "args": map[string]string{"cmd": "true"}, "concurrency": 1})

		resp := adminPost(testServerURL, "/api/v1/nodes/commands", testAdminPassword, map[string]any{
			"selector": map[string]any{"nodeIDs": []string{agents[1].id}},
			"command":  "reboot",
		})

		Expect(resp.StatusCode).To(Equal(http.StatusConflict))
		var body map[string]any
		decodeJSON(resp, &body)
		Expect(body["error"]).To(Equal("nodes are already in a running batch"))
		Expect(body["nodeIDs"]).To(ConsistOf(agents[1].id))
	})

	Describe("a reboot batch", func() {
		var first, batchID string

		BeforeEach(func() {
			online(2)
			batchID = startGroup(map[string]any{"command": "reboot", "concurrency": 1})["id"].(string)
			first = nextCommand(agents[0], 5*time.Second)
			Expect(first).NotTo(BeEmpty())
			report(agents[0], first, "Completed", "rebooting")
		})

		It("starts the next node when a rebooted node heartbeats from its new boot", func() {
			// A heartbeat counts as a new boot only when the boot it reports
			// is more than five seconds after the reboot was scheduled, the
			// margin that keeps whole-second uptimes from passing the boot
			// before the reboot off as a new one.
			time.Sleep(6 * time.Second)

			heartbeat(agents[0], 0)

			Expect(nextCommand(agents[1], 5*time.Second)).NotTo(BeEmpty())
		})

		It("starts the next node when a rebooted node heartbeats over REST", func() {
			time.Sleep(6 * time.Second)

			resp := doPost(testServerURL, "/api/v1/nodes/"+agents[0].id+"/heartbeat", agents[0].apiKey, map[string]any{
				"agentVersion": "v4.4.0",
				"bootState":    "active",
				"metrics":      map[string]any{"uptimeSeconds": 0},
			})
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			resp.Body.Close()

			Expect(nextCommand(agents[1], 5*time.Second)).NotTo(BeEmpty())
		})

		It("does not start the next node on a heartbeat from before the reboot", func() {
			heartbeat(agents[0], time.Hour)

			Expect(nextCommand(agents[1], time.Second)).To(BeEmpty())
		})

		It("starts the next node when a rebooted node registers again", func() {
			// The status report travels over the socket and the registration
			// over REST, and a registration only settles a node the batch
			// already waits on, so wait for the batch to apply the report.
			Eventually(func() string {
				b, err := nodeGormStore.BatchGet(context.Background(), batchID)
				Expect(err).NotTo(HaveOccurred())
				return b.Nodes[0].State
			}, 5*time.Second, 50*time.Millisecond).Should(Equal(store.BatchNodeRebooting))

			again, _ := registerNode(testServerURL, testRegToken, agents[0].machineID, "host-batch-0")
			Expect(again).To(Equal(agents[0].id))

			Expect(nextCommand(agents[1], 5*time.Second)).NotTo(BeEmpty())
		})
	})

	Describe("an upgrade batch whose node registers again", func() {
		var batchID string

		// reregister sends a registration without uptime, as an agent whose
		// heartbeats carry no metrics does after it boots.
		reregister := func(a *agent, bootState string) {
			resp := doPost(testServerURL, "/api/v1/nodes/register", "", map[string]any{
				"registrationToken": testRegToken,
				"machineID":         a.machineID,
				"hostname":          "host-batch-0",
				"bootState":         bootState,
			})
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			resp.Body.Close()
		}

		upgradeState := func(a *agent) func() string {
			return func() string {
				n, err := nodeGormStore.NodeGetByID(context.Background(), a.id)
				Expect(err).NotTo(HaveOccurred())
				return n.UpgradeState
			}
		}

		rowState := func() string {
			b, err := nodeGormStore.BatchGet(context.Background(), batchID)
			Expect(err).NotTo(HaveOccurred())
			return b.Nodes[0].State
		}

		BeforeEach(func() {
			online(1)
			batchID = startGroup(map[string]any{
				"command": "upgrade",
				"args":    map[string]string{"source": "oci:quay.io/kairos/kairos:v4"},
			})["id"].(string)
		})

		It("settles the upgrade the batch timed out from the boot state it reports", func() {
			cmd := nextCommand(agents[0], 5*time.Second)
			Expect(cmd).NotTo(BeEmpty())
			report(agents[0], cmd, "Completed", "rebooting")
			Eventually(rowState, 5*time.Second, 50*time.Millisecond).Should(Equal(store.BatchNodeRebooting))

			_, err := nodeGormStore.BatchAdvance(context.Background(), batchID, time.Now().Add(time.Hour))
			Expect(err).NotTo(HaveOccurred())
			Expect(rowState()).To(Equal(store.BatchNodeTimedOut))
			Expect(upgradeState(agents[0])()).To(Equal(store.UpgradeStateFailed))

			reregister(agents[0], "active")

			Expect(upgradeState(agents[0])()).To(Equal(store.UpgradeStateDone))
			Expect(rowState()).To(Equal(store.BatchNodeTimedOut))
		})

		It("keeps an upgrade failed when its command failed", func() {
			cmd := nextCommand(agents[0], 5*time.Second)
			Expect(cmd).NotTo(BeEmpty())
			report(agents[0], cmd, "Failed", "image pull failed")
			Eventually(upgradeState(agents[0]), 5*time.Second, 50*time.Millisecond).Should(Equal(store.UpgradeStateFailed))

			reregister(agents[0], "active")

			Expect(upgradeState(agents[0])()).To(Equal(store.UpgradeStateFailed))
		})
	})

	Describe("the batch endpoints", func() {
		getBatch := func(id string) map[string]any {
			resp := adminGet(testServerURL, "/api/v1/batches/"+id, testAdminPassword)
			ExpectWithOffset(1, resp.StatusCode).To(Equal(http.StatusOK))
			var b map[string]any
			decodeJSON(resp, &b)
			return b
		}

		It("reports why a batch stopped", func() {
			online(2)
			id := startGroup(map[string]any{"command": "exec", "args": map[string]string{"cmd": "false"}, "concurrency": 1})["id"].(string)
			first := nextCommand(agents[0], 5*time.Second)
			Expect(first).NotTo(BeEmpty())
			report(agents[0], first, "Failed", "exit status 1")

			Eventually(func() any { return getBatch(id)["phase"] }, 5*time.Second, 50*time.Millisecond).Should(Equal("stopped"))
			b := getBatch(id)
			Expect(b["stoppedByNodeID"]).To(Equal(agents[0].id))
			Expect(b["stopReason"]).NotTo(BeEmpty())
			nodes := b["nodes"].([]any)
			Expect(nodes).To(HaveLen(2))
			Expect(nodes[0].(map[string]any)["nodeID"]).To(Equal(agents[0].id))
			Expect(nodes[0].(map[string]any)["state"]).To(Equal("failed"))
			Expect(nodes[1].(map[string]any)["state"]).To(Equal("canceled"))
		})

		It("lists a group's batches", func() {
			online(1)
			id := startGroup(map[string]any{"command": "exec", "args": map[string]string{"cmd": "true"}})["id"].(string)

			resp := adminGet(testServerURL, "/api/v1/batches?groupID="+groupID, testAdminPassword)
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var out struct {
				Batches []map[string]any `json:"batches"`
				Total   int              `json:"total"`
			}
			decodeJSON(resp, &out)
			Expect(out.Total).To(Equal(1))
			Expect(out.Batches).To(HaveLen(1))
			Expect(out.Batches[0]["id"]).To(Equal(id))
		})

		It("stops a running batch", func() {
			online(2)
			id := startGroup(map[string]any{"command": "exec", "args": map[string]string{"cmd": "true"}, "concurrency": 1})["id"].(string)

			resp := adminPost(testServerURL, "/api/v1/batches/"+id+"/stop", testAdminPassword, nil)
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var b map[string]any
			decodeJSON(resp, &b)
			Expect(b["id"]).To(Equal(id))
			Expect(b["phase"]).To(Equal("stopped"))
			Expect(b["stoppedAt"]).NotTo(BeNil())
		})

		It("refuses to stop a batch that is not running", func() {
			online(1)
			id := startGroup(map[string]any{"command": "exec", "args": map[string]string{"cmd": "true"}})["id"].(string)
			Expect(adminPost(testServerURL, "/api/v1/batches/"+id+"/stop", testAdminPassword, nil).StatusCode).To(Equal(http.StatusOK))

			resp := adminPost(testServerURL, "/api/v1/batches/"+id+"/stop", testAdminPassword, nil)
			Expect(resp.StatusCode).To(Equal(http.StatusConflict))
			var body map[string]any
			decodeJSON(resp, &body)
			Expect(body["error"]).To(Equal("batch is not running"))
		})

		It("answers 404 for an unknown batch", func() {
			resp := adminGet(testServerURL, "/api/v1/batches/nope", testAdminPassword)
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			var body map[string]any
			decodeJSON(resp, &body)
			Expect(body["error"]).To(Equal("batch not found"))
			Expect(adminPost(testServerURL, "/api/v1/batches/nope/stop", testAdminPassword, nil).StatusCode).To(Equal(http.StatusNotFound))
		})
	})
})
