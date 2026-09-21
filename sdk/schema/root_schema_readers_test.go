package schema_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	. "github.com/kairos-io/kairos/v4/sdk/schema"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// readOutsideConfig lists the top-level keys that are honoured somewhere other
// than sdk/types/config.Config, with the reader, so a key can only be here on
// purpose.
var readOutsideConfig = map[string]string{
	"p2p":     "provider-kairos, provider/internal/role",
	"stages":  "yip, through the cloud-init runner",
	"users":   "the yip users plugin, run from the initramfs stage",
	"upgrade": "agent/pkg/config.NewUpgradeSpec and NewUkiUpgradeSpec",
}

// The parity check in agent/pkg/config guards one direction: every field of
// sdk/types/config.Config has to exist on RootSchema. Nothing guarded the
// other one, so RootSchema could and did declare top-level keys no reader ever
// looks at. Worse, the forward check cannot notice: it compares the yaml tag,
// and getTagName maps a `yaml:"-"` tag to the empty string, which matches
// RootSchema's blank title field. Every runtime-only field of Config, Platform
// among them, therefore satisfies it for free. See kairos-io/kairos#4714.
var _ = Describe("RootSchema top-level keys", func() {
	It("names a reader for every key it declares", func() {
		raw, err := GenerateSchema(RootSchema{}, "")
		Expect(err).ToNot(HaveOccurred())

		var doc map[string]interface{}
		Expect(json.Unmarshal([]byte(raw), &doc)).To(Succeed())
		props, ok := doc["properties"].(map[string]interface{})
		Expect(ok).To(BeTrue(), "generated schema has no properties object")

		// Only the yaml tags Config actually decodes count. A `yaml:"-"`
		// field is runtime plumbing the collector never fills in, so a
		// schema key backed by one is a key nothing reads.
		decoded := map[string]bool{}
		ct := reflect.TypeOf(sdkConfig.Config{})
		for i := 0; i < ct.NumField(); i++ {
			name, _, _ := strings.Cut(ct.Field(i).Tag.Get("yaml"), ",")
			if name == "" || name == "-" {
				continue
			}
			decoded[name] = true
		}

		var undeclared []string
		for key := range props {
			if decoded[key] {
				continue
			}
			if _, ok := readOutsideConfig[key]; ok {
				continue
			}
			undeclared = append(undeclared, key)
		}

		Expect(undeclared).To(BeEmpty(), fmt.Sprintf(
			"RootSchema declares %v, which no field of sdk/types/config.Config decodes. "+
				"Either wire the key up or drop it; if it is read elsewhere, add it to "+
				"readOutsideConfig with the reader.", undeclared))
	})

	It("keeps readOutsideConfig honest", func() {
		raw, err := GenerateSchema(RootSchema{}, "")
		Expect(err).ToNot(HaveOccurred())

		var doc map[string]interface{}
		Expect(json.Unmarshal([]byte(raw), &doc)).To(Succeed())
		props := doc["properties"].(map[string]interface{})

		for key, reader := range readOutsideConfig {
			Expect(props).To(HaveKey(key), fmt.Sprintf(
				"readOutsideConfig still claims %q is declared and read by %s", key, reader))
		}
	})
})
