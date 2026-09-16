package install

import (
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Install is only ever filled by decoding a cloud config into
// sdk/types/config.Config, which is yaml. It is never a mapstructure decode
// target, so these tests drive the yaml decoder the runtime actually uses.

// TestPassiveDoesNotAliasRecovery guards the tag on the Passive field. It used
// to be "recovery-system", the same tag Recovery already owned, so a config
// setting install.passive alone was dropped and setting
// install.recovery-system leaked into both fields.
func TestPassiveDoesNotAliasRecovery(t *testing.T) {
	src := []byte(`
passive:
  size: 1234
recovery-system:
  size: 5678
`)
	var got Install
	if err := yaml.Unmarshal(src, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Passive.Size != 1234 {
		t.Errorf("passive.size: want 1234, got %d", got.Passive.Size)
	}
	if got.Recovery.Size != 5678 {
		t.Errorf("recovery-system.size: want 5678, got %d", got.Recovery.Size)
	}
}

// TestNoFormatDecodesFromKebabCase pins the "no-format" spelling the runtime
// InstallSpec reads. Any drift to "no_format" would leave install.no-format
// silently ignored.
func TestNoFormatDecodesFromKebabCase(t *testing.T) {
	var got Install
	if err := yaml.Unmarshal([]byte("no-format: true\n"), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.NoFormat {
		t.Error("install.no-format: want true, got false")
	}
}

// TestNoFormatUnderscoreStaysInert pins that the deprecated spelling still does
// nothing. agent/pkg/config warns about it; it must never start formatting or
// skipping a format on its own.
func TestNoFormatUnderscoreStaysInert(t *testing.T) {
	var got Install
	if err := yaml.Unmarshal([]byte("no_format: true\n"), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.NoFormat {
		t.Error("install.no_format: want NoFormat to stay false, got true")
	}
}

// TestTagsDescribeTheYamlDecoderOnly keeps the key names in one place. Every
// field names itself the same way to yaml and to json, and none of them carries
// a mapstructure tag: nothing decodes this type with mapstructure, so such a
// tag would be a second, unread spelling of the same key. The live mapstructure
// tags live on the spec structs in agent/pkg/implementations/spec and on the
// sdk/types/{images,partitions} structs those specs embed as fields.
func TestTagsDescribeTheYamlDecoderOnly(t *testing.T) {
	for _, typ := range []reflect.Type{
		reflect.TypeOf(Install{}),
		reflect.TypeOf(SelinuxOptions{}),
	} {
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			yamlName := tagName(f.Tag.Get("yaml"))
			jsonName := tagName(f.Tag.Get("json"))
			if yamlName == "" {
				t.Errorf("%s.%s: no yaml key name", typ.Name(), f.Name)
			}
			if yamlName != jsonName {
				t.Errorf("%s.%s: yaml key %q but json key %q", typ.Name(), f.Name, yamlName, jsonName)
			}
			if tag, ok := f.Tag.Lookup("mapstructure"); ok {
				t.Errorf("%s.%s: unread mapstructure tag %q, the yaml tag %q is the only key name",
					typ.Name(), f.Name, tag, yamlName)
			}
		}
	}
}

func tagName(tag string) string {
	return strings.Split(tag, ",")[0]
}
