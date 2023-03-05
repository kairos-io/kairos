package config_test

import (
	"reflect"
	"strings"

	. "github.com/kairos-io/kairos/pkg/config/schemas"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func getTagName(s string) string {
	if len(s) < 1 {
		return ""
	}

	f := func(c rune) bool {
		return c == '"' || c == ','
	}
	return s[:strings.IndexFunc(s, f)]
}

func structContainsField(f, t string, str interface{}) bool {
	values := reflect.ValueOf(str)
	types := values.Type()

	for j := 0; j < values.NumField(); j++ {
		tagName := getTagName(types.Field(j).Tag.Get("json"))
		if types.Field(j).Name == f || tagName == t {
			return true
		} else {
			if types.Field(j).Type.Kind() == reflect.Struct {
				if types.Field(j).Type.Name() != "" {
					model := reflect.New(types.Field(j).Type)
					if instance, ok := model.Interface().(OneOfModel); ok {
						for _, childSchema := range instance.JSONSchemaOneOf() {
							if structContainsField(f, t, childSchema) {
								return true
							}
						}
					}
				}
			}
		}
	}

	return false
}

var _ = Describe("Schema", func() {
	Context("NewConfigFromYAML", func() {
		var config *KConfig
		var err error
		var yaml string

		JustBeforeEach(func() {
			config, err = NewConfigFromYAML(yaml, RootSchema{})
		})

		Context("With invalid YAML syntax", func() {
			BeforeEach(func() {
				yaml = `#cloud-config
this is:
- invalid
yaml`
			})

			It("errors", func() {
				Expect(err.Error()).To(MatchRegexp("yaml: line 4: could not find expected ':'"))
			})
		})

		Context("When `users` is empty", func() {
			BeforeEach(func() {
				yaml = `#cloud-config
users: []`
			})

			It("errors", func() {
				Expect(err).ToNot(HaveOccurred())
				Expect(config.IsValid()).NotTo(BeTrue())
				Expect(config.ValidationError.Error()).To(MatchRegexp("minimum 1 items required, but found 0 items"))
			})
		})

		Context("without a valid header", func() {
			BeforeEach(func() {
				yaml = `---
users:
  - name: kairos
    passwd: kairos`
			})

			It("is successful but HasHeader returns false", func() {
				Expect(err).ToNot(HaveOccurred())
				Expect(config.HasHeader()).To(BeFalse())
			})
		})

		Context("With a valid config", func() {
			BeforeEach(func() {
				yaml = `#cloud-config
users:
  - name: kairos
    passwd: kairos`
			})

			It("is successful", func() {
				Expect(err).ToNot(HaveOccurred())
				Expect(config.HasHeader()).To(BeTrue())
			})
		})
	})

	Context("GenerateSchema", func() {
		var url string
		var schema string
		var err error

		type TestSchema struct {
			Key interface{} `json:"key,omitemtpy" required:"true"`
		}

		JustBeforeEach(func() {
			schema, err = GenerateSchema(TestSchema{}, url)
			Expect(err).ToNot(HaveOccurred())
		})

		It("does not include the $schema key by default", func() {
			Expect(strings.Contains(schema, `$schema`)).To(BeFalse())
		})

		It("can use any type of schma", func() {
			wants := `{
 "required": [
  "key"
 ],
 "properties": {
  "key": {}
 },
 "type": "object"
}`
			Expect(schema).To(Equal(wants))
		})

		Context("with a URL", func() {
			BeforeEach(func() {
				url = "http://foobar"
			})

			It("appends the $schema key", func() {
				Expect(strings.Contains(schema, `$schema": "http://foobar"`)).To(BeTrue())
			})
		})

	})
})
