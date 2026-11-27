package config

import (
	"strings"

	"github.com/kairos-io/kairos/v4/sdk/collector"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkImages "github.com/kairos-io/kairos/v4/sdk/types/images"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func p2pConfig(block interface{}) *sdkConfig.Config {
	values := collector.ConfigValues{}
	if block != nil {
		values[p2pSourceKey] = block
	}
	return &sdkConfig.Config{Collector: collector.Config{Values: values}, Logger: logger.NewNullLogger()}
}

var _ = Describe("p2p source options (EXPERIMENTAL)", Label("p2p"), func() {
	p2pSrc := func(uri string) *sdkImages.ImageSource {
		s, err := sdkImages.NewSrcFromURI(uri)
		Expect(err).ToNot(HaveOccurred())
		return s
	}
	pinned := "p2p://10.0.0.5/active?sha256=" + strings.Repeat("ab", 32)
	BeforeEach(func() { GinkgoT().Setenv("KAIROS_P2P_EXPERIMENTAL", "") })

	It("reads every key of the block", func() {
		cfg := p2pConfig(collector.ConfigValues{
			"enabled": true, "experimental": true, "bind": "10.0.0.2", "port": 4242,
			"allow": []interface{}{"10.0.0.0/24"}, "upload-limit": 2048, "max-peers": 7, "disable-announce": true,
		})
		Expect(applyP2POptions(cfg, p2pSrc(pinned))).To(Succeed())
		Expect(cfg.P2PSource).To(Equal(&sdkImages.P2POptions{
			Enabled: true, Experimental: true, Bind: "10.0.0.2", Port: 4242,
			Allow: []string{"10.0.0.0/24"}, UploadLimit: 2048, MaxPeers: 7, DisableAnnounce: true,
		}))
	})

	It("refuses a p2p source until the user opted in", func() {
		cfg := p2pConfig(nil)
		Expect(applyP2POptions(cfg, p2pSrc(pinned))).To(MatchError(ContainSubstring("EXPERIMENTAL")))
		Expect(cfg.P2PSource).To(BeNil())
		// the environment is an opt in too
		GinkgoT().Setenv("KAIROS_P2P_EXPERIMENTAL", "1")
		Expect(applyP2POptions(cfg, p2pSrc(pinned))).To(Succeed())
		Expect(cfg.P2PSource).To(Equal(&sdkImages.P2POptions{}))
	})

	It("ignores the block, and clears stale options, when no source is a p2p source", func() {
		cfg := p2pConfig(collector.ConfigValues{"experimental": true})
		cfg.P2PSource = &sdkImages.P2POptions{Bind: "stale"}
		Expect(applyP2POptions(cfg, sdkImages.NewDockerSrc("quay.io/x/y:1"), nil)).To(Succeed())
		Expect(cfg.P2PSource).To(BeNil())
	})

	DescribeTable("refuses a wrong block",
		func(block interface{}, msg string) {
			cfg := p2pConfig(block)
			err := applyP2POptions(cfg, p2pSrc(pinned))
			Expect(err).To(MatchError(ContainSubstring(msg)))
			Expect(cfg.P2PSource).To(BeNil())
		},
		Entry("an unknown key, like a typo", collector.ConfigValues{"experimental": true, "alow": []interface{}{"x"}}, "must be an object"),
		Entry("a block that is not an object", "yes", "must be an object"),
		Entry("a bad cidr", collector.ConfigValues{"experimental": true, "allow": []interface{}{"10.0.0.0/99"}}, "p2p-source.allow"),
		Entry("a negative limit", collector.ConfigValues{"experimental": true, "upload-limit": -1}, "within range"),
		Entry("a port out of range", collector.ConfigValues{"experimental": true, "port": 70000}, "within range"),
		Entry("a non string bind", collector.ConfigValues{"experimental": true, "bind": 5}, "must be an object"),
	)

	DescribeTable("checks the image kind against the slot it is deployed to",
		func(uri string, recoverySlot bool, ok bool) {
			err := checkP2PImage(p2pSrc(uri), recoverySlot)
			if ok {
				Expect(err).ToNot(HaveOccurred())
			} else {
				Expect(err).To(HaveOccurred())
			}
		},
		Entry("active to the system slot", "p2p://a/active", false, true),
		Entry("passive to the system slot", "p2p://a/passive", false, true),
		Entry("recovery to the system slot", "p2p://a/recovery", false, false),
		Entry("recovery to the recovery slot", "p2p://a/recovery", true, true),
		Entry("active to the recovery slot", "p2p://a/active", true, false),
	)
	It("lets other sources through the kind check", func() {
		Expect(checkP2PImage(nil, true)).To(Succeed())
		Expect(checkP2PImage(sdkImages.NewDockerSrc("quay.io/x/y:1"), true)).To(Succeed())
	})

	It("reads the limits and the insecure opt in", func() {
		cfg := p2pConfig(collector.ConfigValues{"experimental": true, "insecure": true, "max-image-size": 100, "timeout": 30})
		Expect(applyP2POptions(cfg, p2pSrc("p2p://10.0.0.5/active"))).To(Succeed()) // insecure is set here
		Expect(cfg.P2PSource.Insecure).To(BeTrue())
		Expect(cfg.P2PSource.MaxImageSize).To(Equal(100))
		Expect(cfg.P2PSource.Timeout).To(Equal(30))
	})

	It("refuses a source without a pin, before any disk is touched, unless insecure is set", func() {
		cfg := p2pConfig(collector.ConfigValues{"experimental": true})
		err := applyP2POptions(cfg, p2pSrc("p2p://10.0.0.5/active"))
		Expect(err).To(MatchError(ContainSubstring("refusing p2p source")))
		Expect(err).To(MatchError(ContainSubstring("?pin=")))
		Expect(cfg.P2PSource).To(BeNil())
		sum := strings.Repeat("ab", 32)
		Expect(applyP2POptions(p2pConfig(collector.ConfigValues{"experimental": true}), p2pSrc("p2p://10.0.0.5/active?sha256="+sum))).To(Succeed())
		Expect(applyP2POptions(p2pConfig(collector.ConfigValues{"experimental": true}), p2pSrc("p2p://10.0.0.5/active?pin="+sum))).To(Succeed())
		Expect(applyP2POptions(p2pConfig(collector.ConfigValues{"experimental": true, "insecure": true}), p2pSrc("p2p://10.0.0.5/active"))).To(Succeed())
	})

	It("does not know about keys: no signing, no trusted keys", func() {
		for _, k := range []string{"trusted-keys", "signing-key-file"} {
			cfg := p2pConfig(collector.ConfigValues{"experimental": true, k: "x"})
			Expect(applyP2POptions(cfg, p2pSrc(pinned))).To(MatchError(ContainSubstring("must be an object")), k)
		}
	})

	It("fails to measure a p2p source nobody serves", func() {
		cfg := p2pConfig(nil)
		cfg.P2PSource = &sdkImages.P2POptions{Experimental: true, Insecure: true, Bind: "127.0.0.2", Allow: []string{"127.0.0.0/8"}}
		_, err := GetSourceSize(cfg, p2pSrc("p2p://127.0.0.1:1/active"))
		Expect(err).To(HaveOccurred())
	})
})
