package utils

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	"github.com/kairos-io/kairos/v4/immucore/internal/constants"
	"golang.org/x/term"
)

// breakpointShells is the search order for the shell a breakpoint hands the
// console to, same as the error-path emergency shell uses. A variable so tests
// can point it at something that is not an interactive shell.
var breakpointShells = []string{"/bin/bash", "/bin/sh", "/sysroot/bin/bash", "/sysroot/bin/sh"}

// runBreakpointShell spawns the shell and blocks until the operator leaves it.
// Indirected through a variable so tests can exercise the surrounding plumbing
// without a console.
var runBreakpointShell = spawnBreakpointShell

// breakpointMu serializes breakpoints. herd runs independent steps
// concurrently, and two shells sharing one console cannot both be typed into.
var breakpointMu sync.Mutex

// BreakpointSteps returns the step names requested on the cmdline via
// rd.immucore.break=. The stanza can be repeated and each occurrence can carry
// a comma-separated list, so rd.immucore.break=load-config,mount-root and
// rd.immucore.break=load-config rd.immucore.break=mount-root mean the same
// thing. Names are the Op* constants; anything else simply never matches.
func BreakpointSteps() []string {
	var out []string
	for _, v := range ReadCMDLineArg(constants.CmdlineBreak) {
		for _, name := range strings.Split(v, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			out = append(out, name)
		}
	}
	return UniqueSlice(out)
}

// BreakpointRequested reports whether step was named on the cmdline.
func BreakpointRequested(step string) bool {
	step = strings.TrimSpace(step)
	if step == "" {
		return false
	}
	for _, want := range BreakpointSteps() {
		if want == step {
			return true
		}
	}
	return false
}

// MaybeBreakpoint drops to a breakpoint shell when step was requested on the
// cmdline, and returns once that shell exits. It returns immediately when no
// breakpoint asks for this step, which is the case for every step on a normal
// boot.
func MaybeBreakpoint(step string) {
	if !BreakpointRequested(step) {
		return
	}
	DropToBreakpointShell(step)
}

// DropToBreakpointShell hands the console to an interactive shell and blocks
// until it exits, then returns so the caller can carry on with the boot.
//
// This is deliberately not DropToEmergencyShell: that one ends in
// syscall.Exec, which replaces the immucore process image. Correct for a
// failure we cannot come back from, useless for a breakpoint, where the whole
// point is that the DAG keeps going afterwards.
func DropToBreakpointShell(step string) {
	breakpointMu.Lock()
	defer breakpointMu.Unlock()

	KLog.Logger.Info().Str("step", step).Msg("Breakpoint reached, dropping to a shell. Exit the shell to resume the boot")

	if err := runBreakpointShell(RenderBreakpointBanner(step)); err != nil {
		KLog.Logger.Warn().Err(err).Str("step", step).Msg("Breakpoint shell failed")
	}

	KLog.Logger.Info().Str("step", step).Msg("Breakpoint released, resuming boot")
}

// RenderBreakpointBanner builds what the operator sees on the console before
// the shell prompt. Kept separate from the spawn logic so it can be tested
// without exec-ing anything, like RenderFailureSummary is.
func RenderBreakpointBanner(step string) string {
	var b strings.Builder
	b.WriteString("========================================\n")
	fmt.Fprintf(&b, "      IMMUCORE BREAKPOINT: %s\n", step)
	b.WriteString("========================================\n")
	fmt.Fprintf(&b, "Stopped before step %q (%s%s).\n", step, constants.CmdlineBreak, step)
	fmt.Fprintf(&b, "Logs: %s\n", constants.LogDir)
	b.WriteString("Exit this shell to resume the boot.\n")
	b.WriteString("========================================\n")
	return b.String()
}

// spawnBreakpointShell runs the first available shell as a child process,
// attached to the console, and waits for it to exit.
func spawnBreakpointShell(banner string) error {
	tty := openBreakpointConsole()
	out := io.Writer(os.Stderr)
	if tty != nil {
		out = tty
		defer func() { _ = tty.Close() }()
	} else {
		KLog.Logger.Warn().Msg("Could not open a console for the breakpoint shell, using the inherited stdio")
	}

	_, _ = fmt.Fprint(out, banner)

	for _, sh := range breakpointShells {
		if _, err := os.Stat(sh); err != nil {
			continue
		}
		started, err := runShellAndWait(sh, tty)
		if !started {
			KLog.Logger.Debug().Err(err).Str("shell", sh).Msg("Could not start the breakpoint shell")
			continue
		}
		_, _ = fmt.Fprint(out, "Resuming boot.\n")
		return err
	}

	return fmt.Errorf("no shell to break into, tried %s", strings.Join(breakpointShells, ", "))
}

// runShellAndWait starts sh as a child process and blocks until it exits.
// started reports whether the child ran at all, so the caller can move on to
// the next candidate when this one could not be executed.
func runShellAndWait(sh string, tty *os.File) (started bool, err error) {
	cmd := breakpointShellCmd(sh, tty, tty != nil)
	if err := cmd.Start(); err != nil {
		if cmd.SysProcAttr == nil {
			return false, err
		}
		// TIOCSCTTY refuses to move a terminal that is already the
		// controlling one of another session. Fall back to sharing
		// immucore's session, which costs job control in the shell but
		// still gives the operator a prompt.
		KLog.Logger.Debug().Err(err).Str("shell", sh).Msg("Could not give the breakpoint shell its own session, sharing immucore's")
		cmd = breakpointShellCmd(sh, tty, false)
		if err := cmd.Start(); err != nil {
			return false, err
		}
	}

	err = cmd.Wait()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		// Leaving an interactive shell non-zero (Ctrl-C, a failed last
		// command) is a normal way out, not a breakpoint failure.
		return true, nil
	}
	return true, err
}

// breakpointShellCmd builds the shell command. With ownSession the child
// becomes a session leader owning the console, so that its job control works
// and a Ctrl-C in the shell does not land on immucore.
func breakpointShellCmd(sh string, tty *os.File, ownSession bool) *exec.Cmd {
	cmd := exec.Command(sh) // #nosec G204 -- sh comes from breakpointShells, not from the cmdline
	cmd.Env = shellEnvWithPath()

	if tty == nil {
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		return cmd
	}

	cmd.Stdin, cmd.Stdout, cmd.Stderr = tty, tty, tty
	if ownSession {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	}
	return cmd
}

// openBreakpointConsole opens the console the operator is actually looking at,
// read-write and without taking it as our own controlling terminal. Returns
// nil when none of the candidates is a usable terminal.
func openBreakpointConsole() *os.File {
	for _, dev := range ConsoleDevices() {
		f, err := os.OpenFile(dev, os.O_RDWR|syscall.O_NOCTTY, 0)
		if err != nil {
			continue
		}
		if !term.IsTerminal(int(f.Fd())) {
			_ = f.Close()
			continue
		}
		return f
	}
	return nil
}
