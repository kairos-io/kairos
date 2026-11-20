package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// An agent that already holds its credentials does not register again after a
// reboot: it reconnects and heartbeats. Its heartbeats are what tell AuroraBoot
// that an upgrade or reset reboot came back, and from which boot entry.
var _ = Describe("Lifecycle settled from heartbeats", func() {
	var (
		nodeID string
		apiKey string
		conn   *websocket.Conn
	)

	BeforeEach(func() {
		suffix := fmt.Sprintf("%d", time.Now().UnixNano())
		nodeID, apiKey = registerNode(testServerURL, testRegToken, "machine-hb-"+suffix, "host-hb-"+suffix)
		conn = connectWS(testServerURL, apiKey)
		DeferCleanup(conn.Close)
	})

	issue := func(command string) string {
		resp := adminPost(testServerURL, "/api/v1/nodes/"+nodeID+"/commands", testAdminPassword,
			map[string]any{"command": command})
		Expect(resp.StatusCode).To(Equal(http.StatusCreated))
		var created map[string]any
		decodeJSON(resp, &created)
		return created["id"].(string)
	}

	complete := func(cmdID string) {
		resp := adminPut(testServerURL, "/api/v1/nodes/"+nodeID+"/commands/"+cmdID+"/status",
			testAdminPassword, map[string]string{"phase": "Completed"})
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		resp.Body.Close()
	}

	node := func() map[string]any {
		resp := adminGet(testServerURL, "/api/v1/nodes/"+nodeID, testAdminPassword)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var n map[string]any
		decodeJSON(resp, &n)
		return n
	}

	field := func(name string) func() any {
		return func() any { return node()[name] }
	}

	heartbeat := func(bootState string, uptime time.Duration) map[string]any {
		return map[string]any{
			"agentVersion": "v4.4.0",
			"bootState":    bootState,
			"addresses":    []map[string]string{{"type": "ipv4", "address": "10.0.0.7"}},
			"metrics":      map[string]any{"uptimeSeconds": uint64(uptime.Seconds())},
		}
	}

	wsHeartbeat := func(bootState string, uptime time.Duration) {
		data, err := json.Marshal(heartbeat(bootState, uptime))
		Expect(err).NotTo(HaveOccurred())
		msg, err := json.Marshal(map[string]any{"type": "heartbeat", "data": json.RawMessage(data)})
		Expect(err).NotTo(HaveOccurred())
		Expect(conn.WriteMessage(websocket.TextMessage, msg)).To(Succeed())
	}

	restHeartbeat := func(bootState string, uptime time.Duration) {
		resp := doPost(testServerURL, "/api/v1/nodes/"+nodeID+"/heartbeat", apiKey, heartbeat(bootState, uptime))
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		resp.Body.Close()
	}

	It("records the boot state and addresses a WebSocket heartbeat reports", func() {
		wsHeartbeat("passive", time.Hour)

		Eventually(field("bootState"), 5*time.Second, 100*time.Millisecond).Should(Equal("passive"))
		Expect(node()["addresses"]).To(ContainElement(HaveKeyWithValue("address", "10.0.0.7")))
	})

	DescribeTable("settles an upgrade when the node heartbeats after rebooting",
		func(send func(string, time.Duration), bootState, want string) {
			complete(issue("upgrade"))

			send(bootState, 0)

			Eventually(field("upgradeState"), 5*time.Second, 100*time.Millisecond).Should(Equal(want))
		},
		Entry("done over the WebSocket", func(b string, u time.Duration) { wsHeartbeat(b, u) }, "active", "done"),
		Entry("done over REST", func(b string, u time.Duration) { restHeartbeat(b, u) }, "active", "done"),
		Entry("failed after falling back to passive", func(b string, u time.Duration) { wsHeartbeat(b, u) }, "passive", "failed"),
		Entry("failed after falling back to recovery", func(b string, u time.Duration) { wsHeartbeat(b, u) }, "recovery", "failed"),
	)

	It("keeps an upgrade its command failed as failed when the node heartbeats from a later boot", func() {
		cmdID := issue("upgrade")
		resp := adminPut(testServerURL, "/api/v1/nodes/"+nodeID+"/commands/"+cmdID+"/status",
			testAdminPassword, map[string]string{"phase": "Failed", "result": "exit status 1"})
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		resp.Body.Close()
		Eventually(field("upgradeState"), 5*time.Second, 100*time.Millisecond).Should(Equal("failed"))

		restHeartbeat("active", 0)

		Consistently(field("upgradeState"), time.Second, 100*time.Millisecond).Should(Equal("failed"))
	})

	It("stamps the last upgrade when it settles as done", func() {
		complete(issue("upgrade"))

		wsHeartbeat("active", 0)

		Eventually(field("lastUpgrade"), 5*time.Second, 100*time.Millisecond).ShouldNot(BeNil())
	})

	It("settles an upgrade whose command was cleared from the history while the node rebooted", func() {
		complete(issue("upgrade"))
		resp := adminDelete(testServerURL, "/api/v1/nodes/"+nodeID+"/commands", testAdminPassword)
		Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
		resp.Body.Close()

		wsHeartbeat("active", 0)

		Eventually(field("upgradeState"), 5*time.Second, 100*time.Millisecond).Should(Equal("done"))
	})

	It("does not settle a later upgrade with the reboot of an earlier one", func() {
		complete(issue("upgrade"))
		wsHeartbeat("active", 0)
		Eventually(field("upgradeState"), 5*time.Second, 100*time.Millisecond).Should(Equal("done"))

		issue("upgrade")
		wsHeartbeat("active", 0)

		Consistently(field("upgradeState"), time.Second, 100*time.Millisecond).Should(Equal("pending"))
	})

	It("leaves the upgrade pending for a heartbeat from a boot before the command completed", func() {
		complete(issue("upgrade"))

		wsHeartbeat("active", time.Hour)

		Consistently(field("upgradeState"), time.Second, 100*time.Millisecond).Should(Equal("pending"))
	})

	It("leaves the upgrade pending while its command has not completed", func() {
		issue("upgrade")

		wsHeartbeat("active", 0)

		Consistently(field("upgradeState"), time.Second, 100*time.Millisecond).Should(Equal("pending"))
	})

	It("leaves the upgrade pending for a heartbeat without an uptime", func() {
		complete(issue("upgrade"))

		data, err := json.Marshal(map[string]any{"agentVersion": "v4.3.0", "bootState": "active"})
		Expect(err).NotTo(HaveOccurred())
		msg, err := json.Marshal(map[string]any{"type": "heartbeat", "data": json.RawMessage(data)})
		Expect(err).NotTo(HaveOccurred())
		Expect(conn.WriteMessage(websocket.TextMessage, msg)).To(Succeed())

		Consistently(field("upgradeState"), time.Second, 100*time.Millisecond).Should(Equal("pending"))
	})

	It("follows a reset through the autoreset boot to done", func() {
		complete(issue("reset"))

		wsHeartbeat("autoreset", 0)
		Eventually(field("resetState"), 5*time.Second, 100*time.Millisecond).Should(Equal("in-progress"))

		wsHeartbeat("active", 0)
		Eventually(field("resetState"), 5*time.Second, 100*time.Millisecond).Should(Equal("done"))
	})

	It("marks a reset failed when the node comes back on passive", func() {
		complete(issue("reset"))

		wsHeartbeat("passive", 0)

		Eventually(field("resetState"), 5*time.Second, 100*time.Millisecond).Should(Equal("failed"))
	})
})
