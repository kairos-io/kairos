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
			Expect(config.ValidationError.Error()).To(MatchRegexp(`value must be one of "", "master", "worker", "none"`))
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

	Context("kubevip", func() {
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

var _ = Describe("P2P Schema role", func() {
	var config *KConfig
	var err error
	var yaml string

	// The provider is the only consumer of p2p.role
	// (provider/internal/role/p2p/k8s.go), and it accepts the empty string,
	// "master" and "worker". Anything else stops the bootstrap, so what the
	// schema declares has to line up with that set.
	JustBeforeEach(func() {
		config, err = NewConfigFromYAML(yaml, RootSchema{})
		Expect(err).ToNot(HaveOccurred())
	})

	DescribeTable("accepts the values the provider reads",
		func(role string) {
			yaml = `#cloud-config
users:
- name: kairos
p2p:
  network_token: "b3RwOgogIGRoYWdlX3NpemU6IDIwOTcxNTIwCg=="
  role: ` + role + `
  auto:
    enable: true`

			var e error
			config, e = NewConfigFromYAML(yaml, RootSchema{})
			Expect(e).ToNot(HaveOccurred())
			Expect(config.IsValid()).To(BeTrue(), func() string {
				if config.ValidationError == nil {
					return ""
				}
				return config.ValidationError.Error()
			})
		},
		// Documented in installation/p2p.md as the way to leave the role
		// unset, and the value the provider checks for.
		Entry("the empty string", `""`),
		Entry("master", `"master"`),
		Entry("worker", `"worker"`),
		// Was the schema's advertised default, so it is in the wild.
		Entry("none", `"none"`),
	)

	Context("with a role no handler implements", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
users:
- name: kairos
p2p:
  network_token: "b3RwOgogIGRoYWdlX3NpemU6IDIwOTcxNTIwCg=="
  role: "leader"
  auto:
    enable: true`
		})

		It("errors", func() {
			Expect(config.IsValid()).To(BeFalse())
			Expect(config.ValidationError.Error()).To(MatchRegexp("role"))
		})
	})
})
