package op_test

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/kairos-io/immucore/internal/constants"
	"github.com/kairos-io/immucore/pkg/op"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("MountWithBaseOverlay", func() {
	var root, base string

	BeforeEach(func() {
		root = GinkgoT().TempDir()
		base = GinkgoT().TempDir()
	})

	It("creates the lowerdir when it is missing from the image", func() {
		operation := op.MountWithBaseOverlay("mnt", root, base)

		Expect(operation.PrepareCallback()).ToNot(HaveOccurred())
		Expect(filepath.Join(root, "mnt")).To(BeADirectory())
	})

	It("reports ErrMountTargetMissing when the lowerdir cannot be created", func() {
		if os.Geteuid() == 0 {
			Skip("root bypasses directory write permissions")
		}
		// Stands in for the real case: the path is absent from the OS image and the
		// rootfs is still mounted read-only, so MkdirAll cannot create it either.
		readOnly := filepath.Join(root, "readonly")
		Expect(os.Mkdir(readOnly, 0500)).ToNot(HaveOccurred())

		operation := op.MountWithBaseOverlay("mnt", readOnly, base)

		err := operation.PrepareCallback()
		Expect(err).To(HaveOccurred())
		Expect(errors.Is(err, constants.ErrMountTargetMissing)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring(filepath.Join(readOnly, "mnt")))
	})
})
