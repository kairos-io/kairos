package utils

import "os"

// ExecTempDirBase is where MkdirExecTemp creates its directories. /tmp is
// mounted noexec on Kairos (CIS 1.1.5), and /var is a persistent, exec-capable
// bind on an installed system and a writable overlay on the live one.
var ExecTempDirBase = "/var/lib/kairos/tmp"

// MkdirExecTemp creates a temporary directory for files that are going to be
// executed, like an extracted binary or a bundle's run.sh. It falls back to
// os.TempDir when ExecTempDirBase cannot be used (not root, or not a Kairos
// system), so callers behave as before outside Kairos.
func MkdirExecTemp(pattern string) (string, error) {
	if err := os.MkdirAll(ExecTempDirBase, 0o700); err == nil {
		if dir, err := os.MkdirTemp(ExecTempDirBase, pattern); err == nil {
			return dir, nil
		}
	}
	return os.MkdirTemp("", pattern)
}
