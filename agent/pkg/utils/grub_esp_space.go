/*
Copyright © 2026 SUSE LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

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

// CheckESPRefreshSpace fails before RefreshESP starts writing rather than
// letting a copy run out of room part way and leave the ESP holding a shim
// that the firmware still lists but that no longer chainloads a working
// grub.efi.
//
// The rotation is not atomic: for each of shim and grub, an existing file at
// the target is overwritten in place. That means the free-space budget the
// new files have to fit in is the raw Bavail on the ESP plus the bytes the
// files currently occupying the target paths give back on rewrite.
//
// sourceDir must hold the rootfs of the installation source, so the shim and
// grub binaries GetEfiShimFiles/GetEfiGrubFiles list are resolvable. efiDir
// must be the mount point of the ESP.
func CheckESPRefreshSpace(fs sdkFS.KairosFS, arch, sourceDir, efiDir string) error {
	needed, sources, err := espRefreshBytes(fs, arch, sourceDir)
	if err != nil {
		return err
	}
	if needed == 0 {
		// Nothing to write: no shim/grub found under sourceDir. RefreshESP
		// itself will surface this as an error, do not shadow it here.
		return nil
	}

	replaced, err := espCurrentTargetBytes(fs, arch, efiDir, sources)
	if err != nil {
		return err
	}

	free, err := freeBytesOn(fs, efiDir)
	if err != nil {
		return err
	}

	room := free + replaced
	if needed > room {
		return fmt.Errorf("not enough space on the EFI partition %s: refreshing shim and grub needs %d bytes, %d free plus %d reclaimable from the current shim and grub. Free space on the EFI partition or skip the ESP refresh",
			efiDir, needed, free, replaced)
	}

	return nil
}

// espRefreshBytes returns the total bytes RefreshESP will write, and the
// source paths those bytes came from so espCurrentTargetBytes can size the
// exact same set of files at the target.
//
// RefreshESP picks the first matching shim under sourceDir and the first
// matching grub under sourceDir, so the sizing walks in the same order and
// stops at the same file.
func espRefreshBytes(fs sdkFS.KairosFS, arch, sourceDir string) (int64, []string, error) {
	var sources []string
	var total int64

	if arch != cnst.ArchRiscv64 {
		size, src, err := firstPresent(fs, sourceDir, utils.GetEfiShimFiles(arch))
		if err != nil {
			return 0, nil, err
		}
		if src != "" {
			// Shim is written twice: once under its real name and once as the
			// removable-media fallback BOOT<arch>.EFI.
			total += size * 2
			sources = append(sources, src)
		}
	}

	size, src, err := firstPresent(fs, sourceDir, utils.GetEfiGrubFiles(arch))
	if err != nil {
		return 0, nil, err
	}
	if src != "" {
		total += size
		if arch == cnst.ArchRiscv64 {
			// On riscv64 with no shim, copyGrub also writes grub as the
			// removable-media fallback.
			total += size
		}
		sources = append(sources, src)
	}

	return total, sources, nil
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
