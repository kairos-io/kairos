package clusterplugin_test

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos/v4/sdk/clusterplugin"
)

var _ = Describe("Role JSON serialization", func() {
	It("marshals and unmarshals Role through JSON correctly", func() {
		c := clusterplugin.Cluster{Role: clusterplugin.RoleWorker}

		// Marshal via pointer
		data, err := json.Marshal(&c)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(data)).To(MatchJSON(`{"role":"worker"}`))

		// Marshal value
		valData, err := json.Marshal(c)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(valData)).To(MatchJSON(`{"role":"worker"}`))

		// Unmarshal
		var d clusterplugin.Cluster
		err = json.Unmarshal([]byte(`{"role":"worker"}`), &d)
		Expect(err).ToNot(HaveOccurred())
		Expect(d.Role).To(Equal(clusterplugin.RoleWorker))

		// Round-trip all defined roles
		for _, role := range []clusterplugin.Role{
			clusterplugin.RoleInit,
			clusterplugin.RoleControlPlane,
			clusterplugin.RoleWorker,
		} {
			input := clusterplugin.Cluster{Role: role}
			b, err := json.Marshal(&input)
			Expect(err).ToNot(HaveOccurred())

			var output clusterplugin.Cluster
			err = json.Unmarshal(b, &output)
			Expect(err).ToNot(HaveOccurred())
			Expect(output.Role).To(Equal(role))
		}
	})
})
