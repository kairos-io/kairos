package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/kairos-io/kairos/v4/sdk/bus"
)

// `kairos provider ...` hands its arguments to the installed provider plugin.
//
// A provider is an executable named agent-provider-* under one of the bus's
// search paths. The agent talks to it over the go-pluggable event protocol, but
// provider-kairos also answers as an ordinary CLI when argv[1] is not an event
// name, which is where `get-kubeconfig`, `role`, `bridge` and the rest live.
// Before the multi-call binary those commands were reachable as `kairos <cmd>`
// because /usr/bin/kairos *was* provider-kairos; /system/providers is not on
// PATH, so after the monorepo rollout they were reachable only by absolute
// path (kairos-io/kairos#4393).
//
// This is a pass-through and deliberately not a contract: the bus only ever
// asks a provider to answer events, so nothing here requires a provider to
// implement any particular command. Whatever the plugin makes of the arguments
// is its own business, and a provider with no CLI simply prints its own error.
//
// Running two providers side by side is not supported today; that is what
// kairos-io/kairos#3926 is about. Rather than guess, resolveProvider refuses
// when it finds more than one. If #3926 lands multi-provider, an explicit
// `kairos provider <name> <args...>` form fits underneath this one without
// changing what `kairos provider` already means.
func runProvider() int {
	return runProviderIn(bus.DefaultProviderPaths, os.Args)
}

// execProvider is syscall.Exec, indirected so tests can observe what would be
// run without replacing the test binary.
var execProvider = syscall.Exec

func runProviderIn(dirs []string, args []string) int {
	path, err := resolveProvider(dirs)
	if err != nil {
		fmt.Fprintln(os.Stderr, "kairos provider:", err)
		return 1
	}

	// Hand the process over rather than wrapping it: the provider's commands
	// are interactive (register takes a screenshot, bridge holds a VPN open),
	// so it needs the terminal and the signals, and its exit code has to be
	// ours. args[0] is "provider" here, having been shifted by the dispatcher;
	// the plugin is told its own name instead.
	argv := append([]string{filepath.Base(path)}, args[1:]...)
	if err := execProvider(path, argv, os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "kairos provider: cannot run %s: %v\n", path, err)
		return 1
	}
	return 0 // not reached; Exec replaces the process
}

// resolveProvider returns the path of the single installed provider plugin,
// searching dirs in order. Directories that do not exist are skipped, since a
// core image has no /system/providers at all.
func resolveProvider(dirs []string) (string, error) {
	prefix := bus.DefaultProviderPrefix + "-"

	var paths []string
	seen := map[string]bool{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if !strings.HasPrefix(name, prefix) || seen[name] {
				continue
			}
			info, err := e.Info()
			if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
				continue
			}
			seen[name] = true
			paths = append(paths, filepath.Join(dir, name))
		}
	}

	switch len(paths) {
	case 1:
		return paths[0], nil
	case 0:
		return "", fmt.Errorf(
			"no provider installed (looked in %s). Providers ship in Kairos standard images, not core ones",
			strings.Join(dirs, ", "))
	default:
		names := make([]string, 0, len(paths))
		for _, p := range paths {
			names = append(names, filepath.Base(p))
		}
		sort.Strings(names)
		return "", fmt.Errorf(
			"several providers installed (%s); running more than one is not supported yet, so invoke the one you want by its own path",
			strings.Join(names, ", "))
	}
}
