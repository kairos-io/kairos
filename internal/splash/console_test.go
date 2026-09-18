package splash

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
)

func TestKmsgText(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		// The ordinary record: priority, sequence, timestamp, flag.
		{"6,1234,5678901,-;Linux version 7.1.8-hadron", "Linux version 7.1.8-hadron"},
		// Records carry extra key=value fields after the flag.
		{"6,1,2,-,caller=T1;usb 1-1: new device", "usb 1-1: new device"},
		// A continuation line of a multi-line record starts with a space.
		{" SUBSYSTEM=usb", " SUBSYSTEM=usb"},
		// A semicolon inside the message must not confuse the split.
		{"3,9,9,-;error: a;b;c", "error: a;b;c"},
		// Something that is not a record is shown rather than dropped: the
		// user pressed Escape to see output, not to see less of it.
		{"just a line", "just a line"},
		{"a header with spaces; and a body", "a header with spaces; and a body"},
		{"", ""},
	} {
		if got := KmsgText(tc.in); got != tc.want {
			t.Errorf("KmsgText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// testConsole returns a KernelConsole wired to temp files and a recording
// signal func, so nothing in the test touches /proc or PID 1.
func testConsole(t *testing.T, printkBody string) (*KernelConsole, *bytes.Buffer, *[]syscall.Signal, string) {
	t.Helper()
	dir := t.TempDir()
	printk := filepath.Join(dir, "printk")
	if err := os.WriteFile(printk, []byte(printkBody), 0o644); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var sigs []syscall.Signal
	out := &bytes.Buffer{}
	k := &KernelConsole{
		Out:        out,
		PrintkPath: printk,
		Signal: func(s syscall.Signal) error {
			mu.Lock()
			defer mu.Unlock()
			sigs = append(sigs, s)
			return nil
		},
	}
	return k, out, &sigs, printk
}

// Quiet has to remember what it clobbered. Without this, a boot that shows the
// splash leaves the kernel at loglevel 1 for everything that runs afterwards.
func TestQuietRestoresThePreviousPrintk(t *testing.T) {
	k, _, sigs, printk := testConsole(t, "4 4 1 7\n")
	k.Quiet()

	raw, err := os.ReadFile(printk)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != quietPrintk {
		t.Errorf("printk after Quiet = %q, want %q", raw, quietPrintk)
	}

	k.Unquiet()
	raw, err = os.ReadFile(printk)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "4 4 1 7\n" {
		t.Errorf("printk after Unquiet = %q, want the original", raw)
	}
	if len(*sigs) != 2 {
		t.Fatalf("signals = %v, want one off and one on", *sigs)
	}
	off, on := (*sigs)[0], (*sigs)[1]
	if off != on+1 {
		t.Errorf("quiet sent %d and unquiet %d; off must be SIGRTMIN+21 and on SIGRTMIN+20", off, on)
	}
}

// An unreadable printk must not stop the splash, and must not make Unquiet
// write a garbage value over whatever the kernel has.
func TestQuietToleratesAnUnreadablePrintk(t *testing.T) {
	k, _, sigs, _ := testConsole(t, "4 4 1 7\n")
	k.PrintkPath = filepath.Join(t.TempDir(), "nope", "printk")
	k.Quiet()
	k.Unquiet()
	if len(*sigs) != 2 {
		t.Errorf("signals = %v", *sigs)
	}
}

// The replay is the point of streaming /dev/kmsg: each open starts at the
// oldest record still in the ring buffer, so Escape shows what was suppressed
// as well as what arrives next.
func TestPumpShowsEveryRecordWithoutItsHeader(t *testing.T) {
	k, out, _, _ := testConsole(t, "4 4 1 7\n")
	body := "6,1,10,-;first record\n6,2,20,-;second record\n"

	stop, done := make(chan struct{}), make(chan struct{})
	go k.pump(strings.NewReader(body), stop, done)
	<-done

	got := out.String()
	if !strings.Contains(got, "first record") || !strings.Contains(got, "second record") {
		t.Errorf("output = %q, want both records", got)
	}
	if strings.Contains(got, "6,1,10,-;") {
		t.Errorf("record header reached the screen: %q", got)
	}
	// The console is in raw mode for the Escape poll, so ONLCR is off and a
	// bare newline would stair-step the output down the screen.
	if !strings.Contains(got, "first record\r\n") {
		t.Errorf("no carriage return before the newline: %q", got)
	}
}

// A single kernel record carrying a stack trace exceeds bufio's default line
// limit, and a Scanner that hits one stops for good rather than skipping it,
// which would freeze the log view mid-boot.
func TestPumpSurvivesAnOverlongRecord(t *testing.T) {
	k, out, _, _ := testConsole(t, "4 4 1 7\n")
	long := strings.Repeat("x", 200*1024)
	body := "6,1,10,-;" + long + "\n6,2,20,-;after the long one\n"

	stop, done := make(chan struct{}), make(chan struct{})
	go k.pump(strings.NewReader(body), stop, done)
	<-done

	if !strings.Contains(out.String(), "after the long one") {
		t.Error("the stream stopped at the overlong record")
	}
}

func TestEnterLogsOpensTheBufferAndHandsBackTheScreen(t *testing.T) {
	k, out, sigs, printk := testConsole(t, "4 4 1 7\n")
	kmsg := filepath.Join(t.TempDir(), "kmsg")
	if err := os.WriteFile(kmsg, []byte("6,1,10,-;a record\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	k.KmsgPath = kmsg
	k.Quiet()

	if err := k.EnterLogs(); err != nil {
		t.Fatal(err)
	}
	// The animation owns the alternate screen; the log has to go to the
	// main one, or it scrolls inside a buffer the user never sees again.
	if !strings.HasPrefix(out.String(), seqLeave) {
		t.Errorf("did not leave the alternate screen: %q", out.String())
	}
	// And the kernel has to be audible again, or pressing Escape shows a
	// replay of the ring buffer and then nothing further.
	raw, err := os.ReadFile(printk)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "4 4 1 7\n" {
		t.Errorf("printk while showing logs = %q, want the original", raw)
	}
	if len(*sigs) != 2 || (*sigs)[1] != showStatusSignal(20) {
		t.Errorf("signals = %v, want a quiet then an unquiet", *sigs)
	}

	if err := k.LeaveLogs(); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(printk)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != quietPrintk {
		t.Errorf("printk after LeaveLogs = %q, want quiet again", raw)
	}
}

func TestEnterLogsFailsWhenThereIsNoKmsg(t *testing.T) {
	k, _, _, _ := testConsole(t, "4 4 1 7\n")
	k.KmsgPath = filepath.Join(t.TempDir(), "absent")
	if err := k.EnterLogs(); err == nil {
		t.Fatal("EnterLogs succeeded with no kmsg")
	}
	// And the state machine's contract: a failed EnterLogs must leave
	// nothing half-started, so LeaveLogs is still safe to call.
	if err := k.LeaveLogs(); err != nil {
		t.Errorf("LeaveLogs after a failed EnterLogs: %v", err)
	}
}

func TestLeaveLogsWithoutEnterIsSafe(t *testing.T) {
	k, _, sigs, _ := testConsole(t, "4 4 1 7\n")
	if err := k.LeaveLogs(); err != nil {
		t.Fatal(err)
	}
	if len(*sigs) == 0 {
		t.Error("LeaveLogs did not re-quiet the console")
	}
}

// The splash is killed at switch-root. If Close did not unquiet, everything
// that boots afterwards would inherit a silent kernel.
func TestCloseUnquietsTheKernel(t *testing.T) {
	k, _, _, printk := testConsole(t, "7 4 1 7\n")
	k.Quiet()
	k.Close()
	raw, err := os.ReadFile(printk)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "7 4 1 7\n" {
		t.Errorf("printk after Close = %q, want the original", raw)
	}
}

// Sending the wrong signal number is worse than sending none: 55 on a
// musl-built systemd is SIGRTMIN+20, so an attempt to quiet the console turns
// status messages on instead.
func TestShowStatusSignalDependsOnTheLibc(t *testing.T) {
	saved := muslLoaderGlob
	t.Cleanup(func() { muslLoaderGlob = saved })

	muslLoaderGlob = filepath.Join(t.TempDir(), "ld-musl-*")
	if got := showStatusSignal(21); got != syscall.Signal(55) {
		t.Errorf("glibc: showStatusSignal(21) = %d, want 55", got)
	}
	if got := showStatusSignal(20); got != syscall.Signal(54) {
		t.Errorf("glibc: showStatusSignal(20) = %d, want 54", got)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ld-musl-x86_64.so.1"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	muslLoaderGlob = filepath.Join(dir, "ld-musl-*")
	if got := showStatusSignal(21); got != syscall.Signal(56) {
		t.Errorf("musl: showStatusSignal(21) = %d, want 56", got)
	}
	if got := showStatusSignal(20); got != syscall.Signal(55) {
		t.Errorf("musl: showStatusSignal(20) = %d, want 55", got)
	}
}
