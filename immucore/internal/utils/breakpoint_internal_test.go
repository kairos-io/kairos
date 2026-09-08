package utils

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/kairos-io/kairos/v4/immucore/internal/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rs/zerolog"
)

var _ = Describe("breakpoints", func() {
	var cmdlinePath string

	writeCmdline := func(s string) {
		Expect(os.WriteFile(cmdlinePath, []byte(s+"\n"), 0o600)).To(Succeed())
	}

	BeforeEach(func() {
		cmdlinePath = filepath.Join(GinkgoT().TempDir(), "cmdline")
		writeCmdline("")

		old, had := os.LookupEnv("HOST_PROC_CMDLINE")
		Expect(os.Setenv("HOST_PROC_CMDLINE", cmdlinePath)).To(Succeed())
		DeferCleanup(func() {
			if had {
				Expect(os.Setenv("HOST_PROC_CMDLINE", old)).To(Succeed())
				return
			}
			Expect(os.Unsetenv("HOST_PROC_CMDLINE")).To(Succeed())
		})
	})

	Context("reading the cmdline", func() {
		It("picks up a single requested step", func() {
			writeCmdline("root=LABEL=COS_ACTIVE rd.immucore.break=mount-root quiet")
			Expect(BreakpointSteps()).To(Equal([]string{constants.OpMountRoot}))
			Expect(BreakpointRequested(constants.OpMountRoot)).To(BeTrue())
			Expect(BreakpointRequested(constants.OpLoadConfig)).To(BeFalse())
		})

		It("requests nothing when the stanza is absent", func() {
			writeCmdline("root=LABEL=COS_ACTIVE rd.immucore.debug quiet")
			Expect(BreakpointSteps()).To(BeEmpty())
			Expect(BreakpointRequested(constants.OpMountRoot)).To(BeFalse())
		})

		It("requests nothing when the stanza carries no value", func() {
			writeCmdline("rd.immucore.break=")
			Expect(BreakpointSteps()).To(BeEmpty())
			Expect(BreakpointRequested(constants.OpMountRoot)).To(BeFalse())
		})

		It("accepts several steps, comma-separated or repeated, without duplicates", func() {
			writeCmdline("rd.immucore.break=load-config,mount-root rd.immucore.break=mount-root rd.immucore.break=overlay-mount")
			Expect(BreakpointSteps()).To(Equal([]string{
				constants.OpLoadConfig,
				constants.OpMountRoot,
				constants.OpOverlayMount,
			}))
			Expect(BreakpointRequested(constants.OpLoadConfig)).To(BeTrue())
			Expect(BreakpointRequested(constants.OpOverlayMount)).To(BeTrue())
		})

		It("matches step names whole, not by prefix", func() {
			writeCmdline("rd.immucore.break=mount")
			Expect(BreakpointRequested(constants.OpMountRoot)).To(BeFalse())
			Expect(BreakpointRequested(constants.OpMountOEM)).To(BeFalse())
			Expect(BreakpointRequested(constants.OpMountState)).To(BeFalse())
		})

		It("never breaks on an unnamed step", func() {
			writeCmdline("rd.immucore.break=mount-root")
			Expect(BreakpointRequested("")).To(BeFalse())
		})
	})

	Context("dropping to the shell", func() {
		var spawned []string

		fakeShell := func(err error) {
			old := runBreakpointShell
			runBreakpointShell = func(banner string) error {
				spawned = append(spawned, banner)
				return err
			}
			DeferCleanup(func() { runBreakpointShell = old })
		}

		BeforeEach(func() {
			spawned = nil
		})

		It("spawns a shell for a requested step and comes back", func() {
			fakeShell(nil)
			writeCmdline("rd.immucore.break=mount-root")

			MaybeBreakpoint(constants.OpMountRoot)

			Expect(spawned).To(HaveLen(1))
			Expect(spawned[0]).To(ContainSubstring(constants.OpMountRoot))
			Expect(spawned[0]).To(ContainSubstring("Exit this shell to resume the boot"))
		})

		It("spawns nothing for a step nobody asked about", func() {
			fakeShell(nil)
			writeCmdline("rd.immucore.break=mount-root")

			MaybeBreakpoint(constants.OpLoadConfig)

			Expect(spawned).To(BeEmpty())
		})

		It("carries on when the shell could not be spawned", func() {
			fakeShell(errors.New("no shell for you"))
			writeCmdline("rd.immucore.break=mount-root")

			MaybeBreakpoint(constants.OpMountRoot)

			Expect(spawned).To(HaveLen(1))
		})
	})

	Context("the shell child process", func() {
		var dir string

		useShells := func(shells ...string) {
			old := breakpointShells
			breakpointShells = shells
			DeferCleanup(func() { breakpointShells = old })
		}

		writeShell := func(name, body string) string {
			path := filepath.Join(dir, name)
			Expect(os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700)).To(Succeed())
			return path
		}

		BeforeEach(func() {
			dir = GinkgoT().TempDir()
		})

		It("waits for the shell to exit before handing control back", func() {
			marker := filepath.Join(dir, "shell-exited")
			useShells(writeShell("fakeshell", "sleep 0.2; touch "+marker))

			Expect(spawnBreakpointShell("banner\n")).To(Succeed())

			// The marker is only written by the child right before it exits,
			// so its presence here is what proves we waited rather than
			// resuming the boot underneath the operator.
			Expect(marker).To(BeAnExistingFile())
		})

		It("treats a non-zero exit as a normal way out of the shell", func() {
			useShells(writeShell("fakeshell", "exit 3"))
			Expect(spawnBreakpointShell("banner\n")).To(Succeed())
		})

		It("falls through to the next candidate when one cannot be run", func() {
			marker := filepath.Join(dir, "shell-exited")
			unusable := filepath.Join(dir, "not-a-shell")
			Expect(os.WriteFile(unusable, []byte("not executable"), 0o600)).To(Succeed())
			useShells(filepath.Join(dir, "missing"), unusable, writeShell("fakeshell", "touch "+marker))

			Expect(spawnBreakpointShell("banner\n")).To(Succeed())
			Expect(marker).To(BeAnExistingFile())
		})

		It("reports when there is no shell to break into", func() {
			useShells(filepath.Join(dir, "missing"))
			Expect(spawnBreakpointShell("banner\n")).To(MatchError(ContainSubstring("no shell to break into")))
		})
	})

	Context("the console banner", func() {
		It("names the step and the stanza that stopped the boot", func() {
			out := RenderBreakpointBanner(constants.OpOverlayMount)
			Expect(out).To(ContainSubstring("IMMUCORE BREAKPOINT: overlay-mount"))
			Expect(out).To(ContainSubstring(constants.CmdlineBreak + constants.OpOverlayMount))
			Expect(out).To(ContainSubstring(constants.LogDir))
		})
	})

	// This is the one branch none of the tests above exercise: what happens
	// when TIOCSCTTY genuinely refuses to hand the console over because
	// another session already owns it as its controlling tty (the exact
	// scenario runShellAndWait's comment describes). A plain file (even a
	// TempDir file opened O_RDWR) can never trigger that refusal -- only a
	// real pty slave that another process has already Setctty'd onto can.
	Context("the console session fallback", func() {
		It("falls back to sharing immucore's session when the console already belongs to another session", func() {
			master, slave, err := pty.Open()
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = master.Close() }()
			defer func() { _ = slave.Close() }()

			// Claim the slave as another session's controlling tty first, so
			// the breakpoint shell's own Setctty attempt below is guaranteed
			// to hit EPERM -- TIOCSCTTY refuses to steal a tty that already
			// is a session's ctty.
			holder := exec.Command("sleep", "5")
			holder.Stdin, holder.Stdout, holder.Stderr = slave, slave, slave
			holder.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
			Expect(holder.Start()).To(Succeed())
			defer func() {
				_ = holder.Process.Kill()
				_, _ = holder.Process.Wait()
			}()
			// give the holder a beat to actually own the tty before we race it
			time.Sleep(150 * time.Millisecond)

			var logbuf bytes.Buffer
			oldLogger := KLog.Logger
			KLog.Logger = zerolog.New(&logbuf).Level(zerolog.DebugLevel)
			defer func() { KLog.Logger = oldLogger }()

			dir := GinkgoT().TempDir()
			marker := filepath.Join(dir, "shell-exited")
			shPath := filepath.Join(dir, "fakeshell")
			Expect(os.WriteFile(shPath, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o700)).To(Succeed())

			started, err := runShellAndWait(shPath, slave)

			Expect(err).NotTo(HaveOccurred())
			Expect(started).To(BeTrue())
			// The marker only exists if the fallback attempt actually ran
			// the shell to completion, proving the retry recovered rather
			// than swallowing the first Start() error.
			Expect(marker).To(BeAnExistingFile())
			Expect(logbuf.String()).To(ContainSubstring("sharing immucore's"))
		})
	})
})
