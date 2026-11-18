package integration_test

import (
	"fmt"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Command lifecycle", func() {
	var (
		nodeID string
	)

	BeforeEach(func() {
		uniqueSuffix := fmt.Sprintf("%d", time.Now().UnixNano())
		nodeID, _ = registerNode(testServerURL, testRegToken, "machine-lifecycle-"+uniqueSuffix, "host-lifecycle")
	})

	It("should show command in history after creation", func() {
		cmdBody := map[string]interface{}{
			"command": "exec",
			"args":    map[string]string{"cmd": "echo hello"},
		}
		resp := adminPost(testServerURL, "/api/v1/nodes/"+nodeID+"/commands", testAdminPassword, cmdBody)
		Expect(resp.StatusCode).To(Equal(http.StatusCreated))
		var created map[string]interface{}
		decodeJSON(resp, &created)
		cmdID := created["id"].(string)

		// GET commands for node — should include the command we just created.
		listResp := adminGet(testServerURL, "/api/v1/nodes/"+nodeID+"/commands", testAdminPassword)
		Expect(listResp.StatusCode).To(Equal(http.StatusOK))
		var cmds []map[string]interface{}
		decodeJSON(listResp, &cmds)
		Expect(cmds).NotTo(BeEmpty())

		found := false
		for _, c := range cmds {
			if c["id"] == cmdID {
				found = true
				break
			}
		}
		Expect(found).To(BeTrue(), "created command should appear in list")
	})

	It("should delete a single command", func() {
		cmdBody := map[string]interface{}{
			"command": "exec",
			"args":    map[string]string{"cmd": "echo delete-me"},
		}
		resp := adminPost(testServerURL, "/api/v1/nodes/"+nodeID+"/commands", testAdminPassword, cmdBody)
		Expect(resp.StatusCode).To(Equal(http.StatusCreated))
		var created map[string]interface{}
		decodeJSON(resp, &created)
		cmdID := created["id"].(string)

		// DELETE the command.
		delResp := adminDelete(testServerURL, "/api/v1/nodes/"+nodeID+"/commands/"+cmdID, testAdminPassword)
		Expect(delResp.StatusCode).To(Equal(http.StatusNoContent))
		delResp.Body.Close()

		// Verify it's gone from the list.
		listResp := adminGet(testServerURL, "/api/v1/nodes/"+nodeID+"/commands", testAdminPassword)
		Expect(listResp.StatusCode).To(Equal(http.StatusOK))
		var cmds []map[string]interface{}
		decodeJSON(listResp, &cmds)
		for _, c := range cmds {
			Expect(c["id"]).NotTo(Equal(cmdID), "deleted command should not appear in list")
		}
	})

	It("should clear all terminal commands", func() {
		// Create several commands.
		for i := 0; i < 3; i++ {
			cmdBody := map[string]interface{}{
				"command": "exec",
				"args":    map[string]string{"cmd": fmt.Sprintf("echo %d", i)},
			}
			resp := adminPost(testServerURL, "/api/v1/nodes/"+nodeID+"/commands", testAdminPassword, cmdBody)
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))
			var created map[string]interface{}
			decodeJSON(resp, &created)

			// Mark some as terminal via status update.
			if i == 0 {
				statusResp := adminPut(testServerURL, "/api/v1/nodes/"+nodeID+"/commands/"+created["id"].(string)+"/status",
					testAdminPassword, map[string]string{"phase": "Completed", "result": "done"})
				Expect(statusResp.StatusCode).To(Equal(http.StatusOK))
				statusResp.Body.Close()
			}
			if i == 1 {
				statusResp := adminPut(testServerURL, "/api/v1/nodes/"+nodeID+"/commands/"+created["id"].(string)+"/status",
					testAdminPassword, map[string]string{"phase": "Failed", "result": "error"})
				Expect(statusResp.StatusCode).To(Equal(http.StatusOK))
				statusResp.Body.Close()
			}
			// i == 2 stays Pending.
		}

		// DELETE all terminal commands.
		delResp := adminDelete(testServerURL, "/api/v1/nodes/"+nodeID+"/commands", testAdminPassword)
		Expect(delResp.StatusCode).To(Equal(http.StatusNoContent))
		delResp.Body.Close()

		// Verify only the Pending command remains.
		listResp := adminGet(testServerURL, "/api/v1/nodes/"+nodeID+"/commands", testAdminPassword)
		Expect(listResp.StatusCode).To(Equal(http.StatusOK))
		var cmds []map[string]interface{}
		decodeJSON(listResp, &cmds)

		for _, c := range cmds {
			phase := c["phase"].(string)
			Expect(phase).NotTo(Equal("Completed"))
			Expect(phase).NotTo(Equal("Failed"))
		}
	})
})
