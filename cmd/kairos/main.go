// Command kairos is the multi-call binary for the Kairos device stack.
//
// It dispatches to a sub-tool based on argv[0]:
//
//	immucore                         -> immucore boot flow
//	kairos-agent                     -> kairos-agent CLI
//	kcrypt-discovery-challenger      -> kcrypt discovery client
//	kairos <sub> [args...]           -> same as invoking the sub directly
//
// Which subs are linked in depends on build tags. See register_*.go for the
// per-sub wiring and the tag that gates it.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos/internal/version"
)

// subEntrypoint runs a sub-tool. os.Args is expected to be already shaped as
// if the sub-tool were invoked directly (os.Args[0] = the sub's own name).
type subEntrypoint func() int

// subs holds the canonical names shown in usage. aliases maps alternate names
// (typically the historical argv[0] names used by symlinks, e.g. "kairos-agent")
// to their canonical short form.
var (
	subs    = map[string]subEntrypoint{}
	aliases = map[string]string{}
)

// register wires a canonical short name to a run func, plus any number of
// alternate names that dispatch to the same sub-tool. Only the canonical name
// is listed in usage.
func register(name string, run subEntrypoint, alts ...string) {
	subs[name] = run
	for _, a := range alts {
		aliases[a] = name
	}
}

func main() {
	exe := filepath.Base(os.Args[0])

	// argv[0] == "kairos": handle top-level flags, then treat argv[1] as the
	// sub name and shift args left so the sub sees a normal os.Args.
	if exe == "kairos" {
		if len(os.Args) < 2 {
			usage()
			os.Exit(2)
		}
		switch os.Args[1] {
		case "--version", "-v", "version":
			fmt.Println("kairos", version.Version)
			os.Exit(0)
		}
		sub := os.Args[1]
		os.Args = append([]string{sub}, os.Args[2:]...)
		exe = sub
	}

	if canonical, ok := aliases[exe]; ok {
		exe = canonical
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
