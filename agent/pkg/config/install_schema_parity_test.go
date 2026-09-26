package config_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	v1 "github.com/kairos-io/kairos/v4/agent/pkg/implementations/spec"
	"github.com/kairos-io/kairos/v4/sdk/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// runtimeOnlyInstallKeys lists the mapstructure tags of InstallSpec and
// InstallUkiSpec that the installer fills in itself and a config must not set.
// Viper matches a bare lowercased field name too, so these are settable by
// accident; declaring them would advertise runtime state as configuration.
var runtimeOnlyInstallKeys = map[string]string{}

// installSchemaProperties returns every property name the install block
// declares. The install node carries both `properties` and, through the
// embedded PowerManagement OneOfExposer, a `oneOf`: reboot and poweroff live
// only in the latter, so a walk that stops at `properties` reports them as
// missing.
func installSchemaProperties(defs map[string]interface{}, node map[string]interface{}) map[string]bool {
	out := map[string]bool{}

	if ref, ok := node["$ref"].(string); ok {
		target, ok := defs[strings.TrimPrefix(ref, "#/definitions/")].(map[string]interface{})
		if !ok {
			return out
		}
		node = target
	}

	if props, ok := node["properties"].(map[string]interface{}); ok {
		for key := range props {
			out[key] = true
		}
	}

	for _, branch := range []string{"oneOf", "allOf", "anyOf"} {
		members, ok := node[branch].([]interface{})
		if !ok {
			continue
		}
		for _, member := range members {
			m, ok := member.(map[string]interface{})
			if !ok {
				continue
			}
			for key := range installSchemaProperties(defs, m) {
				out[key] = true
			}
		}
	}

	return out
}

// The install block is decoded into InstallSpec and InstallUkiSpec by
// unmarshallFullSpec, not into sdk/types/config.Config. Both existing parity
// guards key off Config, so a key read only by an action spec is invisible to
// them from either end, which is how firmware, part-table, cloud-init, iso,
// tty, skip-entries and allow-insecure-registries stayed undeclared while
// being read on every install. See kairos-io/kairos#4938.
var _ = Describe("The install schema", func() {
	It("declares every install key the action specs decode", func() {
		raw, err := schema.GenerateSchema(schema.RootSchema{}, "")
		Expect(err).ToNot(HaveOccurred())

		var doc map[string]interface{}
		Expect(json.Unmarshal([]byte(raw), &doc)).To(Succeed())

		defs, _ := doc["definitions"].(map[string]interface{})
		if defs == nil {
			defs = map[string]interface{}{}
		}
		node, ok := doc["properties"].(map[string]interface{})["install"].(map[string]interface{})
		Expect(ok).To(BeTrue(), "the generated schema has no install block")

		declared := installSchemaProperties(defs, node)
		Expect(declared).ToNot(BeEmpty(), "the install block resolved to no properties at all")

		for _, spec := range []interface{}{v1.InstallSpec{}, v1.InstallUkiSpec{}} {
			t := reflect.TypeOf(spec)

			var undeclared []string
			for i := 0; i < t.NumField(); i++ {
				// Only an explicit mapstructure tag is decodable here; a field
				// without one is runtime state the spec fills in.
				key, _, _ := strings.Cut(t.Field(i).Tag.Get("mapstructure"), ",")
				if key == "" || key == "-" {
					continue
				}
				if _, ok := runtimeOnlyInstallKeys[key]; ok {
					continue
				}
				if !declared[key] {
					undeclared = append(undeclared, key)
				}
			}

			Expect(undeclared).To(BeEmpty(), fmt.Sprintf(
				"%s decodes install.%v, which InstallSchema does not declare, so the key gets "+
					"no validation and print-schema does not document it. Declare it, or add it "+
					"to runtimeOnlyInstallKeys with the reason it is not configuration.",
				t.Name(), undeclared))
		}
	})

	It("keeps runtimeOnlyInstallKeys honest", func() {
		decoded := map[string]bool{}
		for _, spec := range []interface{}{v1.InstallSpec{}, v1.InstallUkiSpec{}} {
			t := reflect.TypeOf(spec)
			for i := 0; i < t.NumField(); i++ {
				key, _, _ := strings.Cut(t.Field(i).Tag.Get("mapstructure"), ",")
				if key != "" && key != "-" {
					decoded[key] = true
				}
			}
		}

		for key, reason := range runtimeOnlyInstallKeys {
			Expect(decoded).To(HaveKey(key), fmt.Sprintf(
				"runtimeOnlyInstallKeys still excuses %q (%s), which no install spec decodes any more",
				key, reason))
		}
	})
})
