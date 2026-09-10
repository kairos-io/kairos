package cmd

import (
	"context"
	"errors"
	"flag"
	"os"
	"strings"
	"testing"

	agentConfig "github.com/kairos-io/kairos/v4/agent/pkg/config"
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
