package ops

import (
	"os/exec"
	"testing"
)

// TestMAASCurtinHookPythonUnitTests runs the Python unit tests for the MAAS
// curtin-hook asset (pkg/ops/assets/maas-curtin-hooks) so they execute as part
// of `go test ./...` and the CI ginkgo run, which compiles and runs every
// Test func in the package. The hook is written in Python because it uses the
// curtin library at deploy time; this bridge keeps its pure-function tests
// (network translation, datasource extraction) from being orphaned in a Go repo.
func TestMAASCurtinHookPythonUnitTests(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available; skipping MAAS curtin-hook python unit tests")
	}
	cmd := exec.Command("python3", "maas_curtin_hooks_test.py")
	cmd.Dir = "assets"
	out, err := cmd.CombinedOutput()
	t.Logf("python output:\n%s", out)
	if err != nil {
		t.Fatalf("MAAS curtin-hook python unit tests failed:\n%s", out)
	}
}
