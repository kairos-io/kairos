# Interactive install: pre-installation checks (prerequisites)

The interactive installer (`kairos-agent interactive-install`) shows a
**Prerequisites** screen before anything else. On that screen, provider plugins
inspect the live system and return a list of *checks* — sanity checks,
prerequisite validations, or actions that need the user's acknowledgement. The
user reviews and answers them, and the providers then act on those answers
before the installation begins.

It reuses the same `go-pluggable` plugin protocol as the rest of the agent, but
the check plugins are a **separate plugin set** with their own name and
directories (see "Plugin discovery" below) so they never mix with the generic
agent providers (`agent-provider-*`).

Two working examples live under `tui-check-examples/`:

- [`tui-check-diskwipe`](tui-check-diskwipe) — an **optional**
  multi-select check: detects disks that still hold partitions from a previous
  Kairos installation and offers to wipe the ones the user selects.
- [`tui-check-magicword`](tui-check-magicword) — a **required**
  text check: the install won't continue until the user types the magic word,
  demonstrating text input, the answer round-trip and the retry path.

## Scope

- **Interactive TUI install only.** The non-interactive paths (`manual-install`,
  auto/unattended install, the web UI) do not run these checks.
- The screen runs **before disk selection** and before `RunInstall`, so it is
  identical for UKI and non-UKI installs.
- If no provider returns any check, the screen is skipped entirely (it never
  shows an empty page).

## Flow

```
TUI opens
  │
  ├─ publish  agent.interactive-install.checks         ──▶ providers inspect system
  │                                                         return []Check
  ├─ render checks, collect the user's answers
  │
  ├─ on "continue":
  │     ├─ if a blocking check is unsatisfied → stay on the screen
  │     └─ publish agent.interactive-install.checks.apply ──▶ providers act on the
  │                (carries the user's decisions)              confirmed/selected
  │                                                            answers, return []ApplyResult
  └─ advance to disk selection
```

Both events go over the standard kairos-agent plugin bus (`go-pluggable`); the
publish is synchronous (the plugin binaries run inline), so the round-trip
completes before the screen advances.

## Events

| Event                                    | Direction         | Payload (sent)  | Response data (returned by provider) |
|------------------------------------------|-------------------|-----------------|--------------------------------------|
| `agent.interactive-install.checks`       | agent → providers | `ChecksPayload` | JSON `[]Check`                       |
| `agent.interactive-install.checks.apply` | agent → providers | `ApplyPayload`  | JSON `[]ApplyResult`                 |

Constants and types are defined in
[`pkg/prereqs`](../pkg/prereqs/prereqs.go).

## Data types

### Check (provider → agent)

```go
type Check struct {
    ID          string   // stable id, echoed back on apply (namespace it, e.g. "diskwipe")
    Title       string   // short label
    Message     string   // detail shown to the user
    Severity    string   // "info" | "warning" | "error"
    Blocking    bool     // if true, install can't proceed until the check is satisfied (answered)
    Required    bool     // if true, a failed apply action blocks the install; if false, it's a warning
    Prompt      string   // "" | "confirm" | "text" | "select" | "multiselect"
    PromptText  string   // the question shown next to the widget
    Options     []Option // choices for select/multiselect ({ID, Label})
    Default     string   // "yes"/"no" (confirm), default text, an Option ID (select), or comma-separated Option IDs (multiselect)
    Placeholder string   // hint for an empty text input
}
```

### Decision (agent → provider, on apply)

```go
type Decision struct {
    ID        string   // matches Check.ID
    Prompt    string   // echo of the check's Prompt type
    Confirmed bool     // PromptConfirm
    Text      string   // PromptText
    Selected  []string // PromptSelect (one element) / PromptMultiSelect (zero or more Option IDs)
}
```

`ApplyPayload` wraps `[]Decision` (plus the current cloud-config); `ApplyResult`
is `{ID, Success, Message}`.

## Prompt types

| Prompt        | Widget                     | Answer field               |
|---------------|----------------------------|----------------------------|
| `""` (none)   | display only, no input     | —                          |
| `confirm`     | yes/no toggle              | `Confirmed`                |
| `text`        | free-text input            | `Text`                     |
| `select`      | cycle through `Options`    | `Selected` (one Option ID) |
| `multiselect` | checkbox list of `Options` | `Selected` (Option IDs)    |

## Severity and blocking

- `info` / `warning`: shown; the install proceeds.
- `error`: shown in red. Combine with `Blocking: true` for a hard prerequisite.
- `Blocking: true`: the install cannot continue until the check is *satisfied*:
  - a `confirm` check is satisfied once confirmed;
  - a `text` check once non-empty;
  - a `select`/`multiselect` check once at least one option is chosen;
  - a **display-only** blocking check can never be satisfied from the TUI — use
    it for unrecoverable failures (e.g. unsupported architecture, not enough
    RAM) where the only option is to quit.

## Failure handling

`Blocking` and `Required` are two independent gates:

- **`Blocking`** is a *pre-apply* gate. A blocking check must be *satisfied*
  (answered) before the user can continue. A blocking display-only check can
  never be satisfied and represents a hard, unrecoverable prerequisite.
- **`Required`** is a *post-apply* gate. It controls what happens when the
  provider's action fails (`ApplyResult.Success == false`):
  - **Required** (`Required: true`): the failure blocks the install. The screen
    shows the error in red; **enter retries** the apply, `q`/`ctrl+c` aborts.
  - **Optional** (`Required: false`, the default): the failure is shown as a
    warning; the user may **press enter to continue anyway**, or **`r` to
    retry**.

Changing any answer clears a pending failure, so the user can adjust their
selection (e.g. deselect the disk that failed to wipe) and continue or retry
from a clean state. A transport/exec error talking to the provider is always
treated as blocking.

All checks render in a **single list** on one screen; the user answers each and
a **single combined apply** runs on continue. Providers return one
`ApplyResult` per action they performed, so partial failures (e.g. 2 of 3 disks
wiped) are reported individually and classified by their check's `Required`
flag.

After an apply attempt, the screen shows a per-action results summary
(`✓`/`✗` with the provider's message) so the user can see exactly which actions
succeeded and which failed before deciding to retry or continue.

The example diskwipe provider leaves `Required` unset (optional): failing to
wipe a leftover *secondary* disk should not stop an install targeting a
different disk.

## Plugin discovery

Check plugins are discovered separately from the generic agent providers:

- **Name:** the executable must be named `tui-check-<name>` (prefix
  `tui-check-`), *not* `agent-provider-*`.
- **Directories:** `/system/tui-checks`, `/usr/local/system/tui-checks`, and the
  agent's working directory (handy for development/tests). `$PATH` is also
  scanned, as with all `go-pluggable` plugins.

The agent builds a dedicated `go-pluggable` manager for these events (it does
*not* reuse the global agent-provider bus), so a misbehaving check plugin can
never abort the install via `EventResponse.Error`. Discovery and every plugin
response are logged (see "Logging").

## Writing a provider

A check plugin is an executable named `tui-check-<name>` placed in one of the
directories above. It speaks the `go-pluggable` protocol: it is invoked with the
event name as `argv[1]` and the JSON `Event` on stdin, and prints an
`EventResponse` JSON to stdout.

The easiest way to write one in Go is the `pluggable.PluginFactory` helper, as in
the [diskwipe example](tui-check-diskwipe/main.go):

```go
factory := pluggable.NewPluginFactory(
    pluggable.FactoryPlugin{EventType: prereqs.EventChecks, PluginHandler: onChecks},
    pluggable.FactoryPlugin{EventType: prereqs.EventChecksApply, PluginHandler: onApply},
)
factory.Run(pluggable.EventType(os.Args[1]), os.Stdin, os.Stdout)
```

- `onChecks` inspects the system and returns `[]Check` (marshalled into
  `EventResponse.Data`).
- `onApply` reads the `ApplyPayload` from the event data, acts on the decisions
  whose `ID` it owns, and returns `[]ApplyResult`.

> **Important:** report problems through the returned data
> (`Severity: "error"` on a check, or `Success: false` on a result), **never**
> through `EventResponse.Error`. A non-empty `Error` is logged and that plugin's
> checks are dropped.

### Building and installing the examples

```bash
go build -o tui-check-diskwipe ./tui-check-examples/tui-check-diskwipe
install -m 0755 tui-check-diskwipe /usr/local/system/tui-checks/

go build -o tui-check-magicword ./tui-check-examples/tui-check-magicword
install -m 0755 tui-check-magicword /usr/local/system/tui-checks/
```

On the next interactive install, `tui-check-diskwipe` lists any disk carrying
`COS_*` partition labels in a multi-select; the selected disks are wiped
(`wipefs`) before the install proceeds. `tui-check-magicword` blocks the install
until the user types `jojo`.

## Logging

Plugin discovery and every plugin response are logged by the agent:

- Which directories were scanned and which `tui-check-*` plugins were found (or
  `No interactive-install check plugins found`).
- The `checks`/`apply` events being published and how many checks/results came
  back.
- Per plugin: its state, error, data size, and — importantly — its captured
  stdout/stderr. `go-pluggable` puts a plugin's own output in
  `EventResponse.Logs`, which the agent re-logs line by line, so anything a
  plugin prints (e.g. via `fmt.Fprintf(os.Stderr, …)`) reaches the agent log /
  journald. Note this output is captured *during the handler*: a plugin must
  write to `os.Stderr`/`os.Stdout` from inside the handler (resolving the
  descriptor at call time), not cache it before `factory.Run`.

## Testing

`pkg/prereqs` is covered by unit tests for the contract helpers (`ParseChecks`,
`Satisfied`, `Blocker`, `BuildDecisions`, `ClassifyFailures`, `DefaultAnswer`).

The end-to-end round-trip is covered in
`internal/agent/prerequisites_test.go`, which drives a dedicated bash test
plugin (`internal/agent/testdata/tui-checks/tui-check-prereqtest`) that emits one
check of every prompt type and records the apply payload, asserting the user's
answers reach the plugin intact. The test builds its own `go-pluggable` manager
loaded only with the fixture, mirroring the TUI and staying fully separate from
the global agent-provider bus.
