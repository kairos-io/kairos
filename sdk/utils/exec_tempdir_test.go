package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// /tmp is mounted noexec (CIS 1.1.5), so a temp dir whose content gets run
// has to live somewhere else.
func TestMkdirExecTempUsesTheExecBase(t *testing.T) {
	base := filepath.Join(t.TempDir(), "kairos-tmp")
	old := ExecTempDirBase
	ExecTempDirBase = base
	t.Cleanup(func() { ExecTempDirBase = old })

	dir, err := MkdirExecTemp("finalize-*")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(dir, base+string(os.PathSeparator)) {
		t.Errorf("temp dir %q is not under %q", dir, base)
	}
	if info, err := os.Stat(base); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("base %q should be created 0700, got %v (%v)", base, info.Mode().Perm(), err)
	}
}

func TestMkdirExecTempFallsBackWhenTheBaseCannotBeCreated(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	old := ExecTempDirBase
	ExecTempDirBase = filepath.Join(blocker, "tmp")
	t.Cleanup(func() { ExecTempDirBase = old })

	dir, err := MkdirExecTemp("finalize-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if !strings.HasPrefix(dir, os.TempDir()) {
		t.Errorf("expected a fallback under %q, got %q", os.TempDir(), dir)
	}
}
