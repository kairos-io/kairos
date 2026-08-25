package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

const validCloudConfig = `#cloud-config
users:
  - name: kairos
`

func TestEditCloudInitSavesValidConfiguration(t *testing.T) {
	dir := t.TempDir()
	path := writeCloudConfig(t, dir, validCloudConfig, 0640)
	editor := writeEditor(t, dir, "valid-editor", "printf '%s' '#cloud-config\nusers:\n  - name: updated\n' > \"$1\"")

	if err := editCloudInit(path, editor, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "#cloud-config\nusers:\n  - name: updated\n" {
		t.Fatalf("unexpected content: %q", content)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0640 {
		t.Fatalf("expected mode 0640, got %o", info.Mode().Perm())
	}
}

func TestEditCloudInitReopensAfterValidationFailure(t *testing.T) {
	dir := t.TempDir()
	path := writeCloudConfig(t, dir, validCloudConfig, 0644)
	editor := writeEditor(t, dir, "retry-editor", `if [ -e "$1.count" ]; then
printf '%s' '#cloud-config
users:
  - name: corrected
' > "$1"
else
touch "$1.count"
printf '%s' 'not yaml: [' > "$1"
fi`)
	var output bytes.Buffer

	if err := editCloudInit(path, editor, &output); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "#cloud-config\nusers:\n  - name: corrected\n" {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestEditCloudInitDoesNotSaveUnchangedContent(t *testing.T) {
	dir := t.TempDir()
	path := writeCloudConfig(t, dir, validCloudConfig, 0644)
	editor := writeEditor(t, dir, "unchanged-editor", ":")

	if err := editCloudInit(path, editor, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != validCloudConfig {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestEditCloudInitLeavesOriginalOnEditorFailure(t *testing.T) {
	dir := t.TempDir()
	path := writeCloudConfig(t, dir, validCloudConfig, 0644)
	editor := writeEditor(t, dir, "failing-editor", "exit 1")

	if err := editCloudInit(path, editor, &bytes.Buffer{}); err == nil {
		t.Fatal("expected editor error")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != validCloudConfig {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestEditCloudInitRequiresEditor(t *testing.T) {
	dir := t.TempDir()
	path := writeCloudConfig(t, dir, validCloudConfig, 0644)

	if err := editCloudInit(path, "", &bytes.Buffer{}); err == nil {
		t.Fatal("expected missing editor error")
	}
}

func TestRunEditorParsesQuotedArguments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	editor := `/bin/sh -c 'printf "%s" "#cloud-config" > "$1"' --`

	if err := runEditor(editor, path); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "#cloud-config" {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestEditCloudInitDiscardsInvalidConfiguration(t *testing.T) {
	dir := t.TempDir()
	path := writeCloudConfig(t, dir, validCloudConfig, 0644)
	editor := writeEditor(t, dir, "invalid-editor", "printf '%s' 'not yaml: [' > \"$1\"")
	var output bytes.Buffer

	if err := editCloudInit(path, editor, &output); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != validCloudConfig {
		t.Fatalf("unexpected content: %q", content)
	}
	if !bytes.Contains(output.Bytes(), []byte("Invalid draft discarded")) {
		t.Fatalf("expected discard confirmation, got %q", output.String())
	}
}

func TestValidationMessageIsNotPartOfConfiguration(t *testing.T) {
	content := []byte(validCloudConfig)
	withMessage := addValidationMessage("/oem/90_custom.yaml", content, os.ErrInvalid)

	if !bytes.Contains(withMessage, []byte("# File: /oem/90_custom.yaml")) {
		t.Fatalf("expected file path, got %q", withMessage)
	}
	if !bytes.Contains(withMessage, []byte("# - invalid argument")) {
		t.Fatalf("expected validation message, got %q", withMessage)
	}
	if actual := removeValidationMessage(withMessage); !bytes.Equal(actual, content) {
		t.Fatalf("expected original configuration, got %q", actual)
	}
}

func writeCloudConfig(t *testing.T, dir, content string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, "90_custom.yaml")
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeEditor(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}
