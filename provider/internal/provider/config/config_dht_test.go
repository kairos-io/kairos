package config_test

import (
	"github.com/kairos-io/kairos/v4/provider/internal/provider/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

// The p2p block reads `dns` and `dynamic_roles` positively and `disable_dht`
// negatively, so one key in the block had to be read backwards. Tier 2 of
// kairos-io/kairos#1016 replaces it with `dht_enabled`. `disable_dht` stays
// accepted and keeps its own meaning, which is the opposite one.
var _ = Describe("dht_enabled and disable_dht", func() {
	decode := func(yml string) (*config.Config, error) {
		cfg := &config.Config{}
		err := yaml.Unmarshal([]byte(yml), cfg)

		return cfg, err
	}

	It("defaults to enabled when neither key is written", func() {
		cfg, err := decode("p2p:\n  network_token: tok\n")
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.P2P.IsDHTEnabled()).To(BeTrue())
		Expect(cfg.DeprecatedKeys()).To(BeEmpty())
	})

	It("reads dht_enabled false", func() {
		cfg, err := decode("p2p:\n  dht_enabled: false\n")
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.P2P.IsDHTEnabled()).To(BeFalse())
		Expect(cfg.DeprecatedKeys()).To(BeEmpty())
	})

	It("reads dht_enabled true", func() {
		cfg, err := decode("p2p:\n  dht_enabled: true\n")
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.P2P.IsDHTEnabled()).To(BeTrue())
	})

	It("still reads the deprecated disable_dht, inverted", func() {
		cfg, err := decode("p2p:\n  disable_dht: true\n")
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.P2P.IsDHTEnabled()).To(BeFalse())
		Expect(cfg.DeprecatedKeys()).To(ConsistOf(
			config.DeprecatedKey{Key: "p2p.disable_dht", Replacement: "p2p.dht_enabled"},
		))
	})

	It("still reads disable_dht false as DHT on", func() {
		cfg, err := decode("p2p:\n  disable_dht: false\n")
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.P2P.IsDHTEnabled()).To(BeTrue())
		Expect(cfg.DeprecatedKeys()).To(ConsistOf(
			config.DeprecatedKey{Key: "p2p.disable_dht", Replacement: "p2p.dht_enabled"},
		))
	})

	It("refuses both keys when they contradict each other", func() {
		_, err := decode("p2p:\n  dht_enabled: true\n  disable_dht: true\n")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(
			"p2p.dht_enabled is true and the deprecated p2p.disable_dht is true"))
	})

	It("refuses the other contradiction too", func() {
		_, err := decode("p2p:\n  dht_enabled: false\n  disable_dht: false\n")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(
			"p2p.dht_enabled is false and the deprecated p2p.disable_dht is false"))
	})

	It("takes both keys when they agree, and still reports the deprecated one", func() {
		cfg, err := decode("p2p:\n  dht_enabled: false\n  disable_dht: true\n")
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.P2P.IsDHTEnabled()).To(BeFalse())
		Expect(cfg.DeprecatedKeys()).To(ConsistOf(
			config.DeprecatedKey{Key: "p2p.disable_dht", Replacement: "p2p.dht_enabled"},
		))
	})

	It("keeps decoding the rest of the p2p block", func() {
		cfg, err := decode("p2p:\n  network_token: tok\n  dht_enabled: false\n  dns: true\n  minimum_nodes: 3\n  auto:\n    enabled: true\n")
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.P2P.IsDHTEnabled()).To(BeFalse())
		Expect(cfg.P2P.NetworkToken).To(Equal("tok"))
		Expect(cfg.P2P.DNS).To(BeTrue())
		Expect(cfg.P2P.MinimumNodes).To(Equal(3))
		Expect(cfg.P2P.Auto.IsEnabled()).To(BeTrue())
	})

	It("reports disable_dht alongside the other deprecated keys", func() {
		cfg, err := decode("p2p:\n  disable_dht: true\n  auto:\n    enable: true\n")
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.DeprecatedKeys()).To(ConsistOf(
			config.DeprecatedKey{Key: "p2p.disable_dht", Replacement: "p2p.dht_enabled"},
			config.DeprecatedKey{Key: "p2p.auto.enable", Replacement: "p2p.auto.enabled"},
		))
	})
})
