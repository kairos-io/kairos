package machine_test

import (
	"io/ioutil"
	"os"

	. "github.com/c3os-io/c3os/internal/machine"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("BootCMDLine", func() {
	Context("parses data", func() {

		It("returns cmdline if provided", func() {
			f, err := ioutil.TempFile("", "test")
			Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(f.Name())

			err = ioutil.WriteFile(f.Name(), []byte(`config_url="foo bar" baz.bar=""`), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())

			b, err := DotToYAML(f.Name())
			Expect(err).ToNot(HaveOccurred())

			Expect(string(b)).To(Equal("baz:\n  bar: \"\"\nconfig_url: foo bar\n"))
		})
	})
})
