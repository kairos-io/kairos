package collector_test

import (
	"encoding/json"
	"testing"

	. "github.com/kairos-io/kairos/v4/sdk/collector"
	"gopkg.in/yaml.v3"
)

// decodeLikeAReader reproduces the yaml-then-json fallback that parseReaders
// (and the other config sources merged at boot) use to turn raw bytes into
// ConfigValues.
func decodeLikeAReader(data []byte) (ConfigValues, bool) {
	var values ConfigValues
	if err := yaml.Unmarshal(data, &values); err == nil {
		return values, true
	}
	if err := json.Unmarshal(data, &values); err == nil {
		return values, true
	}
	return nil, false
}

// FuzzDeepMerge exercises the parse-then-merge path every #cloud-config
// source goes through at boot: two independently decoded documents merged
// with DeepMerge. A panic here is a denial-of-service against the boot path.
func FuzzDeepMerge(f *testing.F) {
	seeds := []string{
		"",
		"{}",
		`{"outer":{"inner":1}}`,
		"outer:\n  inner: 1\n",
		`{"a":[1,2]}`,
		"a: [1, 2]\n",
		"not even yaml: [",
	}
	for _, a := range seeds {
		for _, b := range seeds {
			f.Add(a, b)
		}
	}

	f.Fuzz(func(t *testing.T, sourceA, sourceB string) {
		a, ok := decodeLikeAReader([]byte(sourceA))
		if !ok {
			return
		}
		b, ok := decodeLikeAReader([]byte(sourceB))
		if !ok {
			return
		}
		_, _ = DeepMerge(a, b)
	})
}
