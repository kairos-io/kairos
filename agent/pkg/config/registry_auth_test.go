package config

import (
	"encoding/base64"

	"github.com/kairos-io/kairos/v4/agent/pkg/implementations/imageextractor"
	v1mock "github.com/kairos-io/kairos/v4/agent/tests/mocks"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	registrytypes "github.com/moby/moby/api/types/registry"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func registryConfig(operation string, auth interface{}) *sdkConfig.Config {
	return &sdkConfig.Config{Collector: collector.Config{Values: collector.ConfigValues{
		operation: collector.ConfigValues{"registry-auth": auth},
	}}, Logger: logger.NewNullLogger()}
}

func imageExtractorAuth(value interface{}) *registrytypes.AuthConfig {
	switch extractor := value.(type) {
	case imageextractor.OCIImageExtractor:
		return extractor.Auth
	case *imageextractor.OCIImageExtractor:
		if extractor != nil {
			return extractor.Auth
		}
	}
	return nil
}

var _ = Describe("registry auth", func() {
	It("parses supported credential forms", func() {
		encoded := base64.StdEncoding.EncodeToString([]byte("encoded-user:encoded-pass"))
		cases := []struct {
			data collector.ConfigValues
			want *registrytypes.AuthConfig
		}{
			{collector.ConfigValues{"username": "user", "password": "pass"}, &registrytypes.AuthConfig{Username: "user", Password: "pass"}},
			{collector.ConfigValues{"auth": encoded}, &registrytypes.AuthConfig{Username: "encoded-user", Password: "encoded-pass"}},
			{collector.ConfigValues{"identity-token": "identity-sentinel-unique"}, &registrytypes.AuthConfig{IdentityToken: "identity-sentinel-unique"}},
			{collector.ConfigValues{"registry-token": "registry-sentinel-unique"}, &registrytypes.AuthConfig{RegistryToken: "registry-sentinel-unique"}},
		}
		for _, operation := range []string{"install", "upgrade"} {
			for _, tc := range cases {
				_, got, err := readRegistryOptions(registryConfig(operation, tc.data), operation)
				Expect(err).ToNot(HaveOccurred())
				Expect(got).To(Equal(tc.want))
			}
		}
	})

	It("treats null and empty objects as no explicit auth", func() {
		for _, value := range []interface{}{nil, collector.ConfigValues{}} {
			_, got, err := readRegistryOptions(registryConfig("upgrade", value), "upgrade")
			Expect(err).ToNot(HaveOccurred())
			Expect(got).To(BeNil())
		}
	})

	It("rejects malformed forms without echoing values", func() {
		cases := []struct {
			data   interface{}
			secret string
		}{
			{"very-unique-scalar-secret", "very-unique-scalar-secret"},
			{collector.ConfigValues{"username": "unique-user-sentinel", "password": 42}, "unique-user-sentinel"},
			{collector.ConfigValues{"username": "unique-incomplete-sentinel"}, "unique-incomplete-sentinel"},
			{collector.ConfigValues{"auth": "not-base64-unique-sentinel"}, "not-base64-unique-sentinel"},
			{collector.ConfigValues{"username": "unique-mixed-sentinel", "password": "pass", "registry-token": "unique-token-sentinel"}, "unique-token-sentinel"},
			{collector.ConfigValues{"password": "pass", "credential-secret": "unique-unknown-sentinel"}, "unique-unknown-sentinel"},
		}
		for _, tc := range cases {
			_, _, err := readRegistryOptions(registryConfig("install", tc.data), "install")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).ToNot(ContainSubstring(tc.secret))
		}
	})

	It("preserves auth and insecure settings on the builtin extractor", func() {
		cfg := registryConfig("install", collector.ConfigValues{"username": "user", "password": "pass"})
		cfg.Collector.Values["install"].(collector.ConfigValues)["allow-insecure-registries"] = true
		cfg.ImageExtractor = imageextractor.OCIImageExtractor{}
		Expect(applyRegistryOptions(cfg, "install")).To(Succeed())
		got := cfg.ImageExtractor.(imageextractor.OCIImageExtractor)
		Expect(got.Insecure).To(BeTrue())
		Expect(got.Auth).To(Equal(&registrytypes.AuthConfig{Username: "user", Password: "pass"}))

		existing := &registrytypes.AuthConfig{Username: "existing-user", Password: "existing-pass"}
		cfg = registryConfig("install", nil)
		cfg.Collector.Values["install"].(collector.ConfigValues)["allow-insecure-registries"] = true
		cfg.ImageExtractor = imageextractor.OCIImageExtractor{Auth: existing}
		Expect(applyRegistryOptions(cfg, "install")).To(Succeed())
		got = cfg.ImageExtractor.(imageextractor.OCIImageExtractor)
		Expect(got.Insecure).To(BeTrue())
		Expect(got.Auth).To(Equal(existing))
	})

	It("leaves injected extractors untouched", func() {
		cfg := registryConfig("upgrade", collector.ConfigValues{"username": "user", "password": "pass"})
		cfg.Collector.Values["upgrade"].(collector.ConfigValues)["allow-insecure-registries"] = true
		fake := v1mock.NewFakeImageExtractor(logger.NewNullLogger())
		cfg.ImageExtractor = fake
		Expect(applyRegistryOptions(cfg, "upgrade")).To(Succeed())
		Expect(cfg.ImageExtractor).To(BeIdenticalTo(fake))
	})

	It("keeps the default extractor on the keychain when auth is absent", func() {
		cfg := registryConfig("install", nil)
		cfg.ImageExtractor = imageextractor.OCIImageExtractor{}
		Expect(applyRegistryOptions(cfg, "install")).To(Succeed())
		Expect(cfg.ImageExtractor).To(Equal(imageextractor.OCIImageExtractor{}))
	})

	It("redacts diagnostics without mutating operational values", func() {
		auth := &registrytypes.AuthConfig{Username: "user-sentinel-unique", Password: "password-sentinel-unique", IdentityToken: "token-sentinel-unique"}
		cfg := registryConfig("install", collector.ConfigValues{"username": auth.Username, "password": auth.Password})
		cfg.ImageExtractor = imageextractor.OCIImageExtractor{Auth: auth}
		dump := RedactedConfigDump(cfg)
		Expect(dump).ToNot(ContainSubstring("user-sentinel-unique"))
		Expect(dump).ToNot(ContainSubstring("password-sentinel-unique"))
		Expect(dump).ToNot(ContainSubstring("token-sentinel-unique"))
		Expect(imageExtractorAuth(cfg.ImageExtractor)).To(Equal(auth))
		Expect(cfg.Collector.Values["install"].(collector.ConfigValues)["registry-auth"]).To(Equal(collector.ConfigValues{"username": auth.Username, "password": auth.Password}))
	})

	It("does not include credentials in parser errors", func() {
		_, _, err := readRegistryOptions(registryConfig("install", collector.ConfigValues{"username": "unique-error-user"}), "install")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).ToNot(ContainSubstring("unique-error-user"))
	})
})
