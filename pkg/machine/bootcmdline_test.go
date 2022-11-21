package machine_test

import (
	"os"

	. "github.com/kairos-io/kairos/pkg/machine"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("BootCMDLine", func() {
	Context("parses data", func() {

		It("returns cmdline if provided", func() {
			f, err := os.CreateTemp("", "test")
			Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(f.Name())

			err = os.WriteFile(f.Name(), []byte(`config_url="foo bar" baz.bar=""`), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())

			b, err := DotToYAML(f.Name())
			Expect(err).ToNot(HaveOccurred())

			Expect(string(b)).To(Equal("baz:\n    bar: \"\"\nconfig_url: foo bar\n"), string(b))
		})
	})
})
