package dag

import (
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/sdk/constants"
	"github.com/kairos-io/kairos/v4/sdk/ghw/mocks"
	"github.com/kairos-io/kairos/v4/sdk/types/partitions"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("oemEncrypted", func() {
	// oemEncrypted decides whether the encrypt-pending step is registered at
	// all on the normal boot DAG, so these specs lock its answer across the
	// install shapes it has to recognise.

	// mockDisk stages a single-disk ghw view and cleans it up with the spec.
	mockDisk := func(parts partitions.PartitionList) {
		var ghwMock mocks.GhwMock
		ghwMock.AddDisk(partitions.Disk{Name: "vda", Partitions: parts})
		ghwMock.CreateDevices()
		DeferCleanup(ghwMock.Clean)
	}

	// mockCmdline points the cmdline readers at a file of our making, so
	// GetOemLabel does not read the test host's /proc/cmdline.
	mockCmdline := func(content string) {
		cmdline := filepath.Join(GinkgoT().TempDir(), "cmdline")
		Expect(os.WriteFile(cmdline, []byte(content), 0o600)).To(Succeed())
		old, had := os.LookupEnv("HOST_PROC_CMDLINE")
		Expect(os.Setenv("HOST_PROC_CMDLINE", cmdline)).To(Succeed())
		DeferCleanup(func() {
			if had {
				Expect(os.Setenv("HOST_PROC_CMDLINE", old)).To(Succeed())
				return
			}
			Expect(os.Unsetenv("HOST_PROC_CMDLINE")).To(Succeed())
		})
	}

	BeforeEach(func() {
		mockCmdline("root=LABEL=COS_ACTIVE\n")
	})

	It("is false when there is no OEM partition at all", func() {
		mockDisk(partitions.PartitionList{
			{Name: "vda1", PartitionLabel: "efi", FilesystemLabel: constants.EfiLabel, FS: "vfat"},
		})
		Expect(oemEncrypted()).To(BeFalse())
	})

	It("is false on a plaintext OEM partition", func() {
		mockDisk(partitions.PartitionList{
			{Name: "vda3", PartitionLabel: "oem", FilesystemLabel: constants.OEMLabel, FS: "ext4"},
		})
		Expect(oemEncrypted()).To(BeFalse())
	})

	It("is true on a post kairos-io/kairos#4403 install (outer label on the container)", func() {
		mockDisk(partitions.PartitionList{
			{Name: "vda3", PartitionLabel: "oem", FilesystemLabel: constants.OEMLUKSLabel, FS: constants.LUKSFs},
		})
		Expect(oemEncrypted()).To(BeTrue())
	})

	It("is true on a pre kairos-io/kairos#4403 install (plain label on the container)", func() {
		mockDisk(partitions.PartitionList{
			{Name: "vda3", PartitionLabel: "oem", FilesystemLabel: constants.OEMLabel, FS: constants.LUKSFs},
		})
		Expect(oemEncrypted()).To(BeTrue())
	})

	It("honors a cmdline OEM label rename", func() {
		mockCmdline("root=LABEL=COS_ACTIVE rd.immucore.oemlabel=MY_OEM\n")
		mockDisk(partitions.PartitionList{
			{Name: "vda3", PartitionLabel: "oem", FilesystemLabel: "MY_OEM", FS: constants.LUKSFs},
		})
		Expect(oemEncrypted()).To(BeTrue())
	})
})
