package schema_test

import (
	"encoding/json"
	"reflect"

	. "github.com/kairos-io/kairos/v4/sdk/schema"
	"github.com/kairos-io/kairos/v4/sdk/types/install"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

// install.skip_copy_kcrypt_plugin used to gate the install-time copy of
// /system/discovery into /oem/system/discovery. That copy went away in #683
// and took the only reader of the key with it, but the schema kept declaring
// it and the Install type kept parsing it, so for three and a half years the
// key was offered to config authors and then ignored. These specs pin the
// removal on both sides. See kairos-io/kairos#4876.
var _ = Describe("The install key that gated the kcrypt plugin copy", func() {
	It("is not declared by the schema", func() {
		raw, err := GenerateSchema(InstallSchema{}, "")
		Expect(err).ToNot(HaveOccurred())

		var doc map[string]interface{}
		Expect(json.Unmarshal([]byte(raw), &doc)).To(Succeed())

		props, ok := doc["properties"].(map[string]interface{})
		Expect(ok).To(BeTrue(), "generated schema has no properties object")
		Expect(props).ToNot(HaveKey("skip_copy_kcrypt_plugin"))
	})

	It("is not a field of the install block the runtime parses", func() {
		t := reflect.TypeOf(install.Install{})
		for i := 0; i < t.NumField(); i++ {
			Expect(t.Field(i).Tag.Get("yaml")).ToNot(HavePrefix("skip_copy_kcrypt_plugin"),
				"install.Install still parses the key, so it can grow a reader again")
		}
	})

	// The install block is closed to undeclared keys (kairos-io/kairos#6004),
	// so validate now names the key as one that does nothing. Loading the
	// config is unchanged: YAML decoding ignores the key, and the agent only
	// warns about a schema error unless --strict-validation is set.
	It("is reported by validate, and still ignored when the config is parsed", func() {
		body := `#cloud-config
device: /dev/sda
skip_copy_kcrypt_plugin: true`

		config, err := NewConfigFromYAML(body, InstallSchema{})
		Expect(err).ToNot(HaveOccurred())
		Expect(config.IsValid()).To(BeFalse())
		Expect(config.ValidationError.Error()).To(ContainSubstring("skip_copy_kcrypt_plugin"))

		var parsed install.Install
		Expect(yaml.Unmarshal([]byte(body), &parsed)).To(Succeed())
		Expect(parsed.Device).To(Equal("/dev/sda"))
	})
})
