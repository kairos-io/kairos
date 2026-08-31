package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/google/shlex"
	"github.com/kairos-io/kairos/v4/sdk/schema"
	"github.com/urfave/cli/v2"
)

const defaultCloudInitPath = "/oem/90_custom.yaml"

const validationMessageStart = "# kairosctl edit-config: validation failed"
const validationMessageEnd = "# kairosctl: end validation errors"

var EditConfigCMD = cli.Command{
	Name:      "edit-config",
	Usage:     "Edit and validate the persistent cloud-init configuration",
	UsageText: "edit-config [--file PATH]",
	Description: `
Opens the persistent cloud-init configuration in $EDITOR. Changes are validated
before the configuration is replaced, so invalid edits are never saved.
`,
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "file",
			Usage: "Cloud-init file to edit",
			Value: defaultCloudInitPath,
		},
	},
	Before: func(c *cli.Context) error {
		if c.Args().Len() != 0 {
			return fmt.Errorf("edit-config does not accept positional arguments")
		}
		if os.Geteuid() != 0 {
			return errors.New("this command requires root privileges")
		}
		return nil
	},
	Action: func(c *cli.Context) error {
		return editCloudInit(c.String("file"), os.Getenv("EDITOR"), c.App.Writer)
	},
}

func editCloudInit(path, editor string, output io.Writer) error {
	original, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read cloud-init file: %w", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat cloud-init file: %w", err)
	}

	temp, err := os.CreateTemp(filepath.Dir(path), ".kairosctl-edit-*.yaml")
	if err != nil {
		return fmt.Errorf("create temporary cloud-init file: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	if _, err := temp.Write(original); err != nil {
		temp.Close()
		return fmt.Errorf("write temporary cloud-init file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary cloud-init file: %w", err)
	}

	var invalidDraft []byte
	for {
		if err := runEditor(editor, tempPath); err != nil {
			return err
		}

		edited, err := os.ReadFile(tempPath)
		if err != nil {
			return fmt.Errorf("read edited cloud-init file: %w", err)
		}
		edited = removeValidationMessage(edited)
		if bytes.Equal(original, edited) {
			return nil
		}
		if bytes.Equal(invalidDraft, edited) {
			fmt.Fprintln(output, "Invalid draft discarded; the original configuration is unchanged.")
			return nil
		}
		if err := os.WriteFile(tempPath, edited, 0600); err != nil {
			return fmt.Errorf("prepare edited cloud-init file: %w", err)
		}

		if err := schema.Validate(tempPath); err != nil {
			if err := os.WriteFile(tempPath, addValidationMessage(path, edited, err), 0600); err != nil {
				return fmt.Errorf("write validation message: %w", err)
			}
			invalidDraft = edited
			continue
		}

		if err := replaceFile(path, edited, info); err != nil {
			return fmt.Errorf("save cloud-init file: %w", err)
		}
		return nil
	}
}

func addValidationMessage(path string, content []byte, validationErr error) []byte {
	var message strings.Builder
	message.WriteString(validationMessageStart)
	message.WriteByte('\n')
	message.WriteString("# File: ")
	message.WriteString(path)
	message.WriteByte('\n')
	message.WriteString("# The original configuration has not been changed. Correct the errors below and save,")
	message.WriteByte('\n')
	message.WriteString("# or exit the editor without saving to discard this invalid draft.")
	message.WriteString("\n#\n# Validation errors:\n")
	for _, line := range strings.Split(validationErr.Error(), "\n") {
		message.WriteString("# - ")
		message.WriteString(line)
		message.WriteByte('\n')
	}
	message.WriteString(validationMessageEnd)
	message.WriteString("\n\n")
	message.Write(content)
	return []byte(message.String())
}

func removeValidationMessage(content []byte) []byte {
	if !bytes.HasPrefix(content, []byte(validationMessageStart+"\n")) {
		return content
	}

	end := []byte(validationMessageEnd + "\n\n")
	if index := bytes.Index(content, end); index >= 0 {
		return content[index+len(end):]
	}
	return content
}

func runEditor(editor, path string) error {
	if editor == "" {
		return errors.New("EDITOR is not set")
	}

	args, err := shlex.Split(editor)
	if err != nil {
		return fmt.Errorf("parse EDITOR: %w", err)
	}
	if len(args) == 0 {
		return errors.New("EDITOR is empty")
	}

	cmd := exec.Command(args[0], append(args[1:], path)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run editor: %w", err)
	}
	return nil
}

func replaceFile(path string, content []byte, info os.FileInfo) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".kairosctl-save-*.yaml")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	if err := temp.Chmod(info.Mode()); err != nil {
		temp.Close()
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		temp.Close()
		return errors.New("read cloud-init file ownership")
	}
	if err := temp.Chown(int(stat.Uid), int(stat.Gid)); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(content); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}

	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
