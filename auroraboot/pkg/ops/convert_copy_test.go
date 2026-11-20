package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("raw-disk conversion on a private copy", func() {
	const rawContent = "original-raw-bytes-that-must-survive-conversion"
	var dir, raw string

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		raw = filepath.Join(dir, "kairos-test.raw")
		Expect(os.WriteFile(raw, []byte(rawContent), 0o644)).To(Succeed())
	})

	Describe("convertRawOnCopy", func() {
		It("runs the converter on a copy and leaves the original raw untouched", func() {
			// A converter that destroys its source (the way Raw2Gce truncates and
			// Raw2Azure renames the raw in place). If it were handed the original
			// instead of a copy, the raw-survives assertion below would fail.
			convert := func(source string) (string, error) {
				Expect(source).NotTo(Equal(raw), "converter must receive a copy, not the original raw")
				Expect(os.Remove(source)).To(Succeed())
				out := source + ".converted"
				return out, os.WriteFile(out, []byte("output"), 0o644)
			}

			dst, err := convertRawOnCopy(dir, convert)
			Expect(err).NotTo(HaveOccurred())

			// The output was moved next to the raw under <raw>.<ext>.
			Expect(dst).To(Equal(filepath.Join(dir, "kairos-test.raw.converted")))
			Expect(dst).To(BeAnExistingFile())

			// The original raw survived, byte-for-byte.
			got, err := os.ReadFile(raw)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(got)).To(Equal(rawContent))

			// The throwaway work directory was cleaned up.
			entries, err := os.ReadDir(dir)
			Expect(err).NotTo(HaveOccurred())
			for _, e := range entries {
				Expect(e.Name()).NotTo(HavePrefix(".convert-"))
			}
		})

		It("errors when the build dir has no raw disk", func() {
			_, err := convertRawOnCopy(GinkgoT().TempDir(), func(string) (string, error) { return "", nil })
			Expect(err).To(MatchError(ContainSubstring("one and only one raw disk")))
		})

		It("propagates the converter error and leaves the raw untouched", func() {
			_, err := convertRawOnCopy(dir, func(string) (string, error) { return "", errors.New("boom") })
			Expect(err).To(MatchError(ContainSubstring("boom")))
			Expect(raw).To(BeAnExistingFile())
			got, _ := os.ReadFile(raw)
			Expect(string(got)).To(Equal(rawContent))
		})

		It("keeps the shared raw intact when converters run concurrently", func() {
			// One converter mutates its copy in place, the other renames its copy
			// away — the two destructive patterns that raced on the shared raw
			// before this change. Run over the same build dir at once; the
			// original raw must survive both.
			mutating := func(source string) (string, error) {
				if err := os.WriteFile(source, []byte("mutated"), 0o644); err != nil {
					return "", err
				}
				out := source + ".gce"
				return out, os.WriteFile(out, []byte("gce"), 0o644)
			}
			renaming := func(source string) (string, error) {
				out := source + ".vhd"
				return out, os.Rename(source, out)
			}

			var wg sync.WaitGroup
			errs := make([]error, 2)
			wg.Add(2)
			go func() { defer wg.Done(); _, errs[0] = convertRawOnCopy(dir, mutating) }()
			go func() { defer wg.Done(); _, errs[1] = convertRawOnCopy(dir, renaming) }()
			wg.Wait()

			Expect(errs[0]).NotTo(HaveOccurred())
			Expect(errs[1]).NotTo(HaveOccurred())
			got, err := os.ReadFile(raw)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(got)).To(Equal(rawContent), "the shared raw must be intact after both conversions")
			Expect(filepath.Join(dir, "kairos-test.raw.gce")).To(BeAnExistingFile())
			Expect(filepath.Join(dir, "kairos-test.raw.vhd")).To(BeAnExistingFile())
		})
	})

	Describe("ConvertRawDiskToMAAS", func() {
		It("produces <raw>.gz and leaves the original raw in place", func() {
			Expect(ConvertRawDiskToMAAS(dir)(context.Background())).To(Succeed())
			Expect(filepath.Join(dir, "kairos-test.raw.gz")).To(BeAnExistingFile())
			got, err := os.ReadFile(raw)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(got)).To(Equal(rawContent))
		})
	})
})
