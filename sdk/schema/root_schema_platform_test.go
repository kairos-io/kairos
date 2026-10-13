package schema_test

import (
	"encoding/json"

	. "github.com/kairos-io/kairos/v4/sdk/schema"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	"github.com/kairos-io/kairos/v4/sdk/types/platform"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

// The schema used to declare two top-level keys nothing reads: `platform`, as
// an object with the Go field names of platform.Platform, and
// `fullcloudconfig`. `platform` was the harmful one, because
// platform.Platform unmarshals from the string form ("linux/amd64") that the
// rest of Kairos prints and accepts, so the one spelling a config author would
// reach for was the one the schema rejected. See kairos-io/kairos#4714.
var _ = Describe("RootSchema top-level keys nothing reads", func() {
	topLevel := func() map[string]interface{} {
		raw, err := GenerateSchema(RootSchema{}, "")
		Expect(err).ToNot(HaveOccurred())

		var doc map[string]interface{}
		Expect(json.Unmarshal([]byte(raw), &doc)).To(Succeed())

		props, ok := doc["properties"].(map[string]interface{})
		Expect(ok).To(BeTrue(), "generated schema has no properties object")
		return props
	}

	validates := func(body string) (bool, error) {
		kc, err := NewConfigFromYAML("#cloud-config\nusers:\n  - name: kairos\n"+body, RootSchema{})
		Expect(err).ToNot(HaveOccurred())
		return kc.IsValid(), kc.ValidationError
	}

	It("declares neither platform nor fullcloudconfig", func() {
		Expect(topLevel()).ToNot(HaveKey("platform"))
		Expect(topLevel()).ToNot(HaveKey("fullcloudconfig"))
	})

	// Dropping the key only makes it unknown, and the root schema does not
	// forbid unknown keys, so a config carrying either one still validates.
	// That is the whole point: before, `platform` in its only parseable form
	// was a validation error under an otherwise valid config.
	It("stops rejecting the platform string the runtime parses", func() {
		str := (&platform.Platform{OS: "linux", Arch: "x86_64", GolangArch: "amd64"}).String()
		Expect(str).To(Equal("linux/amd64"))

		var parsed platform.Platform
		Expect(yaml.Unmarshal([]byte(str), &parsed)).To(Succeed(),
			"platform.Platform unmarshals from a string, so that is the wire shape")

		ok, err := validates("platform: " + str + "\n")
		Expect(err).ToNot(HaveOccurred())
		Expect(ok).To(BeTrue())
	})

	It("keeps accepting a config that still carries fullcloudconfig", func() {
		ok, err := validates("fullcloudconfig: /oem/90_custom.yaml\n")
		Expect(err).ToNot(HaveOccurred())
		Expect(ok).To(BeTrue())
	})

	// arch is the key that really selects the platform: sanitizeConfig turns
	// it into a platform.Platform when one was not set programmatically.
	It("still declares arch, which is the key the runtime reads", func() {
		Expect(topLevel()).To(HaveKey("arch"))

		var c sdkConfig.Config
		Expect(yaml.Unmarshal([]byte("arch: arm64\n"), &c)).To(Succeed())
		Expect(c.Arch).To(Equal("arm64"))
	})
})
