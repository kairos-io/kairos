package unstructured_test

import (
	. "github.com/kairos-io/kairos/sdk/unstructured"
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
})
