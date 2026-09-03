package phonehome

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/kairos-io/kairos/v4/agent/pkg/action"
	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
)

// execCommand is a seam for tests. Production points at exec.Command.
var execCommand = exec.Command

// ExtensionArgs is the decoded form of the `extension` command arguments.
type ExtensionArgs struct {
	Type      string
	Action    string
	Name      string
	Source    string
	BootState string
	Now       bool
}

func parseExtensionArgs(in map[string]string) (ExtensionArgs, error) {
	out := ExtensionArgs{
		Type:      in["type"],
		Action:    in["action"],
		Name:      in["name"],
		Source:    in["source"],
		BootState: in["bootState"],
		Now:       in["now"] == "true",
	}
	if out.Type != "sysext" && out.Type != "confext" {
		return out, fmt.Errorf("extension: unsupported type %q (want sysext or confext)", out.Type)
	}
	switch out.Action {
	case "install", "enable", "disable", "remove":
	default:
		return out, fmt.Errorf("extension: unsupported action %q (want install|enable|disable|remove)", out.Action)
	}
	if out.Name == "" {
		return out, fmt.Errorf("extension: name is required")
	}
	if out.Action == "install" && out.Source == "" {
		return out, fmt.Errorf("extension: source is required for action=install")
	}
	if out.Action != "remove" && out.BootState == "" {
		return out, fmt.Errorf("extension: bootState is required for action=%s", out.Action)
	}
	switch out.BootState {
	case "", constants.BootActive, constants.BootPassive, constants.BootRecovery, constants.BootCommon:
	default:
		return out, fmt.Errorf("extension: unsupported bootState %q", out.BootState)
	}
	return out, nil
}

func handleExtension(ctx context.Context, cmd CommandData) (string, error) {
	args, err := parseExtensionArgs(cmd.Args)
	if err != nil {
		return "", err
	}
	switch args.Action {
	case "install":
		return extInstall(ctx, args)
	case "enable", "disable":
		return extToggle(ctx, args, args.Action)
	case "remove":
		return extRemove(ctx, args)
	default:
		// parseExtensionArgs already rejects anything else.
		return "", fmt.Errorf("extension: action %q not implemented", args.Action)
	}
}

// extInstall is install plus enable. The kairos-agent `install` subcommand only
// downloads the .raw; `enable` creates the symlink under the chosen scope. Doing
// both keeps AuroraBoot's Install action card a single round-trip for the operator.
func extInstall(ctx context.Context, a ExtensionArgs) (string, error) {
	out1, err := runCLI(ctx, a.Type, "install", a.Source)
	if err != nil {
		return out1, fmt.Errorf("extension install: %w: %s", err, out1)
	}
	out2, err := runCLI(ctx, enableArgs(a, "enable")...)
	if err != nil {
		return out1 + "\n" + out2, fmt.Errorf("extension enable: %w: %s", err, out2)
	}
	return fmt.Sprintf("Extension %s installed and enabled in %s\n%s\n%s",
		a.Name, a.BootState, strings.TrimSpace(out1), strings.TrimSpace(out2)), nil
}

func extToggle(ctx context.Context, a ExtensionArgs, action string) (string, error) {
	out, err := runCLI(ctx, enableArgs(a, action)...)
	if err != nil {
		return out, fmt.Errorf("extension %s: %w: %s", action, err, out)
	}
	return strings.TrimSpace(out), nil
}

// enableArgs builds the argv for the enable and disable subcommands. The scope
// flag must come before the positional name: urfave/cli v2 stops parsing flags
// at the first positional argument, so a trailing --common makes the Before
// hook reject the call with "extension name required".
func enableArgs(a ExtensionArgs, action string) []string {
	args := []string{a.Type, action, "--" + a.BootState}
	if a.Now {
		args = append(args, "--now")
	}
	return append(args, a.Name)
}

func extRemove(ctx context.Context, a ExtensionArgs) (string, error) {
	cliArgs := []string{a.Type, "remove"}
	if a.Now {
		cliArgs = append(cliArgs, "--now")
	}
	cliArgs = append(cliArgs, a.Name)
	out, err := runCLI(ctx, cliArgs...)
	if err != nil {
		return out, fmt.Errorf("extension remove: %w: %s", err, out)
	}
	return strings.TrimSpace(out), nil
}

// runCLI shells out to kairos-agent. The context is accepted but not bound to
// the process: an extension install must not be killed when the websocket to
// the fleet server drops, which is the same reason handleUpgrade uses
// exec.Command rather than exec.CommandContext.
func runCLI(_ context.Context, args ...string) (string, error) {
	out, err := execCommand("kairos-agent", args...).CombinedOutput() //nosec G204 -- args are built from validated ExtensionArgs fields
	return string(out), err
}

// BundledExtension is one entry of the upgrade command's `extensions` argument.
// CommandData.Args is a map[string]string, so the list travels as a
// JSON-encoded array under Args["extensions"].
type BundledExtension struct {
	Type   string `json:"type"`
	Name   string `json:"name"`
	Source string `json:"source"`
}

// extensionScopeDir resolves the persistent directory for one scope. It is a
// var so tests can point it at a temp tree.
var extensionScopeDir = func(bootState, extType string) string {
	return action.ExtensionDirFromBootState(bootState, extType)
}

// extensionEnabledAnywhere reports whether any of the four scope directories
// holds an entry named "<name>.<something>", the convention that
// `auroraboot sysext` produces. It is used to keep the operator's earlier scope
// choice during a compound upgrade instead of forcing the extension back to the
// default scope.
//
// Matching is prefix plus a dot, so "tailscale-agent" does not match
// "tailscale-agent-helper".
func extensionEnabledAnywhere(extType, name string) bool {
	prefix := name + "."
	for _, scope := range []string{constants.BootActive, constants.BootPassive, constants.BootRecovery, constants.BootCommon} {
		dir := extensionScopeDir(scope, extType)
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if strings.HasPrefix(e.Name(), prefix) {
				return true
			}
		}
	}
	return false
}

// installBundledExtension downloads the extension, overwriting any earlier copy,
// and enables it at the given scope unless it is already enabled somewhere.
// scope is "active" for upgrade and "recovery" for upgrade-recovery. --now is
// left out on purpose: the OS upgrade is about to reboot, and the new active
// boot picks the extension up then.
func installBundledExtension(ctx context.Context, e BundledExtension, scope string) error {
	if out, err := runCLI(ctx, e.Type, "install", e.Source); err != nil {
		return fmt.Errorf("install %s/%s: %w: %s", e.Type, e.Name, err, out)
	}
	if extensionEnabledAnywhere(e.Type, e.Name) {
		return nil
	}
	if out, err := runCLI(ctx, e.Type, "enable", "--"+scope, e.Name); err != nil {
		return fmt.Errorf("enable %s/%s --%s: %w: %s", e.Type, e.Name, scope, err, out)
	}
	return nil
}

func parseBundledExtensions(raw string) ([]BundledExtension, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var list []BundledExtension
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil, fmt.Errorf("extensions arg: %w", err)
	}
	for i, e := range list {
		if e.Type != "sysext" && e.Type != "confext" {
			return nil, fmt.Errorf("extensions[%d]: unsupported type %q", i, e.Type)
		}
		if e.Name == "" {
			return nil, fmt.Errorf("extensions[%d]: name is required", i)
		}
		if e.Source == "" {
			return nil, fmt.Errorf("extensions[%d]: source is required", i)
		}
	}
	return list, nil
}
