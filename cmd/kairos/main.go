// Command kairos is the multi-call binary for the Kairos device stack.
//
// Every sub-tool is a subcommand of the kairos root command:
//
//	kairos immucore                  -> immucore boot flow
//	kairos agent                     -> kairos-agent CLI
//	kairos kcrypt                    -> kcrypt discovery client
//
// A symlink named after a sub-tool, or after one of its aliases, runs that
// sub-tool the way a busybox applet does: argv[0] "immucore" or "init" is
// read as `kairos immucore`. See register_*.go for each sub-tool's wiring and
// the names it answers to.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/internal/version"
	"github.com/spf13/cobra"
)

// subEntrypoint runs a sub-tool that reads its own command line from os.Args
// and returns a process exit code.
type subEntrypoint func() int

// subTool is a sub-tool registered with the root: a constructor for its
// command, and how it reports a fatal error. A nil report writes the error to
// stderr.
type subTool struct {
	newCommand func() *cobra.Command
	report     func(error)
}

// subTools holds every sub-tool linked into this build. Each register_*.go
// adds its own from init().
var subTools []subTool

// register adds a sub-tool to the root. newCommand returns the sub-tool's
// command, already carrying its canonical name and its aliases.
func register(newCommand func() *cobra.Command, report func(error)) {
	subTools = append(subTools, subTool{newCommand: newCommand, report: report})
}

// exitCode is the error a passthrough sub-tool returns to make the process
// exit with its own non-zero code. It has already reported the failure.
type exitCode int

func (e exitCode) Error() string {
	return fmt.Sprintf("exit status %d", int(e))
}

// usageError is a kairos invocation that names no sub-tool, or one that is not
// linked in. It exits 2 with the usage.
type usageError struct {
	err error
}

func (e usageError) Error() string {
	return e.err.Error()
}

func main() {
	os.Exit(run(os.Args))
}

// run runs the kairos command line in args, shaped as os.Args, and returns the
// process exit code.
func run(args []string) int {
	args = argv0Args(args)

	// Version query, in any of its forms: `kairos --version`, the symlink form
	// `immucore --version`, and the explicit form `kairos agent --version`. The
	// monorepo reports one version everywhere, injected at build time into
	// internal/version.Version, so the sub-tool is never asked.
	if (len(args) >= 2 && isVersionRequest(args[1])) || (len(args) >= 3 && isVersionRequest(args[2])) {
		fmt.Println("kairos", version.Version)
		return 0
	}

	root, reports := newRoot()
	root.SetArgs(args[1:])
	cmd, err := root.ExecuteC()
	if err == nil {
		return 0
	}

	var code exitCode
	if errors.As(err, &code) {
		return int(code)
	}

	var usage usageError
	if errors.As(err, &usage) {
		fmt.Fprintln(os.Stderr, "kairos:", usage.err)
		_ = root.Usage()
		return 2
	}

	if report := reports[subToolOf(cmd)]; report != nil {
		report(err)
	} else {
		fmt.Fprintln(os.Stderr, err)
	}
	return 1
}

// argv0Args returns args as a `kairos <sub-tool> [args...]` command line. When
// the binary runs under another name, through a symlink such as
// /usr/bin/immucore, that name is the sub-tool, and the root resolves it like
// any other subcommand name or alias.
func argv0Args(args []string) []string {
	exe := filepath.Base(args[0])
	if exe == "kairos" {
		return args
	}
	return append([]string{"kairos", exe}, args[1:]...)
}

func isVersionRequest(arg string) bool {
	switch arg {
	case "--version", "-v", "version":
		return true
	}
	return false
}

// newRoot builds the kairos root command, with every registered sub-tool as a
// subcommand. It also returns how each sub-tool reports a fatal error, keyed
// by the sub-tool's command.
func newRoot() (*cobra.Command, map[*cobra.Command]func(error)) {
	root := &cobra.Command{
		Use:   "kairos",
		Short: "Multi-call binary for the Kairos device stack",
		// run reports every error itself, in the way the sub-tool that
		// failed reports its own.
		SilenceErrors:     true,
		SilenceUsage:      true,
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 {
				return usageError{fmt.Errorf("unknown sub-tool %q", args[0])}
			}
			return nil
		},
		RunE: func(*cobra.Command, []string) error {
			if len(subTools) == 0 {
				return usageError{errors.New("no sub-tools linked in this build")}
			}
			return usageError{errors.New("no sub-tool given")}
		},
	}

	reports := map[*cobra.Command]func(error){}
	for _, tool := range subTools {
		cmd := tool.newCommand()
		addHelpCommand(cmd)
		root.AddCommand(cmd)
		reports[cmd] = tool.report
	}
	return root, reports
}

// addHelpCommand gives a sub-tool with subcommands its own `help [command]`.
// Cobra adds one only to the root of a tree, so without this
// `kairos-agent help upgrade` would stop working once the agent is a
// subcommand of kairos rather than a binary of its own.
func addHelpCommand(cmd *cobra.Command) {
	if !cmd.HasSubCommands() {
		return
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "help [command]",
		Short: "Help about any command",
		RunE: func(_ *cobra.Command, args []string) error {
			target, _, err := cmd.Find(args)
			if err != nil {
				return err
			}
			return target.Help()
		},
	})
}

// subToolOf returns the sub-tool command that cmd belongs to, or nil when cmd
// is the root itself.
func subToolOf(cmd *cobra.Command) *cobra.Command {
	for cmd != nil && cmd.HasParent() && cmd.Parent().HasParent() {
		cmd = cmd.Parent()
	}
	if cmd == nil || !cmd.HasParent() {
		return nil
	}
	return cmd
}

// passthrough wraps a sub-tool that parses its own command line in a
// subcommand of the root. Flag parsing is off, so everything after the
// sub-tool's name, --help included, reaches it untouched, and its exit code
// is the process's.
func passthrough(name, short string, run subEntrypoint, aliases ...string) func() *cobra.Command {
	return func() *cobra.Command {
		return &cobra.Command{
			Use:                name,
			Short:              short,
			Aliases:            aliases,
			DisableFlagParsing: true,
			RunE: func(cmd *cobra.Command, args []string) error {
				// The sub-tool sees os.Args as if it had been invoked
				// directly, under the name it was called by.
				os.Args = append([]string{cmd.CalledAs()}, args...)
				if code := run(); code != 0 {
					return exitCode(code)
				}
				return nil
			},
		}
	}
}
