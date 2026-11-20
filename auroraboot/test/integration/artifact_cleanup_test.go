package integration_test

import (
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Artifact cleanup", func() {
	It("should delete a single artifact", func() {
		// Create an artifact via POST.
		body := map[string]interface{}{
			"baseImage": "quay.io/kairos/ubuntu:24.04",
			"iso":       true,
		}
		createResp := adminPost(testServerURL, "/api/v1/artifacts", testAdminPassword, body)
		Expect(createResp.StatusCode).To(Equal(http.StatusCreated))
		var created map[string]interface{}
		decodeJSON(createResp, &created)
		artID := created["id"].(string)
		Expect(artID).NotTo(BeEmpty())

		// DELETE the artifact.
		delResp := adminDelete(testServerURL, "/api/v1/artifacts/"+artID, testAdminPassword)
		Expect(delResp.StatusCode).To(Equal(http.StatusNoContent))
		delResp.Body.Close()

		// Verify it's gone.
		getResp := adminGet(testServerURL, "/api/v1/artifacts/"+artID, testAdminPassword)
		Expect(getResp.StatusCode).To(Equal(http.StatusNotFound))
		getResp.Body.Close()
	})
})
