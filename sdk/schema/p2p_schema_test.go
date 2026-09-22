package schema_test

import (
	"encoding/json"
	"strings"

	. "github.com/kairos-io/kairos/v4/sdk/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// schemaProps generates the P2PSchema and walks it down to the "properties"
// object at the given definition name, or the top-level one when name is "".
// Tests use it to assert on the shape of the generated JSON schema itself,
// for cases (like an unused/renamed key) that additionalProperties-permissive
// validation cannot tell apart from a typo.
func schemaProps(definition string) map[string]interface{} {
	raw, err := GenerateSchema(P2PSchema{}, "")
	ExpectWithOffset(1, err).ToNot(HaveOccurred())

	var doc map[string]interface{}
	ExpectWithOffset(1, json.Unmarshal([]byte(raw), &doc)).To(Succeed())

	if definition == "" {
		props, ok := doc["properties"].(map[string]interface{})
		ExpectWithOffset(1, ok).To(BeTrue(), "generated schema has no top-level properties object")
		return props
	}

	definitions, ok := doc["definitions"].(map[string]interface{})
	ExpectWithOffset(1, ok).To(BeTrue(), "generated schema has no definitions object")
	def, ok := definitions[definition].(map[string]interface{})
	ExpectWithOffset(1, ok).To(BeTrue(), "generated schema has no definition named %q", definition)
	props, ok := def["properties"].(map[string]interface{})
	ExpectWithOffset(1, ok).To(BeTrue(), "definition %q has no properties object", definition)
	return props
}

var _ = Describe("P2P Schema", func() {
	var config *KConfig
	var err error
	var yaml string

	JustBeforeEach(func() {
		config, err = NewConfigFromYAML(yaml, P2PSchema{})
		Expect(err).ToNot(HaveOccurred())
	})

	Context("with role master", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
role: master
network_token: "b3RwOgogIGRoYWdlX3NpemU6IDIwOTcxNTIwCg=="`
		})

		It("succeeds", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})

	Context("with role worker", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
role: worker
network_token: "b3RwOgogIGRoYWdlX3NpemU6IDIwOTcxNTIwCg=="`
		})

		It("succeeds", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})

	Context("with role none", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
role: none
network_token: "b3RwOgogIGRoYWdlX3NpemU6IDIwOTcxNTIwCg=="`
		})

		It("succeeds", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})

	Context("with other role", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
role: foobar
network_token: "b3RwOgogIGRoYWdlX3NpemU6IDIwOTcxNTIwCg=="`
		})

		It("errors", func() {
			Expect(config.IsValid()).NotTo(BeTrue())
			Expect(config.ValidationError.Error()).To(MatchRegexp(`value must be one of "master", "worker", "none"`))
		})
	})

	Context("With a network_token and p2p.auto.enable = false", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
network_token: "b3RwOgogIGRoYWdlX3NpemU6IDIwOTcxNTIwCg=="
auto:
  enable: false`
		})

		It("errors", func() {
			Expect(config.IsValid()).NotTo(BeTrue())
			Expect(
				strings.Contains(config.ValidationError.Error(), `value must be true`),
			).To(BeTrue())
		})
	})

	Context("With an empty network_token and p2p.auto.enable = true", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
network_token: ""
auto:
  enable: true`
		})

		It("Fails", func() {
			Expect(config.IsValid()).NotTo(BeTrue())
			Expect(
				strings.Contains(config.ValidationError.Error(),
					"length must be >= 1, but got 0",
				),
			).To(BeTrue())
		})
	})

	Context("With a network_token and p2p.auto.enable = true", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
network_token: "b3RwOgogIGRoYWdlX3NpemU6IDIwOTcxNTIwCg=="
auto:
  enable: true`
		})

		It("succeeds", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})

	Context("With a p2p.auto.enable = false and ha.enable = true", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
network_token: ""
auto:
  enable: false
  ha:
    enable: true`
		})

		It("errors", func() {
			Expect(config.IsValid()).NotTo(BeTrue())
			Expect(config.ValidationError.Error()).To(MatchRegexp("(length must be >= 1, but got 0|value must be true)"))
		})
	})

	Context("HA with 0 master nodes", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
network_token: "b3RwOgogIGRoYWdlX3NpemU6IDIwOTcxNTIwCg=="
auto:
  enable: true
  ha:
    enable: true
    master_nodes: 0`
		})

		It("fails", func() {
			Expect(config.IsValid()).NotTo(BeTrue())
			Expect(config.ValidationError.Error()).To(MatchRegexp("must be >= 1 but found 0"))
		})
	})

	Context("HA", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
network_token: "b3RwOgogIGRoYWdlX3NpemU6IDIwOTcxNTIwCg=="
auto:
  enable: true
  ha:
    enable: true
    master_nodes: 2`
		})

		It("succeedes", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})

	// kubevip is a top-level key, a sibling of p2p in the cloud-config, not
	// nested under it. It has no place in this file, which validates against
	// P2PSchema{} only; see sdk/schema/root_schema_test.go for its coverage
	// against RootSchema{}.

	Context("vpn", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
network_token: "b3RwOgogIGRoYWdlX3NpemU6IDIwOTcxNTIwCg=="
auto:
  enable: true
  ha:
    enable: true
vpn:
  env:
    DCHP: "true"`
		})

		It("succeedes", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})

	Context("vpn.create (kairos-io/kairos#4667)", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
network_token: "b3RwOgogIGRoYWdlX3NpemU6IDIwOTcxNTIwCg=="
auto:
  enable: true
  ha:
    enable: true
    master_nodes: 2
vpn:
  create: false`
		})

		It("validates a config that sets vpn.create", func() {
			Expect(config.IsValid()).To(BeTrue())
		})

		It("publishes create as the vpn property, and not the old unused vpn spelling", func() {
			props := schemaProps("SchemaVPN")
			Expect(props).To(HaveKey("create"))
			Expect(props).NotTo(HaveKey("vpn"))
		})
	})

	Context("loglevel", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
network_token: "b3RwOgogIGRoYWdlX3NpemU6IDIwOTcxNTIwCg=="
auto:
  enable: true
loglevel: debug`
		})

		It("validates", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})

	Context("minimum_nodes", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
network_token: "b3RwOgogIGRoYWdlX3NpemU6IDIwOTcxNTIwCg=="
auto:
  enable: true
minimum_nodes: 3`
		})

		It("validates", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})

	Context("dynamic_roles", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
network_token: "b3RwOgogIGRoYWdlX3NpemU6IDIwOTcxNTIwCg=="
auto:
  enable: true
dynamic_roles: true`
		})

		It("validates", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})

	Context("auto.ha.external_db", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
network_token: "b3RwOgogIGRoYWdlX3NpemU6IDIwOTcxNTIwCg=="
auto:
  enable: true
  ha:
    enable: true
    master_nodes: 2
    external_db: "https://etcd.example.com:2379"`
		})

		It("validates", func() {
			Expect(config.IsValid()).To(BeTrue())
		})

		It("is only declared on the auto-enabled branch of the oneOf", func() {
			enabled := schemaProps("SchemaP2PAutoEnabled")
			ha, ok := enabled["auto"].(map[string]interface{})["properties"].(map[string]interface{})["ha"].(map[string]interface{})["properties"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(ha).To(HaveKey("external_db"))

			disabled := schemaProps("SchemaP2PAutoDisabled")
			haDisabled, ok := disabled["auto"].(map[string]interface{})["properties"].(map[string]interface{})["ha"].(map[string]interface{})["properties"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(haDisabled).NotTo(HaveKey("external_db"))
		})
	})
})
