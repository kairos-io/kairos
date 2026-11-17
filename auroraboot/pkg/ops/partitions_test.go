package ops

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("RawImage emitPartitionImages", Label("raw"), func() {
	var (
		r       *RawImage
		out     string
		boot    string
		oem     string
		recover string
	)

	BeforeEach(func() {
		out = GinkgoT().TempDir()
		src := GinkgoT().TempDir()
		r = NewEFIRawImage(RawImageParams{Source: src, Output: out, NoDefaultCloudConfig: true})
		r.SeparatePartitionsImages = true

		// Stand-in partition images already built in the temp dir.
		tmp := GinkgoT().TempDir()
		boot = filepath.Join(tmp, "efi.img")
		oem = filepath.Join(tmp, "oem.img")
		recover = filepath.Join(tmp, "recovery.img")
		Expect(os.WriteFile(boot, []byte("efi-content"), 0o600)).To(Succeed())
		Expect(os.WriteFile(oem, []byte("oem-content"), 0o600)).To(Succeed())
		Expect(os.WriteFile(recover, []byte("recovery-content"), 0o600)).To(Succeed())
	})

	It("copies each partition image to the output dir with the script-matching names", func() {
		Expect(r.emitPartitionImages(boot, oem, recover)).To(Succeed())

		efiOut := filepath.Join(out, "efi.img")
		oemOut := filepath.Join(out, "oem.img")
		recoveryOut := filepath.Join(out, "recovery_partition.img")

		for name, content := range map[string]string{
			efiOut:      "efi-content",
			oemOut:      "oem-content",
			recoveryOut: "recovery-content",
		} {
			data, err := os.ReadFile(name)
			Expect(err).NotTo(HaveOccurred(), "expected %s to exist", name)
			Expect(string(data)).To(Equal(content))
		}
	})

	It("does not produce a merged kairos-*.raw disk in partitions mode", func() {
		Expect(r.emitPartitionImages(boot, oem, recover)).To(Succeed())

		matches, err := filepath.Glob(filepath.Join(out, "kairos-*.raw"))
		Expect(err).NotTo(HaveOccurred())
		Expect(matches).To(BeEmpty())
	})

	It("errors when a source partition image is missing", func() {
		err := r.emitPartitionImages(filepath.Join(GinkgoT().TempDir(), "missing.img"), oem, recover)
		Expect(err).To(HaveOccurred())
	})
})
