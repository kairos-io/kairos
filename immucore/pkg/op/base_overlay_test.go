package op_test

import (
	"path/filepath"

	"github.com/kairos-io/kairos/v4/immucore/pkg/op"
	"github.com/kairos-io/kairos/v4/immucore/pkg/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("BaseOverlay backing base", func() {
	var base string

	BeforeEach(func() {
		base = filepath.Join(GinkgoT().TempDir(), "run", "overlay")
	})

	// ParseMount resolves only LABEL= and UUID=, so a colon form used to reach
	// mount as the literal source and fail at boot with "no such device".
	DescribeTable("resolves a device written with either separator",
		func(backingBase, want string) {
			operation, err := op.BaseOverlay(schema.Overlay{Base: base, BackingBase: backingBase})
			Expect(err).ToNot(HaveOccurred())
			Expect(operation.MountOption.Source).To(Equal(want))
		},
		Entry("LABEL=", "LABEL=COS_PERSISTENT", "/dev/disk/by-label/COS_PERSISTENT"),
		Entry("LABEL:", "LABEL:COS_PERSISTENT", "/dev/disk/by-label/COS_PERSISTENT"),
		Entry("UUID=", "UUID=1234-ABCD", "/dev/disk/by-uuid/1234-ABCD"),
		Entry("UUID:", "UUID:1234-ABCD", "/dev/disk/by-uuid/1234-ABCD"),
	)

	It("suggests a device form when the backing base is invalid", func() {
		_, err := op.BaseOverlay(schema.Overlay{Base: base, BackingBase: "bogus"})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("LABEL=COS_PERSISTENT"))
	})

	It("keeps the tmpfs colon form", func() {
		operation, err := op.BaseOverlay(schema.Overlay{Base: base, BackingBase: "tmpfs:30%"})
		Expect(err).ToNot(HaveOccurred())
		Expect(operation.MountOption.Options).To(Equal([]string{"size=30%"}))
	})
})
