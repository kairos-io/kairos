# Installer ↔ kairos-agent contract

kairos-agent owns partitioning/configuration/install. The interactive **UX**
is owned by a separate `kairos-installer` binary. This document is the stable
contract between them.

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
