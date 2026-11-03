package integration_test

import (
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Labels", func() {
	It("should set labels on a node", func() {
		nodeID, _ := registerNode(testServerURL, testRegToken, "machine-label-1", "host-label-1")

		labels := map[string]string{"env": "production", "region": "us-east"}
		resp := adminPut(testServerURL, "/api/v1/nodes/"+nodeID+"/labels", testAdminPassword, map[string]interface{}{
			"labels": labels,
		})
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		resp.Body.Close()

		// Verify labels are set.
		resp = adminGet(testServerURL, "/api/v1/nodes/"+nodeID, testAdminPassword)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var node map[string]interface{}
		decodeJSON(resp, &node)

		nodeLabels := node["labels"].(map[string]interface{})
		Expect(nodeLabels["env"]).To(Equal("production"))
		Expect(nodeLabels["region"]).To(Equal("us-east"))
	})

	It("should overwrite labels on a node", func() {
		nodeID, _ := registerNode(testServerURL, testRegToken, "machine-label-2", "host-label-2")

		// Set initial labels.
		resp := adminPut(testServerURL, "/api/v1/nodes/"+nodeID+"/labels", testAdminPassword, map[string]interface{}{
			"labels": map[string]string{"env": "staging", "team": "infra"},
		})
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		resp.Body.Close()

		// Overwrite labels.
		resp = adminPut(testServerURL, "/api/v1/nodes/"+nodeID+"/labels", testAdminPassword, map[string]interface{}{
			"labels": map[string]string{"env": "production"},
		})
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		resp.Body.Close()

		// Verify only new labels remain.
		resp = adminGet(testServerURL, "/api/v1/nodes/"+nodeID, testAdminPassword)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var node map[string]interface{}
		decodeJSON(resp, &node)
		nodeLabels := node["labels"].(map[string]interface{})
		Expect(nodeLabels["env"]).To(Equal("production"))
		Expect(nodeLabels).NotTo(HaveKey("team"))
	})

	It("should filter nodes by labels", func() {
		// Register nodes with different labels.
		nodeA, _ := registerNode(testServerURL, testRegToken, "machine-label-filter-a", "host-filter-a")
		nodeB, _ := registerNode(testServerURL, testRegToken, "machine-label-filter-b", "host-filter-b")
		registerNode(testServerURL, testRegToken, "machine-label-filter-c", "host-filter-c")

		resp := adminPut(testServerURL, "/api/v1/nodes/"+nodeA+"/labels", testAdminPassword, map[string]interface{}{
			"labels": map[string]string{"tier": "frontend"},
		})
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		resp.Body.Close()

		resp = adminPut(testServerURL, "/api/v1/nodes/"+nodeB+"/labels", testAdminPassword, map[string]interface{}{
			"labels": map[string]string{"tier": "frontend"},
		})
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		resp.Body.Close()

		// Filter by label tier:frontend.
		resp = adminGet(testServerURL, "/api/v1/nodes?label=tier:frontend", testAdminPassword)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var nodes []map[string]interface{}
		decodeJSON(resp, &nodes)

		Expect(len(nodes)).To(BeNumerically(">=", 2))
		ids := []string{}
		for _, n := range nodes {
			ids = append(ids, n["id"].(string))
		}
		Expect(ids).To(ContainElements(nodeA, nodeB))
	})
})
