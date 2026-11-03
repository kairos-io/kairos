package blockdev

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ParseSnapshotStatus", func() {
	It("reads the fill level while the store has room", func() {
		st, err := ParseSnapshotStatus("0 4194304 snapshot 4096/1048576 16\n")
		Expect(err).ToNot(HaveOccurred())
		Expect(st).To(Equal(SnapshotStatus{UsedSectors: 4096, TotalSectors: 1048576, MetadataSectors: 16, State: "active"}))
		Expect(st.UsedPercent()).To(Equal(0))
	})

	It("accepts the status part on its own", func() {
		st, err := ParseSnapshotStatus("524288/1048576 16")
		Expect(err).ToNot(HaveOccurred())
		Expect(st.UsedPercent()).To(Equal(50))
	})

	It("reports a full persistent store as overflow", func() {
		st, err := ParseSnapshotStatus("0 4194304 snapshot Overflow")
		Expect(err).ToNot(HaveOccurred())
		Expect(st.State).To(Equal("overflow"))
		Expect(st.UsedPercent()).To(Equal(0))
	})

	It("reports an invalidated store", func() {
		st, err := ParseSnapshotStatus("0 4194304 snapshot Invalid")
		Expect(err).ToNot(HaveOccurred())
		Expect(st.State).To(Equal("invalid"))
	})

	It("rejects a line for some other target", func() {
		_, err := ParseSnapshotStatus("0 4194304 linear 8:5 0")
		Expect(err).To(HaveOccurred())
		_, err = ParseSnapshotStatus("")
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("SizeSectors", func() {
	It("reads the size from sysfs by major:minor", func() {
		root := GinkgoT().TempDir()
		previousSys, previousRdev := sysDevBlock, rdevOf
		sysDevBlock = root
		rdevOf = func(device string) (string, error) {
			Expect(device).To(Equal("/dev/sda5"))
			return "8:5", nil
		}
		DeferCleanup(func() { sysDevBlock, rdevOf = previousSys, previousRdev })

		Expect(os.MkdirAll(filepath.Join(root, "8:5"), 0755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(root, "8:5", "size"), []byte("4194304\n"), 0644)).To(Succeed())

		Expect(SizeSectors("/dev/sda5")).To(Equal(uint64(4194304)))
	})
})
