package agent_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	. "github.com/kairos-io/kairos/internal/agent"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Validate", func() {
	Context("JSONSchema", func() {
		It("returns a schema with a url to the given version", func() {
			out, err := JSONSchema("0.0.0")
			Expect(err).ToNot(HaveOccurred())
			Expect(strings.Contains(out, `$schema": "https://kairos.io/0.0.0/cloud-config.json"`)).To(BeTrue())
		})
	})

	Context("Validate", func() {
		var yaml string

		Context("with a valid config", func() {
			BeforeEach(func() {
				yaml = `#cloud-config
users:
  - name: kairos
    passwd: kairos`
			})

			It("is successful", func() {
				f, err := ioutil.TempDir("", "tests")
				Expect(err).ToNot(HaveOccurred())
				defer os.RemoveAll(f)

				path := filepath.Join(f, "config.yaml")
				err = os.WriteFile(path, []byte(yaml), 0655)
				Expect(err).ToNot(HaveOccurred())
				err = Validate(path)
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Context("without a header", func() {
			BeforeEach(func() {
				yaml = `users:
  - name: kairos
    passwd: kairos`
			})

			It("is fails", func() {
				f, err := ioutil.TempDir("", "tests")
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
				f, err := ioutil.TempDir("", "tests")
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
	// Context("With the wrong header", func() {
	// 	BeforeEach(func() {
	// 		yaml = `---
	// users:
	// - name: "kairos"
	// passwd: "kairos"`
	// 	})

	// 	It("errors", func() {
	// 		Expect(err.Error()).To(MatchRegexp("missing #cloud-config header"))
	// 	})
	// })
})
