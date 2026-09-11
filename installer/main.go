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
	// The default is deliberately the empty string and not
	// mcp.DefaultListenAddress: this value is never read, because it is
	// replaced below whenever the flag was not passed, and flag.PrintDefaults
	// only stays quiet about a zero value. Any other default would print a
	// "(default ...)" note contradicting the help text.
	mcpAddress := flag.String("mcp-address", "",
		"address the Model Context Protocol server listens on, so an agent can drive the installation; "+
			"nothing on it is authenticated, so an empty value switches it off and \":8090\" makes it "+
			"reachable from the network. Defaults to what /etc/kairos/agent.yaml says under mcp:, "+
			"or 127.0.0.1:8090")
	flag.Parse()

	// kairos-agent execs this binary with a fixed argument list, so on a real
	// boot no flag ever arrives and the agent config is the only say an operator
	// gets. Someone running the installer by hand has said what they want, so
	// the flag wins when it is actually passed.
	if !flagWasPassed("mcp-address") {
		*mcpAddress = mcp.ListenAddressFromConfig()
	}

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

// flagWasPassed reports whether the named flag appeared on the command line, as
// opposed to holding its default. flag.Visit walks only the flags that were set.
func flagWasPassed(name string) bool {
	passed := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			passed = true
		}
	})

	return passed
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
