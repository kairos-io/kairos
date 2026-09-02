package utils

import (
	"fmt"
	"os"
	"path/filepath"
)

// systemdUnitDir is the drop-in directory that survives an image change,
// because /etc/systemd is one of the persistent state binds.
const systemdUnitDir = "etc/systemd/system"

// packagedUnitDirs are the unit load paths an OS image can ship a unit in.
// /etc is deliberately absent: it is the directory being cleaned.
var packagedUnitDirs = []string{
	"usr/local/lib/systemd/system",
	"usr/lib/systemd/system",
	"lib/systemd/system",
}

// CleanStaleUnitSymlinks removes unit symlinks in <root>/etc/systemd/system
// that dangle while the image ships a real unit under the same name, and
// returns the names it removed.
//
// /etc/systemd is a persistent state bind and the state directory is synced
// from the image with rsync and no --delete, so the sync only ever adds. A
// unit symlink written by an earlier image therefore outlives it. When the
// new image names that unit differently the symlink dangles, and because
// /etc wins over /usr in systemd's unit load path it shadows the packaged
// unit into LoadState=not-found: enabling Ubuntu's ssh.service leaves
// /etc/systemd/system/sshd.service -> /usr/lib/systemd/system/ssh.service
// behind (Alias=sshd.service), which kills sshd once the node moves to an
// image whose real unit is sshd.service (kairos-io/kairos#4085).
//
// The rule is deliberately narrow, so that removing a symlink can only ever
// reveal a unit that works:
//
//   - only symlinks directly in etc/systemd/system, never the enablement
//     symlinks under .wants/ and .requires/. Those shadow nothing, and a unit
//     that a sysext provides is not merged yet at this point in the boot, so
//     its enablement symlink legitimately dangles here.
//   - only symlinks whose target is missing.
//   - never a mask, whose target is /dev/null and which the sysroot has no
//     device node for yet.
//   - only when a packaged unit of the same name is there to take over.
func CleanStaleUnitSymlinks(root string) ([]string, error) {
	unitDir := filepath.Join(root, systemdUnitDir)
	entries, err := os.ReadDir(unitDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", unitDir, err)
	}

	var removed []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.Type()&os.ModeSymlink == 0 {
			continue
		}
		path := filepath.Join(unitDir, name)
		target, err := os.Readlink(path)
		if err != nil {
			continue
		}
		if isMask(target) {
			continue
		}
		if _, err := os.Stat(resolveUnitTarget(root, unitDir, target)); err == nil {
			continue
		}
		if !packagedUnitExists(root, name) {
			continue
		}
		if err := os.Remove(path); err != nil {
			return removed, fmt.Errorf("removing stale unit symlink %s: %w", path, err)
		}
		removed = append(removed, name)
	}
	// os.ReadDir sorts by name, so removed comes out deterministic.
	return removed, nil
}

// isMask reports whether the symlink target is a systemd mask. A mask points
// at /dev/null, which the sysroot has no device node for while immucore runs,
// so it would otherwise look like any other broken link.
func isMask(target string) bool {
	return filepath.Clean(target) == "/dev/null"
}

// resolveUnitTarget maps a symlink target to a path in the running initramfs.
// An absolute target is relative to the sysroot, not to /.
func resolveUnitTarget(root, unitDir, target string) string {
	if filepath.IsAbs(target) {
		return filepath.Join(root, target)
	}
	return filepath.Join(unitDir, target)
}

// packagedUnitExists reports whether the image ships a resolvable unit called
// name outside of /etc.
func packagedUnitExists(root, name string) bool {
	for _, dir := range packagedUnitDirs {
		if _, err := os.Stat(filepath.Join(root, dir, name)); err == nil {
			return true
		}
	}
	return false
}
