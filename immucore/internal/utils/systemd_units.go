package utils

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/hashicorp/go-multierror"
)

// systemdUnitDir is the drop-in directory that survives an image change,
// because /etc/systemd is one of the persistent state binds.
const systemdUnitDir = "etc/systemd/system"

// staleUnitDir is where a shadowing symlink is parked. It sits next to
// systemdUnitDir so the move is a rename on one filesystem, it is inside the
// same persistent bind so the parked symlink is still there after the boot,
// and it is in none of systemd's unit load paths so nothing loads from it.
const staleUnitDir = "etc/systemd/kairos-stale-units"

// maxStaleUnitCollisions bounds the numbered suffixes tried when a symlink of
// the same name, but a different target, is already parked.
const maxStaleUnitCollisions = 20

// packagedUnitDirs are the unit load paths an OS image can ship a unit in.
// /etc is deliberately absent: it is the directory being swept.
var packagedUnitDirs = []string{
	"usr/local/lib/systemd/system",
	"usr/lib/systemd/system",
	"lib/systemd/system",
}

// QuarantineStaleUnitSymlinks moves unit symlinks in <root>/etc/systemd/system
// that dangle while the image ships a real unit under the same name into
// <root>/etc/systemd/kairos-stale-units, and returns the names it moved.
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
// Nothing is deleted. Getting the symlink out of the unit load path is all
// that is needed to stop it shadowing, so the symlink is parked instead,
// under its own name, where an admin can read what was moved and put it back
// with a single mv. A rule that decides wrongly then costs a rename, not a
// file.
//
// The rule is still deliberately narrow, so that a move can only ever reveal
// a unit that works:
//
//   - only symlinks directly in etc/systemd/system, never the enablement
//     symlinks under .wants/ and .requires/. Those shadow nothing, and a unit
//     that a sysext provides is not merged yet at this point in the boot, so
//     its enablement symlink legitimately dangles here.
//   - only symlinks whose target is missing.
//   - never a mask, whose target is /dev/null and which the sysroot has no
//     device node for yet.
//   - only when a packaged unit of the same name is there to take over.
func QuarantineStaleUnitSymlinks(root string) ([]string, error) {
	unitDir := filepath.Join(root, systemdUnitDir)
	entries, err := os.ReadDir(unitDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", unitDir, err)
	}

	var moved []string
	var errs *multierror.Error
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
		if isMask(root, unitDir, target) {
			continue
		}
		// Only a genuine ENOENT means the target is gone. Any other stat
		// error (EACCES on a path component, ELOOP, ENOTDIR) tells us
		// nothing about the target, and acting on a guess would be worse
		// than leaving the symlink alone.
		if _, err := os.Stat(resolveUnitTarget(root, unitDir, target)); !os.IsNotExist(err) {
			continue
		}
		if !packagedUnitExists(root, name) {
			continue
		}
		if err := parkStaleUnit(root, path, name, target); err != nil {
			// Keep sweeping: one symlink we cannot move must not hide
			// every stale symlink after it.
			errs = multierror.Append(errs, err)
			continue
		}
		moved = append(moved, name)
	}
	// os.ReadDir sorts by name, so moved comes out deterministic.
	return moved, errs.ErrorOrNil()
}

// parkStaleUnit renames one shadowing symlink out of the unit load path.
//
// An entry of the same name already parked by an earlier boot is only
// replaced when it points at the same target, because then it carries no
// information the incoming one does not. A different target gets a numbered
// suffix, so the older evidence survives.
func parkStaleUnit(root, path, name, target string) error {
	dir := filepath.Join(root, staleUnitDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	dest, err := staleUnitDest(dir, name, target)
	if err != nil {
		return fmt.Errorf("parking stale unit symlink %s: %w", path, err)
	}
	if err := os.Rename(path, dest); err != nil {
		return fmt.Errorf("parking stale unit symlink %s: %w", path, err)
	}
	return nil
}

// staleUnitDest picks the name to park a symlink under inside dir.
func staleUnitDest(dir, name, target string) (string, error) {
	dest := filepath.Join(dir, name)
	for i := 1; ; i++ {
		existing, err := os.Readlink(dest)
		if os.IsNotExist(err) || (err == nil && existing == target) {
			return dest, nil
		}
		if i > maxStaleUnitCollisions {
			return "", fmt.Errorf("%s already holds %d entries called %s", dir, maxStaleUnitCollisions, name)
		}
		dest = filepath.Join(dir, fmt.Sprintf("%s.%d", name, i))
	}
}

// isMask reports whether the symlink target is a systemd mask. A mask points
// at /dev/null, which the sysroot has no device node for while immucore runs,
// so it would otherwise look like any other broken link.
//
// The target is checked both literally and after resolution, because a mask
// written relative to the unit directory (../../../dev/null) reaches the same
// device node and must not be mistaken for a dangling link and unmasked.
func isMask(root, unitDir, target string) bool {
	if filepath.Clean(target) == "/dev/null" {
		return true
	}
	return resolveUnitTarget(root, unitDir, target) == filepath.Join(root, "dev", "null")
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
