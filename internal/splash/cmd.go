package splash

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/term"
)

// cmdlinePath is the kernel command line, read to honour kairos.splash=0.
const cmdlinePath = "/proc/cmdline"

// Main is the `kairos splash` entry point.
//
// It always exits 0 on anything that is merely a console it cannot draw on. A
// splash is cosmetic, it runs as a wanted unit inside the initramfs, and the
// one outcome that must be impossible is a boot that stops because the logo
// could not be painted.
func Main() int {
	fs := flag.NewFlagSet("splash", flag.ContinueOnError)
	brandDir := fs.String("branding-dir", DefaultBrandingDir,
		"directory holding the wordmark, tagline, palette and name files")
	ttyPath := fs.String("tty", "",
		"console device to draw on; empty uses stdout and stdin")
	frame := fs.Duration("frame", DefaultFrame, "time between frames")
	once := fs.Bool("once", false,
		"draw a single frame and exit, for a smoke test on a built image")
	noToggle := fs.Bool("no-toggle", false,
		"do not read the console, so Escape does not switch to the kernel log")
	cmdline := fs.String("cmdline", cmdlinePath,
		"kernel command line to read the kairos.splash=0 kill switch from")
	osRelease := fs.String("os-release", "/etc/os-release",
		"file to read VERSION_ID from for the version line")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return 2
	}

	if splashDisabled(*cmdline) {
		return 0
	}

	branding, errs := LoadBranding(*brandDir)
	for _, err := range errs {
		// stderr, not the console: the animation owns the screen, and a
		// branding complaint belongs in the journal.
		fmt.Fprintln(os.Stderr, "kairos splash:", err)
	}

	out, in, cleanup, err := openConsole(*ttyPath)
	if err != nil {
		// No console at all. Still print the one-line fallback, which is
		// the only trace that the unit ran.
		_ = writeFallback(os.Stdout, branding, ReadOSVersion(*osRelease))
		fmt.Fprintln(os.Stderr, "kairos splash:", err)
		return 0
	}
	defer cleanup()

	isTTY := term.IsTerminal(int(out.Fd()))
	rows, cols := 24, 80
	if isTTY {
		if c, r, err := term.GetSize(int(out.Fd())); err == nil && r > 0 && c > 0 {
			rows, cols = r, c
		}
	}

	opts := Options{
		Out:       out,
		Rows:      rows,
		Cols:      cols,
		IsTTY:     isTTY,
		Branding:  branding,
		Version:   ReadOSVersion(*osRelease),
		Frame:     *frame,
		MaxFrames: 0,
	}
	if *once {
		opts.MaxFrames = 1
	}

	// The ESC toggle needs the console in non-canonical mode, and it needs a
	// kernel to quiet. Both are skipped when the toggle is off, when there
	// is no readable console, and when this is a one-frame smoke test.
	kc := &KernelConsole{Out: out}
	if isTTY && !*noToggle && !*once && in != nil {
		if restore, err := term.MakeRaw(int(in.Fd())); err == nil {
			defer func() { _ = term.Restore(int(in.Fd()), restore) }()
			opts.In = in
			opts.Console = kc
		} else {
			fmt.Fprintln(os.Stderr, "kairos splash: no raw mode, Escape disabled:", err)
		}
	}
	if !*once {
		kc.Quiet()
		defer kc.Close()
	}

	done := make(chan struct{})
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sig
		close(done)
	}()
	opts.Done = done

	if err := Run(opts); err != nil {
		fmt.Fprintln(os.Stderr, "kairos splash:", err)
	}
	return 0
}

// openConsole returns the writer and reader for the console, plus a cleanup.
// An empty path uses the process's own stdout and stdin, which is how the
// systemd units drive it (TTYPath plus StandardInput=tty).
func openConsole(path string) (out, in *os.File, cleanup func(), err error) {
	if path == "" {
		return os.Stdout, os.Stdin, func() {}, nil
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		// Read-write is what the ESC poll needs, but a write-only console
		// is still worth animating on.
		w, werr := os.OpenFile(path, os.O_WRONLY, 0)
		if werr != nil {
			return nil, nil, nil, err
		}
		return w, nil, func() { w.Close() }, nil
	}
	return f, f, func() { f.Close() }, nil
}

// splashDisabled reports whether the kernel command line turns the splash off.
//
// Only the explicit off switch counts. "Is this a splash boot at all" is a
// question for the unit that starts this, where systemd answers it
// declaratively with ConditionKernelCommandLine=splash and no code at all.
// Reading it here as well would mean `kairos splash` exits silently on any
// machine whose cmdline does not say splash, which is every developer box.
//
// The kill switch stays in the binary because it has to survive someone
// starting the unit by hand, and because the cost of getting it wrong is a
// console nobody can get boot messages out of.
//
// A cmdline that cannot be read is not treated as off: that is what a
// container or a dev box looks like.
func splashDisabled(path string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, f := range strings.Fields(string(raw)) {
		switch f {
		case "kairos.splash=0", "kairos.splash=off", "kairos.splash=false", "kairos.splash=no":
			return true
		}
	}
	return false
}
