package integration_test

import (
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Groups", func() {
	It("should create a group", func() {
		resp := adminPost(testServerURL, "/api/v1/groups", testAdminPassword, map[string]interface{}{
			"name":        "test-create-group",
			"description": "A test group",
		})
		Expect(resp.StatusCode).To(Equal(http.StatusCreated))

		var group map[string]interface{}
		decodeJSON(resp, &group)
		Expect(group["id"]).NotTo(BeEmpty())
		Expect(group["name"]).To(Equal("test-create-group"))
		Expect(group["description"]).To(Equal("A test group"))
	})

	It("should list groups", func() {
		// Create a group first.
		resp := adminPost(testServerURL, "/api/v1/groups", testAdminPassword, map[string]interface{}{
			"name":        "test-list-group",
			"description": "For listing",
		})
		Expect(resp.StatusCode).To(Equal(http.StatusCreated))
		resp.Body.Close()

		// List groups.
		resp = adminGet(testServerURL, "/api/v1/groups", testAdminPassword)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		var groups []map[string]interface{}
		decodeJSON(resp, &groups)
		Expect(len(groups)).To(BeNumerically(">=", 1))

		found := false
		for _, g := range groups {
			if g["name"] == "test-list-group" {
				found = true
				break
			}
		}
		Expect(found).To(BeTrue())
	})

	It("should get a group by ID", func() {
		resp := adminPost(testServerURL, "/api/v1/groups", testAdminPassword, map[string]interface{}{
			"name":        "test-get-group",
			"description": "For getting",
		})
		Expect(resp.StatusCode).To(Equal(http.StatusCreated))
		var created map[string]interface{}
		decodeJSON(resp, &created)
		groupID := created["id"].(string)

		resp = adminGet(testServerURL, "/api/v1/groups/"+groupID, testAdminPassword)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var group map[string]interface{}
		decodeJSON(resp, &group)
		Expect(group["name"]).To(Equal("test-get-group"))
	})

	It("should update a group", func() {
		resp := adminPost(testServerURL, "/api/v1/groups", testAdminPassword, map[string]interface{}{
			"name":        "test-update-group",
			"description": "Before update",
		})
		Expect(resp.StatusCode).To(Equal(http.StatusCreated))
		var created map[string]interface{}
		decodeJSON(resp, &created)
		groupID := created["id"].(string)

		// Update.
		resp = adminPut(testServerURL, "/api/v1/groups/"+groupID, testAdminPassword, map[string]interface{}{
			"name":        "test-update-group-renamed",
			"description": "After update",
		})
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var updated map[string]interface{}
		decodeJSON(resp, &updated)
		Expect(updated["name"]).To(Equal("test-update-group-renamed"))
		Expect(updated["description"]).To(Equal("After update"))
	})

	It("should delete a group", func() {
		resp := adminPost(testServerURL, "/api/v1/groups", testAdminPassword, map[string]interface{}{
			"name":        "test-delete-group",
			"description": "To be deleted",
		})
		Expect(resp.StatusCode).To(Equal(http.StatusCreated))
		var created map[string]interface{}
		decodeJSON(resp, &created)
		groupID := created["id"].(string)

		// Delete.
		resp = adminDelete(testServerURL, "/api/v1/groups/"+groupID, testAdminPassword)
		Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
		resp.Body.Close()

		// Verify it's gone.
		resp = adminGet(testServerURL, "/api/v1/groups/"+groupID, testAdminPassword)
		Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		resp.Body.Close()
	})

	It("should reject creating a group without name", func() {
		resp := adminPost(testServerURL, "/api/v1/groups", testAdminPassword, map[string]interface{}{
			"description": "No name",
		})
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		resp.Body.Close()
	})
})
