package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	sdkConstants "github.com/kairos-io/kairos/v4/sdk/constants"
	"github.com/kairos-io/kairos/v4/sdk/installer"
)

// The installer locations and the resolution order (KAIROS_INSTALLER env ->
// override path -> default path) are defined once in the kairos-sdk installer
// package and consumed here via installer.Resolve.

// installerShutdownGrace is how long a signalled installer gets to release its
// listen address before it is killed. It is a var so a test does not have to
// wait it out.
var installerShutdownGrace = 10 * time.Second

// installerCommand builds the *exec.Cmd that invokes the installer, forwarding
// the install source when present plus any extra flags the caller needs. It
// does not wire stdio (see runExternalInstaller) so it can be unit-tested.
func installerCommand(path, source string, extra ...string) *exec.Cmd {
	return installerCommandContext(context.Background(), path, source, extra...)
}

// installerCommandContext is installerCommand bound to ctx, so the caller can
// set Cancel/WaitDelay. exec.Cmd only honours those two fields on a command
// built from a context.
func installerCommandContext(ctx context.Context, path, source string, extra ...string) *exec.Cmd {
	args := []string{}
	if source != "" {
		args = append(args, "--source", source)
	}
	args = append(args, extra...)
	cmd := exec.CommandContext(ctx, path, args...)
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

// runExternalInstallerSupervised runs the installer like runExternalInstaller,
// but forwards SIGTERM and SIGINT to it and gives it installerShutdownGrace to
// release its listen address before killing it.
//
// The web UI path needs this and the interactive one does not. `kairos-agent
// webui` runs under openrc's supervise-daemon, which signals the pid it
// spawned, that is the agent, not the installer it delegates to. Without the
// forward an `rc-service kairos-webui restart` leaves an orphan holding the
// port, the respawned installer cannot bind and exits non-zero, and the
// supervisor respawns into a loop. The interactive installer shares the
// foreground process group on the inherited tty, so it already gets the
// signal.
//
// Pdeathsig is deliberately not used: on Linux it is per-thread and fires when
// the OS thread that created the child exits, which the Go runtime may do at
// any point.
func runExternalInstallerSupervised(path, source string, extra ...string) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	return runExternalInstallerCtx(ctx, path, source, extra...)
}

// runExternalInstallerCtx runs the installer and terminates it when ctx is
// cancelled. It is the half of runExternalInstallerSupervised that does not
// touch process-wide signal state, so a test can drive it.
func runExternalInstallerCtx(ctx context.Context, path, source string, extra ...string) error {
	cmd := installerCommandContext(ctx, path, source, extra...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = installerShutdownGrace

	err := cmd.Run()
	// Being asked to stop is not an installer failure. Once Cancel has
	// fired, exec reports the context error and masks the installer's own
	// exit status, so there is nothing left to propagate.
	if err != nil && ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		return nil
	}
	return err
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
