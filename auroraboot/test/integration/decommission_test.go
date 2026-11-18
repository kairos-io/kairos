package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Decommission covers the POST /api/v1/nodes/:id/decommission endpoint.
// The handler unit test in pkg/handlers exercises the offline path
// synthetically; this suite runs against the real server so we can drive
// the online path through an actual WebSocket connection and observe the
// `unregister` command land on the wire.
var _ = Describe("Decommission", func() {
	It("reports nodeOnline=false for an offline node", func() {
		// Register but never connect the WS, so hub.IsOnline returns false.
		nodeID, _ := registerNode(testServerURL, testRegToken, "machine-decomm-offline-"+unique(), "host-decomm-offline")

		resp := adminPost(testServerURL, "/api/v1/nodes/"+nodeID+"/decommission", testAdminPassword, nil)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var body map[string]interface{}
		decodeJSON(resp, &body)
		Expect(body["nodeOnline"]).To(Equal(false))
		Expect(body["commandID"]).To(Equal(""))

		// No command should have been queued; the CLI fallback is the
		// operator's job.
		cmdsResp := adminGet(testServerURL, "/api/v1/nodes/"+nodeID+"/commands", testAdminPassword)
		Expect(cmdsResp.StatusCode).To(Equal(http.StatusOK))
		var cmds []map[string]interface{}
		decodeJSON(cmdsResp, &cmds)
		for _, cmd := range cmds {
			Expect(cmd["command"]).ToNot(Equal("unregister"), "offline decommission must not queue a command")
		}
	})

	It("delivers an `unregister` command over the WS when the node is online", func() {
		nodeID, apiKey := registerNode(testServerURL, testRegToken, "machine-decomm-online-"+unique(), "host-decomm-online")

		// Connect a mock agent and wait until the server records Online.
		conn := connectWS(testServerURL, apiKey)
		defer conn.Close()
		Eventually(func() string {
			resp := adminGet(testServerURL, "/api/v1/nodes/"+nodeID, testAdminPassword)
			var node map[string]interface{}
			decodeJSON(resp, &node)
			if p, ok := node["phase"].(string); ok {
				return p
			}
			return ""
		}, 3*time.Second, 100*time.Millisecond).Should(Equal("Online"))

		resp := adminPost(testServerURL, "/api/v1/nodes/"+nodeID+"/decommission", testAdminPassword, nil)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var body map[string]interface{}
		decodeJSON(resp, &body)
		Expect(body["nodeOnline"]).To(Equal(true))
		cmdID, ok := body["commandID"].(string)
		Expect(ok).To(BeTrue())
		Expect(cmdID).ToNot(BeEmpty())

		// Read the WS frame — the server must push the command immediately
		// rather than waiting for the agent to poll.
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		_, raw, err := conn.ReadMessage()
		Expect(err).ToNot(HaveOccurred(), "expected unregister command frame")

		var frame struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		Expect(json.Unmarshal(raw, &frame)).To(Succeed())
		Expect(frame.Type).To(Equal("command"))

		var payload struct {
			ID      string `json:"id"`
			Command string `json:"command"`
		}
		Expect(json.Unmarshal(frame.Data, &payload)).To(Succeed())
		Expect(payload.Command).To(Equal("unregister"))
		Expect(payload.ID).To(Equal(cmdID))

		// And the persisted command record must match — the UI polls
		// /commands as a fallback when the WebSocket is stale.
		cmdsResp := adminGet(testServerURL, "/api/v1/nodes/"+nodeID+"/commands", testAdminPassword)
		var cmds []map[string]interface{}
		decodeJSON(cmdsResp, &cmds)
		var found map[string]interface{}
		for _, cmd := range cmds {
			if cmd["id"] == cmdID {
				found = cmd
				break
			}
		}
		Expect(found).ToNot(BeNil(), "persisted command not found")
		Expect(found["command"]).To(Equal("unregister"))
		Expect(found["expiresAt"]).ToNot(BeNil(), "ExpiresAt should be set to bound the UI's wait window")
	})

	It("returns 404 when the node does not exist", func() {
		resp := adminPost(testServerURL, "/api/v1/nodes/no-such-node/decommission", testAdminPassword, nil)
		Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
	})
})

// unique returns a per-test suffix so machine IDs don't collide across
// parallel specs sharing the same server + DB.
func unique() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
