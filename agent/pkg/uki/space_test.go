package uki

import (
	"bytes"

	cnst "github.com/kairos-io/kairos/v4/agent/pkg/constants"
	fsutils "github.com/kairos-io/kairos/v4/agent/pkg/utils/fs"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

// hugeFile is larger than any filesystem a test can run on, so a check that
// asks for it is guaranteed to come back short. The files are sparse, so
// writing one costs nothing.
const hugeFile int64 = 512 << 40 // 512TiB

var _ = Describe("EFI partition space checks", func() {
	var fs vfs.FS
	var err error
	var logger sdkLogger.KairosLogger

	// write creates a sparse file of the given size under /efi
	write := func(name string, size int64) {
		f, err := fs.Create("/efi/" + name)
		Expect(err).ToNot(HaveOccurred())
		Expect(f.Close()).ToNot(HaveOccurred())
		Expect(fs.Truncate("/efi/"+name, size)).ToNot(HaveOccurred())
	}

	BeforeEach(func() {
		fs, _, err = vfst.NewTestFS(map[string]interface{}{})
		Expect(err).ToNot(HaveOccurred())
		logger = sdkLogger.NewBufferLogger(&bytes.Buffer{})
		logger.SetLevel("debug")
		Expect(fsutils.MkdirAll(fs, "/efi/EFI/kairos", cnst.DirPerm)).ToNot(HaveOccurred())
		Expect(fsutils.MkdirAll(fs, "/efi/loader/entries", cnst.DirPerm)).ToNot(HaveOccurred())
	})

	Describe("artifactSetSize", func() {
		It("adds up only the files belonging to the role", func() {
			write("EFI/kairos/norole.efi", 300)
			write("loader/entries/norole.conf", 45)
			write("EFI/kairos/active.efi", 900)

			Expect(artifactSetSize(fs, "/efi", UnassignedArtifactRole)).To(Equal(int64(345)))
			Expect(artifactSetSize(fs, "/efi", "active")).To(Equal(int64(900)))
		})

		It("counts files renamed by the boot assessment", func() {
			write("EFI/kairos/active+3.efi", 100)
			write("loader/entries/active+3.conf", 20)

			Expect(artifactSetSize(fs, "/efi", "active")).To(Equal(int64(120)))
		})

		It("reports zero for a role that is not installed", func() {
			write("EFI/kairos/active.efi", 100)

			Expect(artifactSetSize(fs, "/efi", "passive")).To(Equal(int64(0)))
		})
	})

	Describe("checkSpaceForInstall", func() {
		// active, passive, recovery and statereset
		const roles = 4

		It("passes when the copies fit", func() {
			write("EFI/kairos/norole.efi", 1024)
			write("loader/entries/norole.conf", 128)

			Expect(checkSpaceForInstall(fs, "/efi", roles, logger)).ToNot(HaveOccurred())
		})

		It("fails before copying when the copies do not fit", func() {
			write("EFI/kairos/norole.efi", hugeFile)

			err := checkSpaceForInstall(fs, "/efi", roles, logger)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not enough space on the EFI partition"))
			Expect(err.Error()).To(ContainSubstring("4 copies"))
		})

		It("charges for a copy per role, not for a single copy", func() {
			free, err := freeSpaceOn(fs, "/efi")
			Expect(err).ToNot(HaveOccurred())

			// Half the free space: one copy fits, four cannot.
			write("EFI/kairos/norole.efi", free/2)

			Expect(checkSpaceForInstall(fs, "/efi", 1, logger)).ToNot(HaveOccurred())
			Expect(checkSpaceForInstall(fs, "/efi", roles, logger)).To(HaveOccurred())
		})
	})

	Describe("checkSpaceForUpgradeRotation", func() {
		It("passes when both copies fit", func() {
			write("EFI/kairos/norole.efi", 1024)
			write("EFI/kairos/active.efi", 1024)
			write("EFI/kairos/passive.efi", 1024)

			Expect(checkSpaceForUpgradeRotation(fs, "/efi", logger)).ToNot(HaveOccurred())
		})

		It("fails before the rotation deletes anything when the new set does not fit", func() {
			write("EFI/kairos/norole.efi", hugeFile)
			write("EFI/kairos/active.efi", 1024)
			write("EFI/kairos/passive.efi", 1024)

			err := checkSpaceForUpgradeRotation(fs, "/efi", logger)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not enough space on the EFI partition"))
			Expect(err.Error()).To(ContainSubstring("rotating"))
		})

		It("fails when copying the current active over passive would not fit", func() {
			write("EFI/kairos/norole.efi", 1024)
			write("EFI/kairos/active.efi", hugeFile)
			write("EFI/kairos/passive.efi", 1024)

			Expect(checkSpaceForUpgradeRotation(fs, "/efi", logger)).To(HaveOccurred())
		})

		It("counts the passive set it is about to free", func() {
			// Nothing here fits in the free space on its own, but dropping the
			// equally large passive set pays for both copies.
			write("EFI/kairos/norole.efi", hugeFile)
			write("EFI/kairos/active.efi", hugeFile)
			write("EFI/kairos/passive.efi", hugeFile)

			Expect(checkSpaceForUpgradeRotation(fs, "/efi", logger)).ToNot(HaveOccurred())
		})

		It("has nothing to free on a machine with no passive set yet", func() {
			write("EFI/kairos/norole.efi", hugeFile)
			write("EFI/kairos/active.efi", hugeFile)

			Expect(checkSpaceForUpgradeRotation(fs, "/efi", logger)).To(HaveOccurred())
		})
	})
})
