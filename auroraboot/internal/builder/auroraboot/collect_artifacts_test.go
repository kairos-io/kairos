package auroraboot

import (
	"os"
	"path/filepath"
	"testing"
)

// collectArtifacts must surface the MAAS ddgz output (kairos-*.raw.gz). It is
// produced alongside the raw by the MAAS conversion step, and without it in the
// extension allowlist the UI would only ever show the bare .raw.
func TestCollectArtifactsIncludesMAASDdgz(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		"kairos-hadron-v0.4.0-core-amd64-generic-v4.1.2.raw",
		"kairos-hadron-v0.4.0-core-amd64-generic-v4.1.2.raw.gz",
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatalf("writing %s: %v", f, err)
		}
	}

	got := map[string]bool{}
	for _, p := range collectArtifacts(dir) {
		got[filepath.Base(p)] = true
	}

	for _, f := range files {
		if !got[f] {
			t.Fatalf("collectArtifacts did not include %q (got %v)", f, got)
		}
	}
}
