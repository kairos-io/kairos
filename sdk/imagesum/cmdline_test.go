package imagesum_test

import (
	"github.com/kairos-io/kairos/v4/sdk/imagesum"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("cmdline", func() {
	It("finds nothing on a cmdline without the keys", func() {
		o := imagesum.ParseCmdline("root=LABEL=COS_ACTIVE rd.immucore.debug quiet")
		Expect(o.Create).To(BeNil())
		Expect(o.Verify).To(BeNil())
		Expect(o.Require).To(BeNil())
		Expect(o.Problems).To(BeEmpty())
	})

	It("reads the three keys", func() {
		o := imagesum.ParseCmdline("ro image-checksum.create=true image-checksum.verify=fail image-checksum.require=false quiet")
		Expect(*o.Create).To(BeTrue())
		Expect(*o.Verify).To(Equal(imagesum.Fail))
		Expect(*o.Require).To(BeFalse())
	})

	It("lets the last one win", func() {
		o := imagesum.ParseCmdline("image-checksum.verify=fail image-checksum.verify=warn")
		Expect(*o.Verify).To(Equal(imagesum.Warn))
	})

	It("reports a value it cannot parse and ignores it", func() {
		o := imagesum.ParseCmdline("image-checksum.verify=sometimes image-checksum.create=maybe image-checksum.require=yes-please")
		Expect(o.Verify).To(BeNil())
		Expect(o.Create).To(BeNil())
		Expect(o.Require).To(BeNil())
		Expect(o.Problems).To(HaveLen(3))
	})

	It("reads no other spelling", func() {
		o := imagesum.ParseCmdline("rd.immucore.verify_image=fail rd.immucore.verify_image_require=true imagechecksum.verify=fail")
		Expect(o.Verify).To(BeNil())
		Expect(o.Require).To(BeNil())
	})

	It("a key without a value is not a setting", func() {
		o := imagesum.ParseCmdline("image-checksum.require image-checksum.verify")
		Expect(o.Require).To(BeNil())
		Expect(o.Verify).To(BeNil())
	})

	It("applies only what was set, an explicit false included", func() {
		yes := true
		c := imagesum.Config{Create: &yes, Verify: imagesum.Fail, Require: &yes}
		c.Apply(imagesum.ParseCmdline("image-checksum.create=false"))
		Expect(c.CreateEnabled()).To(BeFalse())
		Expect(c.Verify).To(Equal(imagesum.Fail))
		Expect(c.RequireEnabled()).To(BeTrue())
	})
})
