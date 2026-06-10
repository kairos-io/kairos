# kairos-installer

The default **interactive installer** for [Kairos](https://kairos.io).

`kairos-installer` is a standalone terminal UI that collects installation
settings (disk, user, SSH keys, post-install action, plus any provider-supplied
fields) and then drives [`kairos-agent`](https://github.com/kairos-io/kairos-agent)
to perform the install. It does **not** partition or install anything itself —
that is `kairos-agent`'s job. The installer only owns the UX and hands a
configuration to the agent.

It is shipped in Kairos images (by
[`kairos-init`](https://github.com/kairos-io/kairos-init)) at
`/system/installer/kairos-installer`, where `kairos-agent interactive-install`
picks it up automatically.

---

## How it fits in (the contract)

`kairos-agent interactive-install` is a **dispatcher**. It resolves an installer
binary and execs it, inheriting the terminal. Resolution order (first existing
wins):

1. `$KAIROS_INSTALLER` — explicit path (testing/override)
2. `/system/installer/installer` — **override slot** (you drop your binary here)
3. `/system/installer/kairos-installer` — the default (this project)

The agent forwards `--source <uri>` to the installer. The installer, in turn,
drives the install by running:

```sh
kairos-agent manual-install --use-default-dirs --source <uri> [--reboot|--poweroff] <config.yaml>
```

where `<config.yaml>` is a `#cloud-config` file the installer generated. If the
child environment has `KAIROS_AGENT_PROGRESS=1`, the agent emits machine-readable
progress as **JSON Lines** on stdout:

```json
{"event":"step","step":"partition"}
{"event":"step","step":"done"}
{"event":"error","message":"no target device found"}
```

The full, authoritative contract is documented in kairos-agent:
**[`docs/installer-contract.md`](https://github.com/kairos-io/kairos-agent/blob/main/docs/installer-contract.md)**.

---

## Overriding with your own installer

You do **not** need to fork this project to ship a different installer. There
are three levels of customization, from lightest to heaviest.

### 1. Add fields without writing an installer — provider plugins

The installer asks Kairos *providers* for extra questions to show, so a distro or
product can extend the flow without touching the installer at all.

Ship an executable named `agent-provider-<name>` in `/system/providers` (or
`/usr/local/system/providers`). When invoked with the event name
`agent.interactive-install` as its first argument and a JSON payload on stdin, it
should print a JSON array of prompts on stdout:

```json
[
  {
    "YAMLSection": "myapp.token",
    "Prompt": "Enrollment token",
    "PlaceHolder": "paste token here",
    "Default": "",
    "Bool": false
  }
]
```

The fields map to `kairos-sdk/bus.YAMLPrompt`. Each prompt becomes a page in the
installer, and the answer is merged into the generated `#cloud-config` at the
dotted `YAMLSection` path (`myapp.token` → `myapp: { token: ... }`). `Bool: true`
renders a yes/no question. This is the same provider/bus mechanism kairos-agent
uses, so existing providers keep working.

### 2. Replace the whole UX — a drop-in binary (any language)

Place any executable at **`/system/installer/installer`**. It takes precedence
over the bundled default. The agent execs it directly, so it can be written in
any language. Your binary must:

- accept `--source <uri>` (the agent forwards it; it may be empty);
- run on the inherited terminal (stdin/stdout/stderr are passed through);
- gather whatever input it wants, write a `#cloud-config` to a temp file, then
  drive the install:
  ```sh
  KAIROS_AGENT_PROGRESS=1 kairos-agent manual-install \
      --use-default-dirs --source "$SOURCE" [--reboot|--poweroff] /tmp/your-config.yaml
  ```
- (optional) read the agent's stdout line by line; lines that parse as JSON with
  an `event` field are progress events (`step`/`error`) — render them however you
  like; everything else is ordinary log output you can show or ignore;
- exit non-zero on failure — the agent/dispatcher propagates the exit code.

You never reimplement partitioning or installation; you only produce a
`#cloud-config` and call `manual-install`.

### 3. Build on this project as a base — Go

Fork or vendor this repo and customize the bubbletea UI. The building blocks:

- **`internal/agentrun`** — the reference implementation of the install
  contract: resolve the agent (`ResolveAgentBin`), build the `manual-install`
  command (`Command`), parse a JSON-Lines progress line (`ParseLine`), and run +
  stream events (`Run`). Reuse this rather than hand-rolling the invocation and
  progress parsing.
- **`internal/bus`** — the provider bus used to gather `YAMLPrompt`s (level 1).
- **`internal/tui`** — the bubbletea model and pages, including
  `cloudconfig.go` which turns the collected model into a `#cloud-config`.

> Reuse note: `agentrun` is the genuinely reusable, TUI-free core. It currently
> lives under `internal/`; promoting it to an importable package (so custom
> installers can depend on it without forking) is on the roadmap.

---

## Architecture

```
main.go               flag(--source) → launch the bubbletea program
internal/tui/         the UX: model, pages, branding, and cloud-config shaping;
                      the install page calls agentrun and renders progress
internal/agentrun/    drives `kairos-agent manual-install` + parses JSON-Lines
                      progress — no TUI dependencies (unit-tested in isolation)
internal/bus/         provider plugin bus (agent.interactive-install → []YAMLPrompt)
```

Decoupling: this module depends only on `kairos-sdk`, the charmbracelet TUI
libraries, and `go-pluggable`. It never imports `kairos-agent` — the only
coupling is the documented CLI contract.

---

## Development

```sh
go build ./...
go run github.com/onsi/ginkgo/v2/ginkgo run -p --race -r ./...   # tests
```

Releases are cut by GoReleaser on a `v*` tag (multi-arch `linux/{amd64,arm64,riscv64}`,
CGO off, no FIPS). `kairos-init` embeds the released binary into images at
`/system/installer/kairos-installer`.
