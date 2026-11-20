# Phonehome VM e2e Tier Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Ginkgo e2e tier that runs AuroraBoot and two real QEMU VMs installed from an AuroraBoot-built ISO, and checks a failing and a successful batch upgrade end to end.

**Architecture:** The tier lives in `e2e/` next to the other VM tiers and reuses their helpers: `startWebServer` for AuroraBoot in a container, and the peg-based VM helpers of `bootable_test.go`, which learn to boot an ISO passed as an argument. The tier drives AuroraBoot only through its HTTP API and checks the nodes over SSH. A Makefile target runs it locally; a separate workflow runs it in CI.

**Tech Stack:** Go, Ginkgo v2 + Gomega, peg (QEMU), Docker, GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-10-23-e2e-phonehome-vm-design.md`

## Global Constraints

- Label `phonehome-vm`; `Describe("phonehome on VMs", Label("phonehome-vm"), Ordered, Serial, ...)` in `e2e/phonehome_vm_test.go`.
- `PHONEHOME_BASE_IMAGE` default `quay.io/kairos/hadron:v0.5.3-core-amd64-generic-v4.4.0-rc2`.
- `PHONEHOME_KAIROS_INIT_IMAGE` default `quay.io/kairos/kairos-init:v4.4.0-rc2`, used for artifact A only.
- `MEMORY` default `4000` and `CPUS` default `2` for this tier's VMs (the other tiers keep their defaults).
- Marker file `/etc/auroraboot-e2e-marker`.
- No fixed sleeps. Every wait is an `Eventually` with a timeout and a description naming what it waited for. Builds 20 minutes, install and first registration 15 minutes, an upgrade coming back 15 minutes.
- VMs and the AuroraBoot container are removed in `AfterAll` whatever the outcome.
- Workflow `.github/workflows/e2e-phonehome.yml`, job timeout 120 minutes.
- Go tests use Ginkgo v2 + Gomega. Comments describe the current code, no history.
- Commits: `git commit --no-gpg-sign`, message ends with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never push. Never `git add` `auroraboot-data/`.

## Review Focus

1. **Live-ISO registrations.** A VM may phone home from the live ISO before it installs, and again from disk. Expected: the tier waits for exactly two nodes in the group with boot state `active`, and later steps only use those two. Covered in Task 2 by the registration wait.
2. **A node that did not actually reboot.** A node that was online before the upgrade stays online if it never reboots. Expected: "came back" is proven by a changed `/proc/sys/kernel/random/boot_id` over SSH, not by the node being online. Task 2, spec 2.
3. **Version skew hiding behind green.** The tier must fail if the agent on the nodes cannot download artifact images. Expected: a run with `PHONEHOME_KAIROS_INIT_IMAGE=quay.io/kairos/kairos-init:v4.3.0` fails spec 2. Task 2, step 4.
4. **A failure leaves debris.** Expected: after a failed spec, no `qemu-system` process and no AuroraBoot container from the tier remain, and the logs directory holds AuroraBoot's log and each VM's serial log. Task 2, step 4.
5. **Another tier's VM helpers change behaviour.** Expected: the `bootable` and `raw-bootable` tiers still boot from the `ISO`/`RAW_IMAGE` variables exactly as before. Task 1.

---

### Task 1: VM helpers that boot a given ISO and keep logs per VM

**Files:**
- Modify: `e2e/bootable_test.go`
- Test: `e2e/vm_opts_test.go` (create)

**Interfaces:**
- Produces:

```go
// vmSpec names what a VM boots from and how big it is. Empty fields fall
// back to the MEMORY and CPUS variables and to their current defaults.
type vmSpec struct {
    ISO      string
    RawImage string
    Memory   string
    CPUs     string
}

// vmOpts builds the machine options for spec without reading ISO or RAW_IMAGE.
func vmOpts(stateDir string, spec vmSpec) []types.MachineOption

// startVMWith starts a VM booting spec, with a 25000 MB drive.
func startVMWith(spec vmSpec) (testVM, error)

// vmLogDir is where saveSerialLog and gatherLogs write for vm:
// <base>/<vm ID>, with <base> from VM_LOG_DIR or the current default.
func vmLogDir(vm testVM) string
```

  `startVM`, `defaultVMOpts` and `defaultVMOptsNoDrives` keep their signatures and behaviour: they read `ISO`/`RAW_IMAGE` (exiting when both are missing, as today) and call `vmOpts`. `saveSerialLog` and `gatherLogs` write under `vmLogDir(vm)` so two VMs do not overwrite each other.

- [ ] **Step 1: Write the failing specs** in `e2e/vm_opts_test.go`, `Describe("vmOpts", Label("e2e"))`. Apply the returned options to a `types.MachineConfig` and assert:
  - `It("boots the ISO it is given")`: `vmOpts(dir, vmSpec{ISO: "/x.iso"})` → config ISO `/x.iso`, with `ISO` unset in the environment (use `GinkgoT().Setenv("ISO", "")`).
  - `It("uses the memory and CPUs it is given over the environment")`: `MEMORY=6000` set, spec Memory `4000` → config memory `4000`.
  - `It("falls back to MEMORY and CPUS")`.
- [ ] **Step 2: Run** `go run github.com/onsi/ginkgo/v2/ginkgo --label-filter e2e --focus vmOpts ./e2e`. Expected: compile failure.
- [ ] **Step 3: Implement** `vmSpec`, `vmOpts`, `startVMWith`, `vmLogDir`, and route the existing helpers through them. Keep `emulateTPM` per VM state dir as today.
- [ ] **Step 4: Run** the specs again (pass), then `go vet ./e2e` and `go run github.com/onsi/ginkgo/v2/ginkgo --dry-run -v --label-filter 'bootable || raw-bootable' ./e2e` (the tiers still list their specs; Review Focus 5).
- [ ] **Step 5: Commit** `test(e2e): boot a VM from a given ISO and keep logs per VM`.

---

### Task 2: The phonehome tier and its Makefile target

**Files:**
- Create: `e2e/phonehome_vm_test.go`, `e2e/phonehome_api_test.go`
- Modify: `Makefile`

**Interfaces:**
- Consumes: `startVMWith`, `vmSpec`, `vmLogDir`, `saveSerialLog`, `gatherLogs` (Task 1); `startWebServer`, `webServer.do` (`e2e/extensions_web_test.go`).
- Produces, in `e2e/phonehome_api_test.go`, methods on `*webServer` that fail the spec on an unexpected status:

```go
func (w *webServer) createGroup(name string) string                       // group ID
func (w *webServer) buildArtifact(body map[string]any) string             // artifact ID, waits up to 20m for Ready, attaches build logs on failure
func (w *webServer) downloadISO(artifactID, dir string) string            // path of the .iso, via GET /api/v1/artifacts/:id/download/<file>
func (w *webServer) groupNodes(groupID string) []map[string]any           // GET /api/v1/nodes?group=<id> (check the handler for the filter name)
func (w *webServer) sendGroupCommand(groupID, command string, args map[string]string) []string // command IDs
func (w *webServer) commandPhase(nodeID, commandID string) string
func (w *webServer) node(nodeID string) map[string]any
```

Flow, as in the spec, with these exact choices:

- `BeforeAll`: `startWebServer("auroraboot:test")`; `createGroup("e2e-phonehome")`; artifact A body `{"name": "e2e-phonehome-a", "baseImage": <base>, "arch": "amd64", "kairosInitImage": <init>, "outputs": {"iso": true}, "provisioning": {"autoInstall": true, "registerAuroraBoot": true, "targetGroupId": <group>}}`; artifact B body `{"name": "e2e-phonehome-b", "arch": "amd64", "dockerfile": "FROM <base>\nRUN echo <token> > /etc/auroraboot-e2e-marker\n"}` where `<token>` is a UUID the tier generates. Check in the handler which outputs B needs for its record to carry a `containerImage`, which `/image` requires. Build A and B concurrently. Download A's ISO into `GinkgoT().TempDir()`. Start two VMs with `startVMWith(vmSpec{ISO: iso, Memory: env or "4000", CPUs: env or "2"})` and `EventuallyConnects`. Wait until `groupNodes` has exactly two nodes with `bootState == "active"` and phase `Online` (Review Focus 1). Record each VM's boot ID over SSH and map VMs to node IDs by hostname.
- Spec 1, `It("reports an upgrade that fails before the reboot and leaves the nodes usable")`: `sendGroupCommand(group, "upgrade", {"source": "artifact:00000000-0000-0000-0000-000000000000"})`; both commands reach `Failed`; both nodes `upgradeState == "failed"`; both still `Online`, `active`, and answer `true` over SSH; boot IDs unchanged.
- Spec 2, `It("upgrades both nodes to the new artifact")`: `sendGroupCommand(group, "upgrade", {"source": "artifact:" + B})`; both commands reach `Completed`; each VM's boot ID changes (Review Focus 2); each node `upgradeState == "done"` and `bootState == "active"`; `cat /etc/auroraboot-e2e-marker` over SSH equals `<token>` on both. From `docker logs <web container>`, the lines matching `image export for artifact <B>:` contain exactly one `built in`, and every other one says `waited for the build of another request` or `served from the cache`.
- `AfterAll`: when any spec failed, write `docker logs` of AuroraBoot to `$PHONEHOME_LOG_DIR/auroraboot.log` (default `<repo>/e2e/logs/phonehome`), call `saveSerialLog` and `gatherLogs` for each VM with `VM_LOG_DIR` set to `$PHONEHOME_LOG_DIR`, and add `journalctl -u kairos-agent-phonehome --no-pager` (check the unit name on a VM) to each VM's logs. Then destroy both VMs and run the web server cleanup.
- `Makefile`: target `test-e2e-phonehome` with a `##` help comment. It builds `auroraboot:test` (reuse the build command the existing e2e targets or `tests.yml` use) and runs `KVM=true FIRMWARE=$${FIRMWARE:-/usr/share/edk2/x64/OVMF_CODE.4m.fd} go run github.com/onsi/ginkgo/v2/ginkgo -v --label-filter phonehome-vm --timeout 2h ./e2e`. Add `e2e/logs/` to `.gitignore`.

- [ ] **Step 1: Write the tier** (both files and the Makefile target) as above.
- [ ] **Step 2: Run** `go vet ./e2e` and `go run github.com/onsi/ginkgo/v2/ginkgo --dry-run -v --label-filter phonehome-vm ./e2e`. Expected: two specs listed.
- [ ] **Step 3: Run it for real:** `make test-e2e-phonehome`. Expected: both specs pass, nothing left behind (`docker ps -a --filter name=auroraboot-e2e-web` empty, no `qemu-system` process from the tier). Fix the tier, not AuroraBoot, unless the failure is a real AuroraBoot or agent bug; in that case stop and report it.
- [ ] **Step 4: Prove it catches the bug it exists for** (Review Focus 3 and 4): `PHONEHOME_KAIROS_INIT_IMAGE=quay.io/kairos/kairos-init:v4.3.0 make test-e2e-phonehome`. Expected: spec 2 fails; the failure names the 401 or the failed command result; `e2e/logs/phonehome` holds `auroraboot.log` and a directory per VM with `serial.log`; nothing is left running.
- [ ] **Step 5: Commit** `test(e2e): upgrade two VMs through AuroraBoot end to end`.

---

### Task 3: CI workflow

**Files:**
- Create: `.github/workflows/e2e-phonehome.yml`

**Interfaces:**
- Consumes: the Makefile target's command line (Task 2), the image build and OVMF/QEMU setup steps of `.github/workflows/tests.yml` (`build-image`, the bootable jobs and `./.github/actions/find-ovmf-firmware` with `prefer_secboot: false`).

Triggers: `pull_request` with `paths` `pkg/handlers/**`, `pkg/ws/**`, `internal/store/**`, `internal/builder/**`, `e2e/phonehome*`, `e2e/bootable_test.go`, `e2e/extensions_web_test.go`, `.github/workflows/e2e-phonehome.yml`; `schedule` `cron: "0 3 * * *"`; `workflow_dispatch`. Concurrency group per ref with cancel-in-progress, as in `tests.yml`. One job on `ubuntu-latest`, `timeout-minutes: 120`: checkout, set up Go from `go.mod`, enable KVM for the runner user (as the bootable jobs do), install QEMU, swtpm and OVMF, build `auroraboot:test`, run the ginkgo command with `KVM=true` and the firmware from the action, and on failure upload `e2e/logs/phonehome` with `actions/upload-artifact`.

- [ ] **Step 1: Write the workflow.**
- [ ] **Step 2: Lint** with `docker run --rm -v "$PWD":/repo -w /repo rhysd/actionlint:latest -color .github/workflows/e2e-phonehome.yml`. Expected: no findings.
- [ ] **Step 3: Commit** `ci: run the phonehome VM tier`.
