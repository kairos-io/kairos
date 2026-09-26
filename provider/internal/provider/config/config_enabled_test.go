package config_test

import (
	"github.com/kairos-io/kairos/v4/provider/internal/provider/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

// The p2p and kubevip blocks spell the enablement flag `enable`, while the
// kubernetes blocks next to them spell it `enabled`. A cloud-config author has
// to remember which block takes which, and the schema published both, so
// kairos-io/kairos#1016 asked for one spelling. `enabled` wins because k3s,
// k3s-agent, k0s and k0s-worker already use it. `enable` stays accepted.
var _ = Describe("enable and enabled", func() {
	decode := func(yml string) (*config.Config, error) {
		cfg := &config.Config{}
		err := yaml.Unmarshal([]byte(yml), cfg)

		return cfg, err
	}

	Describe("p2p.auto", func() {
		It("reads enabled", func() {
			cfg, err := decode("p2p:\n  auto:\n    enabled: false\n")
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.P2P.Auto.IsEnabled()).To(BeFalse())
			Expect(cfg.DeprecatedKeys()).To(BeEmpty())
		})

		It("still reads the deprecated enable", func() {
			cfg, err := decode("p2p:\n  auto:\n    enable: false\n")
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.P2P.Auto.IsEnabled()).To(BeFalse())
			Expect(cfg.DeprecatedKeys()).To(ConsistOf(
				config.DeprecatedKey{Key: "p2p.auto.enable", Replacement: "p2p.auto.enabled"},
			))
		})

		It("refuses both spellings at once", func() {
			_, err := decode("p2p:\n  auto:\n    enabled: true\n    enable: false\n")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("p2p.auto.enabled and p2p.auto.enable are both set"))
		})

		It("defaults to enabled when neither is written", func() {
			cfg, err := decode("p2p:\n  network_token: tok\n")
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.P2P.Auto.IsEnabled()).To(BeTrue())
		})
	})

	Describe("p2p.auto.ha", func() {
		It("reads enabled", func() {
			cfg, err := decode("p2p:\n  auto:\n    ha:\n      enabled: true\n")
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.P2P.Auto.HA.IsEnabled()).To(BeTrue())
			Expect(cfg.DeprecatedKeys()).To(BeEmpty())
		})

		It("still reads the deprecated enable", func() {
			cfg, err := decode("p2p:\n  auto:\n    ha:\n      enable: true\n")
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.P2P.Auto.HA.IsEnabled()).To(BeTrue())
			Expect(cfg.DeprecatedKeys()).To(ConsistOf(
				config.DeprecatedKey{Key: "p2p.auto.ha.enable", Replacement: "p2p.auto.ha.enabled"},
			))
		})

		It("refuses both spellings at once", func() {
			_, err := decode("p2p:\n  auto:\n    ha:\n      enabled: true\n      enable: false\n")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("p2p.auto.ha.enabled and p2p.auto.ha.enable are both set"))
		})
	})

	Describe("kubevip", func() {
		It("reads enabled", func() {
			cfg, err := decode("kubevip:\n  enabled: true\n")
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.KubeVIP.IsEnabled()).To(BeTrue())
			Expect(cfg.DeprecatedKeys()).To(BeEmpty())
		})

		It("still reads the deprecated enable", func() {
			cfg, err := decode("kubevip:\n  enable: true\n")
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.KubeVIP.IsEnabled()).To(BeTrue())
			Expect(cfg.DeprecatedKeys()).To(ConsistOf(
				config.DeprecatedKey{Key: "kubevip.enable", Replacement: "kubevip.enabled"},
			))
		})

		It("reads enabled: false over an eip that would otherwise enable it", func() {
			cfg, err := decode("kubevip:\n  eip: 192.168.1.110\n  enabled: false\n")
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.KubeVIP.IsEnabled()).To(BeFalse())
		})

		It("refuses both spellings at once", func() {
			_, err := decode("kubevip:\n  enabled: true\n  enable: true\n")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("kubevip.enabled and kubevip.enable are both set"))
		})

		// KubeVIP now decodes through an UnmarshalYAML of its own, so the
		// keys it already read have to keep arriving. The embedded
		// kubevip.Config is left as it was: yaml.v3 does not inline an
		// anonymous struct field without `,inline`, so its keys were
		// already unreachable before this change, and still are.
		It("keeps decoding the rest of the block", func() {
			cfg, err := decode("kubevip:\n  enabled: true\n  eip: 192.168.1.110\n  interface: ens18\n  static_pod: true\n  version: v0.6.0\n  image: ghcr.io/kube-vip/kube-vip:v0.6.0\n  manifest_url: https://example.invalid/kubevip.yaml\n")
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.KubeVIP.EIP).To(Equal("192.168.1.110"))
			Expect(cfg.KubeVIP.Interface).To(Equal("ens18"))
			Expect(cfg.KubeVIP.StaticPod).To(BeTrue())
			Expect(cfg.KubeVIP.Version).To(Equal("v0.6.0"))
			Expect(cfg.KubeVIP.Image).To(Equal("ghcr.io/kube-vip/kube-vip:v0.6.0"))
			Expect(cfg.KubeVIP.ManifestURL).To(Equal("https://example.invalid/kubevip.yaml"))
		})
	})

	Describe("the kubernetes blocks", func() {
		It("already spell it enabled, which is why enabled is the one that stays", func() {
			cfg, err := decode("k3s:\n  enabled: true\nk0s:\n  enabled: true\n")
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.K3s.IsEnabled()).To(BeTrue())
			Expect(cfg.K0s.IsEnabled()).To(BeTrue())
		})
	})

	Describe("DeprecatedKeys", func() {
		It("reports every block that used the deprecated spelling", func() {
			cfg, err := decode("p2p:\n  auto:\n    enable: true\n    ha:\n      enable: true\nkubevip:\n  enable: true\n")
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.DeprecatedKeys()).To(ConsistOf(
				config.DeprecatedKey{Key: "p2p.auto.enable", Replacement: "p2p.auto.enabled"},
				config.DeprecatedKey{Key: "p2p.auto.ha.enable", Replacement: "p2p.auto.ha.enabled"},
				config.DeprecatedKey{Key: "kubevip.enable", Replacement: "kubevip.enabled"},
			))
		})

		It("says nothing when the p2p block is absent", func() {
			cfg, err := decode("k3s:\n  enabled: true\n")
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.DeprecatedKeys()).To(BeEmpty())
		})
	})
})
