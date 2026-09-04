package schema_test

import (
	"testing"

	. "github.com/kairos-io/kairos/v4/sdk/schema"
)

// FuzzValidateConfig exercises YAML parsing and JSON Schema validation
// with arbitrary input. Both run against every #cloud-config file a
// Kairos node reads at boot, so a panic here is a denial-of-service
// against that path, not just a bad error message.
func FuzzValidateConfig(f *testing.F) {
	seeds := []string{
		"",
		"#cloud-config",
		"#cloud-config\nusers:\n  - name: kairos\n    passwd: kairos",
		"#cloud-config\nusers:\n  - name: 007\n    passwd: kairos",
		"#cloud-config\nstages:\n  example:\n    - commands: \"echo hi\"",
		"#kairos-config\nusers: []",
		"#node-config\n{}",
		"not even yaml: [",
		"#cloud-config\n" + "a: " + "&a [*a]",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, source string) {
		kc, err := NewConfigFromYAML(source, RootSchema{})
		if err != nil {
			return
		}
		_ = kc.HasHeader()
		_ = kc.IsValid()
		_, _ = kc.ValidateSemantics()
	})
}
