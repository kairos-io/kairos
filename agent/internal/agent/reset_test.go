package agent

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("scanResetConfig", func() {
	var dir string

	BeforeEach(func() {
		var err error
		dir, err = os.MkdirTemp("", "reset-strict")
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() { os.RemoveAll(dir) })

		// install.device only accepts "auto", a /dev path or a script:// url,
		// so "sda" is a schema error the scan has to decide what to do with.
		err = os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(
			"#cloud-config\ninstall:\n  device: sda\n"), 0644)
		Expect(err).ToNot(HaveOccurred())
	})

	It("fails on a config that does not match the schema when strict validation is on", func() {
		_, err := scanResetConfig("{}", true, dir)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("device"))
	})

	It("warns and carries on when strict validation is off", func() {
		c, err := scanResetConfig("{}", false, dir)
		Expect(err).ToNot(HaveOccurred())
		Expect(c.Install.Device).To(Equal("sda"))
	})
})
