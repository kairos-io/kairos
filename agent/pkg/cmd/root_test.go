package cmd

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kairos-io/kairos/v4/agent/internal/phonehome"
	agentConfig "github.com/kairos-io/kairos/v4/agent/pkg/config"
	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
	"github.com/kairos-io/kairos/v4/sdk/bundles"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	extensiontypes "github.com/kairos-io/kairos/v4/sdk/types/extensions"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/twpayne/go-vfs/v5/vfst"
	"github.com/urfave/cli/v2"
)

type failingCatalogClient struct {
	destination string
}

func TestSysextInstallUsesDefaultCatalog(t *testing.T) {
	command := sysextInstallCommand(t)
	ctx := commandContext(t, command, "sysext", "git")
	cfg := agentConfig.NewConfig()

	got := extensionCatalogURLs(ctx, cfg, "sysext")
	if len(got) != 1 || got[0] != extensiontypes.DefaultCatalogURL {
		t.Fatalf("catalog default = %q, want [%q]", got, extensiontypes.DefaultCatalogURL)
	}
	if got := extensionCatalogURLs(ctx, cfg, "confext"); got != nil {
		t.Fatalf("confext catalogs = %q, want none", got)
	}
}

func TestSysextInstallCatalogsComeFromTheConfig(t *testing.T) {
	command := sysextInstallCommand(t)
	cfg := agentConfig.NewConfig()
	cfg.Extensions.Catalogs = []string{"https://example.test/one.json", "https://example.test/two.json"}

	// With no flag, the cloud config decides, replacing the default.
	got := extensionCatalogURLs(commandContext(t, command, "sysext", "git"), cfg, "sysext")
	if len(got) != 2 || got[0] != "https://example.test/one.json" || got[1] != "https://example.test/two.json" {
		t.Fatalf("catalogs = %q, want the two configured ones in order", got)
	}

	// Repeated flags override the config, in the order they were given.
	ctx := commandContext(t, command, "sysext", "--catalog", "https://example.test/flag-a.json", "--catalog", "https://example.test/flag-b.json", "git")
	got = extensionCatalogURLs(ctx, cfg, "sysext")
	if len(got) != 2 || got[0] != "https://example.test/flag-a.json" || got[1] != "https://example.test/flag-b.json" {
		t.Fatalf("catalogs = %q, want the two flagged ones in order", got)
	}
}

func TestCatalogMissPreservesErrorForNonURI(t *testing.T) {
	fs, cleanup, err := vfst.NewTestFS(map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	client := &failingCatalogClient{}
	cfg := agentConfig.NewConfig(agentConfig.WithFs(fs), agentConfig.WithClient(client))

	err = installCatalogOrURIExtension(cfg, []string{extensiontypes.DefaultCatalogURL}, "not-a-source", "", "sysext")
	if err == nil || !strings.Contains(err.Error(), "download failed") {
		t.Fatalf("expected catalog error for a non-URI request, got %v", err)
	}
}

func (c *failingCatalogClient) GetURL(_ sdkLogger.KairosLogger, _ string, destination string) error {
	c.destination = destination
	return errors.New("download failed")
}

func sysextInstallCommand(t *testing.T) *cli.Command {
	t.Helper()
	for _, command := range cmds {
		if command.Name == "sysext" {
			for _, subcommand := range command.Subcommands {
				if subcommand.Name == "install" {
					return subcommand
				}
			}
		}
	}
	t.Fatal("sysext install command not found")
	return nil
}

func commandContext(t *testing.T, command *cli.Command, extType string, args ...string) *cli.Context {
	t.Helper()
	set := flag.NewFlagSet(command.Name, flag.ContinueOnError)
	for _, commandFlag := range command.Flags {
		if err := commandFlag.Apply(set); err != nil {
			t.Fatal(err)
		}
	}
	if err := set.Parse(args); err != nil {
		t.Fatal(err)
	}
	ctx := cli.NewContext(nil, set, nil)
	ctx.Context = context.WithValue(context.Background(), extTypeCtxKey, extType)
	return ctx
}

func TestSysextInstallCatalogValidation(t *testing.T) {
	command := sysextInstallCommand(t)

	if err := command.Action(commandContext(t, command, "confext", "--catalog", "https://example.test/catalog.json", "git")); err == nil || err.Error() != "--catalog is only supported for sysext" {
		t.Fatalf("expected confext catalog error, got %v", err)
	}
	if err := command.Action(commandContext(t, command, "confext", "--version", "1.0.0", "oci:example.test/git:1.0.0")); err == nil || err.Error() != "--version requires --catalog" {
		t.Fatalf("expected version validation error, got %v", err)
	}
}

func TestCatalogDownloadFailureDoesNotLeaveTemporaryFile(t *testing.T) {
	fs, cleanup, err := vfst.NewTestFS(map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	client := &failingCatalogClient{}
	cfg := agentConfig.NewConfig(agentConfig.WithFs(fs), agentConfig.WithClient(client))

	err = installCatalogExtension(cfg, []string{"https://example.test/catalog.json"}, "git", "")
	if err == nil || !strings.Contains(err.Error(), "download failed") {
		t.Fatalf("expected download failure, got %v", err)
	}
	if client.destination == "" {
		t.Fatal("catalog download was not attempted")
	}
	if _, statErr := cfg.Fs.Stat(client.destination); !os.IsNotExist(statErr) {
		t.Fatalf("temporary catalog was not removed: %v", statErr)
	}
}

// The edgevpn API address is chosen by the p2p provider, not by the agent.
// The agent only forwards whatever the operator passed on the command line,
// so an unset --api must reach the provider as an empty string. If the agent
// substitutes a default of its own, that default silently overrides the
// provider's and the two ends of the API disagree: the provider writes the
// agent's address into the edgevpn daemon's APILISTEN while its own CLI keeps
// defaulting to the provider's socket, so `get-kubeconfig` and `role list`
// talk to an address nothing is listening on and print empty output.
func TestStartAPIFlagHasNoDefault(t *testing.T) {
	var start *cli.Command
	for _, c := range cmds {
		if c.Name == "start" {
			start = c
			break
		}
	}
	if start == nil {
		t.Fatal("no start command registered")
	}

	var api *cli.StringFlag
	for _, f := range start.Flags {
		if sf, ok := f.(*cli.StringFlag); ok && sf.Name == "api" {
			api = sf
			break
		}
	}
	if api == nil {
		t.Fatal("start command has no --api flag")
	}

	if api.Value != "" {
		t.Fatalf("start --api default = %q, want %q so the provider picks the address", api.Value, "")
	}
}

// `start --force` has never done anything: the option was copied from
// `upgrade --force` and nothing reads it. Dropping the flag would break
// units and scripts that still pass it, so it stays accepted and says in
// --help that it is a no-op.
func TestStartForceFlagIsDeprecatedNoOp(t *testing.T) {
	var start *cli.Command
	for _, c := range cmds {
		if c.Name == "start" {
			start = c
			break
		}
	}
	if start == nil {
		t.Fatal("no start command registered")
	}

	var force *cli.BoolFlag
	for _, f := range start.Flags {
		if bf, ok := f.(*cli.BoolFlag); ok && bf.Name == "force" {
			force = bf
			break
		}
	}
	if force == nil {
		t.Fatal("start command no longer accepts --force, existing invocations would fail")
	}
	if !strings.HasPrefix(force.Usage, "Deprecated") {
		t.Fatalf("start --force usage = %q, want it to say the flag is deprecated", force.Usage)
	}
}

// interactive-install must still install unattended by default: the whole
// point of the command reading install.auto is that an ISO booted with an
// autoinstall datasource does not stop at a TUI with nobody there to answer
// it. --skip-auto-install is the documented way out, for an operator who
// booted the media to look around, so it has to default to off.
func TestSkipAutoInstallDefaultsOff(t *testing.T) {
	command := interactiveInstallCommand(t)

	var skip *cli.BoolFlag
	for _, f := range command.Flags {
		if bf, ok := f.(*cli.BoolFlag); ok && bf.Name == skipAutoInstallFlag {
			skip = bf
			break
		}
	}
	if skip == nil {
		t.Fatalf("interactive-install has no --%s flag", skipAutoInstallFlag)
	}
	if skip.Value {
		t.Fatalf("--%s default = true, want false so an unattended config still owns the boot", skipAutoInstallFlag)
	}

	if got := skipAutoInstall(commandContext(t, command, "")); got {
		t.Fatal("skipAutoInstall with no flag and no cmdline token = true, want false")
	}
	if got := skipAutoInstall(commandContext(t, command, "", "--"+skipAutoInstallFlag)); !got {
		t.Fatalf("skipAutoInstall with --%s = false, want true", skipAutoInstallFlag)
	}
}

// The kairos-interactive unit's ExecStart is fixed, so the GRUB edit line is
// the only place an operator booting an ISO can ask for this. A substring
// match would let an unrelated longer token turn it on, and would miss the
// =true spelling people reach for.
func TestSkipAutoInstallCmdlineMatchesWholeTokens(t *testing.T) {
	for _, tc := range []struct {
		cmdline string
		want    bool
	}{
		{"console=tty1 kairos.skip-auto-install rd.immucore.debug", true},
		{"kairos.skip-auto-install=true", true},
		{"kairos.skip-auto-install=1", true},
		{"console=tty1 install-mode-interactive", false},
		{"kairos.skip-auto-install-never", false},
		{"nokairos.skip-auto-install", false},
		{"kairos.skip-auto-install=false", false},
		{"", false},
	} {
		if got := cmdlineEnables(tc.cmdline, skipAutoInstallCmdline); got != tc.want {
			t.Errorf("cmdlineEnables(%q) = %v, want %v", tc.cmdline, got, tc.want)
		}
	}
}

func interactiveInstallCommand(t *testing.T) *cli.Command {
	t.Helper()
	for _, command := range cmds {
		if command.Name == "interactive-install" {
			return command
		}
	}
	t.Fatal("interactive-install command not found")
	return nil
}

// The two halves of the flag: with it, install.auto is never read and the
// installer runs; without it, install.auto is read first. Both paths end in
// the same "no installer found" error here, so the install.auto call itself is
// what distinguishes them.
func TestInteractiveInstallHonoursSkipAutoInstall(t *testing.T) {
	command := interactiveInstallCommand(t)

	original := autoInstallFn
	t.Cleanup(func() { autoInstallFn = original })

	called := false
	autoInstallFn = func(string, bool, ...string) (bool, *sdkConfig.Config, error, error) {
		called = true
		return false, nil, nil, nil
	}

	err := command.Action(commandContext(t, command, "", "--"+skipAutoInstallFlag))
	if err == nil || !strings.Contains(err.Error(), "no installer found") {
		t.Fatalf("--%s did not reach the installer, got %v", skipAutoInstallFlag, err)
	}
	if called {
		t.Fatalf("--%s still consulted install.auto", skipAutoInstallFlag)
	}

	err = command.Action(commandContext(t, command, ""))
	if err == nil || !strings.Contains(err.Error(), "no installer found") {
		t.Fatalf("interactive-install did not reach the installer, got %v", err)
	}
	if !called {
		t.Fatal("interactive-install skipped install.auto by default, which is the behaviour this command exists to have")
	}
}

// `kairos-agent webui` existed only so the kairos-webui service kept working
// while the web UI moved into the installer. The service is gone, and the
// installer serves the web UI in its own process on every interactive boot,
// so the subcommand has no caller left. The alias matters as much as the
// name: `kairos-agent w` used to reach it.
func TestNoWebUISubcommandIsRegistered(t *testing.T) {
	for _, c := range cmds {
		if c.Name == "webui" {
			t.Fatal("webui is registered again; the web UI is a frontend of the installer, run it with --no-tui")
		}
		for _, a := range c.Aliases {
			if a == "w" {
				t.Fatalf("%q took over the freed webui alias w", c.Name)
			}
		}
	}
}

// `state apply` was a registered subcommand with an empty body: it advertised
// itself in --help as applying a machine state, did nothing, and exited 0.
// Dropping the subcommand is not enough on its own, because `state` has an
// action of its own, so an unmatched argument falls through to it, prints the
// machine state and still exits 0. A caller scripting an apply cannot tell
// that from a successful one, which is the whole bug.
func TestStateRejectsAnArgumentItCannotApply(t *testing.T) {
	app := &cli.App{
		Name:      "kairos-agent",
		Commands:  cmds,
		Writer:    io.Discard,
		ErrWriter: io.Discard,
	}

	// "a" was the alias apply carried, "gte" stands for any other typo: both
	// used to reach the state dump instead of being refused.
	for _, arg := range []string{"apply", "a", "gte"} {
		err := app.Run([]string{"kairos-agent", "state", arg})
		if err == nil {
			t.Fatalf("`state %s` exited 0, so it is indistinguishable from a command that did the work", arg)
		}
		if !strings.Contains(err.Error(), arg) {
			t.Fatalf("`state %s` = %v, want an error naming %q", arg, err, arg)
		}
	}
}

// Nothing may re-register a placeholder under `state`, and `get` has to stay.
func TestStateRegistersGetAndNoApply(t *testing.T) {
	var stateCmd *cli.Command
	for _, c := range cmds {
		if c.Name == "state" {
			stateCmd = c
			break
		}
	}
	if stateCmd == nil {
		t.Fatal("no state command registered")
	}

	names := map[string]bool{}
	for _, sub := range stateCmd.Subcommands {
		names[sub.Name] = true
		for _, alias := range sub.Aliases {
			names[alias] = true
		}
	}
	if names["apply"] || names["a"] {
		t.Errorf("state advertises an apply subcommand again, subcommands %v", names)
	}
	if !names["get"] {
		t.Errorf("state lost its get subcommand, subcommands %v", names)
	}
}

func runStageCommand(t *testing.T) *cli.Command {
	t.Helper()
	for _, command := range cmds {
		if command.Name == "run-stage" {
			return command
		}
	}
	t.Fatal("run-stage command not found")
	return nil
}

// #4665: --override-cloud-init-paths advertises "removing defaults", but the
// paths it set were prepended with constants.GetCloudInitPaths() anyway, so it
// behaved exactly like --cloud-init-paths. Running one cloud-config on its own
// was not expressible, and for a stage like initramfs the machine's whole /oem
// was re-applied alongside it.
func TestRunStageOverrideDropsTheDefaults(t *testing.T) {
	command := runStageCommand(t)
	cfg := agentConfig.NewConfig()
	cfg.CloudInitPaths = []string{"/from/config"}

	ctx := commandContext(t, command, "",
		"--cloud-init-paths", "/extra",
		"--override-cloud-init-paths", "/only/this/path",
		"initramfs")

	got := runStageCloudInitPaths(ctx, cfg)
	if len(got) != 1 || got[0] != "/only/this/path" {
		t.Fatalf("override paths = %v, want [/only/this/path]", got)
	}
	for _, unwanted := range append(constants.GetCloudInitPaths(), "/from/config", "/extra") {
		for _, p := range got {
			if p == unwanted {
				t.Fatalf("override kept %q; got %v", unwanted, got)
			}
		}
	}
}

// The other half of the contract: --cloud-init-paths still means "the defaults
// plus these", so nothing changes for anyone already using it.
func TestRunStageExtraPathsKeepTheDefaults(t *testing.T) {
	command := runStageCommand(t)
	cfg := agentConfig.NewConfig()
	cfg.CloudInitPaths = []string{"/from/config"}

	ctx := commandContext(t, command, "", "--cloud-init-paths", "/extra", "initramfs")

	got := runStageCloudInitPaths(ctx, cfg)
	for _, wanted := range append(constants.GetCloudInitPaths(), "/from/config", "/extra") {
		found := false
		for _, p := range got {
			if p == wanted {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing %q from %v", wanted, got)
		}
	}
}

// With neither flag the stage reads the defaults plus whatever the scanned
// config contributed, which is what every other RunStage caller gets.
func TestRunStageWithoutFlagsUsesDefaults(t *testing.T) {
	command := runStageCommand(t)
	cfg := agentConfig.NewConfig()
	cfg.CloudInitPaths = []string{"/from/config"}

	ctx := commandContext(t, command, "", "initramfs")

	got := runStageCloudInitPaths(ctx, cfg)
	want := append(constants.GetCloudInitPaths(), "/from/config")
	if len(got) != len(want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("paths = %v, want %v", got, want)
		}
	}
}

// listReleasesCommand returns the registered "upgrade list-releases" command,
// so that the tests exercise the command the binary actually serves rather than
// a copy of it.
func listReleasesCommand(t *testing.T) *cli.Command {
	t.Helper()

	for _, c := range cmds {
		if c.Name != "upgrade" {
			continue
		}
		for _, sub := range c.Subcommands {
			if sub.Name == "list-releases" {
				return sub
			}
		}
	}

	t.Fatal("no upgrade list-releases command registered")
	return nil
}

func TestParseReleasesOutputAcceptsTheDocumentedFormats(t *testing.T) {
	for in, want := range map[string]string{
		"":         outputFormatTerminal,
		"terminal": outputFormatTerminal,
		"json":     outputFormatJSON,
		"yaml":     outputFormatYAML,
		"JSON":     outputFormatJSON,
		"Yaml":     outputFormatYAML,
	} {
		got, err := parseReleasesOutput(in)
		if err != nil {
			t.Fatalf("parseReleasesOutput(%q) returned %v, want no error", in, err)
		}
		if got != want {
			t.Fatalf("parseReleasesOutput(%q) = %q, want %q", in, got, want)
		}
	}
}

// A typo in a script has to be visible. Falling back to the terminal format
// would hand the caller output it cannot parse and an exit status of 0.
func TestParseReleasesOutputRefusesAnUnknownFormat(t *testing.T) {
	got, err := parseReleasesOutput("jsonn")
	if err == nil {
		t.Fatalf("parseReleasesOutput(\"jsonn\") = %q with no error, want it refused", got)
	}
	if !strings.Contains(err.Error(), "jsonn") {
		t.Fatalf("error %v does not name the value the caller passed", err)
	}
}

// The same validation has to run before anything is printed, which is why it
// sits in the command's Before hook.
func TestListReleasesRefusesAnUnknownOutputFormatBeforeRunning(t *testing.T) {
	command := listReleasesCommand(t)

	if command.Before == nil {
		t.Fatal("list-releases lost its Before hook, so --output is no longer validated")
	}
	if err := command.Before(commandContext(t, command, "", "--output", "jsonn")); err == nil {
		t.Fatal("list-releases accepted --output jsonn, so an unparsable format reaches the caller as terminal output")
	}
	if err := command.Before(commandContext(t, command, "", "--output", "json")); err != nil {
		t.Fatalf("list-releases refused --output json: %v", err)
	}
}

// The regression this guards: the flag was declared and documented for three
// years while nothing read it.
func TestListReleasesStillDeclaresTheOutputFlag(t *testing.T) {
	command := listReleasesCommand(t)

	ctx := commandContext(t, command, "", "--output", "yaml")
	if got := ctx.String("output"); got != "yaml" {
		t.Fatalf("list-releases --output parsed as %q, want %q", got, "yaml")
	}
}

func TestReleasesToOutputRendersEachFormat(t *testing.T) {
	rels := []string{"v3.5.0", "v3.4.2"}

	if got := ReleasesToOutput(rels, outputFormatTerminal); len(got) != 2 || got[0] != "v3.5.0" || got[1] != "v3.4.2" {
		t.Fatalf("terminal format = %q, want one release per element", got)
	}

	got := ReleasesToOutput(rels, outputFormatJSON)
	if len(got) != 1 || got[0] != `["v3.5.0","v3.4.2"]` {
		t.Fatalf("json format = %q, want a single JSON document", got)
	}

	got = ReleasesToOutput(rels, outputFormatYAML)
	if len(got) != 1 || got[0] != "- v3.5.0\n- v3.4.2\n" {
		t.Fatalf("yaml format = %q, want a single YAML document", got)
	}
}

// An empty result is still a list. Marshalling the nil slice straight through
// gives "null", which a caller ranging over the document cannot use.
func TestReleasesToOutputRendersAnEmptyListAsAList(t *testing.T) {
	if got := ReleasesToOutput(nil, outputFormatJSON); len(got) != 1 || got[0] != "[]" {
		t.Fatalf("json format of no releases = %q, want [\"[]\"]", got)
	}
	if got := ReleasesToOutput(nil, outputFormatYAML); len(got) != 1 || got[0] != "[]\n" {
		t.Fatalf("yaml format of no releases = %q, want [\"[]\\n\"]", got)
	}
}

func TestPrintReleasesWritesOnePerLineOnTheTerminal(t *testing.T) {
	var out strings.Builder

	printReleases(&out, []string{"v3.5.0", "v3.4.2"}, outputFormatTerminal)

	if out.String() != "v3.5.0\nv3.4.2\n" {
		t.Fatalf("terminal output = %q, want one release per line", out.String())
	}
}

func TestPrintReleasesWritesOneDocumentForAMachineFormat(t *testing.T) {
	var out strings.Builder

	printReleases(&out, []string{"v3.5.0", "v3.4.2"}, outputFormatJSON)

	if out.String() != "[\"v3.5.0\",\"v3.4.2\"]\n" {
		t.Fatalf("json output = %q, want a single JSON document", out.String())
	}
}

// The headers and warnings are useful to a person and are noise in the middle
// of a document, so a machine readable format moves them off stdout.
func TestReleasesNotesKeepsStdoutOnlyForTheTerminalFormat(t *testing.T) {
	if got := releasesNotes(outputFormatTerminal); got != os.Stdout {
		t.Fatal("the terminal format stopped writing its notes to stdout")
	}
	for _, format := range []string{outputFormatJSON, outputFormatYAML} {
		if got := releasesNotes(format); got != os.Stderr {
			t.Fatalf("the %s format writes its notes to stdout, which corrupts the document", format)
		}
	}
}

// The provider branch is the one the Action returns from early, so it has its
// own rendering path and its own chance to lose the format again.
func TestPrintProviderReleasesRendersTheRequestedFormat(t *testing.T) {
	var out, notes strings.Builder

	if !printProviderReleases(&out, &notes, []string{"v3.5.0", "v3.4.2"}, outputFormatJSON) {
		t.Fatal("expected printProviderReleases to report that it printed something")
	}
	if out.String() != "[\"v3.5.0\",\"v3.4.2\"]\n" {
		t.Fatalf("stdout = %q, want only the JSON document", out.String())
	}
	if notes.String() != "Available releases from provider:\n" {
		t.Fatalf("notes = %q, want only the header", notes.String())
	}
}

// Every release the providers answer with has to be listed, and the header
// belongs with the releases on a terminal.
func TestPrintProviderReleasesListsEveryReleaseOnTheTerminal(t *testing.T) {
	var out strings.Builder

	if !printProviderReleases(&out, &out, []string{"v3.5.0", "v3.4.2", "v3.4.1"}, outputFormatTerminal) {
		t.Fatal("expected printProviderReleases to report that it printed something")
	}
	if out.String() != "Available releases from provider:\nv3.5.0\nv3.4.2\nv3.4.1\n" {
		t.Fatalf("terminal output = %q, want the header and every release", out.String())
	}
}

func TestPrintProviderReleasesWithoutReleasesFallsThrough(t *testing.T) {
	var out, notes strings.Builder

	if printProviderReleases(&out, &notes, nil, outputFormatJSON) {
		t.Fatal("expected printProviderReleases to report that it printed nothing")
	}
	if out.String() != "" || notes.String() != "" {
		t.Fatalf("expected no output, got stdout %q and notes %q", out.String(), notes.String())
	}
}

// TestKcryptNVIndexFlagLeavesRoomForTheConfig guards the precedence the three
// kcrypt NV subcommands document, "explicit flag > config > default", at the
// entry point that decides it. The flag used to carry
// kcrypt.DefaultLocalPassphraseNVIndex as its Value, so c.String("nv-index")
// was never empty and action.resolveNVIndexAndDevice could never reach its
// config branch: a node setting kcrypt.nv_index had checknv, readnv and
// cleanupnv silently operate on 0x1500000. The default is applied by the
// resolver instead, which is the only place that can see the config.
// See kairos-io/kairos#5183.
func TestKcryptNVIndexFlagLeavesRoomForTheConfig(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "unset", args: []string{"kairos-agent"}, want: ""},
		{name: "given", args: []string{"kairos-agent", "--nv-index", "0x1500001"}, want: "0x1500001"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			app := &cli.App{
				Flags:  []cli.Flag{&kcryptNVIndexFlag},
				Action: func(c *cli.Context) error { got = c.String("nv-index"); return nil },
			}
			if err := app.Run(tc.args); err != nil {
				t.Fatalf("running the app: %v", err)
			}
			if got != tc.want {
				t.Fatalf("c.String(%q) = %q, want %q", "nv-index", got, tc.want)
			}
		})
	}
}

// `upgrade --dry-run` must be registered as a bool flag that defaults to off,
// so a plain `upgrade` keeps running the real upgrade.
func TestUpgradeRegistersDryRunFlag(t *testing.T) {
	var upgradeCmd *cli.Command
	for _, c := range cmds {
		if c.Name == "upgrade" {
			upgradeCmd = c
			break
		}
	}
	if upgradeCmd == nil {
		t.Fatal("no upgrade command registered")
	}

	for _, f := range upgradeCmd.Flags {
		for _, name := range f.Names() {
			if name != "dry-run" {
				continue
			}
			bf, ok := f.(*cli.BoolFlag)
			if !ok {
				t.Fatalf("dry-run is a %T, want *cli.BoolFlag", f)
			}
			if bf.Value {
				t.Fatal("dry-run defaults to true, a plain upgrade would not upgrade")
			}
			return
		}
	}
	t.Fatal("upgrade does not register a --dry-run flag")
}

func TestReleaseCollectorKeepsEveryProvidersReleases(t *testing.T) {
	rc := &releaseCollector{}
	rc.collect(`["v3.5.0","v3.4.2"]`)
	rc.collect(`["v3.3.1"]`)

	got := rc.releases()
	want := []string{"v3.5.0", "v3.4.2", "v3.3.1"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestReleaseCollectorSkipsEmptyAndUnreadableResponses(t *testing.T) {
	rc := &releaseCollector{}
	rc.collect("")
	rc.collect("not json")
	rc.collect(`["v3.5.0"]`)

	got := rc.releases()
	if len(got) != 1 || got[0] != "v3.5.0" {
		t.Fatalf("expected only the readable response to be kept, got %v", got)
	}
}

// Providers answer from their own goroutines, so the collector has to be safe
// to call concurrently. Run with -race for this to mean anything.
func TestReleaseCollectorIsSafeForConcurrentProviders(t *testing.T) {
	rc := &releaseCollector{}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rc.collect(fmt.Sprintf(`["v3.%d.0","v3.%d.1"]`, i, i))
		}(i)
	}
	wg.Wait()

	if got := rc.releases(); len(got) != 16 {
		t.Fatalf("expected the releases of all 8 providers, got %d: %v", len(got), got)
	}
}

func phoneHomeCommand(t *testing.T) *cli.Command {
	t.Helper()
	for _, command := range cmds {
		if command.Name == "phone-home" {
			return command
		}
	}
	t.Fatal("phone-home command not found")
	return nil
}

func phoneHomeContext(t *testing.T, args ...string) *cli.Context {
	t.Helper()
	command := phoneHomeCommand(t)
	set := flag.NewFlagSet(command.Name, flag.ContinueOnError)
	for _, commandFlag := range command.Flags {
		if err := commandFlag.Apply(set); err != nil {
			t.Fatal(err)
		}
	}
	if err := set.Parse(args); err != nil {
		t.Fatal(err)
	}
	return cli.NewContext(nil, set, nil)
}

// The unit the agent installs runs "kairos-agent phone-home" with no flags, so
// a heartbeat-interval taken from the flag's default would overwrite the value
// cloud-config carries and phonehome.heartbeat_interval would never reach the
// client.
func TestPhoneHomeKeepsTheConfiguredHeartbeatWhenTheFlagIsAbsent(t *testing.T) {
	cfg := phonehome.Config{
		URL:               "ws://configured.test",
		RegistrationToken: "config-token",
		Group:             "config-group",
		HeartbeatInterval: 5 * time.Minute,
		ReconnectBackoff:  time.Minute,
	}

	MergePhoneHomeFlags(phoneHomeContext(t), &cfg)

	if cfg.HeartbeatInterval != 5*time.Minute {
		t.Fatalf("heartbeat interval = %s, want the configured 5m", cfg.HeartbeatInterval)
	}
	if cfg.URL != "ws://configured.test" || cfg.RegistrationToken != "config-token" || cfg.Group != "config-group" {
		t.Fatalf("string fields were overwritten: %+v", cfg)
	}
	if cfg.ReconnectBackoff != time.Minute {
		t.Fatalf("reconnect backoff = %s, want the configured 1m", cfg.ReconnectBackoff)
	}
}

func TestPhoneHomeFlagsOverrideTheConfiguredValues(t *testing.T) {
	cfg := phonehome.Config{
		URL:               "ws://configured.test",
		RegistrationToken: "config-token",
		Group:             "config-group",
		HeartbeatInterval: 5 * time.Minute,
	}

	MergePhoneHomeFlags(phoneHomeContext(t,
		"--url", "ws://flag.test",
		"--token", "flag-token",
		"--group", "flag-group",
		"--heartbeat-interval", "90s",
	), &cfg)

	if cfg.URL != "ws://flag.test" || cfg.RegistrationToken != "flag-token" || cfg.Group != "flag-group" {
		t.Fatalf("string flags did not win: %+v", cfg)
	}
	if cfg.HeartbeatInterval != 90*time.Second {
		t.Fatalf("heartbeat interval = %s, want the 90s from the flag", cfg.HeartbeatInterval)
	}
}

// With nothing on either side the client is the one that picks the default, so
// the merge must leave the zero value in place rather than copy the flag's.
func TestPhoneHomeLeavesTheHeartbeatZeroWhenNobodySetsIt(t *testing.T) {
	cfg := phonehome.Config{URL: "ws://configured.test"}

	MergePhoneHomeFlags(phoneHomeContext(t), &cfg)

	if cfg.HeartbeatInterval != 0 {
		t.Fatalf("heartbeat interval = %s, want zero so the client applies its default", cfg.HeartbeatInterval)
	}
}

// A phonehome: section with a type error used to be dropped, so the command
// reported "phonehome URL required" for a config whose url was set.
func TestLoadPhoneHomeConfigReportsAParseError(t *testing.T) {
	scanned := &sdkConfig.Config{Collector: collector.Config{Values: collector.ConfigValues{
		"phonehome": map[string]interface{}{
			"url":                "http://127.0.0.1:1",
			"heartbeat_interval": 30,
		},
	}}}

	_, err := LoadPhoneHomeConfig(scanned)
	if err == nil || !strings.Contains(err.Error(), "time.Duration") {
		t.Fatalf("err = %v, want the heartbeat_interval parse error", err)
	}
}

func TestLoadPhoneHomeConfigReadsAValidSection(t *testing.T) {
	scanned := &sdkConfig.Config{Collector: collector.Config{Values: collector.ConfigValues{
		"phonehome": map[string]interface{}{
			"url":                "http://127.0.0.1:1",
			"heartbeat_interval": "30s",
		},
	}}}

	cfg, err := LoadPhoneHomeConfig(scanned)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.URL != "http://127.0.0.1:1" || cfg.HeartbeatInterval != 30*time.Second {
		t.Fatalf("cfg = %+v, want the configured url and 30s", cfg)
	}
}

func TestLoadPhoneHomeConfigWithoutASectionIsEmpty(t *testing.T) {
	cfg, err := LoadPhoneHomeConfig(&sdkConfig.Config{Collector: collector.Config{Values: collector.ConfigValues{}}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.URL != "" {
		t.Fatalf("cfg = %+v, want an empty config", cfg)
	}
}

func installBundleCommand(t *testing.T) *cli.Command {
	t.Helper()
	for _, command := range cmds {
		if command.Name == "install-bundle" {
			return command
		}
	}
	t.Fatal("install-bundle command not found")
	return nil
}

func TestInstallBundleExamplesAreValidTargets(t *testing.T) {
	command := installBundleCommand(t)
	knownSchemes := map[string]bool{"container": true, "docker": true, "run": true, "package": true}

	examples := 0
	localFileExample := false
	for _, line := range strings.Split(command.Description, "\n") {
		_, example, found := strings.Cut(line, "kairos-agent install-bundle ")
		if !found {
			continue
		}
		examples++

		ctx := commandContext(t, command, "", strings.Fields(example)...)
		if ctx.Args().Len() != 1 {
			t.Errorf("example %q leaves %d positional arguments, want 1", line, ctx.Args().Len())
			continue
		}
		config := bundles.BundleConfig{Target: ctx.Args().First()}
		scheme, err := config.TargetScheme()
		if err != nil {
			t.Errorf("example %q has an invalid target: %v", line, err)
			continue
		}
		if !knownSchemes[scheme] {
			t.Errorf("example %q uses scheme %q, want one of %v", line, scheme, knownSchemes)
		}
		if ctx.Bool("local-file") {
			localFileExample = true
		}
	}

	if examples == 0 {
		t.Fatal("install-bundle description has no examples")
	}
	if !localFileExample {
		t.Error("install-bundle description has no --local-file example")
	}
}

func TestInstallBundleFlagsAfterTargetAreNotParsed(t *testing.T) {
	command := installBundleCommand(t)

	ctx := commandContext(t, command, "", "container:///absolute/path/mybundle.tar", "--local-file")
	if ctx.Bool("local-file") {
		t.Fatal("--local-file after the target was parsed as a flag, the help text says it is not")
	}
	if ctx.Args().Len() != 2 {
		t.Fatalf("got %d positional arguments, want 2", ctx.Args().Len())
	}
}

// --repository reaches LuetInstaller and nothing else, --local-file reaches
// the two OCI installers and nothing else. Passing either to the other scheme
// used to be accepted and then dropped, so an operator who pointed
// --repository at their own mirror and asked for a container:// bundle still
// pulled from the registry the target named, with nothing said about it.
func TestInstallBundleRefusesAFlagTheSchemeCannotRead(t *testing.T) {
	command := installBundleCommand(t)

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{
			name: "repository on a container target",
			args: []string{"--repository", "docker://quay.io/myorg/packages", "container://quay.io/myorg/mybundle:latest"},
			want: "--repository is only read by a package:// target",
		},
		{
			name: "repository on a run target",
			args: []string{"--repository", "docker://quay.io/myorg/packages", "run://quay.io/myorg/mybundle:latest"},
			want: "--repository is only read by a package:// target",
		},
		{
			name: "local-file on a package target",
			args: []string{"--local-file", "package://utils/edgevpn"},
			want: "--local-file is not read by a package:// target",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := commandContext(t, command, "", tc.args...)
			err := checkInstallBundleFlags(ctx, ctx.Args().First())
			if err == nil {
				t.Fatalf("args %q were accepted, the flag would have been dropped without a word", tc.args)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want one containing %q", err, tc.want)
			}
		})
	}
}

// The check has to be wired into the action, not only exist. The refusal
// returns before RunBundles, and --root-path keeps a regression that lets the
// call through from writing into the test host's /.
func TestInstallBundleActionRunsTheFlagCheck(t *testing.T) {
	command := installBundleCommand(t)

	ctx := commandContext(t, command, "", "--root-path", t.TempDir(), "--repository", "docker://quay.io/myorg/packages", "container://quay.io/myorg/mybundle:latest")
	err := command.Action(ctx)
	if err == nil || !strings.Contains(err.Error(), "--repository is only read by a package:// target") {
		t.Fatalf("install-bundle action = %v, want it to refuse --repository on a container target", err)
	}
}

// The flags each scheme does read, and the default --repository carries, have
// to keep working. A default value is not a request, so only a --repository
// the operator actually passed may refuse a container:// bundle.
func TestInstallBundleAcceptsTheFlagsTheSchemeReads(t *testing.T) {
	command := installBundleCommand(t)

	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "no flags", args: []string{"container://quay.io/myorg/mybundle:latest"}},
		{name: "unset repository keeps its default", args: []string{"--local-file", "container:///absolute/path/mybundle.tar"}},
		{name: "local-file on a run target", args: []string{"--local-file", "run:///absolute/path/mybundle.tar"}},
		{name: "repository on a package target", args: []string{"--repository", "docker://quay.io/myorg/packages", "package://utils/edgevpn"}},
		{name: "root-path on any target", args: []string{"--root-path", "/usr/local", "docker://quay.io/myorg/mybundle:latest"}},
		{name: "a target with no scheme is left to RunBundles", args: []string{"--repository", "docker://quay.io/myorg/packages", "quay.io/myorg/mybundle:latest"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := commandContext(t, command, "", tc.args...)
			if err := checkInstallBundleFlags(ctx, ctx.Args().First()); err != nil {
				t.Fatalf("args %q were refused: %v", tc.args, err)
			}
		})
	}
}

// Every example the help prints has to survive the check the command now runs,
// or the help tells people to type a command that is refused.
func TestInstallBundleExamplesPassTheFlagCheck(t *testing.T) {
	command := installBundleCommand(t)

	for _, line := range strings.Split(command.Description, "\n") {
		_, example, found := strings.Cut(line, "kairos-agent install-bundle ")
		if !found {
			continue
		}
		ctx := commandContext(t, command, "", strings.Fields(example)...)
		if err := checkInstallBundleFlags(ctx, ctx.Args().First()); err != nil {
			t.Errorf("example %q is refused by the command: %v", line, err)
		}
	}
}

func TestInstallBundleFlagsHaveUsage(t *testing.T) {
	command := installBundleCommand(t)

	for _, commandFlag := range command.Flags {
		docFlag, ok := commandFlag.(cli.DocGenerationFlag)
		if !ok {
			t.Fatalf("flag %v does not expose its usage text", commandFlag.Names())
		}
		if strings.TrimSpace(docFlag.GetUsage()) == "" {
			t.Errorf("flag %v has no usage text", commandFlag.Names())
		}
	}
}
