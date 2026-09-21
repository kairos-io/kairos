package config

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/kairos-io/kairos/v4/agent/pkg/implementations/imageextractor"
	v1mock "github.com/kairos-io/kairos/v4/agent/tests/mocks"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	"github.com/kairos-io/kairos/v4/sdk/types/fs"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	registrytypes "github.com/moby/moby/api/types/registry"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5/vfst"
)

type unreadableRegistryFS struct{ fs.KairosFS }

func (unreadableRegistryFS) ReadFile(string) ([]byte, error) {
	return nil, os.ErrPermission
}

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
	It("rejects misspelled authentication blocks without exposing their contents", func() {
		for _, operation := range []string{"install", "upgrade", "INSTALL"} {
			for _, key := range []string{"registry_auth", "registy-auth", "REGISTRY-AUTH", "auth-secret-key-sentinel"} {
				for _, plainMap := range []bool{false, true} {
					cfg := registryConfig(operation, nil)
					values := collector.ConfigValues{key: collector.ConfigValues{"password": "typo-secret-sentinel"}}
					cfg.Collector.Values[operation] = values
					if plainMap {
						cfg.Collector.Values[operation] = map[string]interface{}(values)
					}
					extractor := imageextractor.OCIImageExtractor{Insecure: true}
					cfg.ImageExtractor = extractor
					err := applyRegistryOptions(cfg, strings.ToLower(operation))
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring(strings.ToLower(operation)))
					Expect(err.Error()).To(ContainSubstring("registry-auth"))
					Expect(err.Error()).ToNot(ContainSubstring("typo-secret-sentinel"))
					Expect(err.Error()).ToNot(ContainSubstring("auth-secret-key-sentinel"))
					Expect(cfg.ImageExtractor).To(Equal(extractor))
					dump := RedactedConfigDump(cfg)
					Expect(dump).To(ContainSubstring("registry-auth"))
					Expect(dump).ToNot(ContainSubstring("typo-secret-sentinel"))
					Expect(dump).ToNot(ContainSubstring("[REDACTED]"))
					Expect(values).To(HaveKey(key))
				}
			}
		}
	})

	It("rejects misspelled blocks during scanning before diagnostics", func() {
		for _, operation := range []string{"install", "upgrade"} {
			for _, key := range []string{"registry_auth", "registy-auth"} {
				var logs bytes.Buffer
				cfg := &sdkConfig.Config{Logger: logger.NewBufferLogger(&logs)}
				input := fmt.Sprintf("debug: true\n%s:\n  %s:\n    password: scan-secret-sentinel\n", operation, key)
				_, err := scan(cfg, collector.Readers(strings.NewReader(input)))
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("registry-auth"))
				Expect(err.Error()).ToNot(ContainSubstring("scan-secret-sentinel"))
				Expect(logs.String()).To(BeEmpty())
			}
		}
	})

	It("preserves unrelated extension settings outside operation authentication keys", func() {
		cfg := registryConfig("install", collector.ConfigValues{"username": "user", "password": "pass"})
		cfg.Collector.Values["custom"] = collector.ConfigValues{"auth": "extension-setting"}
		cfg.Collector.Values["install"].(collector.ConfigValues)["custom"] = collector.ConfigValues{"auth": "nested-setting"}
		_, auth, err := readRegistryOptions(cfg, "install")
		Expect(err).ToNot(HaveOccurred())
		Expect(auth.Username).To(Equal("user"))
	})

	It("parses supported credential forms", func() {
		encoded := base64.StdEncoding.EncodeToString([]byte("encoded-user:encoded-pass"))
		cases := []struct {
			data collector.ConfigValues
			want *registrytypes.AuthConfig
		}{
			{collector.ConfigValues{"username": "user", "password": "pass"}, &registrytypes.AuthConfig{Username: "user", Password: "pass"}},
			{collector.ConfigValues{"username": "user", "password": ""}, &registrytypes.AuthConfig{Username: "user", Password: ""}},
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

	It("rejects non-string and empty credential fields", func() {
		for _, key := range []string{"username", "password", "auth", "identity-token", "registry-token"} {
			for _, value := range []interface{}{42, nil, ""} {
				if key == "password" && value == "" {
					continue
				}
				fields := map[string]interface{}{key: value}
				if key == "username" {
					fields["password"] = "secret"
				}
				if key == "password" {
					fields["username"] = "user"
				}
				_, err := parseRegistryAuth(fields, "install")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).ToNot(ContainSubstring("secret"))
			}
		}
	})

	It("reads credential files without copying secrets into config or diagnostics", func() {
		fileSystem, cleanup, err := vfst.NewTestFS(nil)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(cleanup)
		for _, operation := range []string{"install", "upgrade"} {
			for _, body := range []string{
				"username: file-user\npassword: file-secret-sentinel\n",
				"auth: " + base64.StdEncoding.EncodeToString([]byte("file-user:file-secret-sentinel")),
				"identity-token: file-secret-sentinel",
				"registry-token: file-secret-sentinel",
			} {
				Expect(fileSystem.WriteFile("/credentials.yaml", []byte(body), 0600)).To(Succeed())
				cfg := registryConfig(operation, collector.ConfigValues{"file": "/credentials.yaml"})
				cfg.Fs = fileSystem
				cfg.ImageExtractor = imageextractor.OCIImageExtractor{}
				before, err := cfg.Collector.String()
				Expect(err).ToNot(HaveOccurred())
				Expect(applyRegistryOptions(cfg, operation)).To(Succeed())
				auth := imageExtractorAuth(cfg.ImageExtractor)
				Expect(auth).ToNot(BeNil())
				Expect([]string{auth.Password, auth.IdentityToken, auth.RegistryToken}).To(ContainElement("file-secret-sentinel"))
				after, err := cfg.Collector.String()
				Expect(err).ToNot(HaveOccurred())
				Expect(after).To(Equal(before))
				Expect(RedactedConfigDump(cfg)).ToNot(ContainSubstring("file-secret-sentinel"))
			}
		}
	})

	It("rejects invalid file references before reading them", func() {
		for _, value := range []collector.ConfigValues{
			{"file": ""}, {"file": 42}, {"file": nil},
			{"file": "/credentials.yaml", "username": "file-secret-sentinel", "password": "pass"},
			{"file": "/credentials.yaml", "registry-token": "file-secret-sentinel"},
		} {
			_, _, err := readRegistryOptions(registryConfig("install", value), "install")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).ToNot(ContainSubstring("file-secret-sentinel"))
		}
	})

	It("fails on missing or unreadable files without changing existing auth", func() {
		fileSystem, cleanup, err := vfst.NewTestFS(nil)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(cleanup)
		for _, fileSystem := range []fs.KairosFS{fileSystem, unreadableRegistryFS{}} {
			cfg := registryConfig("upgrade", collector.ConfigValues{"file": "/missing.yaml"})
			cfg.Fs = fileSystem
			extractor := imageextractor.OCIImageExtractor{Auth: &registrytypes.AuthConfig{Username: "existing", Password: "existing"}}
			cfg.ImageExtractor = extractor
			Expect(applyRegistryOptions(cfg, "upgrade")).To(MatchError(ContainSubstring("registry-auth.file cannot be read")))
			Expect(cfg.ImageExtractor).To(Equal(extractor))
		}
	})

	DescribeTable("rejects invalid credential files without exposing contents",
		func(body string) {
			fileSystem, cleanup, err := vfst.NewTestFS(nil)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(cleanup)
			Expect(fileSystem.WriteFile("/credentials.yaml", []byte(body), 0600)).To(Succeed())
			cfg := registryConfig("install", collector.ConfigValues{"file": "/credentials.yaml"})
			cfg.Fs = fileSystem
			_, auth, err := readRegistryOptions(cfg, "install")
			Expect(err).To(HaveOccurred())
			Expect(auth).To(BeNil())
			Expect(err.Error()).ToNot(ContainSubstring("file-secret-sentinel"))
		},
		Entry("empty", ""),
		Entry("null", "null"),
		Entry("empty object", "{}"),
		Entry("scalar", "file-secret-sentinel"),
		Entry("invalid YAML", "password: [file-secret-sentinel"),
		Entry("duplicate fields", "username: file-secret-sentinel\nusername: duplicate\npassword: pass"),
		Entry("multiple documents", "registry-token: file-secret-sentinel\n---\nregistry-token: another"),
		Entry("nested file", "file: file-secret-sentinel"),
		Entry("unknown field", "file-secret-sentinel: value"),
		Entry("incomplete credentials", "username: file-secret-sentinel"),
		Entry("mixed credentials", "identity-token: file-secret-sentinel\nregistry-token: token"),
	)

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
