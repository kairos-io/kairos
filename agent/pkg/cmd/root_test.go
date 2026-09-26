package cmd

import (
	"context"
	"errors"
	"flag"
	"os"
	"strings"
	"testing"

	agentConfig "github.com/kairos-io/kairos/v4/agent/pkg/config"
	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
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
	autoInstallFn = func(string, bool, ...string) (bool, *sdkConfig.Config, error) {
		called = true
		return false, nil, nil
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

// The web UI passes --source straight through to `manual-install`, so a bad
// one has to be rejected by the process the operator invoked. Without a Before
// the rejection only surfaces in the browser's progress stream, after the user
// has typed a whole cloud-config.
func TestWebUIRejectsABadSourceAtParseTime(t *testing.T) {
	var webui *cli.Command
	for _, c := range cmds {
		if c.Name == "webui" {
			webui = c
			break
		}
	}
	if webui == nil {
		t.Fatal("no webui command registered")
	}
	if webui.Before == nil {
		t.Fatal("webui has no Before, so --source is never validated")
	}

	set := flag.NewFlagSet("webui", flag.ContinueOnError)
	set.String("source", "not-a-uri", "")
	err := webui.Before(cli.NewContext(nil, set, nil))
	if err == nil || !strings.Contains(err.Error(), "not-a-uri") {
		t.Fatalf("webui --source not-a-uri = %v, want an error naming the source", err)
	}

	// The kairos-webui service passes no --source, so it must still start.
	empty := flag.NewFlagSet("webui", flag.ContinueOnError)
	empty.String("source", "", "")
	if err := webui.Before(cli.NewContext(nil, empty, nil)); err != nil {
		t.Fatalf("webui with no --source = %v, want nil", err)
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
