package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kairos-io/kairos/v4/sdk/agentrun"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"

	"github.com/kairos-io/kairos/v4/installer/internal/debugbundle"
	"github.com/kairos-io/kairos/v4/installer/internal/tui"
	"github.com/kairos-io/kairos/v4/installer/internal/webui"
)

// webUILogPath is where echo's own output goes while the TUI owns the
// terminal. It matches the path the openrc kairos-webui service already uses.
// It is a var so a test can point it at a writable directory.
var webUILogPath = "/var/log/kairos/webui.log"

func main() {
	source := flag.String("source", "", "installation source (passed through to kairos-agent)")
	collect := flag.Bool("collect-debug-bundle", false,
		"collect a debug bundle non-interactively (no TUI), print its path, and exit")
	noTUI := flag.Bool("no-tui", false,
		"serve only the web UI, without the terminal installer, for boots that ask for an unattended install")
	flag.Parse()

	if *collect {
		os.Exit(collectDebugBundle())
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Web-UI-only mode has no terminal UI to protect, so echo logs to stdout
	// and lands in the journal, and serving it is the whole job.
	if *noTUI {
		if err := webui.Start(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "web UI:", err)
			os.Exit(1)
		}
		return
	}

	logger := sdkLogger.NewKairosLoggerWithExtraDirs("installer", "info", true, "/var/log/kairos/")

	// The web UI runs alongside the TUI so a user can install from either.
	// It gets a file-backed logger because echo writes JSON to stdout by
	// default, which would land on top of the TUI's alt screen.
	go func() {
		if err := webui.StartConfigured(ctx, webUILogger()); err != nil {
			logger.Warnf("web UI stopped: %s", err.Error())
		}
	}()

	p := tea.NewProgram(tui.InitialModel(&logger, *source), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

// webUILogger returns a logger writing to webUILogPath, or one writing nowhere
// if that file cannot be opened. It deliberately never falls back to stdout:
// the TUI owns the terminal, and losing the web UI's log is better than
// scribbling over the screen the user is installing from.
func webUILogger() *slog.Logger {
	if err := os.MkdirAll(filepath.Dir(webUILogPath), 0755); err == nil {
		f, err := os.OpenFile(webUILogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err == nil {
			return slog.New(slog.NewJSONHandler(f, nil))
		}
	}
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

// collectDebugBundle generates a debug bundle without starting the TUI, for use
// when the interactive installer can't run (e.g. broken terminal size/fonts).
// It prints the bundle path to stdout and returns the process exit code.
func collectDebugBundle() int {
	agentBin := agentrun.ResolveAgentBin()
	ctx := debugbundle.Context{AgentBin: agentBin}
	if agentBin != "" {
		ctx.AgentArgs = agentrun.Command(agentBin, "<config>", "", "").Args[1:]
	}
	out, err := debugbundle.GenerateBundle(agentBin, ctx, time.Now())
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to generate debug bundle:", err)
		return 1
	}
	fmt.Println(out)
	fmt.Fprintln(os.Stderr, "Review the bundle before sharing — it may contain sensitive data.")
	return 0
}
