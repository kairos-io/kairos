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
	"fmt"
	"reflect"
	"strings"

	"github.com/kairos-io/kairos/v4/sdk/schema"
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
		// it into entry: recovery.
		Entry("upgrade", v1.UpgradeSpec{}, schema.UpgradeSchema{}, "upgrade", "recovery"),
		Entry("reset", v1.ResetSpec{}, schema.ResetSchema{}, "reset"),
	)
})
