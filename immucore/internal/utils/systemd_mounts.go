package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kairos-io/kairos/v4/immucore/internal/constants"
)

// MountUnitName returns the name of the systemd mount unit for a path, e.g.
// /var/log/audit -> var-log-audit.mount. systemd-fstab-generator derives the
// same name from the fstab entry immucore writes, which is what lets another
// unit depend on a mount immucore established.
//
// This implements the subset of systemd-escape the unit name needs: leading
// and trailing slashes go, a path separator becomes a dash, a literal dash and
// a leading dot are hex-escaped, and so is anything outside [A-Za-z0-9_.].
// The root path is -.mount.
func MountUnitName(path string) string {
	p := strings.Trim(filepath.Clean(path), "/")
	if p == "" || p == "." {
		return "-.mount"
	}

	var b strings.Builder
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c == '/':
			b.WriteByte('-')
		case c == '.' && i == 0:
			b.WriteString(`\x2e`)
		case c == '_' || c == '.' ||
			(c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z'):
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, `\x%02x`, c)
		}
	}
	b.WriteString(".mount")
	return b.String()
}

// WriteMountRequirementDropIn writes a drop-in that makes unit require, and
// order itself after, the mount of mountPath, and returns the path of the file
// it wrote.
//
// The drop-in goes to a volatile unit directory (constants.RunSystemdUnitDir)
// on purpose. It is regenerated on every boot from what immucore actually
// mounts, so it never lingers the way a file in the persistent /etc/systemd
// bind does (QuarantineStaleUnitSymlinks exists because of what lingering unit
// files there cost us), and /etc still wins over /run for an admin who wants
// to override it. A file written here in the initramfs reaches the booted
// systemd because /run is the same tmpfs on both sides of switch_root, the
// same property EnableSysAndConfExtensions relies on for /run/extensions.
//
// It is written whether or not unit is installed: a drop-in for a unit that is
// not there is inert. That matters because auditd ships from the distro
// package rather than from this repo, so the ordering cannot be expressed in
// the unit itself.
//
// Requires= on the mount unit is what makes the failure loud. If the mount
// never happened there is no fstab entry, so there is no mount unit either,
// and unit fails to start with a missing dependency instead of quietly writing
// to the ephemeral directory underneath the missing mount.
func WriteMountRequirementDropIn(unitDir, unit, mountPath string) (string, error) {
	dir := filepath.Join(unitDir, fmt.Sprintf("%s.d", unit))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("creating %s: %w", dir, err)
	}

	mountUnit := MountUnitName(mountPath)
	content := fmt.Sprintf(`# Written by immucore on every boot.
# %[1]s is a bind mount from the persistent partition. Starting %[2]s before
# it is in place would send the audit trail to the ephemeral directory
# underneath, where the next boot loses it.
[Unit]
RequiresMountsFor=%[1]s
Requires=%[3]s
After=%[3]s
`, mountPath, unit, mountUnit)

	file := filepath.Join(dir, constants.MountRequirementDropInName)
	if err := os.WriteFile(file, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("writing %s: %w", file, err)
	}
	KLog.Logger.Debug().Str("file", file).Str("unit", unit).Str("mount", mountUnit).
		Msg("Wrote mount requirement drop-in")
	return file, nil
}
