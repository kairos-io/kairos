package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
