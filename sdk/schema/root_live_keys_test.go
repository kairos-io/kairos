package schema_test

import (
	"encoding/json"
	"strings"

	. "github.com/kairos-io/kairos/v4/sdk/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The runtime reads these three top-level keys (sdk/kcrypt for the two PCR
// lists, agent/internal/agent/logs.go for `logs`), so print-schema has to
// describe them. They used to be missing, and the parity test in
// agent/pkg/config skipped them instead of failing.
var _ = Describe("RootSchema keys the runtime reads", func() {
	var properties map[string]interface{}

	BeforeEach(func() {
		generated, err := GenerateSchema(RootSchema{}, "")
		Expect(err).ToNot(HaveOccurred())

		var doc struct {
			Properties map[string]interface{} `json:"properties"`
		}
		Expect(json.Unmarshal([]byte(generated), &doc)).To(Succeed())
		properties = doc.Properties
	})

	It("describes bind-pcrs, bind-public-pcrs and logs", func() {
		// Named individually rather than in a loop so a regression says
		// which key went missing.
		Expect(properties).To(HaveKey("bind-pcrs"))
		Expect(properties).To(HaveKey("bind-public-pcrs"))
		Expect(properties).To(HaveKey("logs"))
	})

	It("gives the two PCR keys a description, so print-schema is usable", func() {
		for _, key := range []string{"bind-pcrs", "bind-public-pcrs"} {
			property, ok := properties[key].(map[string]interface{})
			Expect(ok).To(BeTrue(), key)
			Expect(property["description"]).To(ContainSubstring("systemd-cryptenroll"), key)
		}
	})

	It("names the two fields under logs", func() {
		generated, err := GenerateSchema(RootSchema{}, "")
		Expect(err).ToNot(HaveOccurred())
		// The reflector hoists named struct types into $defs, so assert on
		// the definition rather than walking the $ref.
		Expect(generated).To(ContainSubstring("SchemaLogsSchema"))
		logs := generated[strings.Index(generated, "SchemaLogsSchema"):]
		Expect(logs).To(ContainSubstring(`"journal"`))
		Expect(logs).To(ContainSubstring(`"files"`))
	})

	It("validates a config that sets all three", func() {
		config, err := NewConfigFromYAML(`#cloud-config
users:
  - name: kairos
bind-pcrs:
  - "7"
bind-public-pcrs:
  - "11"
logs:
  journal:
    - kairos-agent
  files:
    - /var/log/foo.log
`, RootSchema{})
		Expect(err).ToNot(HaveOccurred())
		Expect(config.IsValid()).To(BeTrue(), func() string {
			if config.ValidationError != nil {
				return config.ValidationError.Error()
			}
			return ""
		}())
	})

	It("rejects a logs block of the wrong shape", func() {
		// Without a real LogsSchema type this passed vacuously, because an
		// undeclared key is accepted whatever it holds.
		config, err := NewConfigFromYAML(`#cloud-config
users:
  - name: kairos
logs:
  journal: kairos-agent
`, RootSchema{})
		Expect(err).ToNot(HaveOccurred())
		Expect(config.IsValid()).To(BeFalse())
		Expect(config.ValidationError.Error()).To(ContainSubstring("journal"))
	})
})
