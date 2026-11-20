package integration_test

import (
	"fmt"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Upgrade lifecycle", func() {
	var nodeID string

	BeforeEach(func() {
		uniqueSuffix := fmt.Sprintf("%d", time.Now().UnixNano())
		nodeID, _ = registerNode(testServerURL, testRegToken, "machine-upgrade-"+uniqueSuffix, "host-upgrade")
	})

	issue := func(command string) string {
		resp := adminPost(testServerURL, "/api/v1/nodes/"+nodeID+"/commands", testAdminPassword,
			map[string]any{"command": command})
		Expect(resp.StatusCode).To(Equal(http.StatusCreated))
		var created map[string]any
		decodeJSON(resp, &created)
		return created["id"].(string)
	}

	report := func(cmdID, phase string) {
		resp := adminPut(testServerURL, "/api/v1/nodes/"+nodeID+"/commands/"+cmdID+"/status",
			testAdminPassword, map[string]string{"phase": phase})
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

	It("marks the upgrade failed when its command fails before the reboot", func() {
		report(issue("upgrade"), "Failed")

		Expect(node()["upgradeState"]).To(Equal("failed"))
	})

	// A completed upgrade has scheduled its reboot and owns the pending state
	// until the node re-registers. A second upgrade issued meanwhile must not
	// take that state over, or its failure would hide the first one's outcome.
	DescribeTable("keeps the pending state of an earlier command that awaits its reboot",
		func(command, stateField, requestedField string) {
			report(issue(command), "Completed")
			requested := node()[requestedField]
			Expect(requested).NotTo(BeNil())

			report(issue(command), "Failed")

			got := node()
			Expect(got[stateField]).To(Equal("pending"))
			Expect(got[requestedField]).To(Equal(requested))
		},
		Entry("upgrade", "upgrade", "upgradeState", "upgradeRequestedAt"),
		Entry("reset", "reset", "resetState", "resetRequestedAt"),
	)
})
