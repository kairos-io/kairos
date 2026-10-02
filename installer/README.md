# installer

> The interactive installer lives in the Kairos monorepo. See the
> [root README](../README.md) for the full repository layout. Import
> path: `github.com/kairos-io/kairos/v4/installer`.

The default **interactive installer** for [Kairos](https://kairos.io).

`kairos-installer` is a standalone terminal UI that collects installation
settings (disk, user, SSH keys, hostname, timezone and keymap, system
extensions, post-install action, plus any provider-supplied fields) and then
drives [`kairos-agent`](../agent/) to perform the install.
It does **not** partition or install anything itself — that is `kairos-agent`'s
job. The installer only owns the UX and hands a configuration to the agent.

It also serves the **web installer**, on `:8080` by default, next to the
terminal UI and in the same process, so a live boot offers both frontends
without two services fighting over the port. Both frontends ask the same
questions: see [The installer steps](#the-installer-steps).

The terminal UI opens on that address: its first screen lists every URL the web
installer answers on, and renders the first of them as a QR code so it can be
opened from a phone. A short terminal drops the QR and keeps the URLs, and a
boot with no address to offer skips the screen instead of showing an empty one.

That first screen also carries an **Advanced** section, offered only when a
provider is installed to answer for it: pressing `a` hands the terminal to
`kairos-agent install`, the pairing flow that prints a go-nodepair QR code and
waits for `kairosctl register` to send a configuration. That is the other
remote install the live media has always had, and it is what the default boot
entry used to run.

One process means one lifetime: quitting the terminal UI ends the web session
too, unless an install started from the browser is still running, which the
installer serves to the end before it exits. Nothing re-execs the installer on
an interactive boot, so bringing the web UI back for that boot means running
the installer again by hand, once the first one has exited and released the
port.

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

Run the installer with `--no-tui` to serve only its web UI and draw no
terminal UI. The web installer is a frontend of the installer, not of the
agent, so an image that ships its own installer serves its own web UI. There
is no `kairos-agent webui` subcommand and no `kairos-webui` service any more:
every boot that runs the installer gets the web UI in that same process.

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

The web UI is another frontend on that same contract, not a separate path into
the agent. The browser builds the cloud-config with the wizard, and the server
sends it to `manual-install` the same way the terminal UI does. Its `/ws`
re-publishes the events above as one JSON object per frame:

```json
{"type":"step","step":"partition"}
{"type":"log","message":"a line of agent output"}
{"type":"error","message":"no target device found"}
{"type":"done","ok":true}
```

The stream is replayed from the start of the run, so reloading the progress
page shows the whole install rather than whatever arrives next.

The full, authoritative contract is documented in kairos-agent:
**[`docs/installer-contract.md`](https://github.com/kairos-io/kairos/blob/master/agent/docs/installer-contract.md)**.

---

## The installer steps

The questions are defined once, in `internal/wizard`, as data: an ordered list
of steps, each with its fields. The terminal UI and the web UI both draw that
list, and both send every answer through the same `wizard.Apply`, so a value
one frontend refuses the other refuses too, with the same message.

| Step | Asks for | Can be skipped |
| --- | --- | --- |
| `disk` | the disk to install to | no |
| `user` | a user name and a password | yes |
| `ssh_keys` | SSH public keys, or `github:` and `gitlab:` user names | yes |
| `hostname` | the host name | yes |
| `locale` | the timezone and the console keymap | yes |
| `extensions` | system extensions from the live media or the catalogs | yes |
| `provider` | the fields a provider plugin asks for; a field the plugin asks only after a yes gets that yes or no in front of it | yes, and it is shown only when a provider asks something |
| `finish` | reboot, power off, or nothing after the install | no, it starts on nothing |

`wizard.Render` turns the answers into a `#cloud-config`, and
`wizard.Finalize` writes the confirmed disk and finish action over it just
before the install starts. So a cloud-config edited by hand still installs to
the disk the operator confirmed, whatever its `install.device` says.

### Terminal UI

After the prerequisites, the terminal UI asks how to install. **Quick
install**, the default, asks for the disk and goes straight to the summary:
no user is created and nothing else is configured, so `enter`, `enter`,
`enter` and `y` install. The summary says that no user was set up, and `esc`
twice goes back to the choice, keeping the disk. **Customize** asks for the
disk, then offers **Start Install** (with the finish action) or **Customize
Further**, which opens a menu of the optional steps. On an image branded with
`interactive_install_advanced_disabled` there is nothing to customize, so the
terminal UI does not ask how to install: it asks for the disk, then offers
**Start Install** with the finish action. The summary page shows the generated cloud-config: `e` opens it in an
editor (`ctrl+s` keeps the edit, `ctrl+r` builds it again from the answers,
`esc` drops the edit), and `v` shows it read-only. `enter` on the summary asks
for a `y` before it erases the disk and starts the install. A long list, such
as the timezones, filters with `/`, and an optional list starts on "(leave
unset)". The extensions step reads the live media and the catalogs when it is
opened, not when the installer starts.

### Web installer

The web installer at `/` is a step-by-step wizard over the same steps. Each
step is one screen, and a rail on the side jumps between them. The last screen,
**Review and install**, shows the generated cloud-config in a text box that can
be edited by hand, checks it against the schema as you type, and has a
**Regenerate from answers** button. Install stays disabled until you tick the
box that confirms the disk will be erased. On an image branded with
`interactive_install_advanced_disabled`, the text box is read only and there is
no Regenerate button, as the terminal UI has no editor there.

The page talks to these endpoints:

| Endpoint | What it does |
| --- | --- |
| `GET /api/wizard` | the steps, with the disks and extensions found on this machine, and `advanced_disabled` |
| `POST /api/step/:id` | check one step's values and return the updated answers, or the errors per field |
| `POST /api/render` | build the cloud-config from the answers |
| `POST /validate-json` | check a cloud-config against the schema |
| `POST /install` | start the install |
| `GET /ws` | the install progress |

`POST /install` takes JSON (`cloud_config`, `device`, `finish_action`) or the
older form fields, so a script can still post a finished cloud-config to it
directly. The JSON keys of the previous web UI, `cloud-config` and
`installation-device`, are still read; the new keys win when both are sent. The device and finish action it is given win over the ones in the
text.

---

## Driving an install with an agent (MCP)

Alongside its other two frontends, `kairos-installer` serves the same install
contract over the [Model Context Protocol](https://modelcontextprotocol.io), so
an AI agent can do what a person does on the screen. The transport is streamable
HTTP, and it is a route on the web installer's own server rather than a second
listener: **`/mcp`** on whatever address the web UI is on, `:8080` by default.

It runs in both modes, including `--no-tui`. That mode is the one with no
console session to install from, so it is the one that most needs an agent to be
able to drive it.

| Tool | What it does | Writes anything? |
| --- | --- | --- |
| `list_disks` | the disks an install can target | no |
| `list_prerequisites` | run the provider `tui-check-*` plugins | no |
| `apply_prerequisites` | act on those checks | yes, whatever the plugin does |
| `get_install_options` | agent binary, disks, finish actions, progress steps | no |
| `install` | perform the install | **repartitions a disk** |
| `collect_debug_bundle` | write a debug bundle | writes the bundle |

`install` refuses to run unless `confirm=true` and the device is an
installation candidate at the moment of the call, and it runs once per boot.

### Turning it off

**Nothing on this endpoint is authenticated.** Anything that can reach the web
installer can call every tool, `install` included, and a `cloud_config` passed
to `install` reaches the installed system.

That is exactly the web installer's own exposure, which is the point of sharing
its listener: there is one address on the machine to reason about, one
`webui.listen_address` that moves it, and `webui.disable` switches this off with
it, because there is no server left to hang the route on. An image that wants
the browser installer without the agent one says so in
`/etc/kairos/agent.yaml`:

```yaml
mcp:
  disable: true
```

That block is what an operator has on a real boot, because `kairos-agent
interactive-install` execs the installer with a fixed argument list and no flag
of yours ever reaches it.

```sh
kairos-installer            # TUI and web UI, MCP at :8080/mcp
kairos-installer --no-tui   # web UI, MCP at :8080/mcp
```

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
  an operator asks for a web-only frontend without a terminal to draw on.
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

- **`internal/wizard`** - the steps both frontends ask, how their answers are
  checked, and how the answers become a `#cloud-config`.
- **`internal/tui`** - the bubbletea model and pages that draw those steps.

---

## Architecture

```
main.go               flags (--source, --no-tui, --collect-debug-bundle),
                      serves the web UI with the MCP endpoint mounted on it,
                      and unless --no-tui runs the bubbletea program alongside
internal/wizard/      the steps both frontends ask, their checks, and the
                      cloud-config the answers render to
internal/tui/         the terminal UX: model, pages and branding over the wizard
                      steps; the install page calls kairos-sdk/agentrun and
                      renders progress
internal/webui/       the web frontend: embedded wizard assets, the /api
                      endpoints over the wizard steps, cloud-config
                      validation, and the install/progress websocket. It calls
                      kairos-sdk/agentrun too, so /ws carries the same typed
                      progress events the TUI renders. It owns the router, so
                      the MCP route hangs off it
internal/mcp/         the same install contract exposed as MCP tools an agent
                      can call, sharing the cloud-config shaping with the TUI.
                      An http.Handler, not a server
internal/checks/      gathers provider prerequisite checks over the bus and
                      applies the answers the user gave
internal/disks/       block-device discovery for the disk-selection page
internal/debugbundle/ collects, serves and copies out a debug bundle
prereqs/              the Check and prompt types providers and the TUI share
```

Echo writes its own log to a file (`/var/log/kairos/webui.log`) whenever the TUI
is running, because its default handler writes JSON to stdout and that would
land on top of the alt screen. With `--no-tui` it logs to stdout, so it ends up
in the journal.

`--source` reaches all three frontends: the web UI passes it to
`manual-install` the same way `agentrun.Command` does for the TUI, and the MCP
server keeps it as the default its `install` tool uses when the caller names no
`source`, reporting it as `default_source` from `get_install_options`. So an
install driven from the browser or by an agent pulls the image the boot asked
for, the same one the terminal installer would.

The reusable pieces live in the **SDK**: `sdk/agentrun` drives
`kairos-agent manual-install` and parses its JSON-Lines progress, and `sdk/bus`
is the provider plugin bus (`agent.interactive-install → []YAMLPrompt`). This
package is the three frontends (TUI, web UI and MCP) on top of those.

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
