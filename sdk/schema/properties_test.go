package schema_test

import (
	"encoding/json"

	. "github.com/kairos-io/kairos/v4/sdk/schema"
	. "github.com/onsi/gomega"
)

func errText(c *KConfig) string {
	if c.ValidationError == nil {
		return ""
	}
	return c.ValidationError.Error()
}

func definitionProperties(schema, definition string) map[string]interface{} {
	var doc struct {
		Properties  map[string]interface{} `json:"properties"`
		Definitions map[string]struct {
			Properties map[string]interface{} `json:"properties"`
		} `json:"definitions"`
	}
	ExpectWithOffset(1, json.Unmarshal([]byte(schema), &doc)).To(Succeed())

	if definition == "" {
		return doc.Properties
	}

	def, ok := doc.Definitions[definition]
	ExpectWithOffset(1, ok).To(BeTrue(), "definition %s is absent from the schema", definition)
	return def.Properties
}

func rootProperties(schema string) map[string]interface{} {
	return definitionProperties(schema, "")
}

func p2pProperties(schema string) map[string]interface{} {
	return definitionProperties(schema, "SchemaP2PSchema")
}

func vpnProperties(schema string) map[string]interface{} {
	return definitionProperties(schema, "SchemaVPN")
}

func kubevipProperties(schema string) map[string]interface{} {
	return definitionProperties(schema, "SchemaKubeVIPSchema")
}
