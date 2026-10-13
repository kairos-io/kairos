//go:build !windows

package logger

import (
	"os"
	"path/filepath"
	"testing"
)

// CIS 4.2.3: log files must not be readable by other. A file that already
// exists with a laxer mode (written by an older build) is tightened too.
func TestLogFilesAreNotWorldReadable(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "stale.log")
	if err := os.WriteFile(stale, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"fresh", "stale"} {
		_ = NewKairosLoggerWithExtraDirs(name, "info", true, dir)
		info, err := os.Stat(filepath.Join(dir, name+".log"))
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm&0o037 != 0 {
			t.Errorf("%s.log has mode %#o, want no group write and nothing for other", name, perm)
		}
	}
}
