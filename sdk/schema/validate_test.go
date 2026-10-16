package schema_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	. "github.com/kairos-io/kairos/v4/sdk/schema"
	"github.com/santhosh-tekuri/jsonschema/v5"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Validate", func() {
	Context("JSONSchema", func() {
		var out string
		var doc map[string]interface{}

		BeforeEach(func() {
			var err error
			out, err = JSONSchema("0.0.0")
			Expect(err).ToNot(HaveOccurred())
			Expect(json.Unmarshal([]byte(out), &doc)).To(Succeed())
		})

		It("names the document after the given version", func() {
			Expect(doc).To(HaveKeyWithValue("$id", "https://kairos.io/0.0.0/cloud-config.json"))
		})

		It("declares the dialect it is written in, not its own address", func() {
			// $schema is the dialect the document is written in. Pointing it at
			// the document's own address makes a validator fetch that address as
			// a meta-schema, and https://kairos.io/<version>/cloud-config.json
			// has not been published since v2.0.1.
			Expect(doc).To(HaveKeyWithValue("$schema", MetaSchemaDraft07))
		})

		It("declares the dialect the reflector actually emits", func() {
			// The reflector writes "definitions", which is draft-07. If it ever
			// starts writing "$defs" the declared dialect has to move with it.
			Expect(doc).To(HaveKey("definitions"))
			Expect(doc).ToNot(HaveKey("$defs"))
		})

		It("is usable by a validator that reads the dialect it declares", func() {
			// The point of the two keys above: a consumer can compile the
			// printed document and get the same verdicts the agent gives.
			// This fails if the declared dialect cannot resolve the $ref
			// targets the reflector emitted.
			compiler := jsonschema.NewCompiler()
			Expect(compiler.AddResource("cloud-config.json", strings.NewReader(out))).To(Succeed())
			sch, err := compiler.Compile("cloud-config.json")
			Expect(err).ToNot(HaveOccurred())

			var valid interface{}
			Expect(json.Unmarshal([]byte(`{"users":[{"name":"kairos"}]}`), &valid)).To(Succeed())
			Expect(sch.Validate(valid)).To(Succeed())

			var invalid interface{}
			Expect(json.Unmarshal([]byte(`{"users":[{"name":7}]}`), &invalid)).To(Succeed())
			Expect(sch.Validate(invalid)).To(MatchError(ContainSubstring("expected string, but got number")))
		})
	})

	Context("Validate", func() {
		var yaml string

		Context("with commands defined as a scalar", func() {
			BeforeEach(func() {
				yaml = `#cloud-config
users:
  - name: example
stages:
  example:
    - commands: "echo whoops"`
			})

			It("rejects the invalid stage", func() {
				Expect(Validate(yaml)).To(MatchError(ContainSubstring("expected array, but got string")))
			})
		})

		Context("With a really long config string", func() {
			BeforeEach(func() {
				yaml = `#cloud-config
users:
  - name: kairos
    passwd: kairos
vpn:
  network_token: "dssdnfjkldashfkjhasdkhfkasjdhfkjhasdjkfhaksjdhfkjashjdkfhioreqwhfuihqweruifhuewrbfhuewrfuyequfhuiehuifheqrihfuiqrehfuirqheiufhreqiuhfuiqheiufhqeuihfuiqrehfiuhqreuifrhiuqehfiuhqeirhfiuewhrfhqwehfriuewhfuihewiuhfruewhrifhwiuehrfiuhweiurfhwueihrfuiwehufhweuihrfuiwerhfuihewruifhewuihfiouwehrfiouhwei"
`
			})
			It("validates", func() {
				Expect(Validate(yaml)).ToNot(HaveOccurred())
			})
		})

		Context("with a valid config", func() {
			BeforeEach(func() {
				yaml = `#cloud-config
users:
  - name: kairos
    passwd: kairos`
			})

			It("is successful reading it from file", func() {
				f, err := os.MkdirTemp("", "tests")
				Expect(err).ToNot(HaveOccurred())
				defer os.RemoveAll(f)

				path := filepath.Join(f, "config.yaml")
				err = os.WriteFile(path, []byte(yaml), 0655)
				Expect(err).ToNot(HaveOccurred())
				err = Validate(path)
				Expect(err).ToNot(HaveOccurred())
			})
			It("is successful reading it from a string", func() {
				Expect(Validate(yaml)).ToNot(HaveOccurred())
			})
		})

		Context("with a header and nothing else", func() {
			// An emptied configuration is a configuration. `kairos
			// edit-config` reopens the editor until the document validates,
			// so a schema that refuses this one is a schema that will not let
			// anybody clear /oem/90_custom.yaml.
			for _, header := range []string{"#cloud-config", "#kairos-config", "#node-config"} {
				It("accepts "+header+" on its own", func() {
					Expect(Validate(header + "\n")).ToNot(HaveOccurred())
				})
			}

			It("accepts a header followed by comments only", func() {
				Expect(Validate("#cloud-config\n# everything here is commented out\n")).ToNot(HaveOccurred())
			})

			It("accepts the empty mapping written out", func() {
				Expect(Validate("#cloud-config\n{}\n")).ToNot(HaveOccurred())
			})

			It("still refuses a body that is not a mapping", func() {
				// Emptiness is the only thing being allowed here. A document
				// whose body is a sequence or a scalar is still not a Kairos
				// configuration.
				Expect(Validate("#cloud-config\n- kairos\n")).To(MatchError(ContainSubstring("expected object, but got array")))
				Expect(Validate("#cloud-config\nkairos\n")).To(MatchError(ContainSubstring("expected object, but got string")))
			})

			It("still refuses an empty document with no header", func() {
				Expect(Validate("")).To(MatchError("missing #cloud-config header"))
			})
		})

		Context("without a header", func() {
			BeforeEach(func() {
				yaml = `users:
  - name: kairos
    passwd: kairos`
			})

			It("is fails", func() {
				f, err := os.MkdirTemp("", "tests")
				Expect(err).ToNot(HaveOccurred())
				defer os.RemoveAll(f)

				path := filepath.Join(f, "config.yaml")
				err = os.WriteFile(path, []byte(yaml), 0655)
				Expect(err).ToNot(HaveOccurred())
				err = Validate(path)
				Expect(err).To(MatchError("missing #cloud-config header"))
			})
		})

		Context("with an invalid rule", func() {
			BeforeEach(func() {
				yaml = `#cloud-config
users:
  - name: 007
    passwd: kairos`
			})

			It("is fails", func() {
				f, err := os.MkdirTemp("", "tests")
				Expect(err).ToNot(HaveOccurred())
				defer os.RemoveAll(f)

				path := filepath.Join(f, "config.yaml")
				err = os.WriteFile(path, []byte(yaml), 0655)
				Expect(err).ToNot(HaveOccurred())
				err = Validate(path)
				Expect(err.Error()).To(MatchRegexp("expected string, but got number"))
			})
		})
	})
})
