package agent

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	sdkConstants "github.com/kairos-io/kairos/v4/sdk/constants"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"

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

		It("appends extra flags after the source", func() {
			cmd := installerCommand("/bin/installer", "oci://foo:bar", "--no-tui")
			Expect(cmd.Args).To(Equal([]string{"/bin/installer", "--source", "oci://foo:bar", "--no-tui"}))
		})
	})

	// The web installer is a frontend of the installer, not of the agent, so
	// `kairos-agent webui` has to reach it through the same resolution the
	// interactive install uses. An image shipping its own installer serves its
	// own web UI.
	Describe("WebUI", func() {
		It("runs the resolved installer with --no-tui", func() {
			dir := GinkgoT().TempDir()
			bin := filepath.Join(dir, "fake-installer")
			argsFile := filepath.Join(dir, "args")
			Expect(os.WriteFile(bin,
				[]byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > "+argsFile+"\n"), 0o755)).To(Succeed())
			GinkgoT().Setenv(sdkConstants.InstallerEnvVar, bin)

			logger := sdkLogger.NewKairosLogger("test", "info", true)
			Expect(WebUI("oci://foo:bar", logger)).To(Succeed())

			recorded, err := os.ReadFile(argsFile)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.Fields(string(recorded))).To(Equal([]string{"--source", "oci://foo:bar", "--no-tui"}))
		})

		// The subcommand is on its way out, so an operator who runs it by
		// hand has to be told where the web UI went.
		It("warns that the subcommand is deprecated", func() {
			dir := GinkgoT().TempDir()
			bin := filepath.Join(dir, "fake-installer")
			Expect(os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755)).To(Succeed())
			GinkgoT().Setenv(sdkConstants.InstallerEnvVar, bin)

			var logged bytes.Buffer
			Expect(WebUI("", sdkLogger.NewBufferLogger(&logged))).To(Succeed())

			Expect(logged.String()).To(ContainSubstring(WebUIDeprecationNotice))
			Expect(logged.String()).To(ContainSubstring("--no-tui"))
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
