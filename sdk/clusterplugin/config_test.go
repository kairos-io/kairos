package clusterplugin

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

var _ = Describe("Cluster JSON", func() {
	// The role used to be handed to encoding/json as raw bytes, so a cluster
	// with a role could not be encoded through a pointer and came back with
	// the quotes still in the field after a round trip. Providers outside
	// this repository read and write Cluster as JSON (kairos-io/kairos#5952).
	DescribeTable("round trips the role as a plain string",
		func(role Role) {
			encoded, err := json.Marshal(&Cluster{Role: role})
			Expect(err).ToNot(HaveOccurred())
			Expect(string(encoded)).To(ContainSubstring(`"role":"` + string(role) + `"`))

			var decoded Cluster
			Expect(json.Unmarshal(encoded, &decoded)).To(Succeed())
			Expect(decoded.Role).To(Equal(role))
		},
		Entry("worker", Role(RoleWorker)),
		Entry("control plane", Role(RoleControlPlane)),
		Entry("init", Role(RoleInit)),
	)

	It("accepts a role written by hand as a bare JSON string", func() {
		var decoded Cluster
		Expect(json.Unmarshal([]byte(`{"role":"worker"}`), &decoded)).To(Succeed())
		Expect(decoded.Role).To(Equal(Role(RoleWorker)))
	})

	It("keeps a role it does not know", func() {
		// The field is typed, not validated here: an unknown role has to
		// survive the round trip rather than silently become empty.
		var decoded Cluster
		Expect(json.Unmarshal([]byte(`{"role":"future-role"}`), &decoded)).To(Succeed())
		Expect(decoded.Role).To(Equal(Role("future-role")))
	})

	It("keeps the YAML decoding the boot path uses", func() {
		// onBoot goes through YAML, and the JSON change must not disturb it.
		var decoded Config
		Expect(yaml.Unmarshal([]byte("cluster:\n  role: worker\n"), &decoded)).To(Succeed())
		Expect(decoded.Cluster).ToNot(BeNil())
		Expect(decoded.Cluster.Role).To(Equal(Role(RoleWorker)))
	})
})
