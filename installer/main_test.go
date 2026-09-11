package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
