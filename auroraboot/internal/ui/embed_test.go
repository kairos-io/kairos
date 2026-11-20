package ui_test

import (
	"os"
	"os/exec"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("embedded UI assets", func() {
	// A build from the module zip, as `go install` does, has no generated
	// internal/ui/dist, because the zip never contains it. The
	// embed has to compile without it.
	It("compiles when the UI has not been built", func() {
		dir := filepath.Join(GinkgoT().TempDir(), "ui")
		Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "..", "go.mod"), []byte("module uiembedcheck\n\ngo 1.22\n"), 0o644)).To(Succeed())
		for _, name := range []string{"embed_ui.go", "embed_noui.go"} {
			src, err := os.ReadFile(name)
			Expect(err).ToNot(HaveOccurred())
			Expect(os.WriteFile(filepath.Join(dir, name), src, 0o644)).To(Succeed())
		}

		cmd := exec.Command("go", "build", "./...")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
		out, err := cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred(), string(out))
	})
})
