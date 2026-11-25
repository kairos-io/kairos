package schema_test

import (
	"github.com/kairos-io/kairos/v4/sdk/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Image checksum schema", func() {
	validate := func(config string) error {
		return schema.Validate("#cloud-config\nusers:\n  - name: kairos\n" + config)
	}

	It("accepts the documented keys", func() {
		Expect(validate(`image-checksum:
  create: true
  verify: fail
  require: true
`)).To(Succeed())
	})

	DescribeTable("accepts every verify action",
		func(action string) {
			Expect(validate("image-checksum:\n  verify: " + action + "\n")).To(Succeed())
		},
		Entry("none", "none"),
		Entry("warn", "warn"),
		Entry("fail", "fail"),
	)

	It("rejects an unknown verify action", func() {
		Expect(validate("image-checksum:\n  verify: sometimes\n")).ToNot(Succeed())
	})

	It("rejects a misspelled key", func() {
		Expect(validate("image-checksum:\n  verfy: fail\n")).ToNot(Succeed())
	})

	It("rejects a non boolean create", func() {
		Expect(validate("image-checksum:\n  create: perhaps\n")).ToNot(Succeed())
	})
})
