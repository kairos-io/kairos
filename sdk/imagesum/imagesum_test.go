package imagesum_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kairos-io/kairos/v4/sdk/imagesum"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

// pollCtx is live for `left` calls to Err, then done. Hashing polls it once per
// read, so it stops hashing at a known point.
type pollCtx struct {
	context.Context
	left int
}

func (c *pollCtx) Err() error {
	if c.left > 0 {
		c.left--
		return nil
	}
	return context.Canceled
}

var _ = Describe("sidecar", func() {
	var fs vfs.FS
	var cleanup func()
	const img = "/state/cOS/active.img"
	content := []byte(strings.Repeat("kairos", 1000))
	digest := func(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

	BeforeEach(func() {
		var err error
		fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{img: content})
		Expect(err).ToNot(HaveOccurred())
	})
	AfterEach(func() { cleanup() })

	// corrupt changes the content the way bit rot does: mtime stays
	corrupt := func(b []byte) {
		info, err := fs.Stat(img)
		Expect(err).ToNot(HaveOccurred())
		Expect(fs.WriteFile(img, b, 0o644)).To(Succeed())
		raw, err := fs.RawPath(img)
		Expect(err).ToNot(HaveOccurred())
		Expect(os.Chtimes(raw, info.ModTime(), info.ModTime())).To(Succeed())
	}

	It("computes the sha256 of a file", func() {
		sum, err := imagesum.Compute(fs, img)
		Expect(err).ToNot(HaveOccurred())
		Expect(sum).To(Equal(digest(content)))
	})

	It("writes a sha256sum compatible sidecar with size and mtime, atomically", func() {
		sum, err := imagesum.Generate(fs, img)
		Expect(err).ToNot(HaveOccurred())
		data, err := fs.ReadFile(img + ".sha256")
		Expect(err).ToNot(HaveOccurred())
		info, err := fs.Stat(img)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(data)).To(Equal(fmt.Sprintf("%s  active.img\n# size=%d mtime_ns=%d\n", sum, info.Size(), info.ModTime().UnixNano())))
		_, err = fs.Stat(img + ".sha256.tmp")
		Expect(err).To(HaveOccurred())
	})

	Describe("time budget", func() {
		BeforeEach(func() {
			_, err := imagesum.Generate(fs, img)
			Expect(err).ToNot(HaveOccurred())
		})

		It("reports TimedOut, not a mismatch or an error, when the budget is already spent", func() {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			r, err := imagesum.CheckContext(ctx, fs, img)
			Expect(err).ToNot(HaveOccurred())
			Expect(r.Result).To(Equal(imagesum.TimedOut))
			Expect(r.Actual).To(BeEmpty())
			Expect(r.Expected).To(Equal(digest(content)))
		})
		It("stops between reads and says how far it got", func() {
			// an image of three buffers, stopped after the first read
			big := make([]byte, 3<<20)
			Expect(fs.WriteFile(img, big, 0o644)).To(Succeed())
			_, err := imagesum.Generate(fs, img)
			Expect(err).ToNot(HaveOccurred())
			r, err := imagesum.CheckContext(&pollCtx{Context: context.Background(), left: 1}, fs, img)
			Expect(err).ToNot(HaveOccurred())
			Expect(r.Result).To(Equal(imagesum.TimedOut))
			Expect(r.BytesHashed).To(Equal(int64(1 << 20)))
		})
		It("is unchanged when the image is hashed inside the budget", func() {
			r, err := imagesum.CheckWithin(fs, img, time.Minute)
			Expect(err).ToNot(HaveOccurred())
			Expect(r.Result).To(Equal(imagesum.OK))
		})
		It("still reports corruption inside the budget", func() {
			rotten := append([]byte{}, content...)
			rotten[10] ^= 1
			corrupt(rotten)
			r, err := imagesum.CheckWithin(fs, img, time.Minute)
			Expect(err).ToNot(HaveOccurred())
			Expect(r.Result).To(Equal(imagesum.Mismatch))
		})
		It("treats a budget of 0 as no limit", func() {
			r, err := imagesum.CheckWithin(fs, img, 0)
			Expect(err).ToNot(HaveOccurred())
			Expect(r.Result).To(Equal(imagesum.OK))
		})
		It("does not time out a missing checksum", func() {
			Expect(imagesum.Remove(fs, img)).To(Succeed())
			r, err := imagesum.CheckWithin(fs, img, time.Nanosecond)
			Expect(err).ToNot(HaveOccurred())
			Expect(r.Result).To(Equal(imagesum.Missing))
		})
	})

	Describe("Check", func() {
		It("reports a missing sidecar without reading the image", func() {
			Expect(fs.Remove(img)).To(Succeed())
			r, err := imagesum.Check(fs, img)
			Expect(err).ToNot(HaveOccurred())
			Expect(r.Result).To(Equal(imagesum.Missing))
		})
		It("accepts a good image and returns both digests", func() {
			_, err := imagesum.Generate(fs, img)
			Expect(err).ToNot(HaveOccurred())
			r, err := imagesum.Check(fs, img)
			Expect(err).ToNot(HaveOccurred())
			Expect(r.Result).To(Equal(imagesum.OK))
			Expect(r.Expected).To(Equal(digest(content)))
			Expect(r.Actual).To(Equal(r.Expected))
		})
		It("detects bit rot as a mismatch with both digests", func() {
			_, err := imagesum.Generate(fs, img)
			Expect(err).ToNot(HaveOccurred())
			rotten := append([]byte{}, content...)
			rotten[len(rotten)/2] ^= 1
			corrupt(rotten)
			r, err := imagesum.Check(fs, img)
			Expect(err).ToNot(HaveOccurred())
			Expect(r.Result).To(Equal(imagesum.Mismatch))
			Expect(r.Expected).To(Equal(digest(content)))
			Expect(r.Actual).To(Equal(digest(rotten)))
		})
		It("calls a replaced image stale and does not hash it", func() {
			_, err := imagesum.Generate(fs, img)
			Expect(err).ToNot(HaveOccurred())
			Expect(fs.WriteFile(img, []byte("a new image written by an older agent"), 0o644)).To(Succeed())
			raw, _ := fs.RawPath(img)
			future := time.Now().Add(time.Hour)
			Expect(os.Chtimes(raw, future, future)).To(Succeed())
			r, err := imagesum.Check(fs, img)
			Expect(err).ToNot(HaveOccurred())
			Expect(r.Result).To(Equal(imagesum.Stale))
			Expect(r.Actual).To(BeEmpty())
		})
		It("calls a truncated image stale (size changed)", func() {
			_, err := imagesum.Generate(fs, img)
			Expect(err).ToNot(HaveOccurred())
			corrupt(content[:10])
			r, err := imagesum.Check(fs, img)
			Expect(err).ToNot(HaveOccurred())
			Expect(r.Result).To(Equal(imagesum.Stale))
		})
		It("still compares a plain sha256sum file without the size/mtime line", func() {
			Expect(fs.WriteFile(img+".sha256", []byte(digest(content)+"  active.img\n"), 0o644)).To(Succeed())
			r, err := imagesum.Check(fs, img)
			Expect(err).ToNot(HaveOccurred())
			Expect(r.Result).To(Equal(imagesum.OK))
			Expect(fs.WriteFile(img+".sha256", []byte(strings.Repeat("ab", 32)+"  active.img\n"), 0o644)).To(Succeed())
			r, err = imagesum.Check(fs, img)
			Expect(err).ToNot(HaveOccurred())
			Expect(r.Result).To(Equal(imagesum.Mismatch))
		})
		It("accepts an upper case digest", func() {
			Expect(fs.WriteFile(img+".sha256", []byte(strings.ToUpper(digest(content))+"  active.img\n"), 0o644)).To(Succeed())
			r, err := imagesum.Check(fs, img)
			Expect(err).ToNot(HaveOccurred())
			Expect(r.Result).To(Equal(imagesum.OK))
		})
		DescribeTable("errors on a malformed sidecar",
			func(body string) {
				Expect(fs.WriteFile(img+".sha256", []byte(body), 0o644)).To(Succeed())
				_, err := imagesum.Check(fs, img)
				Expect(err).To(MatchError(ContainSubstring("malformed checksum file")))
			},
			Entry("garbage", "garbage\n"),
			Entry("empty", ""),
			Entry("comment only", "# size=1 mtime_ns=2\n"),
			Entry("non hex of the right length", strings.Repeat("zz", 32)+"  active.img\n"),
		)
		It("errors when the image cannot be read but has a sidecar", func() {
			_, err := imagesum.Generate(fs, img)
			Expect(err).ToNot(HaveOccurred())
			Expect(fs.Remove(img)).To(Succeed())
			_, err = imagesum.Check(fs, img)
			Expect(err).To(HaveOccurred())
		})
	})

	It("removes a sidecar and tolerates a missing one", func() {
		_, err := imagesum.Generate(fs, img)
		Expect(err).ToNot(HaveOccurred())
		Expect(imagesum.Remove(fs, img)).To(Succeed())
		Expect(imagesum.Remove(fs, img)).To(Succeed())
		_, found, _ := imagesum.Read(fs, img)
		Expect(found).To(BeFalse())
	})

	It("only applies to .img files", func() {
		Expect(imagesum.Applies("/x/active.img")).To(BeTrue())
		Expect(imagesum.Applies("/x/recovery.squashfs")).To(BeFalse())
	})
})
