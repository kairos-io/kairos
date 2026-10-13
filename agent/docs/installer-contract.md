# Installer ↔ kairos-agent contract

kairos-agent owns partitioning/configuration/install. The interactive **UX**
is owned by a separate `kairos-installer` binary. This document is the stable
contract between them.

## Unattended installs come first

Both live boot entrypoints, `install` and `interactive-install`, call
`agent.AutoInstall` before they show anything. If the config says
`install.auto: true`, that call performs the installation and the command
returns: a config that says "install me without asking" leaves nothing to ask,
and the live CD must not stop at a prompt with nobody there to answer it.

An installer is therefore resolved and launched only when there is a decision
left for a human to make. `interactive-install` itself does not read the
config; the switch is at the call site, not inside the installer path.

### Opting out: `--skip-auto-install`

An operator who boots the media to look at a machine rather than to install it
needs the installer even on a node whose datasource says `install.auto: true`.
`interactive-install` takes `--skip-auto-install` for that: the `install.auto`
check is not made at all and the installer runs, which is what the command did
before it learned to read the config.

It is off by default, and it stays off by default: unattended installs are the
case with nobody at the console, so they win unless someone present says
otherwise. There are three ways to say so, in this order:

1. `kairos-agent interactive-install --skip-auto-install`
2. `KAIROS_SKIP_AUTO_INSTALL=true` in the environment
3. `kairos.skip-auto-install` on the kernel command line (also `=true` / `=1`)

The third exists because the first two cannot be reached from a booted ISO: the
`kairos-interactive` unit's `ExecStart` is fixed at
`/usr/bin/kairos-agent interactive-install --shell`, so editing the GRUB entry
is what an operator at the console can actually do.

`install` has no such flag. It has always honoured `install.auto`, so there is
no previous behaviour to preserve there.

`--shell` spawns a shell after the installer exits, so it applies only when an
installer was launched. On the unattended path none was, and the flag has
nothing to run after; `interactive-install` logs that it ignored it rather than
dropping it without a word. `--skip-auto-install` is what gets both the
installer and the shell on such a node.

## Discovery & launch

`kairos-agent interactive-install` resolves an installer binary in this order
(first existing path wins):

1. `KAIROS_INSTALLER` environment variable — an explicit path (testing/override),
   used only if the file exists.
2. `/system/installer/installer` — override slot; a customizer drops their own
   binary here and it takes precedence.
3. `/system/installer/kairos-installer` — the default, placed in the image by
   kairos-init.

If none exist, `interactive-install` exits with an error — there is no in-process
TUI fallback. When an installer is found, the agent execs it with the tty
inherited (stdin/stdout/stderr passed through) and forwards `--source <source>`
when a source was given. The installer's exit code is propagated.

## Driving the install

The installer gathers configuration, writes a `#cloud-config` file, and runs:

    kairos-agent manual-install --source <source> [--reboot|--poweroff] <config.yaml>

To receive progress markers (below), the installer MUST set the environment
variable `KAIROS_AGENT_PROGRESS=1` in that child process.

## Progress events (stdout)

When `KAIROS_AGENT_PROGRESS` is non-empty, the install action writes one JSON
object per line ("JSON Lines") to stdout. Each line has an `event` field.

Step events:

    {"event":"step","step":"<step>"}

where `<step>` is one of, in order:

    partition  before-install  active  bootloader  recovery  passive  after-install  done

Steps that do not run are omitted: e.g. `partition` is not emitted when the
install reuses a pre-prepared disk (`NoFormat`). Consumers should treat the
sequence as monotonic-but-possibly-sparse, not a fixed-length list.

Failure event:

    {"event":"error","message":"<message>"}

Consumers should parse each line as JSON; lines that are not valid JSON are
ordinary agent log output and may be shown or ignored. For forward
compatibility, ignore lines whose `event` is unknown and tolerate additional
fields. Events are NOT emitted when the env var is unset (human installs and
the in-process TUI fallback are unaffected).
