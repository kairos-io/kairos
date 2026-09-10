package schema_test

import (
	"github.com/kairos-io/kairos/v4/sdk/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Extensions schema", func() {
	validate := func(config string) error {
		return schema.Validate("#cloud-config\nusers:\n  - name: kairos\n" + config)
	}

	It("accepts catalogs and the signature opt-out", func() {
		Expect(validate(`extensions:
  catalogs:
    - https://kairos-io.github.io/hadron-layers/releases.json
    - https://example.org/releases.json
  ignore_signatures: true
`)).To(Succeed())
	})

	It("accepts every form a declared extension can take", func() {
		Expect(validate(`install:
  device: auto
  extensions:
    - fwupd
    - fwupd@2.1.7
    - name: git
      version: ">= 2.50, < 3"
    - oci://ghcr.io/example/tools.sysext.raw
    - /run/initramfs/live/extra.sysext.raw
`)).To(Succeed())
	})

	It("rejects an extension with no name", func() {
		Expect(validate(`install:
  extensions:
    - version: 2.1.7
`)).To(HaveOccurred())
	})

	It("rejects catalogs that are not a list of strings", func() {
		Expect(validate(`extensions:
  catalogs: https://example.org/releases.json
`)).To(HaveOccurred())
	})
})
