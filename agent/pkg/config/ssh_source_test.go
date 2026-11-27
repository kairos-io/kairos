package config

import (
	"github.com/kairos-io/kairos/v4/sdk/collector"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkImages "github.com/kairos-io/kairos/v4/sdk/types/images"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func sshConfig(operation string, block interface{}) *sdkConfig.Config {
	op := collector.ConfigValues{}
	if block != nil {
		op[sshSourceKey] = block
	}
	return &sdkConfig.Config{
		Collector: collector.Config{Values: collector.ConfigValues{operation: op}},
		Logger:    logger.NewNullLogger(),
	}
}

var _ = Describe("ssh source options", Label("ssh"), func() {
	sshSrc := func() *sdkImages.ImageSource {
		s, err := sdkImages.NewSrcFromURI("ssh://root@node1/active")
		Expect(err).ToNot(HaveOccurred())
		return s
	}

	rootAndAction := func(op string, root, action interface{}) *sdkConfig.Config {
		cfg := sshConfig(op, action)
		if root != nil {
			cfg.Collector.Values[sshSourceKey] = root
		}
		return cfg
	}

	It("reads the options of the operation", func() {
		for _, op := range []string{"install", "upgrade", "reset"} {
			cfg := sshConfig(op, collector.ConfigValues{
				"user":                  "fleet",
				"identity-file":         "/oem/id",
				"known-hosts-file":      "/oem/kh",
				"host-key-fingerprints": []interface{}{"SHA256:abc"},
			})
			Expect(applySSHOptions(cfg, op, sshSrc())).To(Succeed())
			Expect(cfg.SSHSource).To(Equal(&sdkImages.SSHSourceOptions{
				User: "fleet", IdentityFile: "/oem/id", KnownHostsFile: "/oem/kh", HostKeyFingerprints: []string{"SHA256:abc"},
			}))
		}
	})

	It("reads the root block for every operation", func() {
		for _, op := range []string{"install", "upgrade", "reset"} {
			cfg := rootAndAction(op, collector.ConfigValues{"identity-file": "/oem/id", "user": "fleet"}, nil)
			Expect(applySSHOptions(cfg, op, sshSrc())).To(Succeed())
			Expect(cfg.SSHSource).To(Equal(&sdkImages.SSHSourceOptions{User: "fleet", IdentityFile: "/oem/id"}))
		}
	})

	It("lets the block of the operation override the root key by key", func() {
		root := collector.ConfigValues{
			"user":                  "fleet",
			"identity-file":         "/oem/root-id",
			"known-hosts-file":      "/oem/root-kh",
			"host-key-fingerprints": []interface{}{"SHA256:root"},
		}
		action := collector.ConfigValues{
			"user":                  "upgrader",
			"host-key-fingerprints": []interface{}{"SHA256:action"},
		}
		cfg := rootAndAction("upgrade", root, action)
		Expect(applySSHOptions(cfg, "upgrade", sshSrc())).To(Succeed())
		Expect(cfg.SSHSource).To(Equal(&sdkImages.SSHSourceOptions{
			User:                "upgrader",
			IdentityFile:        "/oem/root-id",
			KnownHostsFile:      "/oem/root-kh",
			HostKeyFingerprints: []string{"SHA256:action"},
		}))
	})

	It("lets an explicitly empty key of the operation clear the root one", func() {
		cfg := rootAndAction("reset", collector.ConfigValues{"identity-file": "/oem/root-id"}, collector.ConfigValues{"identity-file": ""})
		Expect(applySSHOptions(cfg, "reset", sshSrc())).To(Succeed())
		Expect(cfg.SSHSource.IdentityFile).To(BeEmpty())
	})

	It("does not read the block of another operation", func() {
		cfg := sshConfig("install", collector.ConfigValues{"identity-file": "/oem/id"})
		Expect(applySSHOptions(cfg, "upgrade", sshSrc())).To(Succeed())
		Expect(cfg.SSHSource).To(Equal(&sdkImages.SSHSourceOptions{}))
	})

	It("works with no block at all, ssh then uses its own configuration", func() {
		cfg := sshConfig("upgrade", nil)
		Expect(applySSHOptions(cfg, "upgrade", sshSrc())).To(Succeed())
		Expect(cfg.SSHSource).To(Equal(&sdkImages.SSHSourceOptions{}))
	})

	It("ignores the blocks when no source is an ssh source", func() {
		cfg := sshConfig("upgrade", collector.ConfigValues{"identity-file": "/oem/id"})
		cfg.SSHSource = &sdkImages.SSHSourceOptions{IdentityFile: "stale"}
		Expect(applySSHOptions(cfg, "upgrade", sdkImages.NewDockerSrc("quay.io/x/y:1"))).To(Succeed())
		Expect(cfg.SSHSource).To(BeNil())
	})

	DescribeTable("refuses a wrong block",
		func(root, action interface{}, msg string) {
			cfg := rootAndAction("upgrade", root, action)
			err := applySSHOptions(cfg, "upgrade", sshSrc())
			Expect(err).To(MatchError(ContainSubstring(msg)))
			Expect(cfg.SSHSource).To(BeNil())
		},
		Entry("a fingerprint of the wrong kind", nil, collector.ConfigValues{"host-key-fingerprints": []interface{}{"MD5:aa"}}, "SHA256:"),
		Entry("a bad root fingerprint", collector.ConfigValues{"host-key-fingerprints": []interface{}{"MD5:aa"}}, nil, "SHA256:"),
		Entry("an unknown key, like a typo", nil, collector.ConfigValues{"insecure": true}, "upgrade.ssh-source must be an object"),
		Entry("an unknown root key", collector.ConfigValues{"insecure": true}, nil, "ssh-source must be an object"),
		Entry("an inline private key", nil, collector.ConfigValues{"identity": "-----BEGIN OPENSSH PRIVATE KEY-----"}, "must be an object"),
		Entry("a block that is not an object", nil, "yes", "must be an object"),
		Entry("a non string path", nil, collector.ConfigValues{"identity-file": 5}, "must be an object"),
	)

	It("does not echo the content of a bad block", func() {
		cfg := sshConfig("upgrade", collector.ConfigValues{"identity": "PRIVATE-KEY-SENTINEL"})
		err := applySSHOptions(cfg, "upgrade", sshSrc())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).ToNot(ContainSubstring("PRIVATE-KEY-SENTINEL"))
	})

	It("fails to measure an ssh source that cannot be reached", func() {
		_, err := GetSourceSize(sshConfig("upgrade", nil), sshSrc())
		Expect(err).To(HaveOccurred())
	})
})
