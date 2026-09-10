# installer

> The interactive installer lives in the Kairos monorepo. See the
> [root README](../README.md) for the full repository layout. Import
> path: `github.com/kairos-io/kairos/v4/installer`.

The default **interactive installer** for [Kairos](https://kairos.io).

`kairos-installer` is a standalone terminal UI that collects installation
settings (disk, user, SSH keys, post-install action, plus any provider-supplied
fields) and then drives [`kairos-agent`](../agent/) to perform the install.
It does **not** partition or install anything itself — that is `kairos-agent`'s
job. The installer only owns the UX and hands a configuration to the agent.

It also serves the **web installer**, on `:8080` by default, next to the
terminal UI and in the same process, so a live boot offers both frontends
without two services fighting over the port.

It is shipped in Kairos images (by [`kairos-init`](../kairos-init/)) at
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

`kairos-agent webui` is a dispatcher onto the same binary, with the same
resolution order, adding `--no-tui`. In that mode the installer serves only its
web UI and never touches the terminal, which is what the non-interactive live
boot entry wants. The web installer is a frontend of the installer, not of the
agent, so an image that ships its own installer serves its own web UI.

That subcommand is **deprecated** and prints a warning: it exists only so the
`kairos-webui` service keeps working, and it goes away with that service. Call
the installer with `--no-tui` instead.

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
- accept `--no-tui`, and in that mode draw no terminal UI: it is how
  `kairos-agent webui` asks for a web-only frontend on a non-interactive boot.
  Plain log lines on stdout/stderr are fine there, since nothing owns the
  screen. Serving nothing and exiting 0 is a valid answer if you have no web UI;
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

The reusable, TUI-free core lives in **kairos-sdk** so you can import it without
forking this repo:

- **[`github.com/kairos-io/kairos/v4/sdk/agentrun`](https://pkg.go.dev/github.com/kairos-io/kairos/v4/sdk/agentrun)**
  — the reference implementation of the install contract: resolve the agent
  (`ResolveAgentBin`), build the `manual-install` command (`Command`), parse a
  JSON-Lines progress line (`ParseLine`), and run + stream events (`Run`). Import
  this rather than hand-rolling the invocation and progress parsing — it's exactly
  what this installer uses.

The provider bus used to gather `YAMLPrompt`s (level 1) is also in the SDK —
`github.com/kairos-io/kairos/v4/sdk/bus` (`bus.NewBus()`), so you don't need to
copy it either.

To customize the UX itself, fork or vendor this repo:

- **`internal/tui`** — the bubbletea model and pages, including
  `cloudconfig.go` which turns the collected model into a `#cloud-config`.

---

## Architecture

```
main.go               flags(--source, --no-tui) → serve the web UI, and unless
                      --no-tui, launch the bubbletea program alongside it
internal/tui/         the UX: model, pages, branding, and cloud-config shaping;
                      the install page calls kairos-sdk/agentrun and renders progress
internal/webui/       the web frontend: embedded assets, cloud-config validation,
                      and the install/progress websocket
```

Echo writes its own log to a file (`/var/log/kairos/webui.log`) whenever the TUI
is running, because its default handler writes JSON to stdout and that would
land on top of the alt screen. With `--no-tui` it logs to stdout, so it ends up
in the journal.

`--source` reaches both frontends: the web UI passes it to `manual-install` the
same way `agentrun.Command` does for the TUI, so an install driven from the
browser pulls the image the boot asked for.

The reusable pieces live in **kairos-sdk**: `kairos-sdk/agentrun` drives
`kairos-agent manual-install` and parses its JSON-Lines progress, and
`kairos-sdk/bus` is the provider plugin bus (`agent.interactive-install →
[]YAMLPrompt`). This project is mostly the bubbletea UI on top of those.

Decoupling: this module depends only on `kairos-sdk`, the charmbracelet TUI
libraries, and `go-pluggable`. It never imports `kairos-agent` — the only
coupling is the documented CLI contract.

---

## Development

Standalone build from the repo root:

```sh
go build ./installer
```

Or as part of the whole monorepo build pipeline:

```sh
make kairos-installer   # produces dist/linux-<arch>/kairos-installer
make binaries           # builds everything: kairos, kcrypt-challenger, kairos-installer, kairos-init
```

`kairos-init` embeds the built binary into images at `/system/installer/kairos-installer` at build time.
