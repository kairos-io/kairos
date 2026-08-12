// Package main is the monorepo-spike multi-call binary for Kairos.
//
// The binary dispatches to a sub-tool based on argv[0]:
//
//	immucore                         -> immucore boot flow
//	kairos-agent                     -> kairos-agent CLI
//	kcrypt-discovery-challenger      -> kcrypt discovery client
//	kairos <sub> [args...]           -> same as invoking the sub directly
//
// Sub-tool wiring is added incrementally as each source repo extracts its
// cobra/urfave root into an importable package.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	immucore "github.com/kairos-io/immucore/pkg/cmd"
)

func init() {
	register("immucore", immucore.Run)
}

// subEntrypoint runs a sub-tool. os.Args is expected to be already shaped as
// if the sub-tool were invoked directly (os.Args[0] = the sub's own name).
type subEntrypoint func() int

var subs = map[string]subEntrypoint{}

func register(name string, run subEntrypoint) {
	subs[name] = run
}

func main() {
	exe := filepath.Base(os.Args[0])

	// argv[0] == "kairos": treat argv[1] as the sub name and shift args left
	// so the sub sees a normal os.Args.
	if exe == "kairos" {
		if len(os.Args) < 2 {
			usage()
			os.Exit(2)
		}
		sub := os.Args[1]
		os.Args = append([]string{sub}, os.Args[2:]...)
		exe = sub
	}

	run, ok := subs[exe]
	if !ok {
		fmt.Fprintf(os.Stderr, "kairos: unknown sub-tool %q\n", exe)
		usage()
		os.Exit(2)
	}
	os.Exit(run())
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: kairos <sub-tool> [args...]")
	if len(subs) == 0 {
		fmt.Fprintln(os.Stderr, "  (no sub-tools linked in this build)")
		return
	}
	fmt.Fprintln(os.Stderr, "sub-tools:")
	for name := range subs {
		fmt.Fprintf(os.Stderr, "  %s\n", name)
	}
}
