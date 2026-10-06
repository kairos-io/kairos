package testartifacts

import (
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("absIfSet", func() {
	It("makes a relative path absolute so docker run -v cannot read it as a volume name", func() {
		got, err := absIfSet("keys/db.pem")
		Expect(err).ToNot(HaveOccurred())
		Expect(filepath.IsAbs(got)).To(BeTrue())
		Expect(got).To(HaveSuffix(filepath.Join("keys", "db.pem")))
	})

	It("leaves an empty path empty", func() {
		got, err := absIfSet("")
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(BeEmpty())
	})
})
