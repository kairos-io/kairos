package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"

	"github.com/kairos-io/kairos-installer/internal/tui"
)

func main() {
	source := flag.String("source", "", "installation source (passed through to kairos-agent)")
	flag.Parse()

	logger := sdkLogger.NewKairosLogger("installer", "info", true)
	p := tea.NewProgram(tui.InitialModel(&logger, *source), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
