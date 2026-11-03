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

package config

import (
	"fmt"
	"testing"

	v1 "github.com/kairos-io/kairos/v4/agent/pkg/implementations/spec"
	"github.com/kairos-io/kairos/v4/sdk/schema"
	sdkSpec "github.com/kairos-io/kairos/v4/sdk/types/spec"
)

// boolKeys are the yes/no keys of the upgrade and reset blocks, with the spec
// the block is decoded into.
var boolKeys = []struct {
	block string
	key   string
	spec  func() sdkSpec.Spec
}{
	{"upgrade", "reboot", func() sdkSpec.Spec { return &v1.UpgradeSpec{} }},
	{"upgrade", "poweroff", func() sdkSpec.Spec { return &v1.UpgradeSpec{} }},
	{"upgrade", "allow-insecure-registries", func() sdkSpec.Spec { return &v1.UpgradeSpec{} }},
	{"reset", "reset-persistent", func() sdkSpec.Spec { return &v1.ResetSpec{} }},
	{"reset", "reset-oem", func() sdkSpec.Spec { return &v1.ResetSpec{} }},
	{"reset", "reboot", func() sdkSpec.Spec { return &v1.ResetSpec{} }},
	{"reset", "poweroff", func() sdkSpec.Spec { return &v1.ResetSpec{} }},
}

// boolValues are the ways a yes/no key gets written, and whether each reader
// reads it. The two readers differ on one value: mapstructure's
// WeaklyTypedInput reads the empty string as false, while configBool hands it
// to strconv.ParseBool, which refuses it. A template with an unset variable
// writes exactly that, so the difference is reachable.
var boolValues = []struct {
	value      string
	viperReads bool
	boolReads  bool
}{
	{"true", true, true},
	{"false", true, true},
	// A templating engine quotes its output, so a generated config writes
	// the literal as a string.
	{`"true"`, true, true},
	{`"TRUE"`, true, true},
	{`"t"`, true, true},
	{"1", true, true},
	{"0", true, true},
	// An unset template variable writes an empty string.
	{`""`, true, false},
	// Unquoted yes is a string in YAML 1.2 and strconv.ParseBool refuses
	// it, so both readers refuse it.
	{"yes", false, false},
	{"maybe", false, false},
}

// The upgrade and reset blocks reach their spec through viper's Unmarshal,
// which sets mapstructure's WeaklyTypedInput, so a quoted string or a number
// is read as a boolean on purpose. upgrade.recovery is read by configBool,
// which accepts the same set by hand, and for the same reason.
//
// A key declared as a plain JSON Schema boolean therefore refuses a config the
// node goes on to apply, which is a warning normally and a hard error under
// --strict-validation. This ties the two together: whatever the decoder reads,
// the schema has to accept, and whatever the decoder refuses the schema is
// free to refuse first, where the message names the key.
func TestBoolKeysAgreeWithTheSchema(t *testing.T) {
	for _, k := range boolKeys {
		for _, v := range boolValues {
			t.Run(fmt.Sprintf("%s.%s=%s", k.block, k.key, v.value), func(t *testing.T) {
				cc := fmt.Sprintf("%s:\n  %s: %s\n", k.block, k.key, v.value)

				vp := subFor(t, cc, k.block)
				err := vp.Unmarshal(k.spec(), setDecoder, decodeHook)
				if decodes := err == nil; decodes != v.viperReads {
					t.Fatalf("decoding %q: err %v, expected it to decode: %v", cc, err, v.viperReads)
				}

				config, err := schema.NewConfigFromYAML("#cloud-config\nusers:\n- name: kairos\n"+cc, schema.RootSchema{})
				if err != nil {
					t.Fatalf("building the config for %q: %v", cc, err)
				}
				if config.IsValid() != v.viperReads {
					t.Fatalf("%q is decoded by the spec: %v, but the schema says it is valid: %v (%v)",
						cc, v.viperReads, config.IsValid(), config.ValidationError)
				}
			})
		}
	}
}

// upgrade.recovery has no spec field: upgradeRecoveryEntry reads it off the
// merged cloud config through configBool and turns it into entry: recovery, so
// it needs the same agreement against that reader rather than against viper.
func TestRecoveryKeyAgreesWithTheSchema(t *testing.T) {
	for _, v := range boolValues {
		t.Run(v.value, func(t *testing.T) {
			cc := fmt.Sprintf("upgrade:\n  recovery: %s\n", v.value)

			_, err := configBool(subFor(t, cc, "upgrade").Get("recovery"))
			if reads := err == nil; reads != v.boolReads {
				t.Fatalf("configBool on %q: err %v, expected it to read: %v", cc, err, v.boolReads)
			}

			config, err := schema.NewConfigFromYAML("#cloud-config\nusers:\n- name: kairos\n"+cc, schema.RootSchema{})
			if err != nil {
				t.Fatalf("building the config for %q: %v", cc, err)
			}
			if config.IsValid() != v.boolReads {
				t.Fatalf("%q is read by configBool: %v, but the schema says it is valid: %v (%v)",
					cc, v.boolReads, config.IsValid(), config.ValidationError)
			}
		})
	}
}
