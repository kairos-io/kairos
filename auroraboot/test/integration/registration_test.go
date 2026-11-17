package integration_test

import (
	"encoding/json"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Node Registration", func() {
	It("should register a new node with valid token", func() {
		nodeID, apiKey := registerNode(testServerURL, testRegToken, "machine-reg-1", "host-reg-1")
		Expect(nodeID).NotTo(BeEmpty())
		Expect(apiKey).NotTo(BeEmpty())

		// Verify via admin API
		resp := adminGet(testServerURL, "/api/v1/nodes/"+nodeID, testAdminPassword)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var node map[string]interface{}
		decodeJSON(resp, &node)
		Expect(node["hostname"]).To(Equal("host-reg-1"))
		Expect(node["phase"]).To(Equal("Registered"))
	})

	It("should reject registration with invalid token", func() {
		body := map[string]interface{}{
			"registrationToken": "wrong-token",
			"machineID":         "machine-bad",
			"hostname":          "host-bad",
		}
		resp := doPost(testServerURL, "/api/v1/nodes/register", "", body)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
	})

	It("should handle re-registration (return existing node)", func() {
		nodeID1, apiKey1 := registerNode(testServerURL, testRegToken, "machine-rereg-1", "host-rereg-1")

		// Register again with same machineID
		nodeID2, apiKey2 := registerNode(testServerURL, testRegToken, "machine-rereg-1", "host-rereg-updated")

		Expect(nodeID2).To(Equal(nodeID1))
		Expect(apiKey2).To(Equal(apiKey1))
	})

	It("should list registered nodes via admin API", func() {
		// Register a node with a unique machineID
		registerNode(testServerURL, testRegToken, "machine-list-1", "host-list-1")

		resp := adminGet(testServerURL, "/api/v1/nodes", testAdminPassword)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var nodes []map[string]interface{}
		decodeJSON(resp, &nodes)
		Expect(len(nodes)).To(BeNumerically(">=", 1))

		// Find our node
		found := false
		for _, n := range nodes {
			if n["machineID"] == "machine-list-1" {
				found = true
				break
			}
		}
		Expect(found).To(BeTrue(), "registered node should appear in list")
	})

	It("should delete a node", func() {
		nodeID, _ := registerNode(testServerURL, testRegToken, "machine-delete-1", "host-delete-1")

		resp := adminDelete(testServerURL, "/api/v1/nodes/"+nodeID, testAdminPassword)
		Expect(resp.StatusCode).To(Equal(http.StatusNoContent))

		// Verify node is gone
		resp2 := adminGet(testServerURL, "/api/v1/nodes/"+nodeID, testAdminPassword)
		Expect(resp2.StatusCode).To(Equal(http.StatusNotFound))
		resp2.Body.Close()
	})

	It("should require machineID for registration", func() {
		body := map[string]interface{}{
			"registrationToken": testRegToken,
			"machineID":         "",
			"hostname":          "host-no-machine",
		}
		resp := doPost(testServerURL, "/api/v1/nodes/register", "", body)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))

		var result map[string]string
		json.NewDecoder(resp.Body).Decode(&result)
		Expect(result["error"]).To(ContainSubstring("machineID"))
	})
})
