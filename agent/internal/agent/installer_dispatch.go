package agent

import (
	"os"
	"os/exec"
)

// The installer locations and the resolution order (KAIROS_INSTALLER env ->
// override path -> default path) are defined once in the kairos-sdk installer
// package and consumed here via installer.Resolve.

// installerCommand builds the *exec.Cmd that invokes the installer, forwarding
// the install source when present. It does not wire stdio (see
// runExternalInstaller) so it can be unit-tested.
func installerCommand(path, source string) *exec.Cmd {
	args := []string{}
	if source != "" {
		args = append(args, "--source", source)
	}
	cmd := exec.Command(path, args...)
	cmd.Env = os.Environ()
	return cmd
}

// runExternalInstaller execs the installer with the current tty inherited and
// returns its error (an *exec.ExitError carries the installer's exit code).
func runExternalInstaller(path, source string) error {
	cmd := installerCommand(path, source)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
