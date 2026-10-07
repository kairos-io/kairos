package agent

import (
	"fmt"
	"os"
	"os/exec"

	sdkConstants "github.com/kairos-io/kairos/v4/sdk/constants"
	"github.com/kairos-io/kairos/v4/sdk/installer"
)

// The installer locations and the resolution order (KAIROS_INSTALLER env ->
// override path -> default path) are defined once in the kairos-sdk installer
// package and consumed here via installer.Resolve.

// installerCommand builds the *exec.Cmd that invokes the installer, forwarding
// the install source when present plus any extra flags the caller needs. It
// does not wire stdio (see runExternalInstaller) so it can be unit-tested.
func installerCommand(path, source string, extra ...string) *exec.Cmd {
	args := []string{}
	if source != "" {
		args = append(args, "--source", source)
	}
	args = append(args, extra...)
	cmd := exec.Command(path, args...)
	cmd.Env = os.Environ()
	return cmd
}

// runExternalInstaller execs the installer with the current tty inherited and
// returns its error (an *exec.ExitError carries the installer's exit code).
func runExternalInstaller(path, source string, extra ...string) error {
	cmd := installerCommand(path, source, extra...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// resolveInstaller returns the installer binary to exec, or an error naming
// every location that was tried.
func resolveInstaller() (string, error) {
	path := installer.Resolve("/")
	if path == "" {
		return "", fmt.Errorf("no installer found (looked for %s, %s; or set %s)",
			sdkConstants.InstallerOverridePath, sdkConstants.InstallerDefaultPath, sdkConstants.InstallerEnvVar)
	}
	return path, nil
}
