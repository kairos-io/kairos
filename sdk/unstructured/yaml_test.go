package unstructured_test

import (
	. "github.com/kairos-io/kairos/v4/sdk/unstructured"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

var _ = Describe("unstructured", Label("unstructured-test"), func() {
	var content []byte
	var data map[string]interface{}

	BeforeEach(func() {
		content = []byte(`
a:
  b:
    c:
      d: 1
      e: "some_string"
`)

		data = map[string]interface{}{}
		err := yaml.Unmarshal(content, &data)
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("YAMLHasKey", func() {
		It("returns true if the key exists in the yaml", func() {
			r, err := YAMLHasKey("a.b", content)
			Expect(err).ToNot(HaveOccurred())
			Expect(r).To(BeTrue())
		})

		It("returns false if the key doesn't exist in the yaml", func() {
			r, err := YAMLHasKey("a.z", content)
			Expect(err).ToNot(HaveOccurred())
			Expect(r).To(BeFalse())
		})
	})

	Describe("LookupString", func() {
		When("key exists", func() {
			When("key is a string", func() {
				It("returns the value", func() {
					r, err := LookupString(".a.b.c.e", data)
					Expect(err).ToNot(HaveOccurred())
					Expect(r).To(Equal("some_string"))
				})
			})

			When("key isn't a string", func() {
				It("returns an error", func() {
					_, err := LookupString(".a.b.c.d", data)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(MatchRegexp("value is not a string"))
				})
			})
		})
		When("key doesn't exist", func() {
			It("returns an error", func() {
				_, err := LookupString(".a.b.c.z", data)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(MatchRegexp("value is not a string"))
			})
		})
	})

	Describe("ReplaceValue", func() {
		When("key exists", func() {
			When("value is a map", func() {
				It("can replace with a string", func() {
					r, err := ReplaceValue(".a=\"test\"", data)
					Expect(err).ToNot(HaveOccurred())
					Expect(r).To(Equal("a: test\n"))
				})
			})
			When("value is a string", func() {
				It("can replace with a string", func() {
					r, err := ReplaceValue(".a.b.c.e=\"test\"", data)
					Expect(err).ToNot(HaveOccurred())
					Expect(r).To(Equal(`a:
    b:
        c:
            d: 1
            e: test
`))
				})
			})
		})
		When("key doesn't exist", func() {
			It("creates the key", func() {
				r, err := ReplaceValue(".a.b.c.z=\"test\"", data)
				Expect(err).ToNot(HaveOccurred())
				Expect(r).To(Equal(`a:
    b:
        c:
            d: 1
            e: some_string
            z: test
`), r)
			})
		})
	})

	Describe("ToYAML", func() {
		for name, val := range map[string]string{
			"a double quote":          `a"b`,
			"a backslash escape":      `C:\new`,
			"jq string interpolation": `\(1+1)`,
			"a jq injection payload":  `x" | .evil="y`,
		} {
			value := val
			It("stores a value with "+name+" verbatim", func() {
				out, err := ToYAML(map[string]interface{}{"foo": value})
				Expect(err).ToNot(HaveOccurred())

				got := map[string]interface{}{}
				Expect(yaml.Unmarshal(out, &got)).To(Succeed())
				Expect(got).To(Equal(map[string]interface{}{"foo": value}), string(out))
			})
		}

		It("does not add a second key from an injected value", func() {
			out, err := ToYAML(map[string]interface{}{"foo": `x" | .evil="y`})
			Expect(err).ToNot(HaveOccurred())

			got := map[string]interface{}{}
			Expect(yaml.Unmarshal(out, &got)).To(Succeed())
			Expect(got).ToNot(HaveKey("evil"))
			Expect(got).To(HaveLen(1))
		})

		toMap := func(in map[string]interface{}) (map[string]interface{}, error) {
			GinkgoHelper()
			out, err := ToYAML(in)
			m := map[string]interface{}{}
			Expect(yaml.Unmarshal(out, &m)).To(Succeed())
			return m, err
		}

		It("nests dotted keys and turns true and false into booleans", func() {
			m, err := toMap(map[string]interface{}{"install.partitions.state.size": "100", "install.auto": "true"})
			Expect(err).ToNot(HaveOccurred())
			Expect(m).To(Equal(map[string]interface{}{
				"install": map[string]interface{}{
					"auto":       true,
					"partitions": map[string]interface{}{"state": map[string]interface{}{"size": "100"}},
				},
			}))
		})

		It("builds lists from [N] indexes", func() {
			m, err := toMap(map[string]interface{}{
				"stages.boot[0].commands[1]": "echo b",
				"stages.boot[0].name":        "first",
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(m).To(Equal(map[string]interface{}{
				"stages": map[string]interface{}{
					"boot": []interface{}{
						map[string]interface{}{
							"commands": []interface{}{nil, "echo b"},
							"name":     "first",
						},
					},
				},
			}))
		})

		It("keeps a key that is a jq expression as a literal key", func() {
			m, err := toMap(map[string]interface{}{`install["device"]`: "/dev/sda"})
			Expect(err).ToNot(HaveOccurred())
			Expect(m).To(Equal(map[string]interface{}{`install["device"]`: "/dev/sda"}))
		})

		It("nests a key that contains a dash", func() {
			m, err := toMap(map[string]interface{}{"k3s-agent.enabled": "true", "p2p.network-token": "x"})
			Expect(err).ToNot(HaveOccurred())
			Expect(m).To(Equal(map[string]interface{}{
				"k3s-agent": map[string]interface{}{"enabled": true},
				"p2p":       map[string]interface{}{"network-token": "x"},
			}))
		})

		It("rejects a list index above 1024 and keeps the other keys", func() {
			m, err := toMap(map[string]interface{}{"a[1025]": "x", "b[99999999999999999999]": "x", "c[1024]": "y"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(`"a[1025]"`))
			Expect(err.Error()).To(ContainSubstring(`"b[99999999999999999999]"`))
			Expect(m).To(HaveLen(1))
			Expect(m["c"]).To(HaveLen(1025))
		})

		It("skips a malformed key, names it and keeps the value out of the error", func() {
			m, err := toMap(map[string]interface{}{
				"a..b":          "secret-value",
				"8250.nr_uarts": "0",
				"hostname":      "box",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(`"a..b"`))
			Expect(err.Error()).To(ContainSubstring(`"8250.nr_uarts"`))
			Expect(err.Error()).ToNot(ContainSubstring("secret-value"))
			Expect(m).To(HaveKeyWithValue("hostname", "box"))
			Expect(m).ToNot(HaveKey("a"))
			Expect(m).ToNot(HaveKey("8250"))
		})

		It("resolves colliding keys the same way on every run", func() {
			in := map[string]interface{}{
				"rd.luks":      "0",
				"rd.luks.uuid": "x",
				"list[0]":      "x",
				"list[0].key":  "y",
			}
			for range 50 {
				m, err := toMap(in)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(`"rd.luks.uuid"`))
				Expect(err.Error()).To(ContainSubstring(`"list[0].key"`))
				Expect(m).To(Equal(map[string]interface{}{
					"list": []interface{}{"x"},
					"rd":   map[string]interface{}{"luks": "0"},
				}))
			}
		})
	})
})
