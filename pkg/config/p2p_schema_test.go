package config_test

import (
	"strings"

	. "github.com/kairos-io/kairos/pkg/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("P2P Schema", func() {
	var config *KConfig
	var err error
	var yaml string

	JustBeforeEach(func() {
		config, err = NewConfigFromYAML(yaml, DefaultHeader, P2PSchema{})
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
			Expect(config.ValidationError()).To(MatchRegexp(`value must be one of "master", "worker", "none"`))
			Expect(config.IsValid()).NotTo(BeTrue())
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
				strings.Contains(config.ValidationError(), `value must be true`),
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
				strings.Contains(config.ValidationError(),
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
			Expect(config.ValidationError()).To(MatchRegexp("(length must be >= 1, but got 0|value must be true)"))
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
			Expect(config.ValidationError()).To(MatchRegexp("must be >= 1 but found 0"))
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
})
