package splash

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

// Paths and values the kernel console is driven through. Overridable so the
// tests never write to the real /proc or signal the real PID 1.
const (
	printkPath = "/proc/sys/kernel/printk"
	kmsgPath   = "/dev/kmsg"
	// quietPrintk is the same value immucore writes when it takes over the
	// console: console loglevel 1 (emergencies only), everything else left
	// at the kernel defaults.
	quietPrintk = "1 4 1 7\n"
)

// KernelConsole quiets the kernel while the animation owns the screen, and
// hands the screen back to the kernel log when the user presses ESC.
//
// Every method is best effort. This is UX polish on the boot path: a kernel
// that refuses to be quieted means boot messages scroll over the splash, which
// is a cosmetic problem, and must never be an error that stops anything.
type KernelConsole struct {
	// Out is where the log stream is written. Required.
	Out io.Writer

	// PrintkPath, KmsgPath and Signal exist for the tests. Zero values use
	// the real kernel interfaces.
	PrintkPath string
	KmsgPath   string
	Signal     func(sig syscall.Signal) error

	mu      sync.Mutex
	saved   string
	quieted bool
	kmsg    io.ReadCloser
	stop    chan struct{}
	done    chan struct{}
}

func (k *KernelConsole) printk() string {
	if k.PrintkPath != "" {
		return k.PrintkPath
	}
	return printkPath
}

func (k *KernelConsole) kmsgFile() string {
	if k.KmsgPath != "" {
		return k.KmsgPath
	}
	return kmsgPath
}

func (k *KernelConsole) signal(sig syscall.Signal) error {
	if k.Signal != nil {
		return k.Signal(sig)
	}
	return syscall.Kill(1, sig)
}

// Quiet stops the kernel and systemd printing to the console, remembering the
// previous printk setting so Unquiet can put it back.
//
// Calling it while already quiet does not re-read printk: the value on disk is
// the quiet one by then, and capturing it would make Unquiet "restore" the
// console to silence. Close hits exactly that path, because it quiets on the
// way through LeaveLogs before unquieting for good.
func (k *KernelConsole) Quiet() {
	k.mu.Lock()
	defer k.mu.Unlock()
	if !k.quieted {
		if raw, err := os.ReadFile(k.printk()); err == nil {
			k.saved = string(raw)
		}
	}
	_ = os.WriteFile(k.printk(), []byte(quietPrintk), 0o644)
	_ = k.signal(showStatusSignal(21))
	k.quieted = true
}

// Unquiet restores the printk level captured by Quiet and re-enables systemd's
// status messages.
func (k *KernelConsole) Unquiet() {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.saved != "" {
		_ = os.WriteFile(k.printk(), []byte(k.saved), 0o644)
	}
	_ = k.signal(showStatusSignal(20))
	k.quieted = false
}

// EnterLogs leaves the alternate screen, unquiets the kernel and starts
// streaming /dev/kmsg.
//
// Streaming /dev/kmsg rather than replaying dmesg and then tailing is the
// whole point: every open of /dev/kmsg starts at the oldest record still in
// the ring buffer, so ESC shows what was suppressed as well as what arrives
// next. A source that only carried new records would show a black screen for
// as long as the boot happened to be quiet.
func (k *KernelConsole) EnterLogs() error {
	f, err := os.Open(k.kmsgFile())
	if err != nil {
		return err
	}

	k.mu.Lock()
	k.kmsg = f
	k.stop = make(chan struct{})
	k.done = make(chan struct{})
	stop, done := k.stop, k.done
	k.mu.Unlock()

	k.Unquiet()
	_, _ = io.WriteString(k.Out, seqLeave)

	go k.pump(f, stop, done)
	return nil
}

// LeaveLogs stops the stream and quiets the kernel again.
func (k *KernelConsole) LeaveLogs() error {
	k.mu.Lock()
	f, stop, done := k.kmsg, k.stop, k.done
	k.kmsg, k.stop, k.done = nil, nil, nil
	k.mu.Unlock()

	if f != nil {
		close(stop)
		// Closing the file unblocks the pump's in-flight read; without it
		// the goroutine would sit in Read until the next kernel message.
		err := f.Close()
		<-done
		k.Quiet()
		return err
	}
	k.Quiet()
	return nil
}

// Close restores the console. Called on the way out, so a splash that is
// killed at switch-root does not leave the kernel silent for whatever runs
// next.
func (k *KernelConsole) Close() {
	_ = k.LeaveLogs()
	k.Unquiet()
}

// pump copies kernel records to the screen until the file is closed.
func (k *KernelConsole) pump(r io.Reader, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	sc := bufio.NewScanner(r)
	// A single kernel record can exceed bufio's default 64KiB line limit
	// once a stack trace lands in it, and Scanner stops for good on a long
	// line rather than skipping it.
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	w := bufio.NewWriter(k.Out)
	for sc.Scan() {
		select {
		case <-stop:
			w.Flush()
			return
		default:
		}
		_, _ = w.WriteString(KmsgText(sc.Text()))
		// The console is in raw mode for the ESC poll, which clears ONLCR,
		// so a bare newline would leave the cursor in the same column.
		_, _ = w.WriteString("\r\n")
		if w.Available() < 4096 {
			w.Flush()
		}
	}
	w.Flush()
}

// KmsgText strips the kernel's record header from a /dev/kmsg line, leaving
// the human-readable message.
//
// The format is "<prio>,<seq>,<usec>,<flag>[,key=value...];<message>", and a
// multi-line record continues on following lines that start with a space. A
// line that does not look like a record is passed through unchanged, because
// showing an unparsed line beats dropping a log the user asked to see.
func KmsgText(line string) string {
	if strings.HasPrefix(line, " ") {
		return strings.TrimRight(line, "\r")
	}
	head, msg, ok := strings.Cut(line, ";")
	if !ok || strings.ContainsAny(head, " ") {
		return strings.TrimRight(line, "\r")
	}
	return strings.TrimRight(msg, "\r")
}

// showStatusSignal returns the real signal number for systemd's SIGRTMIN+off,
// where off is 20 to enable status messages on the console and 21 to disable
// them.
//
// The number depends on the libc systemd was built against, because SIGRTMIN
// is a libc constant and not a kernel one:
//
//   - glibc reserves 32-33 for NPTL   -> SIGRTMIN=34
//   - musl reserves 32-34 internally  -> SIGRTMIN=35
//
// Guessing wrong is worse than doing nothing: 55 on a musl-built systemd is
// SIGRTMIN+20, so an attempt to quiet the console would turn status messages
// on instead. immucore detects this the same way, by looking for musl's
// dynamic loader (internal/utils/common.go, signalShowStatusOff); the check is
// repeated here rather than shared because that package is internal to
// immucore.
// muslLoaderGlob is a variable so a test can point it at a temp dir instead of
// depending on whatever libc this build host happens to have.
var muslLoaderGlob = "/lib/ld-musl-*"

func showStatusSignal(off int) syscall.Signal {
	sigrtmin := 34
	if matches, _ := filepath.Glob(muslLoaderGlob); len(matches) > 0 {
		sigrtmin = 35
	}
	return syscall.Signal(sigrtmin + off)
}
