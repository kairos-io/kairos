package agent

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Installer resolution (KAIROS_INSTALLER env -> override -> default) lives in the
// kairos-sdk installer package and is tested there. These tests cover the
// agent-specific exec wiring.

var _ = Describe("installer dispatch", func() {
	Describe("installerCommand", func() {
		It("forwards the source as --source", func() {
			cmd := installerCommand("/bin/installer", "oci://foo:bar")
			Expect(cmd.Args).To(Equal([]string{"/bin/installer", "--source", "oci://foo:bar"}))
		})

		It("omits --source when no source is given", func() {
			cmd := installerCommand("/bin/installer", "")
			Expect(cmd.Args).To(Equal([]string{"/bin/installer"}))
		})
	})

	Describe("runExternalInstaller", func() {
		It("propagates the installer's exit code", func() {
			dir := GinkgoT().TempDir()
			bin := filepath.Join(dir, "failing-installer")
			Expect(os.WriteFile(bin, []byte("#!/bin/sh\nexit 7\n"), 0o755)).To(Succeed())

			err := runExternalInstaller(bin, "")
			var exitErr *exec.ExitError
			Expect(errors.As(err, &exitErr)).To(BeTrue())
			Expect(exitErr.ExitCode()).To(Equal(7))
		})
	})
})
