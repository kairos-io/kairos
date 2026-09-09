package utils

import (
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"

	cnst "github.com/kairos-io/kairos/v4/agent/pkg/constants"
	sdkFS "github.com/kairos-io/kairos/v4/sdk/types/fs"
	"github.com/kairos-io/kairos/v4/sdk/utils"
)

// CheckESPRefresh reports whether an ESP refresh can be carried out from
// sourceDir into efiDir, so a refresh that cannot complete is skipped before
// it writes anything rather than part way through, leaving the ESP holding a
// shim that the firmware still lists but that no longer chainloads a working
// grub.efi.
//
// It rejects two cases. First, a source missing either of the binaries
// RefreshESP copies: shim and grub, or grub alone on riscv64, which boots
// grub.efi directly. Writing a new shim next to the old grub, or the other
// way round, is worse than leaving both alone, so a distro that moves one of
// them to a path Kairos does not know skips the refresh. Second, an ESP with
// no room for them.
//
// The rotation is not atomic: for each of shim and grub, an existing file at
// the target is overwritten in place. That means the free-space budget the
// new files have to fit in is the raw Bavail on the ESP plus the bytes the
// files currently occupying the target paths give back on rewrite.
//
// sourceDir must hold the rootfs of the installation source, so the shim and
// grub binaries GetEfiShimFiles/GetEfiGrubFiles list are resolvable. efiDir
// must be the mount point of the ESP.
func CheckESPRefresh(fs sdkFS.KairosFS, arch, sourceDir, efiDir string) error {
	refresh, err := espRefreshSources(fs, arch, sourceDir)
	if err != nil {
		return err
	}
	if refresh.shim == "" && arch != cnst.ArchRiscv64 {
		return fmt.Errorf("no shim found under %s at any known path, leaving the current shim and grub in place", sourceDir)
	}
	if refresh.grub == "" {
		return fmt.Errorf("no grub found under %s at any known path, leaving the current shim and grub in place", sourceDir)
	}

	replaced, err := espCurrentTargetBytes(fs, arch, efiDir, refresh.sources())
	if err != nil {
		return err
	}

	free, err := freeBytesOn(fs, efiDir)
	if err != nil {
		return err
	}

	room := free + replaced
	if refresh.bytes > room {
		return fmt.Errorf("not enough space on the EFI partition %s: refreshing shim and grub needs %d bytes, %d free plus %d reclaimable from the current shim and grub. Free space on the EFI partition or skip the ESP refresh",
			efiDir, refresh.bytes, free, replaced)
	}

	return nil
}

// espRefresh is the shim and grub an ESP refresh would copy out of a source,
// with the total bytes RefreshESP writes for them.
type espRefresh struct {
	shim  string
	grub  string
	bytes int64
}

// sources returns the source paths the refresh reads, so espCurrentTargetBytes
// can size the exact same set of files at the target.
func (e espRefresh) sources() []string {
	var sources []string
	if e.shim != "" {
		sources = append(sources, e.shim)
	}
	if e.grub != "" {
		sources = append(sources, e.grub)
	}
	return sources
}

// espRefreshSources finds the shim and grub RefreshESP would pick under
// sourceDir and sizes what it would write for them.
//
// RefreshESP picks the first matching shim under sourceDir and the first
// matching grub under sourceDir, so the sizing walks in the same order and
// stops at the same file.
func espRefreshSources(fs sdkFS.KairosFS, arch, sourceDir string) (espRefresh, error) {
	var refresh espRefresh

	if arch != cnst.ArchRiscv64 {
		size, src, err := firstPresent(fs, sourceDir, utils.GetEfiShimFiles(arch))
		if err != nil {
			return refresh, err
		}
		if src != "" {
			// Shim is written twice: once under its real name and once as the
			// removable-media fallback BOOT<arch>.EFI.
			refresh.bytes += size * 2
			refresh.shim = src
		}
	}

	size, src, err := firstPresent(fs, sourceDir, utils.GetEfiGrubFiles(arch))
	if err != nil {
		return refresh, err
	}
	if src != "" {
		refresh.bytes += size
		if arch == cnst.ArchRiscv64 {
			// On riscv64 with no shim, copyGrub also writes grub as the
			// removable-media fallback.
			refresh.bytes += size
		}
		refresh.grub = src
	}

	return refresh, nil
}

// espCurrentTargetBytes sums the sizes of the files currently at the target
// paths that RefreshESP would overwrite. Missing files count as zero, since
// a first-ever refresh has nothing to reclaim.
func espCurrentTargetBytes(fs sdkFS.KairosFS, arch, efiDir string, sources []string) (int64, error) {
	var total int64

	for _, src := range sources {
		_, name := filepath.Split(src)
		name = strings.TrimSuffix(name, ".signed")
		target := filepath.Join(efiDir, "EFI/boot", name)
		size, err := sizeOrZero(fs, target)
		if err != nil {
			return 0, err
		}
		total += size
	}

	// Both shim (always) and grub (riscv64 only) also write to the fallback
	// BOOT<arch>.EFI path. sizeOrZero returns zero on a missing file, so
	// counting the fallback here is safe even on a first-ever refresh.
	fallback := filepath.Join(efiDir, "EFI/boot", cnst.GetFallBackEfi(arch))
	fallbackSize, err := sizeOrZero(fs, fallback)
	if err != nil {
		return 0, err
	}
	total += fallbackSize

	return total, nil
}

// firstPresent stats each candidate under sourceDir and returns the size and
// path of the first one that exists, matching what copyShim/copyGrub select.
func firstPresent(fs sdkFS.KairosFS, sourceDir string, candidates []string) (int64, string, error) {
	for _, f := range candidates {
		src := filepath.Join(sourceDir, f)
		st, err := fs.Stat(src)
		if err != nil {
			continue
		}
		return st.Size(), src, nil
	}
	return 0, "", nil
}

func sizeOrZero(fs sdkFS.KairosFS, path string) (int64, error) {
	st, err := fs.Stat(path)
	if err != nil {
		// The vfs used in tests can return either os.ErrNotExist or a
		// wrapper, so match on the string a real stat also produces.
		if strings.Contains(err.Error(), "no such file or directory") {
			return 0, nil
		}
		return 0, fmt.Errorf("reading size of %s: %w", path, err)
	}
	return st.Size(), nil
}

// freeBytesOn returns the bytes still writable on the filesystem that holds
// path. It reports Bavail rather than Bfree, so it does not count blocks that
// only root may use.
func freeBytesOn(fs sdkFS.KairosFS, path string) (int64, error) {
	realPath, err := fs.RawPath(path)
	if err != nil {
		return 0, fmt.Errorf("resolving %s: %w", path, err)
	}

	var stat unix.Statfs_t
	if err := unix.Statfs(realPath, &stat); err != nil {
		return 0, fmt.Errorf("reading free space on %s: %w", path, err)
	}

	return int64(stat.Bavail) * int64(stat.Bsize), nil
}
