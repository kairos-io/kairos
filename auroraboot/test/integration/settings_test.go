package integration_test

import (
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Settings", func() {
	// Rotation specs mutate the server's registration token, which every
	// other spec in the suite depends on via the package-global
	// `testRegToken`. The rotation middleware now correctly reads the
	// live pointer on every request (was a latent bug: it captured the
	// pre-rotation value at server startup), so a rotation here really
	// does invalidate the token every other test uses.
	//
	// After each rotation spec, refresh `testRegToken` from the server so
	// whatever spec runs next (in any order, parallel or serial) sees the
	// current token. Cheaper than refactoring to per-spec servers.
	AfterEach(func() {
		resp := adminGet(testServerURL, "/api/v1/settings/registration-token", testAdminPassword)
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return
		}
		var cur map[string]string
		decodeJSON(resp, &cur)
		if t := cur["registrationToken"]; t != "" {
			testRegToken = t
		}
	})

	It("should get registration token", func() {
		resp := adminGet(testServerURL, "/api/v1/settings/registration-token", testAdminPassword)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var result map[string]string
		decodeJSON(resp, &result)
		Expect(result["registrationToken"]).NotTo(BeEmpty())
	})

	It("should rotate registration token", func() {
		// Get the current token.
		resp := adminGet(testServerURL, "/api/v1/settings/registration-token", testAdminPassword)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var before map[string]string
		decodeJSON(resp, &before)
		oldToken := before["registrationToken"]

		// Rotate.
		resp = adminPost(testServerURL, "/api/v1/settings/registration-token/rotate", testAdminPassword, nil)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var rotated map[string]string
		decodeJSON(resp, &rotated)
		newToken := rotated["registrationToken"]

		Expect(newToken).NotTo(BeEmpty())
		Expect(newToken).NotTo(Equal(oldToken))

		// Verify the new token is returned on subsequent get.
		resp = adminGet(testServerURL, "/api/v1/settings/registration-token", testAdminPassword)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var after map[string]string
		decodeJSON(resp, &after)
		Expect(after["registrationToken"]).To(Equal(newToken))
	})

	It("should reject old token after rotation", func() {
		// Get current token.
		resp := adminGet(testServerURL, "/api/v1/settings/registration-token", testAdminPassword)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var before map[string]string
		decodeJSON(resp, &before)
		oldToken := before["registrationToken"]

		// Rotate.
		resp = adminPost(testServerURL, "/api/v1/settings/registration-token/rotate", testAdminPassword, nil)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		resp.Body.Close()

		// Try to register with old token -- should fail.
		body := map[string]interface{}{
			"registrationToken": oldToken,
			"machineID":         "machine-old-token",
			"hostname":          "host-old-token",
		}
		resp = doPost(testServerURL, "/api/v1/nodes/register", "", body)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
	})

	It("should reject unauthenticated settings request", func() {
		req, err := http.NewRequest("GET", testServerURL+"/api/v1/settings/registration-token", nil)
		Expect(err).NotTo(HaveOccurred())
		// No auth header.
		resp, err := http.DefaultClient.Do(req)
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
	})
})
