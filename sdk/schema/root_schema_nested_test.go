package schema_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	. "github.com/kairos-io/kairos/v4/sdk/schema"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// undeclaredOnPurpose lists the nested cloud-config paths that the runtime
// decodes and the published schema deliberately does not declare, with the
// reason, so a path can only be here on purpose.
//
// An entry whose key later gets declared is inert rather than stale: the walk
// only consults this map when the schema lookup misses. So the entries naming
// an open pull request can be deleted as that pull request merges, and the
// test does not go red in between either way.
var undeclaredOnPurpose = map[string]string{
	// Discovery outputs. ghw fills these in from a running system during the
	// partition scan; no install path reads them from config. Declaring them
	// would offer the user a key that is overwritten before anything looks at
	// it. See kairos-io/kairos#4903.
	"install.partitions.oem.partition_label":        "discovery output, filled by the ghw scan",
	"install.partitions.oem.flags":                  "discovery output, filled by the ghw scan",
	"install.partitions.oem.uuid":                   "discovery output, filled by the ghw scan",
	"install.partitions.recovery.partition_label":   "discovery output, filled by the ghw scan",
	"install.partitions.recovery.flags":             "discovery output, filled by the ghw scan",
	"install.partitions.recovery.uuid":              "discovery output, filled by the ghw scan",
	"install.partitions.state.partition_label":      "discovery output, filled by the ghw scan",
	"install.partitions.state.flags":                "discovery output, filled by the ghw scan",
	"install.partitions.state.uuid":                 "discovery output, filled by the ghw scan",
	"install.partitions.persistent.partition_label": "discovery output, filled by the ghw scan",
	"install.partitions.persistent.flags":           "discovery output, filled by the ghw scan",
	"install.partitions.persistent.uuid":            "discovery output, filled by the ghw scan",
	"install.extra-partitions.partition_label":      "discovery output, filled by the ghw scan",
	"install.extra-partitions.flags":                "discovery output, filled by the ghw scan",
	"install.extra-partitions.uuid":                 "discovery output, filled by the ghw scan",

	// SetDefaultLabels overwrites the filesystem label of each named
	// partition with the constant the rest of the system looks the partition
	// up by, whatever the config said, so the key is as dead as the discovery
	// outputs above. An extra partition is different: its label is read, and
	// that one is kairos-io/kairos#4903.
	"install.partitions.oem.label":        "overwritten by SetDefaultLabels",
	"install.partitions.recovery.label":   "overwritten by SetDefaultLabels",
	"install.partitions.state.label":      "overwritten by SetDefaultLabels",
	"install.partitions.persistent.label": "overwritten by SetDefaultLabels",

	// Open, with a pull request. Delete the entry when it merges.
	"bind-pcrs":                      "kairos-io/kairos#4677, pull request #4678",
	"bind-public-pcrs":               "kairos-io/kairos#4677, pull request #4678",
	"logs":                           "kairos-io/kairos#4677, pull request #4678",
	"install.source":                 "kairos-io/kairos#4693, pull request #4694",
	"install.nousers":                "kairos-io/kairos#4693, pull request #4694",
	"install.system.source":          "kairos-io/kairos#4693, pull request #4694",
	"install.recovery-system.source": "kairos-io/kairos#4693, pull request #4694",
	"install.passive.source":         "kairos-io/kairos#4693, pull request #4694",
	"install.system.fs":              "kairos-io/kairos#4914, pull request #4915",
	"install.system.label":           "kairos-io/kairos#4914, pull request #4915",
	"install.recovery-system.fs":     "kairos-io/kairos#4914, pull request #4915",
	"install.recovery-system.label":  "kairos-io/kairos#4914, pull request #4915",
	"install.passive.fs":             "kairos-io/kairos#4914, pull request #4915",
	"install.passive.label":          "kairos-io/kairos#4914, pull request #4915",
	"install.extra-partitions.label": "kairos-io/kairos#4903, pull request #4904",
}

// maxSchemaDepth bounds the walk. The deepest live path today is three
// segments (install.partitions.oem.size), so this leaves room and still stops
// a type that contains itself.
const maxSchemaDepth = 8

// elemStruct unwraps pointers and lists down to the struct type they carry,
// and reports whether it found one.
func elemStruct(t reflect.Type) (reflect.Type, bool) {
	for i := 0; i < maxSchemaDepth; i++ {
		switch t.Kind() {
		case reflect.Ptr, reflect.Slice, reflect.Array:
			t = t.Elem()
		case reflect.Struct:
			return t, true
		default:
			return t, false
		}
	}
	return t, false
}

// resolveRef follows a $ref into the document's definitions. The generator
// emits a $ref for every named struct, so without this step every nested
// block looks like a node with no properties at all.
func resolveRef(doc, node map[string]interface{}) map[string]interface{} {
	for i := 0; i < maxSchemaDepth; i++ {
		ref, ok := node["$ref"].(string)
		if !ok {
			return node
		}
		defs, ok := doc["definitions"].(map[string]interface{})
		if !ok {
			return node
		}
		next, ok := defs[ref[strings.LastIndex(ref, "/")+1:]].(map[string]interface{})
		if !ok {
			return node
		}
		node = next
	}
	return node
}

// declaredChild returns the schema node for key under node.
//
// It descends through three kinds of indirection, and all three are load
// bearing: items, because a list of structs declares its keys on the element;
// and oneOf/anyOf/allOf, because an embedded struct and a mutually exclusive
// group are both flattened into a branch list rather than into properties.
// install.reboot and install.poweroff live in PowerManagement's oneOf, so
// without that step they read as undeclared when they are not.
func declaredChild(doc, node map[string]interface{}, key string, depth int) (map[string]interface{}, bool) {
	if depth > maxSchemaDepth {
		return nil, false
	}
	node = resolveRef(doc, node)

	if items, ok := node["items"].(map[string]interface{}); ok {
		if c, ok := declaredChild(doc, items, key, depth+1); ok {
			return c, true
		}
	}
	if props, ok := node["properties"].(map[string]interface{}); ok {
		if c, ok := props[key].(map[string]interface{}); ok {
			return c, true
		}
	}
	for _, branches := range []string{"oneOf", "anyOf", "allOf"} {
		arr, ok := node[branches].([]interface{})
		if !ok {
			continue
		}
		for _, b := range arr {
			bm, ok := b.(map[string]interface{})
			if !ok {
				continue
			}
			if c, ok := declaredChild(doc, bm, key, depth+1); ok {
				return c, true
			}
		}
	}
	return nil, false
}

// walkDecoded reports every yaml key the struct decodes that the schema node
// does not declare, as a dotted path. Maps are not descended into: a
// map[string]string has no fields to compare, and the schema declares it as a
// free-form object on purpose.
func walkDecoded(doc, node map[string]interface{}, st reflect.Type, path string, depth int, seen map[reflect.Type]bool) []string {
	if depth > maxSchemaDepth || seen[st] {
		return nil
	}
	seen[st] = true
	defer delete(seen, st)

	var undeclared []string
	for i := 0; i < st.NumField(); i++ {
		f := st.Field(i)
		if !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		// A yaml:"-" field is runtime plumbing the collector never fills in,
		// and an embedded struct with no tag contributes its own fields at
		// this level rather than a key of its own.
		if name == "-" {
			continue
		}
		if name == "" {
			if ft, ok := elemStruct(f.Type); ok && f.Anonymous {
				undeclared = append(undeclared, walkDecoded(doc, node, ft, path, depth, seen)...)
			}
			continue
		}

		child := name
		if path != "" {
			child = path + "." + name
		}
		c, ok := declaredChild(doc, node, name, 0)
		if !ok {
			undeclared = append(undeclared, child)
			continue
		}
		if ft, isStruct := elemStruct(f.Type); isStruct {
			undeclared = append(undeclared, walkDecoded(doc, c, ft, child, depth+1, seen)...)
		}
	}
	return undeclared
}

// hasDecodedPath reports whether the dotted path still names a chain of yaml
// keys on the runtime type, so a stale entry in undeclaredOnPurpose cannot sit
// there after the field behind it is renamed or dropped.
func hasDecodedPath(st reflect.Type, path string) bool {
	for _, segment := range strings.Split(path, ".") {
		next, ok := decodedField(st, segment, 0)
		if !ok {
			return false
		}
		st, _ = elemStruct(next)
	}
	return true
}

func decodedField(st reflect.Type, name string, depth int) (reflect.Type, bool) {
	if depth > maxSchemaDepth || st.Kind() != reflect.Struct {
		return nil, false
	}
	for i := 0; i < st.NumField(); i++ {
		f := st.Field(i)
		if !f.IsExported() {
			continue
		}
		tag, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		if tag == name {
			return f.Type, true
		}
		if tag == "" && f.Anonymous {
			if ft, ok := elemStruct(f.Type); ok {
				if t, ok := decodedField(ft, name, depth+1); ok {
					return t, true
				}
			}
		}
	}
	return nil, false
}

// The parity check in agent/pkg/config walks NumField() once and compares yaml
// tag names, so it only ever sees the top level of each struct it is handed.
// It stops at install.Install's Active field and never compares images.Image
// against schema.Image, which is why kairos-io/kairos#4693, #4903 and #4914
// each had to be found by hand. This descends instead, against the generated
// document rather than the RootSchema struct, because the $ref and oneOf
// indirection the generator emits is invisible to a field-by-field struct
// comparison. See kairos-io/kairos#5118.
var _ = Describe("RootSchema nested keys", func() {
	var doc map[string]interface{}

	BeforeEach(func() {
		raw, err := GenerateSchema(RootSchema{}, "")
		Expect(err).ToNot(HaveOccurred())
		Expect(json.Unmarshal([]byte(raw), &doc)).To(Succeed())
	})

	It("declares every nested key the runtime decodes", func() {
		undeclared := walkDecoded(doc, doc, reflect.TypeOf(sdkConfig.Config{}), "", 0, map[reflect.Type]bool{})

		var unexpected []string
		for _, path := range undeclared {
			if _, ok := undeclaredOnPurpose[path]; !ok {
				unexpected = append(unexpected, path)
			}
		}
		sort.Strings(unexpected)

		Expect(unexpected).To(BeEmpty(), fmt.Sprintf(
			"%v are decoded by sdk/types/config.Config but not declared anywhere in the "+
				"generated schema, so print-schema neither offers nor documents them. "+
				"Either declare them or, if the runtime overwrites them and nothing reads "+
				"them from config, add them to undeclaredOnPurpose with the reason.", unexpected))
	})

	It("keeps undeclaredOnPurpose honest", func() {
		ct := reflect.TypeOf(sdkConfig.Config{})
		for path, reason := range undeclaredOnPurpose {
			Expect(hasDecodedPath(ct, path)).To(BeTrue(), fmt.Sprintf(
				"undeclaredOnPurpose still excuses %q (%s), which sdk/types/config.Config "+
					"no longer decodes", path, reason))
		}
	})
})
