package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Commands", func() {
	var (
		node1ID, node2ID, node3ID string
		groupID                   string
	)

	BeforeEach(func() {
		// Use a unique group name per test to avoid UNIQUE constraint failures.
		uniqueSuffix := fmt.Sprintf("%d", time.Now().UnixNano())

		// Create a group.
		groupResp := adminPost(testServerURL, "/api/v1/groups", testAdminPassword, map[string]interface{}{
			"name":        "cmd-test-group-" + uniqueSuffix,
			"description": "Group for command tests",
		})
		Expect(groupResp.StatusCode).To(Equal(http.StatusCreated))
		var group map[string]interface{}
		decodeJSON(groupResp, &group)
		groupID = group["id"].(string)

		// Register 3 nodes with unique machine IDs.
		node1ID, _ = registerNode(testServerURL, testRegToken, "machine-cmd-1-"+uniqueSuffix, "host-cmd-1")
		node2ID, _ = registerNode(testServerURL, testRegToken, "machine-cmd-2-"+uniqueSuffix, "host-cmd-2")
		node3ID, _ = registerNode(testServerURL, testRegToken, "machine-cmd-3-"+uniqueSuffix, "host-cmd-3")

		// Assign nodes 1 and 2 to the group.
		resp := adminPut(testServerURL, "/api/v1/nodes/"+node1ID+"/group", testAdminPassword, map[string]string{"groupID": groupID})
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		resp.Body.Close()

		resp = adminPut(testServerURL, "/api/v1/nodes/"+node2ID+"/group", testAdminPassword, map[string]string{"groupID": groupID})
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		resp.Body.Close()

		// Set labels on nodes.
		resp = adminPut(testServerURL, "/api/v1/nodes/"+node1ID+"/labels", testAdminPassword, map[string]interface{}{
			"labels": map[string]string{"env": "prod", "role": "worker"},
		})
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		resp.Body.Close()

		resp = adminPut(testServerURL, "/api/v1/nodes/"+node2ID+"/labels", testAdminPassword, map[string]interface{}{
			"labels": map[string]string{"env": "prod", "role": "master"},
		})
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		resp.Body.Close()

		resp = adminPut(testServerURL, "/api/v1/nodes/"+node3ID+"/labels", testAdminPassword, map[string]interface{}{
			"labels": map[string]string{"env": "staging", "role": "worker"},
		})
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		resp.Body.Close()
	})

	It("should send command to single node", func() {
		cmdBody := map[string]interface{}{
			"command": "exec",
			"args":    map[string]string{"cmd": "uname -a"},
		}
		resp := adminPost(testServerURL, "/api/v1/nodes/"+node1ID+"/commands", testAdminPassword, cmdBody)
		Expect(resp.StatusCode).To(Equal(http.StatusCreated))
		var cmd map[string]interface{}
		decodeJSON(resp, &cmd)
		Expect(cmd["id"]).NotTo(BeEmpty())
		Expect(cmd["command"]).To(Equal("exec"))
		Expect(cmd["managedNodeID"]).To(Equal(node1ID))
		Expect(cmd["phase"]).To(Equal("Pending"))
	})

	// batchNodeIDs returns the IDs of the nodes a batch record lists.
	batchNodeIDs := func(batch map[string]interface{}) []string {
		var ids []string
		for _, n := range batch["nodes"].([]interface{}) {
			ids = append(ids, n.(map[string]interface{})["nodeID"].(string))
		}
		return ids
	}

	It("should start a batch over all nodes in a group", func() {
		cmdBody := map[string]interface{}{
			"command": "upgrade",
			"args":    map[string]string{"version": "1.0.0"},
		}
		resp := adminPost(testServerURL, "/api/v1/groups/"+groupID+"/commands", testAdminPassword, cmdBody)
		Expect(resp.StatusCode).To(Equal(http.StatusCreated))

		var batch map[string]interface{}
		decodeJSON(resp, &batch)

		// Nodes 1 and 2 are in the group.
		Expect(batch["command"]).To(Equal("upgrade"))
		Expect(batch["groupID"]).To(Equal(groupID))
		Expect(batchNodeIDs(batch)).To(ConsistOf(node1ID, node2ID))
	})

	It("should start a batch by label selector", func() {
		cmdBody := map[string]interface{}{
			"selector": map[string]interface{}{
				"labels": map[string]string{"role": "worker"},
			},
			"command": "reset",
			"args":    map[string]string{},
		}
		resp := adminPost(testServerURL, "/api/v1/nodes/commands", testAdminPassword, cmdBody)
		Expect(resp.StatusCode).To(Equal(http.StatusCreated))

		var batch map[string]interface{}
		decodeJSON(resp, &batch)

		// Nodes 1 and 3 have role=worker; earlier specs left more.
		Expect(batch["selection"]).To(Equal("label role=worker"))
		Expect(batchNodeIDs(batch)).To(ContainElements(node1ID, node3ID))
	})

	It("should start a batch by group + label (AND)", func() {
		cmdBody := map[string]interface{}{
			"selector": map[string]interface{}{
				"groupID": groupID,
				"labels":  map[string]string{"role": "worker"},
			},
			"command": "exec",
			"args":    map[string]string{"cmd": "date"},
		}
		resp := adminPost(testServerURL, "/api/v1/nodes/commands", testAdminPassword, cmdBody)
		Expect(resp.StatusCode).To(Equal(http.StatusCreated))

		var batch map[string]interface{}
		decodeJSON(resp, &batch)

		// Only node1 is in group AND has role=worker.
		Expect(batchNodeIDs(batch)).To(ConsistOf(node1ID))
		Expect(batch["groupID"]).To(Equal(groupID))
		Expect(batch["selection"]).To(MatchRegexp(`^group cmd-test-group-\d+, label role=worker$`))
	})

	It("describes picked nodes by their count", func() {
		for text, ids := range map[string][]string{
			"1 selected node":  {node1ID},
			"2 selected nodes": {node2ID, node3ID},
		} {
			resp := adminPost(testServerURL, "/api/v1/nodes/commands", testAdminPassword, map[string]interface{}{
				"selector": map[string]interface{}{"nodeIDs": ids},
				"command":  "exec",
			})
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))
			var batch map[string]interface{}
			decodeJSON(resp, &batch)
			Expect(batch["selection"]).To(Equal(text))
		}
	})

	It("should reject command without command field", func() {
		cmdBody := map[string]interface{}{
			"args": map[string]string{"cmd": "uname"},
		}
		resp := adminPost(testServerURL, "/api/v1/nodes/"+node1ID+"/commands", testAdminPassword, cmdBody)
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		var result map[string]string
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()
		Expect(result["error"]).To(ContainSubstring("command"))
	})
})
