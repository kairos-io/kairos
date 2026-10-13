package uki

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	cnst "github.com/kairos-io/kairos/v4/agent/pkg/constants"
	fsutils "github.com/kairos-io/kairos/v4/agent/pkg/utils/fs"
	sdkFs "github.com/kairos-io/kairos/v4/sdk/types/fs"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
	"golang.org/x/sys/unix"
)

// sparseMargin is how far past the capacity tooBigFor goes. It has to clear
// the small fixture files that a check counts as room it will get back (the
// passive set an upgrade rotation frees), and 1MiB is far past those.
const sparseMargin int64 = 1 << 20

// capacityOf returns the total size of the filesystem that holds path,
// including the blocks already in use.
func capacityOf(fs sdkFs.KairosFS, path string) (int64, error) {
	realPath, err := fs.RawPath(path)
	if err != nil {
		return 0, fmt.Errorf("resolving %s: %w", path, err)
	}

	var stat unix.Statfs_t
	if err := unix.Statfs(realPath, &stat); err != nil {
		return 0, fmt.Errorf("reading the size of %s: %w", path, err)
	}

	return int64(stat.Blocks) * int64(stat.Bsize), nil
}

// tooBigFor returns a file size that cannot fit on the filesystem behind path,
// so a check that has to make a copy of a file this size is guaranteed to come
// back short.
//
// The size is measured rather than hardcoded. A constant picked to be larger
// than any filesystem is refused outright by a filesystem with a smaller
// per-file limit: ext4 caps a single file at 16TiB and returns EFBIG, so
// 512TiB truncates fine on a tmpfs /tmp and fails on an ext4 one.
//
// It measures the whole filesystem rather than the free space on it. The tests
// share /tmp with every other package that `go test ./...` runs beside them,
// and a package that deletes its scratch files hands those blocks back, so the
// free space a check reads can be tens of MiB above the free space measured
// here moments earlier. Free space can grow, but it can never grow past the
// capacity, so a file larger than the whole filesystem stays too big whatever
// the neighbours do.
//
// The file stays sparse at this size, so it costs no blocks and does not move
// the free space the checks read.
func tooBigFor(fs vfs.FS, path string) int64 {
	capacity, err := capacityOf(fs, path)
	ExpectWithOffset(1, err).ToNot(HaveOccurred())

	return capacity + sparseMargin
}

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
			write("EFI/kairos/norole.efi", tooBigFor(fs, "/efi"))

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
			write("EFI/kairos/norole.efi", tooBigFor(fs, "/efi"))
			write("EFI/kairos/active.efi", 1024)
			write("EFI/kairos/passive.efi", 1024)

			err := checkSpaceForUpgradeRotation(fs, "/efi", logger)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not enough space on the EFI partition"))
			Expect(err.Error()).To(ContainSubstring("rotating"))
		})

		It("fails when copying the current active over passive would not fit", func() {
			write("EFI/kairos/norole.efi", 1024)
			write("EFI/kairos/active.efi", tooBigFor(fs, "/efi"))
			write("EFI/kairos/passive.efi", 1024)

			Expect(checkSpaceForUpgradeRotation(fs, "/efi", logger)).To(HaveOccurred())
		})

		It("counts the passive set it is about to free", func() {
			// Nothing here fits in the free space on its own, but dropping the
			// equally large passive set pays for both copies.
			write("EFI/kairos/norole.efi", tooBigFor(fs, "/efi"))
			write("EFI/kairos/active.efi", tooBigFor(fs, "/efi"))
			write("EFI/kairos/passive.efi", tooBigFor(fs, "/efi"))

			Expect(checkSpaceForUpgradeRotation(fs, "/efi", logger)).ToNot(HaveOccurred())
		})

		It("has nothing to free on a machine with no passive set yet", func() {
			write("EFI/kairos/norole.efi", tooBigFor(fs, "/efi"))
			write("EFI/kairos/active.efi", tooBigFor(fs, "/efi"))

			Expect(checkSpaceForUpgradeRotation(fs, "/efi", logger)).To(HaveOccurred())
		})

		It("still refuses when a neighbour hands free space back mid-test", func() {
			// The test filesystem lives in the /tmp that every other package
			// `go test ./...` runs shares, so one of them deleting its scratch
			// files grows the free space this check reads. Hold some blocks,
			// size the active set, then release them, which is that race made
			// to happen on purpose.
			ballast := filepath.Join(os.TempDir(), "uki-space-ballast")
			Expect(os.WriteFile(ballast, make([]byte, 32<<20), 0600)).To(Succeed())
			defer os.Remove(ballast)
			unix.Sync()

			write("EFI/kairos/norole.efi", 1)
			write("EFI/kairos/active.efi", tooBigFor(fs, "/efi"))
			write("EFI/kairos/passive.efi", 1)

			Expect(os.Remove(ballast)).To(Succeed())
			unix.Sync()

			Expect(checkSpaceForUpgradeRotation(fs, "/efi", logger)).To(HaveOccurred())
		})
	})
})
