package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kairos-io/kairos/v4/sdk/agentrun"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"

	"github.com/kairos-io/kairos/v4/installer/internal/debugbundle"
	"github.com/kairos-io/kairos/v4/installer/internal/mcp"
	"github.com/kairos-io/kairos/v4/installer/internal/tui"
)

func main() {
	source := flag.String("source", "", "installation source (passed through to kairos-agent)")
	collect := flag.Bool("collect-debug-bundle", false,
		"collect a debug bundle non-interactively (no TUI), print its path, and exit")
	mcpAddress := flag.String("mcp-address", mcp.DefaultListenAddress,
		"address the Model Context Protocol server listens on, so an agent can drive the installation; empty disables it")
	flag.Parse()

	if *collect {
		os.Exit(collectDebugBundle())
	}

	logger := sdkLogger.NewKairosLoggerWithExtraDirs("installer", "info", true, "/var/log/kairos/")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The MCP server is a frontend on the same install contract as the TUI, and
	// runs alongside it the way the web UI does. It must never write to the
	// terminal the TUI is drawing on, which is why it logs to the installer log.
	if *mcpAddress != "" {
		go func() {
			if err := mcp.ListenAndServe(ctx, logger, *mcpAddress); err != nil {
				// A port that will not bind leaves the TUI perfectly usable, so
				// this is logged rather than taken as a reason to give up.
				logger.Logger.Error().Err(err).Str("address", *mcpAddress).Msg("MCP server stopped")
			}
		}()
	}

	p := tea.NewProgram(tui.InitialModel(&logger, *source), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
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
