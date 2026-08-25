package cmd

import (
	"context"
	"errors"
	"flag"
	"os"
	"strings"
	"testing"

	agentConfig "github.com/kairos-io/kairos/v4/agent/pkg/config"
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
	if got := extensionCatalogURL(ctx, "sysext"); got != defaultExtensionCatalogURL {
		t.Fatalf("catalog default = %q, want %q", got, defaultExtensionCatalogURL)
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

	err = installCatalogOrURIExtension(cfg, defaultExtensionCatalogURL, "not-a-source", "", "sysext")
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
	ctx.Context = context.WithValue(context.Background(), "extType", extType)
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

	_, err = installCatalogExtension(cfg, "https://example.test/catalog.json", "git", "")
	if err == nil || err.Error() != "download failed" {
		t.Fatalf("expected download failure, got %v", err)
	}
	if client.destination == "" {
		t.Fatal("catalog download was not attempted")
	}
	if _, statErr := cfg.Fs.Stat(client.destination); !os.IsNotExist(statErr) {
		t.Fatalf("temporary catalog was not removed: %v", statErr)
	}
}
