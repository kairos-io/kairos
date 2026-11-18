# End-to-end tests for phonehome with real VMs

Date: 2026-10-23
Related: kairos-io/kairos#4195. This lands as its own PR before rolling batches
(`2026-10-22-rolling-batches-design.md`), which then adds its concurrency spec
to this tier.

## Why

A batch upgrade failed on real nodes while every test passed. The two causes
were outside what the current tests reach:

- AuroraBoot's kairos-init step replaced the base image's agent with an older
  one, which got a 401 on the image download. Only an artifact built through
  AuroraBoot and installed on a node shows this.
- The agent looked for the persistent partition where the installer mounts it,
  not where a running system has it. Only a node that booted from disk shows
  this.

Rolling batches add a reboot loop on top: a node upgrades, reboots, registers
again, and only then does the next one start. Containers cannot reboot, so a
test with containers standing in for nodes would pass while that loop is
broken. This tier runs AuroraBoot and two real VMs, so phonehome features no
longer need manual testing on every change.

## Goals

- One Ginkgo tier that, against a real AuroraBoot container and two QEMU VMs
  installed from an ISO AuroraBoot built:
  - registers both nodes;
  - runs a batch upgrade that fails before the reboot and checks that both
    nodes report the failure and stay usable;
  - runs a batch upgrade to a second artifact and checks that both nodes
    rebooted into it, registered again and report the upgrade done;
  - checks that AuroraBoot exported the upgrade image once for both nodes.
- It runs in CI on pull requests that touch phonehome code, nightly, and by
  hand.
- When it fails, the report holds what is needed to tell why without
  rerunning it: AuroraBoot's logs, each VM's serial log, and each node's agent
  logs.
- A developer can run it locally with one `make` target.

## Non-goals

- Reset, reboot and the other commands. They can be added to the same tier
  later; upgrade is the path that broke and the one rolling batches builds on.
- Rolling batch behavior (concurrency, stop on failure, timeouts). The rolling
  batches branch adds those specs.
- A node that never comes back. Staging it in a VM is slow and fiddly; the
  store tests of rolling batches cover timeouts.
- Secure boot, UKI and TPM. Other e2e tiers cover booting those; this tier
  boots a plain EFI system.
- Running on every pull request. It is too slow for that.

## Anti-goals

- A flaky tier. A tier that fails at random will be ignored. All waits poll a
  condition with a generous timeout; there are no fixed sleeps, and timeouts
  name what they waited for.
- Leftovers. VMs, their state directories and the AuroraBoot container are
  removed whether the specs pass or fail.
- Testing a fake. No step may replace the agent, the reboot or the image
  download with a stand-in.

## How it works

### Where it lives

- `e2e/phonehome_vm_test.go`: one `Describe("phonehome on VMs",
  Label("phonehome-vm"), Ordered, Serial)`.
- It reuses the suite's helpers: `startWebServer` (AuroraBoot in a container on
  the host network, with `AURORABOOT_URL=http://10.0.2.2:<port>`, the host as
  seen from a QEMU user-network guest), and the VM helpers of
  `bootable_test.go` (`defaultVMOpts`, `saveSerialLog`, `gatherLogs`). The VM
  helpers read the ISO from the `ISO` environment variable today; they get a
  variant that takes the ISO path as an argument, so this tier can pass the
  ISO it built.

### Inputs

Environment variables, with defaults so a local run needs none:

- `PHONEHOME_BASE_IMAGE`: the Kairos image the artifacts are built from,
  `quay.io/kairos/hadron:v0.5.3-core-amd64-generic-v4.4.0-rc2`. It must ship an
  agent with the persistent-mount fix (v4.4.0-rc2 or later): artifact B is a
  Kairos image already, so AuroraBoot skips kairos-init for it and the nodes
  run the base image's own agent after the upgrade. The core flavor keeps
  builds and boots short; nothing in the tier needs Kubernetes.
- `PHONEHOME_KAIROS_INIT_IMAGE`: the kairos-init image the first artifact uses,
  `quay.io/kairos/kairos-init:v4.4.0-rc2`. AuroraBoot's default is v4.3.0, whose
  agent cannot download artifact images. The variable and the override go away
  when the default moves to v4.4.0.
- `KVM`, `MEMORY`, `CPUS`: as for the other VM tiers. This tier defaults
  `MEMORY` to 4000 per VM, since it runs two at once.

### Flow

`BeforeAll`:

1. Start AuroraBoot from the `auroraboot:test` image.
2. Build artifact A through the API: an ISO from `PHONEHOME_BASE_IMAGE` with
   `kairosInitImage` set to `PHONEHOME_KAIROS_INIT_IMAGE`, and a cloud-config
   that installs to the first disk and reboots, and creates user
   `kairos`/`kairos` for SSH. The phonehome URL and registration token come
   from AuroraBoot, as for any artifact it builds.
3. Build artifact B, the upgrade target: a Dockerfile build `FROM
   PHONEHOME_BASE_IMAGE` that adds `/etc/auroraboot-e2e-marker` containing a token
   the tier generates (B's ID is not known before the build). The marker proves the node runs B after the upgrade.
4. Download A's ISO and boot two VMs from it in parallel. Wait until both
   nodes are registered and online with boot state `active`, which means they
   installed, rebooted from disk and phoned home.
5. Put both nodes in a group created for the tier.

Specs, in order:

1. **An upgrade that fails before the reboot.** Batch upgrade of the group
   from `artifact:<an ID AuroraBoot does not know>`. The image download gets a
   404 from AuroraBoot, so the failure goes through AuroraBoot's own download
   path and needs no outside registry. Both commands end `Failed`, both
   nodes report upgrade state `failed`, and both are still online on `active`
   and reachable over SSH.
2. **A batch upgrade.** Batch upgrade of the group to `artifact:<B>`. Both
   commands end `Completed`; both nodes register again with boot state
   `active` and upgrade state `done`; over SSH, `/etc/auroraboot-e2e-marker`
   holds that token on both. AuroraBoot's log has exactly one
   `image export for artifact <B>: built` line, and every other
   `image export for artifact <B>:` line says it waited for that build or was
   served from the cache. The number of downloads is not checked: the agent
   may retry or resume one, and that is correct behavior.

`AfterAll`: destroy both VMs and remove the AuroraBoot container. When a spec
failed, first attach AuroraBoot's full log, each VM's serial log, and each
node's `journalctl` for the phonehome service and the upgrade to the report,
using the existing gather helpers.

### Timeouts

Generous and named, as the builder sees fit: in the order of 20 minutes for
each artifact build, 15 minutes for an install and first registration, 15
minutes for an upgrade to come back.

### CI

A new workflow, `.github/workflows/e2e-phonehome.yml`:

- Triggers: `pull_request` with a `paths` filter on `pkg/handlers/**`,
  `pkg/ws/**`, `internal/store/**`, `internal/builder/**`, `e2e/phonehome*`,
  `e2e/bootable_test.go`, `e2e/extensions_web_test.go` and the workflow file;
  `schedule` nightly; `workflow_dispatch`.
- One job on an Ubuntu runner with KVM: build the `auroraboot:test` image the
  same way `tests.yml` does, install QEMU and OVMF as the bootable jobs do,
  run `ginkgo --label-filter phonehome-vm ./e2e`, and upload the gathered logs
  as an artifact when it fails. Job timeout 120 minutes.

### Local run

`make test-e2e-phonehome` builds `auroraboot:test` and runs the tier with
`KVM=true`.

## Decided by me (not discussed)

- Two VMs, plain EFI, no secure boot.
- The upgrade target is a second artifact with a marker file, not a newer
  Kairos release, so the test does not depend on two published releases.
- A failing upgrade first, then the successful one, so the cheap spec runs
  before the expensive one and the success spec also shows that a failed
  upgrade leaves nodes able to upgrade later.
- Its own workflow with a paths filter, so the pull request checks stay fast
  for unrelated changes.
- The base image default, the failing source and the export assertion above.

## Left to the builder

- The exact API payloads for building the artifacts and starting the batch
  (read them from the UI's API calls and the handlers).
- Whether the phonehome config needs `allowed_commands` for `upgrade`, which
  depends on the agent's defaults.
- How to tell "rebooted and registered again" from "still online from before":
  for example the node's `lastUpgrade` timestamp, or the VM's boot ID over SSH.
- How the image build is shared with `tests.yml`, if at all.
