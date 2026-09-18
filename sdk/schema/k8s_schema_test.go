package schema_test

import (
	"encoding/json"

	. "github.com/kairos-io/kairos/v4/sdk/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// declaredProperties returns the top-level property names of the generated
// root schema. Asserting on these has teeth; asserting that a config
// validates does not, because the schema declares no additionalProperties
// and so accepts every unknown key.
func declaredProperties() map[string]interface{} {
	generated, err := GenerateSchema(RootSchema{}, "")
	ExpectWithOffset(1, err).ToNot(HaveOccurred())

	var root struct {
		Properties map[string]interface{} `json:"properties"`
	}
	ExpectWithOffset(1, json.Unmarshal([]byte(generated), &root)).To(Succeed())

	return root.Properties
}

func validates(body string) bool {
	config, err := NewConfigFromYAML("#cloud-config\nusers:\n- name: kairos\n"+body, RootSchema{})
	ExpectWithOffset(1, err).ToNot(HaveOccurred())

	return config.IsValid()
}

var _ = Describe("Kubernetes blocks", func() {
	Context("the generated root schema", func() {
		It("declares every block the provider unmarshals", func() {
			properties := declaredProperties()

			for _, block := range []string{"k3s", "k3s-agent", "k0s", "k0s-worker", "kubevip"} {
				Expect(properties).To(HaveKey(block), "%q is read by provider config.Config but is not in RootSchema", block)
			}
		})
	})

	Context("with a value of the wrong type", func() {
		It("rejects a k3s.args string, which the provider needs as a list", func() {
			Expect(validates("k3s:\n  args: \"--disable=traefik\"\n")).To(BeFalse())
		})

		It("rejects a non-boolean k0s-worker.enabled", func() {
			Expect(validates("k0s-worker:\n  enabled: 1\n")).To(BeFalse())
		})

		It("rejects a non-boolean k3s-agent.enabled", func() {
			Expect(validates("k3s-agent:\n  enabled: \"yes\"\n")).To(BeFalse())
		})

		It("rejects a non-boolean k0s.enabled", func() {
			Expect(validates("k0s:\n  enabled: \"true\"\n")).To(BeFalse())
		})

		It("rejects a kubevip.interface boolean, which the provider reads as a string", func() {
			Expect(validates("kubevip:\n  interface: true\n")).To(BeFalse())
		})
	})

	// Declaring these blocks must not start rejecting configs that work
	// today. The schema sets additionalProperties nowhere, so unknown keys
	// stay accepted, which is what keeps the embedded kubevip.Config keys
	// and any newer provider option valid.
	Context("with keys the schema does not declare", func() {
		It("still accepts the kubevip.Config keys the provider embeds", func() {
			Expect(validates("kubevip:\n  eip: 192.168.1.110\n  vip_interface: eth0\n  vip_leaderelection: true\n")).To(BeTrue())
		})

		It("still accepts an unknown k3s key", func() {
			Expect(validates("k3s:\n  enabled: true\n  some_future_option: hello\n")).To(BeTrue())
		})
	})

	Context("with the keys the provider actually reads", func() {
		It("accepts a fully populated k3s block", func() {
			Expect(validates("k3s:\n  enabled: true\n  replace_env: true\n  replace_args: false\n  embedded_registry: true\n  args:\n  - --disable=traefik\n  env:\n    K3S_TOKEN: secret\n")).To(BeTrue())
		})

		It("accepts a fully populated k0s block", func() {
			Expect(validates("k0s:\n  enabled: true\n  replace_env: false\n  replace_args: true\n  args:\n  - --single\n  env:\n    FOO: bar\n")).To(BeTrue())
		})

		It("accepts every kubevip key the provider reads", func() {
			Expect(validates("kubevip:\n  eip: 192.168.1.110\n  manifest_url: https://example.com/kube-vip.yaml\n  interface: ens18\n  enable: true\n  static_pod: true\n  version: v0.6.4\n  image: ghcr.io/kube-vip/kube-vip:v0.6.4\n")).To(BeTrue())
		})
	})
})
