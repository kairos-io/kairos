package machine_test

import (
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/sdk/machine"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Umount and Remount run two different commands, and both once reported a
// failure as "failed umounting" because the second was copied from the first.
// A caller reading a boot log has nothing else to tell the two apart with.
var _ = Describe("what a failed mount table change reports", func() {
	var binDir string

	// stub puts a failing executable named name ahead of the real one on PATH.
	stub := func(name string) {
		script := "#!/bin/sh\necho \"" + name + ": no\" >&2\nexit 32\n"
		Expect(os.WriteFile(filepath.Join(binDir, name), []byte(script), 0o755)).To(Succeed())
	}

	BeforeEach(func() {
		binDir = GinkgoT().TempDir()
		stub("mount")
		stub("umount")
		GinkgoT().Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	})

	It("says umounting when umount fails", func() {
		err := machine.Umount("/kairos-sdk-machine-test")

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed umounting"))
	})

	It("says remounting when the remount fails", func() {
		err := machine.Remount("ro", "/kairos-sdk-machine-test")

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed remounting"))
	})
})
