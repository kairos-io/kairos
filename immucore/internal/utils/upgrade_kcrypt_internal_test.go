package utils

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakeBin puts an executable script named bin at the front of PATH for the
// duration of the spec, so the code under test runs it instead of the real one.
func fakeBin(bin, script string) {
	dir := GinkgoT().TempDir()
	Expect(os.WriteFile(filepath.Join(dir, bin), []byte("#!/bin/sh\n"+script+"\n"), 0o755)).To(Succeed())

	old := os.Getenv("PATH")
	Expect(os.Setenv("PATH", dir+string(os.PathListSeparator)+old)).To(Succeed())
	DeferCleanup(func() {
		Expect(os.Setenv("PATH", old)).To(Succeed())
	})
}

var _ = Describe("reading the LUKS UUID of a partition", func() {
	It("Ignores a warning cryptsetup wrote to stderr", func() {
		// cryptsetup prints this whenever /run/cryptsetup is missing, and
		// still exits 0 with the UUID on stdout. Merging the two streams
		// gives uuid.FromString the warning line as well, which fails the
		// kcrypt upgrade boot step.
		fakeBin("cryptsetup", `echo "WARNING: Locking directory /run/cryptsetup is missing!" >&2
echo "5f8c1a94-4c7e-4e21-9f1f-2b3c4d5e6f70"`)

		got, err := luksUUID("/dev/vda2")
		Expect(err).ToNot(HaveOccurred())
		Expect(got.String()).To(Equal("5f8c1a94-4c7e-4e21-9f1f-2b3c4d5e6f70"))
	})

	It("Reports the reason cryptsetup gave when it fails", func() {
		fakeBin("cryptsetup", `echo "Device /dev/vda2 does not exist or access denied." >&2
exit 4`)

		_, err := luksUUID("/dev/vda2")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("does not exist or access denied"))
	})

	It("Refuses output that is not a UUID", func() {
		fakeBin("cryptsetup", `echo "not a uuid"`)

		_, err := luksUUID("/dev/vda2")
		Expect(err).To(HaveOccurred())
	})
})
