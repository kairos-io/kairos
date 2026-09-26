package schema_test

import (
	"strings"

	. "github.com/kairos-io/kairos/v4/sdk/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

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

	Context("HA with a non-string external datastore", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
network_token: "b3RwOgogIGRoYWdlX3NpemU6IDIwOTcxNTIwCg=="
auto:
  enable: true
  ha:
    enable: true
    master_nodes: 2
    external_db: 5432`
		})

		It("errors", func() {
			Expect(config.IsValid()).To(BeFalse())
			Expect(config.ValidationError.Error()).To(MatchRegexp("external_db"))
		})
	})

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
})

var _ = Describe("P2P Schema keys the provider reads", func() {
	var schema string

	BeforeEach(func() {
		var err error
		schema, err = GenerateSchema(RootSchema{}, "")
		Expect(err).ToNot(HaveOccurred())
	})

	// The provider unmarshals the cloud-config into
	// provider/internal/provider/config. Every key asserted here is read
	// from there, so a key missing from the schema is a key users get no
	// completion or validation for. sdk/schema cannot import that internal
	// package, hence the assertions on the generated schema.
	It("names the vpn creation key the provider reads", func() {
		Expect(schema).To(ContainSubstring(`"SchemaVPN"`))
		Expect(vpnProperties(schema)).To(HaveKey("create"))
		// `vpn` nested under the vpn block is read by nothing.
		Expect(vpnProperties(schema)).ToNot(HaveKey("vpn"))
	})

	It("describes the remaining p2p keys", func() {
		for _, key := range []string{"loglevel", "minimum_nodes", "dynamic_roles"} {
			Expect(p2pProperties(schema)).To(HaveKey(key))
		}
	})

	It("describes the kubevip block, with interface as a string", func() {
		Expect(rootProperties(schema)).To(HaveKey("kubevip"))
		iface, ok := kubevipProperties(schema)["interface"].(map[string]interface{})
		Expect(ok).To(BeTrue(), "kubevip.interface is not described")
		Expect(iface["type"]).To(Equal("string"))
		for _, key := range []string{"static_pod", "version", "image"} {
			Expect(kubevipProperties(schema)).To(HaveKey(key))
		}
	})
})

var _ = Describe("kubevip validation", func() {
	var config *KConfig
	var err error
	var yaml string

	JustBeforeEach(func() {
		config, err = NewConfigFromYAML(yaml, RootSchema{})
		Expect(err).ToNot(HaveOccurred())
	})

	Context("with the interface name the description gives as an example", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
users:
- name: kairos
kubevip:
  eip: 192.168.1.110
  interface: ens18
  static_pod: true
  version: v1.2.3`
		})

		It("succeeds", func() {
			Expect(config.IsValid()).To(BeTrue(), errText(config))
		})
	})

	Context("with a non-string interface", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
users:
- name: kairos
kubevip:
  interface: true`
		})

		It("errors", func() {
			Expect(config.IsValid()).To(BeFalse())
			Expect(config.ValidationError.Error()).To(MatchRegexp("interface"))
		})
	})
})
