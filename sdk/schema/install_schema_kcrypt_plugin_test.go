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

	// Dropping a declaration must not turn a config that still carries the key
	// into an error: the schema does not forbid unknown properties and YAML
	// decoding ignores them, so such a config keeps behaving as it does today.
	It("leaves a config that still sets it valid and inert", func() {
		body := `#cloud-config
device: /dev/sda
skip_copy_kcrypt_plugin: true`

		config, err := NewConfigFromYAML(body, InstallSchema{})
		Expect(err).ToNot(HaveOccurred())
		Expect(config.IsValid()).To(BeTrue(), func() string { return config.ValidationError.Error() })

		var parsed install.Install
		Expect(yaml.Unmarshal([]byte(body), &parsed)).To(Succeed())
		Expect(parsed.Device).To(Equal("/dev/sda"))
	})
})
