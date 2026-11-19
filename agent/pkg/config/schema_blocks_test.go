/*
Copyright © 2026 Kairos authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/kairos-io/kairos/v4/sdk/schema"
	"github.com/kairos-io/kairos/v4/sdk/types/install"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/kairos-io/kairos/v4/agent/pkg/implementations/spec"
)

// decodedKeys returns the cloud-config keys a spec struct decodes on purpose.
// unmarshallFullSpec hands the block to viper, which decodes through
// mapstructure, so an explicit mapstructure tag is the mark of a key the spec
// is meant to accept. Fields without one are runtime state the caller fills
// in, not configuration.
func decodedKeys(spec interface{}) []string {
	t := reflect.TypeOf(spec)
	var keys []string
	for i := 0; i < t.NumField(); i++ {
		tag, _, _ := strings.Cut(t.Field(i).Tag.Get("mapstructure"), ",")
		if tag == "" || tag == "-" {
			continue
		}
		keys = append(keys, tag)
	}
	return keys
}

// declaredKeys returns the keys a schema block declares.
func declaredKeys(block interface{}) []string {
	t := reflect.TypeOf(block)
	var keys []string
	for i := 0; i < t.NumField(); i++ {
		tag, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if tag == "" || tag == "-" {
			continue
		}
		keys = append(keys, tag)
	}
	return keys
}

// The upgrade and reset blocks are decoded straight from the collector into an
// action spec, so sdk/types/config.Config has no field for either one and the
// Config-to-RootSchema parity check above cannot see them. They went
// undeclared for that reason: a wrong type or a misspelled key in either block
// validated cleanly and only failed once the operation was already running on
// the node. See kairos-io/kairos#4925.
var _ = Describe("Schema blocks that are read straight into an action spec", func() {
	DescribeTable("declares every key its spec decodes",
		func(spec, block interface{}, blockName string) {
			declared := declaredKeys(block)
			for _, key := range decodedKeys(spec) {
				Expect(declared).To(ContainElement(key), fmt.Sprintf(
					"%s.%s is decoded by %T but not declared by %T, so nothing validates it",
					blockName, key, spec, block))
			}
		},
		Entry("upgrade", v1.UpgradeSpec{}, schema.UpgradeSchema{}, "upgrade"),
		Entry("reset", v1.ResetSpec{}, schema.ResetSchema{}, "reset"),
	)

	DescribeTable("declares no key its spec cannot read",
		func(spec, block interface{}, blockName string, readElsewhere ...string) {
			decoded := append(decodedKeys(spec), readElsewhere...)
			for _, key := range declaredKeys(block) {
				Expect(decoded).To(ContainElement(key), fmt.Sprintf(
					"%s.%s is declared by %T but read by nothing", blockName, key, block))
			}
		},
		// upgrade.recovery is not a spec field: NewUpgradeSpec reads it off
		// the collector values directly, before the spec is built, and turns
		// it into entry: recovery. Registry credentials are read by readRegistryOptions.
		Entry("upgrade", v1.UpgradeSpec{}, schema.UpgradeSchema{}, "upgrade", "recovery", "registry-auth"),
		Entry("reset", v1.ResetSpec{}, schema.ResetSchema{}, "reset"),
	)
})

// yamlKeys returns the keys a struct decodes from YAML.
func yamlKeys(v interface{}) []string {
	t := reflect.TypeOf(v)
	var keys []string
	for i := 0; i < t.NumField(); i++ {
		tag, _, _ := strings.Cut(t.Field(i).Tag.Get("yaml"), ",")
		if tag == "" || tag == "-" {
			continue
		}
		keys = append(keys, tag)
	}
	return keys
}

// generatedInstallKeys returns the properties of the install block in the
// generated schema. Unlike declaredKeys it sees reboot and poweroff, which
// InstallSchema has no field for.
func generatedInstallKeys() []string {
	raw, err := schema.GenerateSchema(schema.RootSchema{}, "")
	Expect(err).ToNot(HaveOccurred())
	var doc struct {
		Properties  map[string]map[string]interface{} `json:"properties"`
		Definitions map[string]map[string]interface{} `json:"definitions"`
	}
	Expect(json.Unmarshal([]byte(raw), &doc)).To(Succeed())

	block := doc.Properties["install"]
	if ref, ok := block["$ref"].(string); ok {
		block = doc.Definitions[strings.TrimPrefix(ref, "#/definitions/")]
	}
	Expect(block["additionalProperties"]).To(Equal(false), "the install block is open to unknown keys")
	properties, ok := block["properties"].(map[string]interface{})
	Expect(ok).To(BeTrue(), "the install block has no properties")

	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	return keys
}

// The install block has more than one reader: sdk/types/install.Install
// through Config, and InstallSpec or InstallUkiSpec through
// unmarshallFullSpec. The schema closes the block to keys it does not declare
// (kairos-io/kairos#6004), so a key one of them reads and the schema lacks
// would be reported as unknown on a config that works.
var _ = Describe("The install schema block", func() {
	readers := map[string][]string{
		"sdk/types/install.Install": yamlKeys(install.Install{}),
		"InstallSpec":               decodedKeys(v1.InstallSpec{}),
		"InstallUkiSpec":            decodedKeys(v1.InstallUkiSpec{}),
		// Read by readRegistryOptions, before the spec is decoded.
		"readRegistryOptions": {"registry-auth"},
	}

	It("declares every key a reader decodes", func() {
		declared := generatedInstallKeys()
		for reader, keys := range readers {
			for _, key := range keys {
				Expect(declared).To(ContainElement(key), fmt.Sprintf(
					"install.%s is read by %s but not declared by the install schema, so validate refuses it", key, reader))
			}
		}
	})

	It("declares no key that nothing reads, other than the deprecated ones", func() {
		// warnDeprecatedKeys names the key to use instead of these two.
		read := []string{"no_format", "image"}
		for _, keys := range readers {
			read = append(read, keys...)
		}
		for _, key := range generatedInstallKeys() {
			Expect(read).To(ContainElement(key), fmt.Sprintf("install.%s is declared by the install schema but read by nothing", key))
		}
	})
})
