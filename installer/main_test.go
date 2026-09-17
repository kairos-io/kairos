package main

import (
	"bytes"
	"context"
	"flag"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"

	"github.com/kairos-io/kairos/v4/installer/internal/webui"
)

// The web UI shares a process with the TUI, and echo's default handler writes
// JSON to stdout, which would land on top of the alt screen. So the logger the
// TUI path hands echo has to go to a file, and when it cannot, to nowhere -
// never back to stdout.
func TestWebUILoggerWritesToItsFile(t *testing.T) {
	dir := t.TempDir()
	webUILogPath = filepath.Join(dir, "logs", "webui.log")

	webUILogger().Info("hello from echo")

	content, err := os.ReadFile(webUILogPath)
	if err != nil {
		t.Fatalf("reading %s: %v", webUILogPath, err)
	}
	if !strings.Contains(string(content), "hello from echo") {
		t.Errorf("log file does not hold the message: %q", content)
	}
}

func TestWebUILoggerDiscardsWhenItsFileIsUnwritable(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "notadir")
	if err := os.WriteFile(blocker, nil, 0600); err != nil {
		t.Fatal(err)
	}
	// MkdirAll cannot create a directory under a regular file, so both the
	// mkdir and the open fail and the logger has to fall back.
	webUILogPath = filepath.Join(blocker, "webui.log")

	// The only assertion available is that this does not panic and does not
	// write the file; anything reaching stdout would corrupt the TUI.
	webUILogger().Info("hello from echo")

	if _, err := os.Stat(webUILogPath); err == nil {
		t.Errorf("expected no log file at %s", webUILogPath)
	}
}

// The installer forwards --source to kairos-agent, and the web UI is one of
// its two frontends. A --no-tui boot pointed at a private registry has to
// install from it, so the flag cannot stop at the TUI.
func TestWebUICarriesTheInstallSourceInBothModes(t *testing.T) {
	webUILogPath = filepath.Join(t.TempDir(), "webui.log")

	if got := noTUIWebUIOptions("oci://foo:bar").Source; got != "oci://foo:bar" {
		t.Errorf("--no-tui dropped the source: %q", got)
	}
	if got := tuiWebUIOptions("oci://foo:bar", nil).Source; got != "oci://foo:bar" {
		t.Errorf("the TUI's web UI dropped the source: %q", got)
	}
}

// The two modes differ only in where echo logs: to stdout when nothing owns
// the terminal, to a file when the TUI does.
func TestWebUILoggerIsSetOnlyForTheTUIMode(t *testing.T) {
	webUILogPath = filepath.Join(t.TempDir(), "webui.log")

	if o := noTUIWebUIOptions(""); o.Logger != nil {
		t.Error("--no-tui should leave echo on stdout, so it reaches the journal")
	}
	if o := tuiWebUIOptions("", nil); o.Logger == nil {
		t.Error("the TUI mode must keep echo off stdout")
	}
}

// `q` on any TUI page returns from p.Run() and the deferred cancel() stops
// echo, so main has to hold the server up while the browser has an install
// running. Nothing re-execs the installer on an interactive boot.
func TestTUIWebUIOptionsCarryTheActivityHandle(t *testing.T) {
	webUILogPath = filepath.Join(t.TempDir(), "webui.log")

	activity := &webui.Activity{}
	if o := tuiWebUIOptions("", activity); o.Activity != activity {
		t.Error("the TUI's web UI cannot report a browser-driven install back to main")
	}
	// --no-tui has no terminal UI to quit, so there is nothing to wait for.
	if o := noTUIWebUIOptions(""); o.Activity != nil {
		t.Error("--no-tui should not need an activity handle")
	}
}

// This is what decides whether the agent config or the command line has the
// last word on the MCP listener, so it is worth more than an eyeball.
func TestFlagWasPassed(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want bool
	}{
		{name: "no arguments at all", args: []string{"kairos-installer"}},
		{name: "another flag", args: []string{"kairos-installer", "--source", "oci:x"}},
		{
			name: "passed with a value",
			args: []string{"kairos-installer", "--mcp-address=127.0.0.1:8090"},
			want: true,
		},
		{
			name: "passed empty, which is how it is switched off",
			args: []string{"kairos-installer", "--mcp-address="},
			want: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oldArgs, oldFlags := os.Args, flag.CommandLine
			defer func() { os.Args, flag.CommandLine = oldArgs, oldFlags }()

			flag.CommandLine = flag.NewFlagSet(tc.args[0], flag.ContinueOnError)
			flag.String("source", "", "")
			// Mirrors main's empty default, which is what makes "passed
			// empty" indistinguishable by value and only detectable by Visit.
			flag.String("mcp-address", "", "")
			os.Args = tc.args
			if err := flag.CommandLine.Parse(tc.args[1:]); err != nil {
				t.Fatal(err)
			}

			if got := flagWasPassed("mcp-address"); got != tc.want {
				t.Errorf("flagWasPassed = %v, want %v for %v", got, tc.want, tc.args)
			}
		})
	}
}

// freeLoopbackAddress returns a loopback address nothing is listening on, by
// binding one and letting it go. A test that asserts something bound this
// address has to name the address up front, which ListenAndServe does not
// report back.
func freeLoopbackAddress(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving an address: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("releasing the reserved address: %v", err)
	}

	return addr
}

// dialUntilListening reports whether addr accepts a connection within the
// timeout. The server it waits for starts in a goroutine, so "not yet" and
// "never" are only distinguishable by waiting.
func dialUntilListening(addr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}

	return false
}

// --no-tui is the mode with no console session to install from, so it is the
// one that most needs an agent to be able to drive it. It used to return
// before the MCP server was started, leaving that boot with the web UI alone.
func TestWebUIOnlyModeServesMCPAlongsideTheWebUI(t *testing.T) {
	mcpAddr := freeLoopbackAddress(t)
	webAddr := freeLoopbackAddress(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	opts := noTUIWebUIOptions("")
	opts.Listen = webAddr
	opts.Logger = slog.New(slog.NewJSONHandler(io.Discard, nil))

	served := make(chan error, 1)
	go func() {
		served <- serveWebUIOnly(ctx, testLogger(), mcpAddr, opts)
	}()

	if !dialUntilListening(webAddr, 5*time.Second) {
		t.Fatalf("the web UI never listened on %s", webAddr)
	}
	if !dialUntilListening(mcpAddr, 5*time.Second) {
		t.Fatalf("the web UI ran without MCP: nothing listened on %s", mcpAddr)
	}

	cancel()
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("serving the web UI: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("serveWebUIOnly did not return after its context was cancelled")
	}
}

// An empty address is how the MCP listener is switched off, and it is what an
// operator gets when the agent config says nothing. The positive half is the
// control: without it, "nothing bound this address" would pass for a startMCP
// that never binds anything at all.
func TestStartMCPBindsOnlyWhenAnAddressIsConfigured(t *testing.T) {
	addr := freeLoopbackAddress(t)

	off, cancelOff := context.WithCancel(context.Background())
	defer cancelOff()

	startMCP(off, testLogger(), "")
	if dialUntilListening(addr, 300*time.Millisecond) {
		t.Fatalf("an empty address bound %s, so there is no off switch", addr)
	}
	cancelOff()

	on, cancelOn := context.WithCancel(context.Background())
	defer cancelOn()

	startMCP(on, testLogger(), addr)
	if !dialUntilListening(addr, 5*time.Second) {
		t.Fatalf("startMCP never bound %s, so the half of this test above proves nothing", addr)
	}
}

// testLogger keeps the MCP server's own output out of the test log. The sdk
// logger otherwise writes to journald or /var/log/kairos, neither of which a
// test should depend on being writable.
func testLogger() sdkLogger.KairosLogger {
	return sdkLogger.NewBufferLogger(&bytes.Buffer{})
}
