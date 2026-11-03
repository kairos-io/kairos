package integration_test

import (
	"fmt"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The agent sends the group and labels from its phonehome config when it
// registers. An artifact built with a target group writes that group's ID
// into the config; older artifacts wrote its name.
var _ = Describe("Registration with a group and labels", func() {
	var suffix string

	BeforeEach(func() {
		suffix = fmt.Sprintf("%d", time.Now().UnixNano())
	})

	register := func(machineID string, extra map[string]any) map[string]any {
		body := map[string]any{
			"registrationToken": testRegToken,
			"machineID":         machineID,
			"hostname":          "host-" + machineID,
		}
		for k, v := range extra {
			body[k] = v
		}
		resp := doPost(testServerURL, "/api/v1/nodes/register", "", body)
		Expect(resp.StatusCode).To(BeElementOf(http.StatusCreated, http.StatusOK))
		var out map[string]any
		decodeJSON(resp, &out)
		return out
	}

	node := func(id string) map[string]any {
		resp := adminGet(testServerURL, "/api/v1/nodes/"+id, testAdminPassword)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var n map[string]any
		decodeJSON(resp, &n)
		return n
	}

	createGroup := func(name string) string {
		resp := adminPost(testServerURL, "/api/v1/groups", testAdminPassword, map[string]any{"name": name})
		Expect(resp.StatusCode).To(Equal(http.StatusCreated))
		var g map[string]any
		decodeJSON(resp, &g)
		return g["id"].(string)
	}

	It("puts a new node in the group it names", func() {
		groupID := createGroup("edge-" + suffix)

		out := register("m-group-"+suffix, map[string]any{"group": "edge-" + suffix})

		Expect(node(out["id"].(string))["groupID"]).To(Equal(groupID))
	})

	It("gives a new node the labels it sends", func() {
		out := register("m-labels-"+suffix, map[string]any{"labels": map[string]string{"role": "worker", "site": "a"}})

		Expect(node(out["id"].(string))["labels"]).To(Equal(map[string]any{"role": "worker", "site": "a"}))
	})

	It("registers a node that names an unknown group without a group", func() {
		out := register("m-nogroup-"+suffix, map[string]any{"group": "missing-" + suffix})

		Expect(node(out["id"].(string))["groupID"]).To(BeNil())
	})

	It("keeps the group and labels an admin set when the node registers again", func() {
		createGroup("first-" + suffix)
		second := createGroup("second-" + suffix)
		machineID := "m-again-" + suffix
		id := register(machineID, map[string]any{"group": "first-" + suffix})["id"].(string)
		resp := adminPut(testServerURL, "/api/v1/nodes/"+id+"/group", testAdminPassword, map[string]any{"groupID": second})
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		resp.Body.Close()

		register(machineID, map[string]any{"group": "first-" + suffix, "labels": map[string]string{"role": "x"}})

		Expect(node(id)["groupID"]).To(Equal(second))
		Expect(node(id)["labels"]).NotTo(HaveKey("role"))
	})
})
