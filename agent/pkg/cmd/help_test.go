package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// The kairos dispatcher only accepts its own sub-tools (agent, provider, ...)
// as the first argument, so help text must name the sub-tool.
func TestHelpTextNamesKairosSubTool(t *testing.T) {
	stale := []string{
		"kairos upgrade list-releases",
		`"kairos register"`,
		`"kairos bridge"`,
		`"kairos recovery"`,
	}

	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for field, text := range map[string]string{"Short": c.Short, "Long": c.Long, "Example": c.Example} {
			for _, s := range stale {
				if strings.Contains(text, s) {
					t.Errorf("%s %s mentions %s, which the kairos dispatcher rejects", c.CommandPath(), field, s)
				}
			}
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(NewCommand())
}
