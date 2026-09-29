# splash

> The boot splash lives in the Kairos monorepo. See the
> [root README](../../README.md) for the full repository layout. Import path:
> `github.com/kairos-io/kairos/v4/internal/splash`.

The animated boot logo. It draws on `/dev/tty1` in two phases, so the
animation covers the whole boot rather than a slice of it:

1. **In the initramfs**, from the moment udev has made `/dev/tty1` until
   switch-root. Run by the `50kairos-splash` dracut module.
2. **In the booted system**, from switch-root until the login prompt. Run by
   `kairos-splash.service`, a oneshot ordered `Before=getty.target`.

Both phases are gated on the `splash` token being on the kernel command line,
which [`kairos-init`](../../kairos-init/) puts there. Dropping that token turns
the whole thing off without editing a unit, and `kairos.splash=0` at the boot
menu does it for a single boot.

## Turning it off

| Scope | How |
| --- | --- |
| One boot | Add `kairos.splash=0` at the boot menu |
| Every boot, whole image | Drop the `splash` token from the kernel command line |
| Build time | Remove `/usr/bin/kairos-splash` before the initramfs is built. The dracut module's `check()` and both units' `ConditionPathExists=` all gate on that path, so nothing is installed and nothing runs |

## Replacing it

Neither phase invokes `kairos splash`. Both exec **`/usr/bin/kairos-splash`**,
and so does the dracut module that pulls the animation into the initramfs.
That path is the override slot. `kairos-init` points it at the multi-call
`kairos` binary **only when nothing is already there**, so an image that ships
its own executable at that path keeps it and inherits all the wiring: the
initramfs copy, both phases on tty1, the kill switch, and the
`ConditionPathExists` that turns the splash off when there is nothing to run.

Drop your executable in before `kairos-init`'s **install** stage, which is
where the slot is claimed:

```dockerfile
COPY --chmod=0755 my-splash /usr/bin/kairos-splash

RUN --mount=type=bind,from=kairos-init,src=/kairos-init,dst=/kairos-init \
    /kairos-init -l debug -s install --version "${VERSION}"
RUN --mount=type=bind,from=kairos-init,src=/kairos-init,dst=/kairos-init \
    /kairos-init -l debug -s init --version "${VERSION}"
```

The install stage sees something at the path and leaves it alone; the init
stage then builds the initramfs, and the dracut module carries your file into
it. No unit, no drop-in and no dracut module has to change.

Rebuilding an image `kairos-init` has already run against works the same way:
replace the file, re-run both stages, and the new initramfs gets it. The
default symlink counts as "something is there" too, so remove it first if you
are replacing it in a second pass.

### What a replacement has to do

- **Be executable, and either an ELF binary or a script with a `#!` line.**
  The dracut module installs it with `inst_multiple`, which pulls in the
  shared libraries an ELF needs, or the interpreter a script names. A script
  that calls tools which are not in the initramfs will work in the second
  phase and not the first.
- **Accept `--duration=<duration>`**, a Go duration string. The
  booted-system phase passes it and expects the process to exit when the time
  is up, because getty waits for it. The initramfs phase passes nothing and
  expects the process to keep drawing until it gets `SIGTERM`, which arrives
  at switch-root.
- **Draw on stdout.** Both units hand over `/dev/tty1` as stdout, stdin and
  stderr, with `TTYReset=yes`, so the console is yours and is reset after you
  exit. Reading stdin is optional; the built-in animation polls it for the ESC
  toggle.
- **Exit on `SIGTERM`**, and exit non-zero rather than hang when it cannot
  draw. The initramfs unit is wired into `initrd.target.wants`, never
  `.requires`, so a failure degrades to "no animation" instead of stalling the
  boot. A process that ignores `SIGTERM` does stall it: that signal is how the
  initramfs phase is ended at switch-root, bounded by `TimeoutStopSec=5s`.

Branding alone does not need a replacement binary: the built-in animation
reads artwork from `/etc/kairos/branding/splash`, and the dracut module copies
whatever is in that directory into the initramfs. Replace the executable only
when you want a different animation, not a different logo.

## Running it by hand

```sh
kairos splash              # until SIGTERM
kairos splash --duration=3s
```

`kairos splash` and `/usr/bin/kairos-splash` are the same sub-tool; the second
is the symlink form, dispatched on `argv[0]`, the same way `/usr/bin/immucore`
resolves to the multi-call binary. The animation honours only the kill switch
and never the absence of the `splash` token, so it still draws on a dev box.
