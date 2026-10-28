# Extensions — Plan 1 of 3: kairos-agent changes

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a new `extension` phonehome command (manual install/enable/disable/remove) and extend the existing `upgrade` / `upgrade-recovery` commands to install bundled extensions before the OS upgrade reboot. Ship as a tagged kairos-agent release that AuroraBoot will vendor.

**Architecture:** All work lives in `kairos-agent/internal/phonehome/`. A new sibling file `handlers_extension.go` holds the new dispatch logic so `handlers.go` stays focused. The dispatch shells out via a swappable `execCommand` indirection (matches the pattern that `handleUpgrade` already uses for `kairos-agent upgrade`). For compound upgrades, the `extensions` field is JSON-encoded into the existing `CommandData.Args map[string]string` (no breaking schema change). Tests use **ginkgo v2 + gomega** matching the existing suite at `internal/phonehome/`; new whitebox test seams are exposed via the existing `export_test.go` pattern (`Set*` helpers that return a restorer).

**Tech Stack:** Go 1.23+, urfave/cli v2, ginkgo v2 / gomega.

**Repository:** `/home/mudler/_git/kairos-agent`

---

## Reference — read these before starting

| File | What it does |
|---|---|
| `internal/phonehome/config.go:140-144` | `CommandData` shape: `ID`, `Command`, `Args map[string]string`. |
| `internal/phonehome/handlers.go:28-65` | `DefaultCommandHandler` — the switch we extend. |
| `internal/phonehome/handlers.go:89-164` | `handleUpgrade` — we extend this to install bundled extensions first. |
| `internal/phonehome/handlers.go:235-242` | `scheduleReboot` — we route through a seam so tests can assert "no reboot scheduled on failure". |
| `internal/phonehome/export_test.go` | Test-only exports pattern (`SetUninstallRunners` is the model to copy). |
| `internal/phonehome/suite_test.go` | Existing ginkgo entry point — already in place, no changes needed. |
| `internal/phonehome/config_test.go:130-188` | Existing `Describe("DefaultCommandHandler policy gating", …)` block — copy this style. |
| `pkg/action/sysext.go:35-52` | Storage layout constants (sysextDir, confExtDir, scope sub-dirs). |
| `pkg/action/sysext.go:154-218` | `EnableExtension` (referenced for behavior only — we shell out to the CLI, not call directly). |
| `pkg/action/sysext.go:283-305` | `InstallExtension` (ditto). |
| `main.go:1222-1488` | The sysext/confext subcommands wired into the CLI; verify flag names + order before shelling out from the handler. |

---

## Task 1: New file + `parseExtensionArgs` validator

**Files:**
- Create: `internal/phonehome/handlers_extension.go`
- Create: `internal/phonehome/handlers_extension_test.go`
- Modify: `internal/phonehome/export_test.go` (add `SetExecCommand`)

The validator parses and type-checks the new `extension` command's `Args` map. No CLI calls yet — just a pure function with full coverage.

- [ ] **Step 1: Write the failing ginkgo spec**

Create `internal/phonehome/handlers_extension_test.go`:

```go
package phonehome_test

import (
	"github.com/kairos-io/kairos-agent/v2/internal/phonehome"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("parseExtensionArgs", func() {
	It("parses a complete install request", func() {
		got, err := phonehome.ParseExtensionArgsForTest(map[string]string{
			"type":      "sysext",
			"action":    "install",
			"name":      "tailscale-agent",
			"source":    "https://aurora/api/v1/extensions/abc/download/tailscale-agent.sysext.raw?token=k",
			"bootState": "common",
			"now":       "true",
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(got.Type).To(Equal("sysext"))
		Expect(got.Action).To(Equal("install"))
		Expect(got.Name).To(Equal("tailscale-agent"))
		Expect(got.BootState).To(Equal("common"))
		Expect(got.Now).To(BeTrue())
	})

	It("rejects missing type", func() {
		_, err := phonehome.ParseExtensionArgsForTest(map[string]string{"action": "install"})
		Expect(err).To(MatchError(ContainSubstring("type")))
	})

	It("rejects an unsupported type", func() {
		_, err := phonehome.ParseExtensionArgsForTest(map[string]string{
			"type": "blob", "action": "install", "name": "x",
		})
		Expect(err).To(MatchError(ContainSubstring("type")))
	})

	It("requires source for action=install", func() {
		_, err := phonehome.ParseExtensionArgsForTest(map[string]string{
			"type": "sysext", "action": "install", "name": "x", "bootState": "common",
		})
		Expect(err).To(MatchError(ContainSubstring("source")))
	})

	It("requires bootState for action=enable", func() {
		_, err := phonehome.ParseExtensionArgsForTest(map[string]string{
			"type": "sysext", "action": "enable", "name": "x",
		})
		Expect(err).To(MatchError(ContainSubstring("bootState")))
	})

	It("requires bootState for action=disable", func() {
		_, err := phonehome.ParseExtensionArgsForTest(map[string]string{
			"type": "sysext", "action": "disable", "name": "x",
		})
		Expect(err).To(MatchError(ContainSubstring("bootState")))
	})

	It("requires name for every action", func() {
		_, err := phonehome.ParseExtensionArgsForTest(map[string]string{
			"type": "sysext", "action": "remove",
		})
		Expect(err).To(MatchError(ContainSubstring("name")))
	})

	It("rejects unsupported bootState", func() {
		_, err := phonehome.ParseExtensionArgsForTest(map[string]string{
			"type": "sysext", "action": "enable", "name": "x", "bootState": "wat",
		})
		Expect(err).To(MatchError(ContainSubstring("bootState")))
	})
})
```

- [ ] **Step 2: Add the test-only export for `parseExtensionArgs`**

Append to `internal/phonehome/export_test.go` (this stays in `package phonehome`):

```go
// ParseExtensionArgsForTest is the test-only entry point for the package-private
// parseExtensionArgs validator. Keeping the production function unexported lets
// us reshape it without breaking external callers.
func ParseExtensionArgsForTest(in map[string]string) (ExtensionArgs, error) {
	return parseExtensionArgs(in)
}
```

(Don't worry that `ExtensionArgs` and `parseExtensionArgs` don't exist yet — they're about to.)

- [ ] **Step 3: Run the spec to verify it fails**

```
cd ~/_git/kairos-agent && go test ./internal/phonehome/... -run TestPhoneHome -ginkgo.focus="parseExtensionArgs" -v
```

Expected: BUILD FAIL — `ExtensionArgs` / `parseExtensionArgs` not defined.

- [ ] **Step 4: Create `handlers_extension.go` with the validator**

Create `internal/phonehome/handlers_extension.go`:

```go
package phonehome

import "fmt"

// ExtensionArgs is the validated, typed shape of an `extension` command's args.
type ExtensionArgs struct {
	Type      string // "sysext" | "confext"
	Action    string // "install" | "enable" | "disable" | "remove"
	Name      string
	Source    string // required for action=install
	BootState string // required for action in {install,enable,disable}
	Now       bool   // optional
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
	if (out.Action == "install" || out.Action == "enable" || out.Action == "disable") && out.BootState == "" {
		return out, fmt.Errorf("extension: bootState is required for action=%s", out.Action)
	}
	switch out.BootState {
	case "", "active", "passive", "recovery", "common":
	default:
		return out, fmt.Errorf("extension: unsupported bootState %q", out.BootState)
	}
	return out, nil
}
```

- [ ] **Step 5: Run the spec, verify it passes**

```
cd ~/_git/kairos-agent && go test ./internal/phonehome/... -run TestPhoneHome -ginkgo.focus="parseExtensionArgs" -v
```

Expected: PASS for all 8 `It` blocks.

- [ ] **Step 6: Commit**

```bash
cd ~/_git/kairos-agent
git add internal/phonehome/handlers_extension.go internal/phonehome/handlers_extension_test.go internal/phonehome/export_test.go
git commit -m "phonehome: add ExtensionArgs validator for new extension command"
```

---

## Task 2: Dispatch skeleton wired into `DefaultCommandHandler`

**Files:**
- Modify: `internal/phonehome/handlers.go` (add a case to the switch at lines 36-63)
- Modify: `internal/phonehome/handlers_extension.go` (add `handleExtension` stub)
- Modify: `internal/phonehome/handlers_extension_test.go` (dispatch specs)

- [ ] **Step 1: Write the failing dispatch specs**

Append to `internal/phonehome/handlers_extension_test.go`:

```go
var _ = Describe("DefaultCommandHandler — extension command", func() {
	It("rejects the command when isAllowed returns false", func() {
		denyAll := func(string) bool { return false }
		handler := phonehome.DefaultCommandHandler("http://example", func() string { return "" }, denyAll, nil)
		_, err := handler(phonehome.CommandData{
			ID: "c1", Command: "extension",
			Args: map[string]string{"type": "sysext", "action": "remove", "name": "x"},
		})
		Expect(err).To(MatchError(ContainSubstring("not permitted")))
	})

	It("surfaces parse errors when args are malformed", func() {
		allow := func(string) bool { return true }
		handler := phonehome.DefaultCommandHandler("http://example", func() string { return "" }, allow, nil)
		_, err := handler(phonehome.CommandData{
			ID: "c1", Command: "extension",
			Args: map[string]string{"type": "wat"},
		})
		Expect(err).To(MatchError(ContainSubstring("unsupported type")))
	})

	It("dispatches to handleExtension when args validate", func() {
		// The stub introduced in this task returns 'not yet implemented';
		// later tasks will replace it with real CLI calls.
		allow := func(string) bool { return true }
		handler := phonehome.DefaultCommandHandler("http://example", func() string { return "" }, allow, nil)
		_, err := handler(phonehome.CommandData{
			ID: "c1", Command: "extension",
			Args: map[string]string{
				"type": "sysext", "action": "remove", "name": "tailscale-agent",
			},
		})
		Expect(err).To(MatchError(ContainSubstring("not yet implemented")))
	})
})
```

- [ ] **Step 2: Run the specs to verify they fail**

```
cd ~/_git/kairos-agent && go test ./internal/phonehome/... -run TestPhoneHome -ginkgo.focus="extension command" -v
```

Expected: FAIL — the dispatch case doesn't exist; `extension` falls into the `default:` arm and returns "unknown command".

- [ ] **Step 3: Add the switch case and a stub `handleExtension`**

In `internal/phonehome/handlers.go`, inside `DefaultCommandHandler`'s switch (between the `unregister` and `default` arms, around line 60), add:

```go
		case "extension":
			return handleExtension(ctx, cmd)
```

In `internal/phonehome/handlers_extension.go`, replace the file body with:

```go
package phonehome

import (
	"context"
	"fmt"
)

// ExtensionArgs is the validated, typed shape of an `extension` command's args.
type ExtensionArgs struct {
	Type      string
	Action    string
	Name      string
	Source    string
	BootState string
	Now       bool
}

func parseExtensionArgs(in map[string]string) (ExtensionArgs, error) {
	// ... keep the body from Task 1 unchanged ...
}

// handleExtension dispatches the manual-flow extension command. The stub
// returned here is replaced in subsequent tasks with the install/enable/
// disable/remove action implementations.
func handleExtension(ctx context.Context, cmd CommandData) (string, error) {
	args, err := parseExtensionArgs(cmd.Args)
	if err != nil {
		return "", err
	}
	_ = ctx
	return "", fmt.Errorf("extension: action %q not yet implemented", args.Action)
}
```

- [ ] **Step 4: Run the dispatch specs, verify they pass**

```
cd ~/_git/kairos-agent && go test ./internal/phonehome/... -run TestPhoneHome -ginkgo.focus="extension command" -v
```

Expected: PASS (3 `It` blocks).

- [ ] **Step 5: Commit**

```bash
git add internal/phonehome/handlers.go internal/phonehome/handlers_extension.go internal/phonehome/handlers_extension_test.go
git commit -m "phonehome: wire `extension` command into DefaultCommandHandler"
```

---

## Task 3: `execCommand` seam + `install` action

**Files:**
- Modify: `internal/phonehome/handlers_extension.go` (add `execCommand` var + `extInstall`)
- Modify: `internal/phonehome/export_test.go` (add `SetExecCommand`)
- Modify: `internal/phonehome/handlers_extension_test.go` (install specs)

`install` is two CLI calls — `kairos-agent <type> install <source>` downloads the `.raw`, `kairos-agent <type> enable …` creates the symlink. Tests use a recorder injected through the `execCommand` seam.

- [ ] **Step 1: Add the `SetExecCommand` test-only export**

Append to `internal/phonehome/export_test.go`:

```go
import (
	"os/exec"
)

// SetExecCommand swaps the shell-out indirection used by the extension
// handlers and (once Task 7 lands) by handleUpgrade. Returns a restorer.
//
// Tests typically pass a function that records args and returns
// `exec.Command("/bin/true")` for success or `exec.Command("/bin/false")`
// to simulate a non-zero exit.
func SetExecCommand(fn func(name string, args ...string) *exec.Cmd) func() {
	prev := execCommand
	execCommand = fn
	return func() { execCommand = prev }
}
```

(Add `"os/exec"` to the existing imports if not already imported by the file.)

- [ ] **Step 2: Write the failing install specs**

Append to `internal/phonehome/handlers_extension_test.go`:

```go
import (
	// ... existing imports
	"os/exec"
)

// commandRecorder captures shell-outs and always returns a command that
// exits successfully. Tests that need failure use a failingRecorder
// instead (added in Task 5).
type commandRecorder struct {
	calls [][]string
}

func (r *commandRecorder) record(name string, args ...string) *exec.Cmd {
	r.calls = append(r.calls, append([]string{name}, args...))
	return exec.Command("/bin/true")
}

var _ = Describe("handleExtension — install action", func() {
	var rec *commandRecorder
	var restore func()

	BeforeEach(func() {
		rec = &commandRecorder{}
		restore = phonehome.SetExecCommand(rec.record)
	})
	AfterEach(func() { restore() })

	It("issues install + enable with --now when now=true", func() {
		out, err := phonehome.HandleExtensionForTest(phonehome.CommandData{
			ID: "c1", Command: "extension",
			Args: map[string]string{
				"type":   "sysext",
				"action": "install",
				"name":   "tailscale-agent",
				"source": "https://aurora/api/v1/extensions/abc/download/tailscale-agent.sysext.raw?token=k",
				"bootState": "common",
				"now":       "true",
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(out).To(ContainSubstring("installed"))
		Expect(rec.calls).To(HaveLen(2))
		Expect(rec.calls[0]).To(Equal([]string{
			"kairos-agent", "sysext", "install",
			"https://aurora/api/v1/extensions/abc/download/tailscale-agent.sysext.raw?token=k",
		}))
		Expect(rec.calls[1]).To(Equal([]string{
			"kairos-agent", "sysext", "enable", "tailscale-agent", "--common", "--now",
		}))
	})

	It("omits --now when now=false", func() {
		_, err := phonehome.HandleExtensionForTest(phonehome.CommandData{
			Command: "extension",
			Args: map[string]string{
				"type": "confext", "action": "install",
				"name": "fluent-bit-config",
				"source": "https://x/file?token=k",
				"bootState": "active",
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(rec.calls).To(HaveLen(2))
		Expect(rec.calls[1]).To(Equal([]string{
			"kairos-agent", "confext", "enable", "fluent-bit-config", "--active",
		}))
	})
})
```

- [ ] **Step 3: Add `HandleExtensionForTest` to the test exports**

Append to `internal/phonehome/export_test.go`:

```go
import (
	"context"
)

// HandleExtensionForTest is the test-only entry point for the package-private
// handleExtension dispatcher. (DefaultCommandHandler also reaches it via the
// switch case, but going through DefaultCommandHandler in every spec is noisy.)
func HandleExtensionForTest(cmd CommandData) (string, error) {
	return handleExtension(context.Background(), cmd)
}
```

- [ ] **Step 4: Run the specs to verify they fail**

```
cd ~/_git/kairos-agent && go test ./internal/phonehome/... -run TestPhoneHome -ginkgo.focus="install action" -v
```

Expected: FAIL — `execCommand` and `SetExecCommand` not yet introduced; `handleExtension` is still the stub returning "not yet implemented".

- [ ] **Step 5: Implement the seam + `extInstall`**

Replace the body of `internal/phonehome/handlers_extension.go`:

```go
package phonehome

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// execCommand is a seam for tests. Production code path is exec.Command.
var execCommand = exec.Command

type ExtensionArgs struct {
	Type      string
	Action    string
	Name      string
	Source    string
	BootState string
	Now       bool
}

func parseExtensionArgs(in map[string]string) (ExtensionArgs, error) {
	// ... unchanged from Task 1 ...
}

func handleExtension(ctx context.Context, cmd CommandData) (string, error) {
	args, err := parseExtensionArgs(cmd.Args)
	if err != nil {
		return "", err
	}
	switch args.Action {
	case "install":
		return extInstall(ctx, args)
	default:
		return "", fmt.Errorf("extension: action %q not yet implemented", args.Action)
	}
}

// extInstall is install + enable. kairos-agent's `install` subcommand only
// downloads the .raw; `enable` creates the symlink under the chosen scope.
// We do both so AuroraBoot's "Install" action card is one atomic round-trip
// from the operator's view.
func extInstall(ctx context.Context, a ExtensionArgs) (string, error) {
	out1, err := runCLI(ctx, a.Type, "install", a.Source)
	if err != nil {
		return out1, fmt.Errorf("extension install: %w: %s", err, out1)
	}
	enableArgs := []string{a.Type, "enable", a.Name, "--" + a.BootState}
	if a.Now {
		enableArgs = append(enableArgs, "--now")
	}
	out2, err := runCLI(ctx, enableArgs...)
	if err != nil {
		return out1 + "\n" + out2, fmt.Errorf("extension enable: %w: %s", err, out2)
	}
	return fmt.Sprintf("Extension %s installed and enabled in %s\n%s\n%s",
		a.Name, a.BootState, strings.TrimSpace(out1), strings.TrimSpace(out2)), nil
}

func runCLI(ctx context.Context, args ...string) (string, error) {
	_ = ctx
	out, err := execCommand("kairos-agent", args...).CombinedOutput()
	return string(out), err
}
```

- [ ] **Step 6: Run the install specs, verify they pass**

```
cd ~/_git/kairos-agent && go test ./internal/phonehome/... -run TestPhoneHome -ginkgo.focus="install action" -v
```

Expected: PASS (2 `It` blocks).

- [ ] **Step 7: Commit**

```bash
git add internal/phonehome/handlers_extension.go internal/phonehome/handlers_extension_test.go internal/phonehome/export_test.go
git commit -m "phonehome: implement extension install (download + enable)"
```

---

## Task 4: `enable`, `disable`, `remove` actions

**Files:**
- Modify: `internal/phonehome/handlers_extension.go` (add `extToggle`, `extRemove`, dispatch cases)
- Modify: `internal/phonehome/handlers_extension_test.go` (specs)

- [ ] **Step 1: Write the failing specs**

Append to `internal/phonehome/handlers_extension_test.go`:

```go
var _ = Describe("handleExtension — enable/disable/remove", func() {
	var rec *commandRecorder
	var restore func()

	BeforeEach(func() {
		rec = &commandRecorder{}
		restore = phonehome.SetExecCommand(rec.record)
	})
	AfterEach(func() { restore() })

	It("issues enable without --now when now=false", func() {
		_, err := phonehome.HandleExtensionForTest(phonehome.CommandData{
			Command: "extension",
			Args: map[string]string{
				"type": "sysext", "action": "enable",
				"name": "tailscale-agent", "bootState": "passive", "now": "false",
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(rec.calls).To(Equal([][]string{
			{"kairos-agent", "sysext", "enable", "tailscale-agent", "--passive"},
		}))
	})

	It("issues disable with --now when now=true", func() {
		_, err := phonehome.HandleExtensionForTest(phonehome.CommandData{
			Command: "extension",
			Args: map[string]string{
				"type": "confext", "action": "disable",
				"name": "fluent-bit-config", "bootState": "common", "now": "true",
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(rec.calls).To(Equal([][]string{
			{"kairos-agent", "confext", "disable", "fluent-bit-config", "--common", "--now"},
		}))
	})

	It("issues remove with --now when now=true", func() {
		_, err := phonehome.HandleExtensionForTest(phonehome.CommandData{
			Command: "extension",
			Args: map[string]string{
				"type": "sysext", "action": "remove",
				"name": "tailscale-agent", "now": "true",
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(rec.calls).To(Equal([][]string{
			{"kairos-agent", "sysext", "remove", "tailscale-agent", "--now"},
		}))
	})
})
```

- [ ] **Step 2: Run the specs to verify they fail**

```
cd ~/_git/kairos-agent && go test ./internal/phonehome/... -run TestPhoneHome -ginkgo.focus="enable/disable/remove" -v
```

Expected: FAIL — `handleExtension` only routes `install` so far.

- [ ] **Step 3: Add `extToggle` + `extRemove` and extend the dispatch**

In `internal/phonehome/handlers_extension.go`, replace the `switch args.Action` in `handleExtension` with:

```go
	switch args.Action {
	case "install":
		return extInstall(ctx, args)
	case "enable":
		return extToggle(ctx, args, "enable")
	case "disable":
		return extToggle(ctx, args, "disable")
	case "remove":
		return extRemove(ctx, args)
	default:
		return "", fmt.Errorf("extension: action %q not yet implemented", args.Action)
	}
```

Append to the same file:

```go
func extToggle(ctx context.Context, a ExtensionArgs, action string) (string, error) {
	cliArgs := []string{a.Type, action, a.Name, "--" + a.BootState}
	if a.Now {
		cliArgs = append(cliArgs, "--now")
	}
	out, err := runCLI(ctx, cliArgs...)
	if err != nil {
		return out, fmt.Errorf("extension %s: %w: %s", action, err, out)
	}
	return strings.TrimSpace(out), nil
}

func extRemove(ctx context.Context, a ExtensionArgs) (string, error) {
	cliArgs := []string{a.Type, "remove", a.Name}
	if a.Now {
		cliArgs = append(cliArgs, "--now")
	}
	out, err := runCLI(ctx, cliArgs...)
	if err != nil {
		return out, fmt.Errorf("extension remove: %w: %s", err, out)
	}
	return strings.TrimSpace(out), nil
}
```

- [ ] **Step 4: Run the specs, verify they pass**

```
cd ~/_git/kairos-agent && go test ./internal/phonehome/... -run TestPhoneHome -ginkgo.focus="enable/disable/remove" -v
```

Expected: PASS (3 `It` blocks).

- [ ] **Step 5: Commit**

```bash
git add internal/phonehome/handlers_extension.go internal/phonehome/handlers_extension_test.go
git commit -m "phonehome: implement extension enable/disable/remove"
```

---

## Task 5: install/enable failure paths

**Files:**
- Modify: `internal/phonehome/handlers_extension_test.go`

The implementation already returns errors on non-zero CLI exit (`runCLI` propagates the error). This task adds coverage that exercises the abort behavior.

- [ ] **Step 1: Write the failing specs**

Append to `internal/phonehome/handlers_extension_test.go`:

```go
// failingRecorder is like commandRecorder but lets the test request a
// specific call (0-based) to exit non-zero.
type failingRecorder struct {
	calls  [][]string
	failOn int
}

func (r *failingRecorder) record(name string, args ...string) *exec.Cmd {
	idx := len(r.calls)
	r.calls = append(r.calls, append([]string{name}, args...))
	if r.failOn == idx {
		return exec.Command("/bin/false")
	}
	return exec.Command("/bin/true")
}

var _ = Describe("handleExtension — install error paths", func() {
	It("returns the install error and does NOT call enable when download fails", func() {
		rec := &failingRecorder{failOn: 0}
		restore := phonehome.SetExecCommand(rec.record)
		defer restore()

		_, err := phonehome.HandleExtensionForTest(phonehome.CommandData{
			Command: "extension",
			Args: map[string]string{
				"type": "sysext", "action": "install",
				"name": "x", "source": "https://x/y", "bootState": "common",
			},
		})
		Expect(err).To(MatchError(ContainSubstring("extension install")))
		Expect(rec.calls).To(HaveLen(1))
	})

	It("returns the enable error when symlink creation fails after a successful download", func() {
		rec := &failingRecorder{failOn: 1}
		restore := phonehome.SetExecCommand(rec.record)
		defer restore()

		_, err := phonehome.HandleExtensionForTest(phonehome.CommandData{
			Command: "extension",
			Args: map[string]string{
				"type": "sysext", "action": "install",
				"name": "x", "source": "https://x/y", "bootState": "common",
			},
		})
		Expect(err).To(MatchError(ContainSubstring("extension enable")))
		Expect(rec.calls).To(HaveLen(2))
	})
})
```

- [ ] **Step 2: Run the specs, verify they pass**

```
cd ~/_git/kairos-agent && go test ./internal/phonehome/... -run TestPhoneHome -ginkgo.focus="install error paths" -v
```

Expected: PASS (both `It` blocks). The implementation from Task 3 already returns errors on non-zero exit — this task only adds the coverage.

- [ ] **Step 3: Commit**

```bash
git add internal/phonehome/handlers_extension_test.go
git commit -m "phonehome: cover extension install error paths"
```

---

## Task 6: Bundled-extensions JSON parser

**Files:**
- Modify: `internal/phonehome/handlers_extension.go` (add `BundledExtension` + parser)
- Modify: `internal/phonehome/export_test.go` (export the parser)
- Modify: `internal/phonehome/handlers_extension_test.go` (parser specs)

The compound `upgrade` command passes its extensions list as a JSON-encoded string under `Args["extensions"]`. This task adds the parser; Task 7 wires it into `handleUpgrade`.

- [ ] **Step 1: Write the failing specs**

Append to `internal/phonehome/handlers_extension_test.go`:

```go
import (
	// ... existing
	"encoding/json"
)

var _ = Describe("parseBundledExtensions", func() {
	It("returns an empty slice for an empty input", func() {
		got, err := phonehome.ParseBundledExtensionsForTest("")
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(BeEmpty())
	})

	It("decodes a well-formed array", func() {
		raw, _ := json.Marshal([]phonehome.BundledExtension{
			{Type: "sysext", Name: "tailscale-agent", Source: "https://x/a"},
			{Type: "confext", Name: "fluent-bit-config", Source: "https://x/b"},
		})
		got, err := phonehome.ParseBundledExtensionsForTest(string(raw))
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(HaveLen(2))
		Expect(got[0].Name).To(Equal("tailscale-agent"))
		Expect(got[1].Type).To(Equal("confext"))
	})

	It("rejects an unsupported type", func() {
		_, err := phonehome.ParseBundledExtensionsForTest(
			`[{"type":"blob","name":"x","source":"https://x"}]`)
		Expect(err).To(MatchError(ContainSubstring("type")))
	})

	It("rejects a missing name", func() {
		_, err := phonehome.ParseBundledExtensionsForTest(
			`[{"type":"sysext","source":"https://x"}]`)
		Expect(err).To(MatchError(ContainSubstring("name")))
	})

	It("rejects a missing source", func() {
		_, err := phonehome.ParseBundledExtensionsForTest(
			`[{"type":"sysext","name":"x"}]`)
		Expect(err).To(MatchError(ContainSubstring("source")))
	})
})
```

- [ ] **Step 2: Add the test-only export**

Append to `internal/phonehome/export_test.go`:

```go
func ParseBundledExtensionsForTest(raw string) ([]BundledExtension, error) {
	return parseBundledExtensions(raw)
}
```

- [ ] **Step 3: Run the specs, verify they fail**

```
cd ~/_git/kairos-agent && go test ./internal/phonehome/... -run TestPhoneHome -ginkgo.focus="parseBundledExtensions" -v
```

Expected: BUILD FAIL — `BundledExtension` / `parseBundledExtensions` not defined.

- [ ] **Step 4: Implement the parser**

Append to `internal/phonehome/handlers_extension.go`:

```go
import (
	// ... existing
	"encoding/json"
)

// BundledExtension is one entry inside the upgrade command's `extensions` arg.
// The on-wire shape is a JSON-encoded array passed as a string under
// CommandData.Args["extensions"] because Args is map[string]string.
type BundledExtension struct {
	Type   string `json:"type"`
	Name   string `json:"name"`
	Source string `json:"source"`
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
```

- [ ] **Step 5: Run the specs, verify they pass**

```
cd ~/_git/kairos-agent && go test ./internal/phonehome/... -run TestPhoneHome -ginkgo.focus="parseBundledExtensions" -v
```

Expected: PASS (5 `It` blocks).

- [ ] **Step 6: Commit**

```bash
git add internal/phonehome/handlers_extension.go internal/phonehome/handlers_extension_test.go internal/phonehome/export_test.go
git commit -m "phonehome: parse bundled extensions arg for compound upgrade"
```

---

## Task 7: Compound upgrade — `extensionEnabledAnywhere` + `installBundledExtension`

**Files:**
- Modify: `internal/phonehome/handlers_extension.go` (add filesystem scanner + bundle install helper)
- Modify: `internal/phonehome/export_test.go` (expose the persistent-root seam + helper entry point)
- Modify: `internal/phonehome/handlers_extension_test.go` (specs)

This task adds the pieces; Task 8 wires them into `handleUpgrade`.

- [ ] **Step 1: Write the failing specs for `extensionEnabledAnywhere`**

Append to `internal/phonehome/handlers_extension_test.go`:

```go
import (
	// ... existing
	"os"
	"path/filepath"
)

var _ = Describe("extensionEnabledAnywhere", func() {
	var rootRestore func()
	var tmpRoot string

	BeforeEach(func() {
		tmpRoot = GinkgoT().TempDir()
		rootRestore = phonehome.SetExtensionsPersistentRoot(func(extType string) string {
			return filepath.Join(tmpRoot, extType+"s")
		})
	})
	AfterEach(func() { rootRestore() })

	It("returns false when no scope dir contains the extension", func() {
		Expect(phonehome.ExtensionEnabledAnywhereForTest("sysext", "tailscale-agent")).To(BeFalse())
	})

	It("returns true when a symlink exists under active/", func() {
		scopeDir := filepath.Join(tmpRoot, "sysexts", "active")
		Expect(os.MkdirAll(scopeDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(scopeDir, "tailscale-agent.sysext.raw"), nil, 0o644)).To(Succeed())
		Expect(phonehome.ExtensionEnabledAnywhereForTest("sysext", "tailscale-agent")).To(BeTrue())
	})

	It("returns true when a symlink exists under common/", func() {
		scopeDir := filepath.Join(tmpRoot, "sysexts", "common")
		Expect(os.MkdirAll(scopeDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(scopeDir, "tailscale-agent.sysext.raw"), nil, 0o644)).To(Succeed())
		Expect(phonehome.ExtensionEnabledAnywhereForTest("sysext", "tailscale-agent")).To(BeTrue())
	})

	It("matches by prefix-then-dot so a longer-named neighbour doesn't false-positive", func() {
		scopeDir := filepath.Join(tmpRoot, "sysexts", "common")
		Expect(os.MkdirAll(scopeDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(scopeDir, "tailscale-agent-helper.sysext.raw"), nil, 0o644)).To(Succeed())
		Expect(phonehome.ExtensionEnabledAnywhereForTest("sysext", "tailscale-agent")).To(BeFalse())
	})
})
```

- [ ] **Step 2: Add the test exports**

Append to `internal/phonehome/export_test.go`:

```go
func SetExtensionsPersistentRoot(fn func(extType string) string) func() {
	prev := extensionsPersistentRoot
	extensionsPersistentRoot = fn
	return func() { extensionsPersistentRoot = prev }
}

func ExtensionEnabledAnywhereForTest(extType, name string) bool {
	return extensionEnabledAnywhere(extType, name)
}

func InstallBundledExtensionForTest(e BundledExtension, scope string) error {
	return installBundledExtension(context.Background(), e, scope)
}
```

- [ ] **Step 3: Run the scanner specs, verify they fail**

```
cd ~/_git/kairos-agent && go test ./internal/phonehome/... -run TestPhoneHome -ginkgo.focus="extensionEnabledAnywhere" -v
```

Expected: BUILD FAIL — `extensionsPersistentRoot`, `extensionEnabledAnywhere`, `installBundledExtension` not defined.

- [ ] **Step 4: Implement the scanner + helper**

Append to `internal/phonehome/handlers_extension.go`:

```go
import (
	// ... existing
	"os"
	"path/filepath"
)

// extensionsPersistentRoot returns the persistent-dir base path for the
// given extension type. Test seam — production wraps the constants from
// pkg/action/sysext.go (sysextDir = /var/lib/kairos/extensions,
// confExtDir = /var/lib/kairos/confexts).
var extensionsPersistentRoot = func(extType string) string {
	if extType == "confext" {
		return "/var/lib/kairos/confexts"
	}
	return "/var/lib/kairos/extensions"
}

// extensionEnabledAnywhere reports whether any of the four scope dirs
// (active/passive/recovery/common) contains a symlink or file named like
// "<name>.<type>.raw" — the convention produced by `auroraboot sysext`.
//
// Matching uses prefix-then-dot so `tailscale-agent` does NOT match
// `tailscale-agent-helper`. Per the spec, this check preserves the
// operator's prior scope choice during a compound upgrade.
func extensionEnabledAnywhere(extType, name string) bool {
	base := extensionsPersistentRoot(extType)
	prefix := name + "."
	for _, scope := range []string{"active", "passive", "recovery", "common"} {
		entries, err := os.ReadDir(filepath.Join(base, scope))
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

// installBundledExtension downloads (overwriting) and conditionally enables
// the extension at the given scope. `scope` is "active" for `upgrade`,
// "recovery" for `upgrade-recovery`. --now is intentionally omitted: the OS
// upgrade about to reboot will pick the extension up on the new active boot.
func installBundledExtension(ctx context.Context, e BundledExtension, scope string) error {
	if out, err := runCLI(ctx, e.Type, "install", e.Source); err != nil {
		return fmt.Errorf("install %s/%s: %w: %s", e.Type, e.Name, err, out)
	}
	if extensionEnabledAnywhere(e.Type, e.Name) {
		return nil
	}
	if out, err := runCLI(ctx, e.Type, "enable", e.Name, "--"+scope); err != nil {
		return fmt.Errorf("enable %s/%s --%s: %w: %s", e.Type, e.Name, scope, err, out)
	}
	return nil
}
```

- [ ] **Step 5: Run the scanner specs, verify they pass**

```
cd ~/_git/kairos-agent && go test ./internal/phonehome/... -run TestPhoneHome -ginkgo.focus="extensionEnabledAnywhere" -v
```

Expected: PASS (4 `It` blocks).

- [ ] **Step 6: Write the failing specs for `installBundledExtension`**

Append to `internal/phonehome/handlers_extension_test.go`:

```go
var _ = Describe("installBundledExtension", func() {
	var rec *commandRecorder
	var restoreExec, restoreRoot func()
	var tmpRoot string

	BeforeEach(func() {
		tmpRoot = GinkgoT().TempDir()
		restoreRoot = phonehome.SetExtensionsPersistentRoot(func(extType string) string {
			return filepath.Join(tmpRoot, extType+"s")
		})
		rec = &commandRecorder{}
		restoreExec = phonehome.SetExecCommand(rec.record)
	})
	AfterEach(func() {
		restoreExec()
		restoreRoot()
	})

	It("installs and enables a brand-new extension at --active", func() {
		// tmpRoot is empty, so the helper considers the extension not enabled.
		Expect(phonehome.InstallBundledExtensionForTest(
			phonehome.BundledExtension{Type: "sysext", Name: "tailscale-agent", Source: "https://x/y"},
			"active",
		)).To(Succeed())
		Expect(rec.calls).To(Equal([][]string{
			{"kairos-agent", "sysext", "install", "https://x/y"},
			{"kairos-agent", "sysext", "enable", "tailscale-agent", "--active"},
		}))
	})

	It("skips enable when the extension is already present at common scope", func() {
		scopeDir := filepath.Join(tmpRoot, "sysexts", "common")
		Expect(os.MkdirAll(scopeDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(scopeDir, "tailscale-agent.sysext.raw"), nil, 0o644)).To(Succeed())

		Expect(phonehome.InstallBundledExtensionForTest(
			phonehome.BundledExtension{Type: "sysext", Name: "tailscale-agent", Source: "https://x/y"},
			"active",
		)).To(Succeed())
		// install only — the existing symlink is preserved.
		Expect(rec.calls).To(Equal([][]string{
			{"kairos-agent", "sysext", "install", "https://x/y"},
		}))
	})

	It("enables at the recovery scope when called with scope=recovery", func() {
		Expect(phonehome.InstallBundledExtensionForTest(
			phonehome.BundledExtension{Type: "sysext", Name: "rescue-tools", Source: "https://x/r"},
			"recovery",
		)).To(Succeed())
		Expect(rec.calls).To(Equal([][]string{
			{"kairos-agent", "sysext", "install", "https://x/r"},
			{"kairos-agent", "sysext", "enable", "rescue-tools", "--recovery"},
		}))
	})
})
```

- [ ] **Step 7: Run the helper specs, verify they pass**

```
cd ~/_git/kairos-agent && go test ./internal/phonehome/... -run TestPhoneHome -ginkgo.focus="installBundledExtension" -v
```

Expected: PASS (3 `It` blocks).

- [ ] **Step 8: Commit**

```bash
git add internal/phonehome/handlers_extension.go internal/phonehome/handlers_extension_test.go internal/phonehome/export_test.go
git commit -m "phonehome: scanner + installBundledExtension helper"
```

---

## Task 8: Extend `handleUpgrade` to install the bundle first

**Files:**
- Modify: `internal/phonehome/handlers.go` (route `scheduleReboot` and the upgrade exec through seams, add the bundle loop)
- Modify: `internal/phonehome/export_test.go` (add `SetScheduleReboot`)
- Modify: `internal/phonehome/handlers_extension_test.go` (specs)

- [ ] **Step 1: Write the failing specs**

Append to `internal/phonehome/handlers_extension_test.go`:

```go
var _ = Describe("handleUpgrade — extensions bundle", func() {
	var rec *commandRecorder
	var restoreExec, restoreRoot, restoreReboot func()
	var rebootCalled bool

	BeforeEach(func() {
		tmpRoot := GinkgoT().TempDir()
		restoreRoot = phonehome.SetExtensionsPersistentRoot(func(extType string) string {
			return filepath.Join(tmpRoot, extType+"s")
		})
		rec = &commandRecorder{}
		restoreExec = phonehome.SetExecCommand(rec.record)
		rebootCalled = false
		restoreReboot = phonehome.SetScheduleReboot(func() { rebootCalled = true })
	})
	AfterEach(func() {
		restoreExec()
		restoreRoot()
		restoreReboot()
	})

	It("is a no-op for extensions when the arg is empty (backward compat)", func() {
		_, err := phonehome.HandleUpgradeForTest(phonehome.CommandData{
			ID: "u1", Command: "upgrade",
			Args: map[string]string{"source": "oci:quay.io/myorg/edge-os:v4.2.0"},
		})
		Expect(err).ToNot(HaveOccurred())
		// One CLI call: the upgrade itself.
		Expect(rec.calls).To(HaveLen(1))
		Expect(rec.calls[0]).To(ContainElement("upgrade"))
		Expect(rebootCalled).To(BeTrue())
	})

	It("installs every bundled extension before running upgrade", func() {
		raw, _ := json.Marshal([]phonehome.BundledExtension{
			{Type: "sysext", Name: "tailscale-agent", Source: "https://x/a"},
			{Type: "confext", Name: "fluent-bit-config", Source: "https://x/b"},
		})
		_, err := phonehome.HandleUpgradeForTest(phonehome.CommandData{
			Command: "upgrade",
			Args: map[string]string{
				"source":     "oci:quay.io/myorg/edge-os:v4.2.0",
				"extensions": string(raw),
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(rec.calls).To(HaveLen(5))
		Expect(rec.calls[0][1:3]).To(Equal([]string{"sysext", "install"}))
		Expect(rec.calls[1][1:3]).To(Equal([]string{"sysext", "enable"}))
		Expect(rec.calls[1]).To(ContainElement("--active"))
		Expect(rec.calls[2][1:3]).To(Equal([]string{"confext", "install"}))
		Expect(rec.calls[3][1:3]).To(Equal([]string{"confext", "enable"}))
		Expect(rec.calls[4]).To(ContainElement("upgrade"))
		Expect(rebootCalled).To(BeTrue())
	})

	It("aborts before the OS upgrade when an extension install fails", func() {
		failRec := &failingRecorder{failOn: 0}
		restoreExec()
		restoreExec = phonehome.SetExecCommand(failRec.record)

		raw, _ := json.Marshal([]phonehome.BundledExtension{
			{Type: "sysext", Name: "x", Source: "https://x/a"},
		})
		_, err := phonehome.HandleUpgradeForTest(phonehome.CommandData{
			Command: "upgrade",
			Args: map[string]string{
				"source":     "oci:quay.io/myorg/edge-os:v4.2.0",
				"extensions": string(raw),
			},
		})
		Expect(err).To(MatchError(ContainSubstring("install sysext/x")))
		Expect(failRec.calls).To(HaveLen(1))
		Expect(rebootCalled).To(BeFalse())
	})

	It("enables bundled extensions at --recovery scope for upgrade-recovery", func() {
		raw, _ := json.Marshal([]phonehome.BundledExtension{
			{Type: "sysext", Name: "rescue-tools", Source: "https://x/r"},
		})
		_, err := phonehome.HandleUpgradeForTest(phonehome.CommandData{
			Command: "upgrade-recovery",
			Args: map[string]string{
				"source":     "oci:quay.io/myorg/edge-os:v4.2.0",
				"recovery":   "true",
				"extensions": string(raw),
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(rec.calls).To(HaveLen(3))
		Expect(rec.calls[1]).To(Equal([]string{"kairos-agent", "sysext", "enable", "rescue-tools", "--recovery"}))
	})
})
```

- [ ] **Step 2: Add the missing test exports**

Append to `internal/phonehome/export_test.go`:

```go
func SetScheduleReboot(fn func()) func() {
	prev := scheduleRebootFn
	scheduleRebootFn = fn
	return func() { scheduleRebootFn = prev }
}

func HandleUpgradeForTest(cmd CommandData) (string, error) {
	return handleUpgrade(context.Background(), cmd, "http://test", "")
}
```

- [ ] **Step 3: Run the specs to verify they fail**

```
cd ~/_git/kairos-agent && go test ./internal/phonehome/... -run TestPhoneHome -ginkgo.focus="handleUpgrade — extensions bundle" -v
```

Expected: BUILD FAIL — `scheduleRebootFn` and the bundle loop in `handleUpgrade` don't exist; `execCommand` isn't used by `handleUpgrade` yet either.

- [ ] **Step 4: Route `scheduleReboot` through a seam**

In `internal/phonehome/handlers.go`, find the current `scheduleReboot` function (around lines 235-242) and replace its body with the seam:

```go
// scheduleRebootFn is a seam for tests to assert "no reboot scheduled on
// failure". Production points at scheduleRebootImpl.
var scheduleRebootFn = scheduleRebootImpl

func scheduleReboot() {
	scheduleRebootFn()
}

func scheduleRebootImpl() {
	go func() {
		_ = exec.Command("sync").Run()   //nosec G204 -- fixed command
		time.Sleep(10 * time.Second)
		_ = exec.Command("reboot").Run() //nosec G204 -- fixed command
	}()
}
```

- [ ] **Step 5: Route `handleUpgrade`'s upgrade exec through `execCommand`**

In `internal/phonehome/handlers.go`, in `handleUpgrade`, locate the line (around line 150):

```go
out, err := exec.Command("kairos-agent", args...).CombinedOutput() //nosec G204 -- args is a fixed set built from validated CommandData fields
```

Change `exec.Command` to `execCommand`:

```go
out, err := execCommand("kairos-agent", args...).CombinedOutput() //nosec G204 -- args is a fixed set built from validated CommandData fields
```

- [ ] **Step 6: Insert the bundle loop into `handleUpgrade`**

In `internal/phonehome/handlers.go`, inside `handleUpgrade`, **immediately after** the source-resolution block (where `args := []string{"upgrade", "--source", source}` has been assembled but before the `execCommand("kairos-agent", args...)` call), insert:

```go
	// Install bundled extensions before the OS upgrade. Each install is
	// idempotent (kairos-agent install overwrites the .raw in place), so a
	// retry of the same compound command after a partial failure is safe.
	bundled, err := parseBundledExtensions(cmd.Args["extensions"])
	if err != nil {
		return "", err
	}
	scope := "active"
	if cmd.Command == "upgrade-recovery" {
		scope = "recovery"
	}
	for _, e := range bundled {
		if err := installBundledExtension(ctx, e, scope); err != nil {
			// Do NOT proceed to the OS upgrade if any extension fails.
			return "", err
		}
	}
```

- [ ] **Step 7: Run the bundle specs to verify they pass**

```
cd ~/_git/kairos-agent && go test ./internal/phonehome/... -run TestPhoneHome -ginkgo.focus="handleUpgrade — extensions bundle" -v
```

Expected: PASS (4 `It` blocks).

- [ ] **Step 8: Run the whole phonehome suite to catch any regression**

```
cd ~/_git/kairos-agent && go test ./internal/phonehome/... -v
```

Expected: all green.

- [ ] **Step 9: Commit**

```bash
git add internal/phonehome/handlers.go internal/phonehome/handlers_extension.go internal/phonehome/handlers_extension_test.go internal/phonehome/export_test.go
git commit -m "phonehome: install bundled extensions before OS upgrade"
```

---

## Task 9: Whole-repo verification

**Files:** none (verification only)

- [ ] **Step 1: Run the full Go test suite**

```
cd ~/_git/kairos-agent && go test ./... -count=1
```

Expected: all packages green. If any pre-existing test fails for an unrelated reason, investigate before continuing.

- [ ] **Step 2: Run gofmt and vet**

```
cd ~/_git/kairos-agent && gofmt -l internal/phonehome/ && go vet ./internal/phonehome/...
```

Expected: no output from `gofmt -l`, `go vet` exits 0.

- [ ] **Step 3: Build the binary**

```
cd ~/_git/kairos-agent && go build -o /tmp/kairos-agent ./
```

Expected: produces `/tmp/kairos-agent`.

- [ ] **Step 4: Smoke-check the CLI surface is unchanged**

```
/tmp/kairos-agent sysext --help
/tmp/kairos-agent confext --help
```

Expected: both list `list`, `enable`, `disable`, `install`, `remove` subcommands. (This task changes only the phonehome dispatcher, not the user-facing CLI.)

- [ ] **Step 5: Lint (if configured)**

If the repo has `.golangci.yml` or runs `golangci-lint` in CI:

```
cd ~/_git/kairos-agent && golangci-lint run ./internal/phonehome/...
```

Expected: no issues. If not configured locally, skip.

---

## Task 10: PR + tag

**Files:** none — maintainer step.

- [ ] **Step 1: Verify clean working tree**

```
cd ~/_git/kairos-agent && git status
```

Expected: "nothing to commit, working tree clean".

- [ ] **Step 2: Push the branch and open a PR**

PR description:

```markdown
## Summary
- Add `extension` phonehome command (install/enable/disable/remove for sysext + confext)
- Extend `upgrade` / `upgrade-recovery` to install bundled extensions before the OS upgrade reboot
- New `extension` command is opt-in (destructive); existing upgrade gate covers the ride-along

## Test plan
- [ ] `go test ./internal/phonehome/...` passes
- [ ] `go build ./` produces a binary
- [ ] Manual smoke: build a sysext via auroraboot, send via phonehome, verify it lands under /var/lib/kairos/extensions/

🤖 Generated with [Claude Code](https://claude.com/claude-code)
```

- [ ] **Step 3: After merge, tag and release**

After CI passes and the PR is merged, the maintainer runs:

```bash
cd ~/_git/kairos-agent
git fetch origin
git checkout main && git pull
# Pick the next version per the repo's semver convention.
git tag -a vX.Y.Z -m "vX.Y.Z: phonehome extension command + bundled upgrade"
git push origin vX.Y.Z
```

GitHub Actions will build release artifacts. AuroraBoot's Plan 2 will bump its `go.mod` to this tag.

---

## Spec coverage check

| Spec section | Covered by |
|---|---|
| Two delivery flows | Tasks 1-5 (manual) and 6-8 (bundled) |
| `extension` command (manual) | Tasks 1-5 |
| Extended `upgrade` with `extensions[]` | Tasks 6-8 |
| `upgrade-recovery` parity | Task 8 |
| JSON-encoded `extensions` arg (string-valued map) | Task 6 |
| Install action = install + enable (two CLI calls) | Task 3 |
| Conditional `enable --active` (skip if already enabled) | Task 7 |
| Abort before OS upgrade on extension failure | Tasks 5, 8 |
| No `--now` during compound upgrade | Task 7 |
| Idempotent re-installs | Task 7 (implicit — install overwrites .raw) |
| Policy gating (`extension` not in safe defaults) | Inherited from existing `DefaultCommandHandler` gate, verified in Task 2 |

## Out of scope for this plan

- Tagging convention / release machinery — defer to maintainer.
- Changes to `pkg/action/sysext.go` itself — none required.
- Multi-extension parallelism — extensions install sequentially per the spec.
