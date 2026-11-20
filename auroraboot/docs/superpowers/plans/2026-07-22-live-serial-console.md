# Live Serial Console Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:test-driven-development to implement this plan task-by-task.

**Goal:** Allow ISO builders to replace AuroraBoot's hard-coded live console arguments while preserving the current default.

**Architecture:** Carry one optional `iso.live_console` value from schema and the `build-iso --live-console` flag into the existing GRUB template renderer. Substitute a dedicated placeholder in normal live entries; leave the debug entry unchanged.

**Tech Stack:** Go, urfave/cli, Ginkgo/Gomega, embedded GRUB template.

## Global Constraints

- Preserve `console=ttyS0 console=tty1` when the option is unset.
- Strip CR and LF from the replacement before rendering GRUB.
- Do not alter the debug menu entry's `console=tty0` behavior.
- Keep the change scoped to live/installer ISOs.

---

### Task 1: Configurable live console `{id: 1, deps: []}`

**Files:**
- Modify: `pkg/schema/config.go`
- Modify: `internal/cmd/build-iso.go`
- Modify: `internal/cmd/build-iso_test.go`
- Modify: `pkg/constants/grub_live_bios.cfg`
- Modify: `pkg/ops/iso.go`
- Modify: `pkg/ops/iso_test.go`

**Interfaces:**
- Consumes: `schema.ISO` passed to `BuildISOAction`.
- Produces: `ISO.LiveConsole string` with YAML key `live_console`; CLI flag `--live-console`; GRUB template substitution.

- [ ] Write failing tests proving a custom console replaces the default, the unset value preserves the default, CR/LF are removed, and the CLI accepts `--live-console`.
- [ ] Run `go test ./pkg/ops ./internal/cmd` and confirm the tests fail because the option and placeholder handling do not exist.
- [ ] Add the minimal schema, CLI plumbing, placeholder, and renderer changes.
- [ ] Run formatting and `go test ./pkg/ops ./internal/cmd` until green.
- [ ] Run `git diff --check`.
- [ ] Commit with DCO and assisted-by trailers.

**Verify:** `go test ./pkg/ops ./internal/cmd && git diff --check`
