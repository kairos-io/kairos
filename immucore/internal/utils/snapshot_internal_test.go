package utils

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SnapshotTable", func() {
	It("asks for a persistent store that overflows rather than invalidates", func() {
		Expect(SnapshotTable(4096, "/dev/sda5", "/dev/loop0")).
			To(Equal("0 4096 snapshot /dev/sda5 /dev/loop0 PO 8"))
	})
})

var _ = Describe("CowStoreSize", func() {
	It("leaves a margin below the tmpfs and rounds down to whole chunks", func() {
		tmpfs := uint64(1 << 30)
		size := CowStoreSize(tmpfs)
		Expect(size).To(BeNumerically("<", tmpfs))
		Expect(size).To(BeNumerically(">", tmpfs*9/10))
		Expect(size % 4096).To(BeZero())
	})

	It("is zero for a tmpfs too small to hold a chunk", func() {
		Expect(CowStoreSize(100)).To(BeZero())
	})
})

var _ = Describe("CreateSparseFile", func() {
	It("makes a file of the asked size without writing it", func() {
		path := filepath.Join(GinkgoT().TempDir(), "persistent.cow")
		Expect(CreateSparseFile(path, 64<<20)).To(Succeed())
		info, err := os.Stat(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(info.Size()).To(Equal(int64(64 << 20)))
	})

	It("replaces whatever was there before", func() {
		path := filepath.Join(GinkgoT().TempDir(), "persistent.cow")
		Expect(os.WriteFile(path, []byte("stale"), 0600)).To(Succeed())
		Expect(CreateSparseFile(path, 4096)).To(Succeed())
		info, err := os.Stat(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(info.Size()).To(Equal(int64(4096)))
	})
})

var _ = Describe("TmpfsSizeBytes", func() {
	It("reports the size of the filesystem under a path", func() {
		size, err := TmpfsSizeBytes(GinkgoT().TempDir())
		Expect(err).ToNot(HaveOccurred())
		Expect(size).To(BeNumerically(">", 0))
	})

	It("fails for a path that is not there", func() {
		_, err := TmpfsSizeBytes(filepath.Join(GinkgoT().TempDir(), "missing"))
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("CowSpec", func() {
	It("reads a bare size as a tmpfs of that size", func() {
		Expect(CowSpec("2G")).To(Equal("tmpfs:2G"))
		Expect(CowSpec("25%")).To(Equal("tmpfs:25%"))
	})

	It("passes the long forms through", func() {
		Expect(CowSpec("tmpfs:2G")).To(Equal("tmpfs:2G"))
		Expect(CowSpec("LABEL=KAIROS_COW")).To(Equal("LABEL=KAIROS_COW"))
		Expect(CowSpec("UUID=0a1b2c3d")).To(Equal("UUID=0a1b2c3d"))
	})

	It("leaves an empty value empty, so the fallback still applies", func() {
		Expect(CowSpec("")).To(Equal(""))
	})
})

var _ = Describe("GetCowBase", func() {
	It("takes the cmdline sub-key first", func() {
		setCmdline("root=LABEL=COS_ACTIVE rd.immucore.write_protected.cow=tmpfs:2G")
		Expect(GetCowBase("tmpfs:25%")).To(Equal("tmpfs:2G"))
	})

	It("accepts a bare size on the cmdline", func() {
		setCmdline("root=LABEL=COS_ACTIVE rd.immucore.write_protected.cow=2G")
		Expect(GetCowBase("tmpfs:25%")).To(Equal("tmpfs:2G"))
	})

	It("falls back to the base overlay when that is a tmpfs", func() {
		setCmdline("root=LABEL=COS_ACTIVE")
		Expect(GetCowBase("tmpfs:40%")).To(Equal("tmpfs:40%"))
	})

	It("ignores a device-backed base overlay, which would be the frozen disk", func() {
		setCmdline("root=LABEL=COS_ACTIVE")
		Expect(GetCowBase("LABEL=COS_PERSISTENT")).To(Equal("tmpfs:25%"))
	})
})
