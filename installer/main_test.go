package main

import (
	"flag"
	"os"
	"testing"
)

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
