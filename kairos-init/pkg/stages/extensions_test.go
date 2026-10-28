package stages

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/config"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
)

func enableExtensions(t *testing.T) {
	t.Helper()
	prev := config.DefaultConfig.Extensions
	config.DefaultConfig.Extensions = true
	t.Cleanup(func() { config.DefaultConfig.Extensions = prev })
}

func writeExtension(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestGetStageExtensionsLoadsStages(t *testing.T) {
	enableExtensions(t)
	dir := t.TempDir()
	t.Setenv("KAIROS_INIT_STAGE_EXTENSIONS_DIR", dir)
	writeExtension(t, dir, "10-ext.yaml", `stages:
  install:
    - name: from-extension
      commands:
        - echo hi
  init:
    - name: other-stage
`)
	writeExtension(t, dir, "notes.txt", "not: [yaml")

	got, err := GetStageExtensions("install", logger.NewNullLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Name != "from-extension" {
		t.Fatalf("got %+v, want a single from-extension stage", got)
	}
}

func TestGetStageExtensionsFailsOnBadFile(t *testing.T) {
	enableExtensions(t)
	dir := t.TempDir()
	t.Setenv("KAIROS_INIT_STAGE_EXTENSIONS_DIR", dir)
	bad := writeExtension(t, dir, "10-bad.yaml", "stages:\n  install:\n    - name: [unclosed\n")

	got, err := GetStageExtensions("install", logger.NewNullLogger())
	if err == nil {
		t.Fatalf("expected an error, got stages %+v", got)
	}
	if !strings.Contains(err.Error(), bad) {
		t.Fatalf("error %q does not name the file %s", err, bad)
	}
}

func TestGetStageExtensionsMissingExplicitDir(t *testing.T) {
	enableExtensions(t)
	t.Setenv("KAIROS_INIT_STAGE_EXTENSIONS_DIR", filepath.Join(t.TempDir(), "missing"))

	if _, err := GetStageExtensions("install", logger.NewNullLogger()); err == nil {
		t.Fatal("expected an error for a missing extensions dir set through the environment")
	}
}

func TestGetStageExtensionsMissingDefaultDir(t *testing.T) {
	if _, err := os.Stat("/etc/kairos-init/stage-extensions"); err == nil {
		t.Skip("default stage extensions dir exists on this machine")
	}
	enableExtensions(t)
	t.Setenv("KAIROS_INIT_STAGE_EXTENSIONS_DIR", "")

	got, err := GetStageExtensions("install", logger.NewNullLogger())
	if err != nil || len(got) != 0 {
		t.Fatalf("got %+v, %v; want no stages and no error", got, err)
	}
}

func TestGetStageExtensionsDisabled(t *testing.T) {
	prev := config.DefaultConfig.Extensions
	config.DefaultConfig.Extensions = false
	t.Cleanup(func() { config.DefaultConfig.Extensions = prev })
	t.Setenv("KAIROS_INIT_STAGE_EXTENSIONS_DIR", filepath.Join(t.TempDir(), "missing"))

	got, err := GetStageExtensions("install", logger.NewNullLogger())
	if err != nil || len(got) != 0 {
		t.Fatalf("got %+v, %v; want no stages and no error", got, err)
	}
}
