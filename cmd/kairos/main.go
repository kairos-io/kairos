// Command kairos is the multi-call binary for the Kairos device stack.
//
// It dispatches to a sub-tool based on argv[0]:
//
//	immucore                         -> immucore boot flow
//	agent                            -> kairos-agent CLI
//	kcrypt                           -> kcrypt discovery client
//	kairos <sub> [args...]           -> same as invoking the sub directly
//
// Which subs are linked in depends on build tags. See register_*.go for the
// per-sub wiring and the tag that gates it.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/internal/version"
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

// versionOwners maps a canonical sub name to a predicate that says which
// version-query arguments that sub answers itself. The dispatcher hands those
// to the sub unchanged rather than printing its own version line. A sub with
// no entry has every version query answered by the dispatcher, which is what
// keeps `immucore version` from starting the boot flow.
var versionOwners = map[string]func(arg string) bool{}

// ownsVersion declares that sub name answers the version queries owns accepts.
func ownsVersion(name string, owns func(arg string) bool) {
	versionOwners[name] = owns
}

func main() {
	os.Exit(dispatch(os.Args, os.Stdout))
}

// dispatch picks the sub-tool for args, shapes os.Args for it and runs it,
// returning the process exit code.
func dispatch(args []string, stdout io.Writer) int {
	exe := filepath.Base(args[0])

	// argv[0] == "kairos": treat argv[1] as the sub name and shift args left
	// so the sub sees a normal os.Args.
	if exe == "kairos" {
		if len(args) < 2 {
			usage()
			return 2
		}
		// Top-level version query (`kairos --version`), no sub chosen yet.
		// The monorepo reports one version everywhere, injected at build
		// time into internal/version.Version.
		if isVersionRequest(args[1]) {
			fmt.Fprintln(stdout, "kairos", version.Version)
			return 0
		}
		args = append([]string{args[1]}, args[2:]...)
		exe = args[0]
	}

	if canonical, ok := aliases[exe]; ok {
		exe = canonical
	}

	// Version query for a chosen sub, symlink form (`immucore --version`) or
	// explicit form (`kairos kcrypt --version`). A sub that has its own
	// version command, like `kairos-agent version --long`, gets the
	// arguments instead.
	if len(args) >= 2 && isVersionRequest(args[1]) && !subOwnsVersion(exe, args[1]) {
		fmt.Fprintln(stdout, "kairos", version.Version)
		return 0
	}

	run, ok := subs[exe]
	if !ok {
		fmt.Fprintf(os.Stderr, "kairos: unknown sub-tool %q\n", exe)
		usage()
		return 2
	}
	os.Args = args
	return run()
}

func subOwnsVersion(sub, arg string) bool {
	owns, ok := versionOwners[sub]
	return ok && owns(arg)
}

func isVersionRequest(arg string) bool {
	switch arg {
	case "--version", "-v", "version":
		return true
	}
	return false
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
