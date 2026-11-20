# Extensions UI & Lifecycle — Design

Date: 2026-05-26
Status: Approved for implementation
Owner: @mudler

## Goal

Expose **sysext** and **confext** building in the AuroraBoot UI and give them the same fleet lifecycle that OS artifacts already have: build → store → push to nodes (install / upgrade / remove). Today the CLI commands `auroraboot sysext` and `auroraboot confext` exist, but there is no UI, no DB record, no fleet delivery, and no phonehome command on the agent side.

Extensions can also be **bundled with an OS artifact** so that an OS upgrade and its dependent extensions land in a single atomic operation on the node — required when a sysext depends on OS bits (kernel modules, libc, systemd directives) that arrive in the new OS image. Bundling is declarative on the artifact and overridable per-send in the upgrade dialog.

## Non-goals

- Editing an existing extension in place (extensions are rebuilt, like artifacts).
- A pluggable plugin registry for templates (use the same hard-coded template list pattern as `ArtifactBuilder.tsx`).
- Multi-arch builds in one record (one extension record = one architecture, mirroring how artifacts work today).
- New CLI flags beyond what's needed for the hierarchies free-list (see *CLI changes* below).
- Integration with external OCI registries beyond what artifacts already support.
- A new SecureBootKeySet-equivalent store. We **reuse** `SecureBootKeySet` for signing.
- A two-phase commit / transactional rollback for compound upgrades on the node. Atomicity is delivered by ordering extension installs *before* the OS upgrade reboot; failure of any extension install aborts the whole compound command before the OS is touched, but mid-flight kernel panics during the OS upgrade itself are out of scope (covered by Kairos's existing dual-partition rollback).

## Two delivery flows

The same `extension` build artifact can reach a node by two paths, and operators see both as first-class flows in the UI:

**1 · Manual flow — standalone push to running systems.** Operator picks an extension from the Extensions list (or the detail page), opens the Install dialog, picks a target (group / labels / specific nodes) and an action (Install / Enable / Disable / Remove). The server sends the new `extension` phonehome command. Used when an operator wants to add, swap, or remove an extension on a fleet that's *already running* the right OS — no reboot of the OS itself, just `kairos-agent sysext install` and (optionally) a `systemctl restart systemd-sysext`.

**2 · Bundled flow — extensions ride with an OS upgrade.** Extensions declared on an artifact (or picked at send time) are pushed *together with* the OS image. The server extends the existing `upgrade` (and `upgrade-recovery`) phonehome command with an `extensions[]` arg. The agent installs each extension into the **passive partition** first, then runs `kairos-agent upgrade`, which reboots into the new partition — the OS image and the extensions become active in the same reboot. Used when a sysext depends on OS bits that only exist after the upgrade.

Both flows share: the same Extensions table; the same `.raw` build artifact; the same authenticated download endpoint; the same per-node version tracking. They differ only in *which* phonehome command carries them and *which* boot scope the agent installs into.

## Extension storage and boot scopes

Verified against `kairos-agent/pkg/action/sysext.go`. **Extensions are partition-independent.** All `.raw` files live in one persistent location: `/var/lib/kairos/extensions/<filename>.raw` for sysexts, `/var/lib/kairos/confexts/<filename>.raw` for confexts. An OS upgrade does not move them; the active↔passive partition flip does not touch them.

The four "boot scopes" — `active`, `passive`, `recovery`, `common` — are subdirectories of each type's parent (`/var/lib/kairos/extensions/{active,passive,recovery,common}/` for sysexts, `/var/lib/kairos/confexts/{active,passive,recovery,common}/` for confexts). They contain **symlinks** to the `.raw` files. Each symlink says "enable this extension when the node boots in this state." At boot, immucore reads the current boot state and creates symlinks under `/run/extensions/` (or `/run/confexts/`) pointing at the matching scope's entries (plus `common`, which always applies). systemd-sysext / systemd-confext then mount what's there.

`kairos-agent <type> install <uri>` only downloads the `.raw` into the persistent dir; it does **not** enable it. Enabling is a separate `enable <name> --<scope> [--now]` step that creates the symlink. `kairos-agent <type> remove <name>` deletes all symlinks and the `.raw` itself.

**Re-installing the same name overwrites the `.raw` file in place.** Any symlinks that previously pointed at it (in any scope) automatically resolve to the new content — there is no need to remove old symlinks before upgrading an extension.

**Manual flow defaults to `common`.** Operator intent for a manual install is "make this stick across boots and rollbacks"; `common` is the scope honored regardless of which boot state the node is in. The install dialog allows `active`, `passive`, `recovery` as overrides. Picking **Active** surfaces an amber callout — *"This extension is only enabled when the node is booted in active mode. If the node is rolled back to passive, this extension won't be loaded."* — so the scoped behavior is opted into knowingly. (The `.raw` itself still survives a rollback; only the symlink scope determines whether it's enabled.) The dialog dispatches `kairos-agent <type> install` followed by `kairos-agent <type> enable --<scope> [--now]`.

**Bundled flow enables at `active` for new extensions, overwrites in place for existing ones.** The agent's compound dispatch, for each `extensions[]` entry:

1. `kairos-agent <type> install <source>` — downloads the `.raw` into the persistent dir, overwriting any prior file at the same name. If the extension was previously enabled at any scope, the existing symlinks now resolve to the new content; no further work is needed for them.
2. If, after step 1, the extension is **not** enabled at any scope on this node, issue `kairos-agent <type> enable --active` **without** `--now`. The `active` scope ensures the post-reboot active boot enables it; omitting `--now` keeps the current OS from reloading and mounting it against a kernel/userspace it wasn't built for. New extensions land enabled-on-next-boot.

Step 2's "if not already enabled anywhere" decision preserves operator intent: if the operator previously chose `common`, the bundle leaves the scope as `common`; if previously `active`, leaves it `active`; only first-time installs get the `active` default. There is no same-name common-remove step — re-install overwrites the `.raw` in place and the symlink picks it up automatically.

**Pre-action diff** in the upgrade dialog reports three lines (see *UI surfaces*): the OS transition, each bundled extension's per-node version transition (with replace/first-install counts), and a summary of extensions already enabled on the target nodes that the bundle isn't touching (carry-forward).

## Architecture overview

```
┌────────────────┐   POST /api/v1/extensions     ┌──────────────────────┐
│  Extensions    │ ────────────────────────────▶ │  ExtensionHandler    │
│  Builder (UI)  │                                │  (pkg/handlers)      │
└────────────────┘                                └──────────┬───────────┘
                                                             │
                                                             ▼
                                                  ┌─────────────────────┐
                                                  │ ExtensionBuilder    │
                                                  │ (pkg/builder)       │
                                                  └──────────┬──────────┘
                                                             ▼
                                                  ┌─────────────────────┐
                                                  │ ExtensionStore (DB) │
                                                  └─────────────────────┘

  FLOW 1 — manual
  ┌────────────────┐  POST /commands {extension, …}  ┌────────────────┐
  │ Install dialog │ ──────────────────────────────▶ │ CommandHandler │
  └────────────────┘                                 └───────┬────────┘
                              WS push                        │
                  (pkg/ws/handler.go SendCommand)            ▼
                                                  ┌─────────────────────┐
                                                  │ kairos-agent        │
                                                  │  case "extension":  │
                                                  │   → sysext install  │
                                                  │   → sysext enable   │
                                                  │   → disable / remove│
                                                  └──────────┬──────────┘
                                                             │ status callback
                                                  PUT /commands/:id/status
                                                             ▼
                                                       AuroraBoot

  FLOW 2 — bundled
  ┌─────────────────────┐  POST /commands {upgrade, extensions[], …}
  │ Artifact upgrade    │ ──────────────────────────────────────────┐
  │ dialog              │                                            ▼
  └─────────────────────┘                                  ┌─────────────────────┐
                                                           │ kairos-agent        │
                                                           │  case "upgrade":    │
                                                           │   1. for each ext:  │
                                                           │      install (.raw  │
                                                           │      overwrite),    │
                                                           │      enable --active│
                                                           │      iff not already│
                                                           │      enabled        │
                                                           │   2. kairos-agent   │
                                                           │      upgrade        │
                                                           │   3. reboot         │
                                                           └─────────────────────┘
```

## UI surfaces

A new top-level **Extensions** entry in the side nav, placed between **Artifacts** and **Nodes**. Three routes:

- `/extensions` — list.
- `/extensions/new` — three-step builder (Source → Configure → Review).
- `/extensions/:id` — detail page (phase, logs, download, Install action).

The list page mirrors the Artifacts list visually: orange-accented page header, filter pills, dense table. Columns: Name · Type · Arch · Version · Signed · Phase · Updated · Actions. Empty state shows a one-line value prop, a primary **Build extension** button, and three template chips (Tailscale, Fluent-bit, Nvidia container toolkit) that pre-fill the builder.

The Type column carries a chip — sky-500 family for `sysext`, violet-500 family for `confext` — used consistently wherever the type appears (list, detail, install dialog, bundle list).

The builder Step 1 has three Source modes, in order: **From artifact** (default tab when at least one Ready artifact exists), **Base image**, **Dockerfile**. "From artifact" picks an existing AuroraBoot artifact and optionally appends Dockerfile-style steps that get wrapped in `FROM <artifact-image>` before the sysext extractor runs. The picker shows a cross-check strip that compares the user-selected hierarchies against the artifact's stored `extensionHierarchies` — green when supported, amber when missing, with an "Add it to the artifact →" link that clones the artifact prefilled with the missing path.

The builder Step 2 carries: Extension type (sysext/confext), Architecture (amd64/arm64/riscv64), Version (free string used server-side to decide staleness), Signing keyset picker (reuses `SecureBootKeySet`; the dropdown shows a `⚠` next to any keyset that doesn't have a usable `db.key`+`db.pem` pair), and — sysext-only — an Additional Hierarchies chip input. `/usr` is implicit and lives in help text, not as a pinned chip. The `service-reload` toggle joins the hierarchies card so all sysext-only knobs live together. A warning callout appears whenever any non-default hierarchy is present, reminding the operator that the target node must declare those paths in `SYSTEMD_SYSEXT_HIERARCHIES`.

The builder Step 3 (Review) shows a summary table plus an equivalent-CLI panel so power users see the exact command AuroraBoot will run.

The extension detail page shows the phase + logs strip, the download link, a "Used by" section listing artifacts that bundle this extension, and an "Installed on" section enumerating nodes currently running it (driven by the new `node_extensions` table — see Data model).

The node detail page gains an **Installed extensions** section: a small table of (Name, Type chip, Version, Boot scope, Installed at). Each row carries a "Remove" action that fires the standalone `extension` flow. This is the v1 version — no inline install on the node detail page (operators install via the extension's own Install dialog).

The **manual-flow Install dialog** opens from the extension detail page or a list row's Install action. Single modal with: target picker (Group / Labels / Specific nodes), four action cards (Install / Enable / Disable / Remove), a Boot Scope row (Active / Passive / Recovery / Common with info tooltips, **Common selected by default**), and an "Activate immediately" toggle. Picking `Active` reveals an amber callout below the row — *"This extension lives in the active partition only. The next OS upgrade will retire it to the rollback partition; it won't be available on the new OS."* — so the partition-flip behavior is opted into knowingly. Above the Cancel/Send row, the dialog shows a pre-action diff using neutral wording — "12 nodes will **replace** v1.72.1 → v1.74.0, 10 first-install, 2 offline". The JSON payload preview is folded behind a `<details>` labeled "Show payload" so the Send button remains the bottom-right anchor.

The **bundled-flow upgrade dialog** is the existing artifact upgrade dialog (the one that's already in `CommandDialog.tsx`) with a new section, "**Also push these extensions**". The artifact's bundled extensions (from the join table — see Data model) are listed as a multi-select with checkboxes pre-selected; operators can untick to drop one, or click "Add extension" to include a non-bundled one ad-hoc. The pre-action diff renders three lines:

- OS transition — e.g. *"12 nodes upgrade OS v4.1.0 → v4.2.0."*
- Bundled changes — for each extension being pushed, the per-node replace/first-install counts — e.g. *"tailscale v1.74 — replaces v1.72 on 12 nodes; fluent-bit-config 2026.05.20 — first install on 12; nvidia-toolkit v1.16 — replaces v1.14 on 8."*
- Carry-forward summary — e.g. *"5 existing common-scoped extensions carry forward unchanged."*

The "Show payload" preview reflects the compound `upgrade` command with `extensions[]`.

The existing **ArtifactBuilder** gains two things in its Access & Security card. First, a folded disclosure "Pre-configure for system extensions · Optional · advanced" that exposes the hierarchies chip inputs for `SYSTEMD_SYSEXT_HIERARCHIES` and (nested) `SYSTEMD_CONFEXT_HIERARCHIES`, plus a "What this bakes" disclosure showing the cloud-config snippet. Second, a separate "**Bundled extensions**" card — a multi-select drawn from existing Ready extensions matching the artifact's arch. Each row in the card carries `{extension, optional pinned version}`; an unpinned row resolves to "latest at upgrade time", a pinned row resolves to that exact version. The card stays empty by default; operators opt in.

The existing **Allowed remote commands** picker in `ArtifactBuilder` gains a new row, `extension`, placed in the **Destructive — opt in per fleet** group alongside `exec`, `reset`, `apply-cloud-config`. The chip uses the destructive (red) palette; there is no orange NEW badge — a small "NEW · destructive" pill suffices. The existing `upgrade` and `upgrade-recovery` commands stay in the safe-default group; their extended `extensions[]` arg does not require a new opt-in (the operator already approved `upgrade`).

## Data model

A new GORM-managed table `extensions`:

```
id                  uuid (pk)
name                string  // user-facing identifier, unique per server
type                string  // "sysext" | "confext"
phase               string  // "Pending" | "Building" | "Ready" | "Error"
message             string  // error excerpt or progress hint
arch                string  // "amd64" | "arm64" | "riscv64"
version             string  // user-supplied, stored verbatim
source_mode         string  // "artifact" | "image" | "dockerfile"
source_artifact_id  uuid    // when source_mode = "artifact"
source_image        string  // when source_mode in {"image","artifact"}
dockerfile          text    // when source_mode in {"dockerfile","artifact" w/ steps}
extra_steps         text    // when source_mode = "artifact" with appended steps
signing_keyset_id   uuid    // FK SecureBootKeySet (nullable)
hierarchies         []string // additional hierarchies (sysext only; /usr implicit)
service_reload      bool     // sysext-only flag
container_image     string   // resolved OCI ref used as the build input
raw_filename        string   // produced .raw filename, relative to artifactsDir
created_at, updated_at
```

A new table `artifact_extension_bundles` (the declarative side of bundling):

```
artifact_id     uuid    // FK Artifact
extension_name  string  // bundled by name, NOT by extension UUID
extension_type  string  // "sysext" | "confext" (denormalized for fast filtering)
pinned_version  string  // nullable; null = "use latest Ready at dispatch time"
order           int     // install order on the node (lower first)
(unique key: artifact_id + extension_name)
```

**Bundles are keyed by name, not UUID.** This is the deliberate choice: a bundle entry survives a rebuild of the underlying extension (new UUID, same name) and survives deletion of an old extension version, so long as another Ready row with the same name exists. Resolution at dispatch time:

- `pinned_version` set: find the Ready `extensions` row with that `name` and `version`. If none, dispatch fails with a structured error naming the missing version.
- `pinned_version` null: find the **newest by `created_at`** Ready `extensions` row with that `name`. If none, dispatch fails. The "latest" tiebreaker is `created_at` because `version` is a free-form string we can't safely semver-sort.

A new table `node_extensions` (the per-node tracking used by the pre-action diff and the node detail page):

```
node_id        uuid
extension_id   uuid
name           string  // denormalized for fast filtering when the extension is later deleted
type           string
version        string  // version installed on the node
boot_state     string  // "active" | "passive" | "recovery" | "common"
installed_at   timestamp
updated_at     timestamp
(primary key: node_id + extension_id + boot_state)
```

The agent reports back via the existing REST status endpoint (`PUT /api/v1/nodes/:nodeID/commands/:commandID/status`, the same path the existing `upgrade` flow uses to mark itself `Completed`/`Failed`) on success/failure of an install / enable / disable / remove. The server then updates `node_extensions` accordingly: an `install`+`enable` writes/upserts a row, a `disable` deletes the row for the relevant scope only, a `remove` deletes all rows for that name on that node. Failures leave existing rows untouched.

Because extensions are partition-independent (see *Extension storage and boot scopes*), an OS upgrade does **not** rotate `node_extensions` rows — scope choices made by the operator (or by the bundle's "enable --active iff not already enabled" rule) persist across upgrades. The compound-upgrade status callback writes the bundled extensions as new rows the first time they're installed on a node, and leaves prior rows untouched (overwritten `.raw` content is captured by bumping `version`).

The existing `Artifact` table gains one nullable column:

```
extension_hierarchies json  // {"sysext": ["/opt","/srv"], "confext": []}
```

Stored verbatim from the ArtifactBuilder; used by the Extensions builder's cross-check and by the cloud-config snippet baked into the image.

## API

```
GET   /api/v1/extensions                         → []Extension
POST  /api/v1/extensions                         → Extension      (start build)
GET   /api/v1/extensions/:id                     → Extension
PATCH /api/v1/extensions/:id                     → Extension      (name only)
DELETE /api/v1/extensions/:id                    → ()
GET   /api/v1/extensions/:id/logs                → text
POST  /api/v1/extensions/:id/cancel              → ()
GET   /api/v1/extensions/:id/download/:filename  → file (admin or node, via DownloadMiddleware)
GET   /api/v1/extensions/:id/nodes               → []NodeExtensionRow

# bundles attached to an artifact:
GET   /api/v1/artifacts/:id/bundle-extensions    → []BundleEntry
PUT   /api/v1/artifacts/:id/bundle-extensions    → []BundleEntry   (replace set; entries are {name, type, pinnedVersion?, order?})

# node-side tracking:
GET   /api/v1/nodes/:id/extensions               → []NodeExtensionRow
```

`POST /api/v1/extensions` body matches the builder form:

```jsonc
{
  "name": "tailscale-agent",
  "type": "sysext",
  "arch": "amd64",
  "version": "v1.74.0",
  "source": {
    "mode": "artifact",
    "artifactId": "…",
    "baseImage": "ubuntu:24.04",
    "dockerfile": "FROM …",
    "extraSteps": "RUN …"
  },
  "signingKeySetId": "…",
  "hierarchies": ["/opt", "/srv"],
  "serviceReload": false
}
```

`POST /api/v1/artifacts` body grows an optional `bundledExtensions` field. Entries are keyed by `name` + `type`, not UUID — bundles survive a rebuild of the underlying extension:

```jsonc
{
  // … existing artifact fields …
  "bundledExtensions": [
    { "name": "tailscale-agent",     "type": "sysext",  "pinnedVersion": null },
    { "name": "fluent-bit-config",   "type": "confext", "pinnedVersion": "2026.05.20" }
  ]
}
```

Validation rules (client and server):

`hierarchies`:
- must start with `/`,
- must not contain `..`,
- must not be exactly `/` or `/usr` (case-sensitive — Linux filesystem semantics),
- length ≤ 256,
- normalized: trailing slashes stripped, duplicates deduped, list re-ordered alphabetically before persistence so the produced regex is deterministic.

`extraSteps`:
- must not begin a line with `FROM ` (case-insensitive, allowing whitespace). The "From artifact" mode pins the base; user steps must not override it.

Server returns 400 with `{field: "hierarchies[N]", message: "…"}` (or `extraSteps`) on the first failing entry.

The download endpoint reuses the existing **`DownloadMiddleware`** (`pkg/auth/middleware.go:57-79`), which accepts either the admin password or a registered node's API key — supplied via the `Authorization: Bearer …` header or a `?token=…` query parameter. This is exactly the auth model the existing `GET /api/v1/artifacts/:id/image` endpoint uses for agent-driven downloads, so we do not introduce signed-URL/TTL machinery. The agent composes `https://<server>/api/v1/extensions/<id>/download/<filename>?token=<node-api-key>` and `kairos-agent <type> install` follows the `https:` URI via its existing `httpSource` path (`kairos-agent/pkg/action/sysext.go:392-394`).

## Phonehome commands

Two commands are touched:

### `extension` — manual flow (new)

```jsonc
{
  "command": "extension",
  "args": {
    "type":      "sysext",
    "action":    "install",
    "name":      "tailscale-agent",
    "source":    "https://aurora/api/v1/extensions/3f9c…/download/tailscale-agent.sysext.raw?token=…",
    "bootState": "common",
    "now":       true
  }
}
```

Dispatched in `kairos-agent/internal/phonehome/handlers.go` as a new switch case. Note that `install` requires **two** CLI calls (download + enable), reflecting that `kairos-agent <type> install` only writes the `.raw` and `kairos-agent <type> enable` is what creates the boot-scope symlink:

```
install → kairos-agent <type> install <source>
          && kairos-agent <type> enable <name> --<bootState> [--now]
enable  → kairos-agent <type> enable  <name> --<bootState> [--now]
disable → kairos-agent <type> disable <name> --<bootState> [--now]
remove  → kairos-agent <type> remove  <name>            [--now]
```

Arg requirements per action: `source` is required for `install` and ignored otherwise; `bootState` is required for `install`, `enable`, and `disable`, ignored otherwise; `name` is required for every action. The agent's handler validates these before shelling out and returns a structured error if anything is missing.

`extension` is **not** in the phonehome safe-default allow list. It is treated as destructive (it can drop arbitrary OCI content into `/usr`, `/etc`, or declared hierarchies) and must be explicitly enabled per fleet in the ArtifactBuilder's allowed-commands picker.

A second install over the same name is the upgrade path — there is no separate `upgrade` action. The Install dialog labels the action card "Install" and the pre-action summary uses neutral wording ("**replace** existing v1.72.1 → v1.74.0") because the free-form version string can't be safely direction-compared.

### `upgrade` / `upgrade-recovery` — bundled flow (extended)

The existing args grow one optional field, `extensions`. **`CommandData.Args` is `map[string]string`** in `kairos-agent/internal/phonehome/config.go:140-144`, so the value is a **JSON-encoded string** (not a native array). The on-wire shape is:

```jsonc
{ "command": "upgrade",
  "args": { "source": "artifact:…",
            "extensions": "[{\"type\":\"sysext\", \"name\":\"…\", \"source\":\"…\"}, …]" }
}
```

The agent parses `args["extensions"]` (if non-empty) into a slice; the rest of the dispatch follows. Conceptually the field is:

```jsonc
{
  "command": "upgrade",
  "args": {
    "source": "artifact:…",
    "extensions": [
      { "type": "sysext",  "name": "tailscale-agent",
        "source": "https://aurora/api/v1/extensions/…/download/tailscale-agent.sysext.raw?token=…" },
      { "type": "confext", "name": "fluent-bit-config",
        "source": "https://aurora/api/v1/extensions/…/download/fluent-bit-config.confext.raw?token=…" }
    ]
  }
}
```

`handleUpgrade` is extended:

1. For each entry in `extensions[]`, in array order:
   - Shell out to `kairos-agent <type> install <source>`. This downloads the `.raw` into the persistent dir (`/var/lib/kairos/{extensions,confexts}/<filename>.raw`), overwriting any prior file at the same name. If the extension was previously enabled at any scope (active/passive/recovery/common), the existing symlinks now resolve to the new content — no further action needed for those.
   - Query the persistent dir's `{active,passive,recovery,common}/` subdirs for an existing symlink with this name. If none exists, shell out to `kairos-agent <type> enable <name> --active` **without** `--now` to enable for the post-reboot active boot, without reloading systemd-sysext against the current (about-to-be-replaced) OS.
2. If any step-1 install/enable returns non-zero, abort: do **not** invoke `kairos-agent upgrade`, do **not** schedule the reboot, return the failed extension's output as the command result so the operator sees which one broke. The node stays on the old OS with the `.raw` files already overwritten in place — operators can retry the same compound command safely (the install step is idempotent).
3. Run `kairos-agent upgrade --source <source>` (existing logic).
4. Schedule the existing 10-second reboot. On reboot, immucore creates `/run/extensions/<name>` symlinks from the matching scope's dir (plus `common`) and systemd-sysext mounts them on the new OS.

`upgrade-recovery` gets the same `extensions[]` arg but the conditional enable uses `--recovery` instead of `--active` (so the extension is enabled only in the recovery boot state). Recovery-bundled extensions are uncommon but cheap to support since the dispatch is otherwise identical.

No new allow-list opt-in is required for the extension ride-along: the operator has already opted into `upgrade` and `upgrade-recovery`. The fact that those commands now optionally carry extensions is an extension of an already-approved capability.

## CLI changes

Extend `internal/cmd/sysext.go`:

- Add a repeatable flag `--include-path` that appends to the extractor allowlist.
- Keep `--with-opt` as an **indefinite** alias for `--include-path=/opt` (no removal date, but emit a one-time deprecation warning per process). The cost of carrying it is negligible; the cost of breaking scripts isn't.
- The handler assembles the regex from `/usr` plus each `--include-path` entry, in lexical order, and surfaces a debug log line so reproducible builds are auditable.

No changes to the `confext` command (its allowlist is `/etc`-only by definition).

## Build pipeline

### Interface boundary

`ExtensionBuilder` lives in `pkg/builder/extension.go` as an **interface**, mirroring the existing `pkg/builder/builder.go` pattern. Handlers and stores depend on the interface, not on any concrete implementation. The in-process implementation lives in `internal/builder/auroraboot/extension.go`.

```go
// pkg/builder/extension.go

type ExtensionSource struct {
    Mode             string // "artifact" | "image" | "dockerfile"
    SourceArtifactID string // when Mode = "artifact"
    BaseImage        string // when Mode in {"image", "artifact"} resolved at build time
    Dockerfile       string // when Mode = "dockerfile"
    ExtraSteps       string // optional, when Mode = "artifact"
    BuildContextDir  string // for COPY in Dockerfile (mirrors ArtifactBuilder.BuildContextDir)
}

type ExtensionSigning struct {
    PrivateKey  string // file path
    Certificate string // file path
}

type ExtensionBuildOptions struct {
    ID            string
    Name          string
    Type          string   // "sysext" | "confext"
    Arch          string   // "amd64" | "arm64" | "riscv64"
    Version       string
    Source        ExtensionSource
    Signing       ExtensionSigning
    Hierarchies   []string // sysext-only; /usr implicit
    ServiceReload bool     // sysext-only
    OutputDir     string
}

type ExtensionBuildStatus struct {
    ID             string `json:"id"`
    Phase          string `json:"phase"` // reuses Build* constants from builder.go
    Message        string `json:"message"`
    RawFile        string `json:"rawFile"`
    ContainerImage string `json:"containerImage"`
}

type ExtensionBuilder interface {
    Build(ctx context.Context, opts ExtensionBuildOptions) (*ExtensionBuildStatus, error)
    Status(ctx context.Context, id string) (*ExtensionBuildStatus, error)
    List(ctx context.Context) ([]*ExtensionBuildStatus, error)
    Cancel(ctx context.Context, id string) error
}
```

This interface boundary is the **swap point** for the Kubernetes-operator deployment story. Today's in-process implementation (`internal/builder/auroraboot/extension.go`) drives `docker build` and `auroraboot sysext|confext` on the host. A future Kubernetes implementation can satisfy the same interface by translating each `Build` call into a custom-resource creation (`Extension` CRD), letting a controller pick up the build in a job, and reporting status back via a watch on the CR — without any handler or UI code changing.

### Behavior of the in-process implementation

1. Resolve the source image:
   - `mode=image`: use as-is.
   - `mode=artifact`: read `Artifact.containerImage` of `source.SourceArtifactID` from the store.
   - `mode=dockerfile`: shell out to `docker build --no-cache -t auroraboot-extbuild:<id> -f <dockerfile-path> <BuildContextDir>` — mirrors `internal/builder/auroraboot/builder.go:583`. `BuildContextDir` defaults to the per-build `outputDir` when unset.
   - `mode=artifact` + `extraSteps`: synthesize a Dockerfile (`FROM <artifact-image>\n<extraSteps>`) and run the same docker build.
2. Invoke `auroraboot sysext|confext` against the resolved image, passing `--arch`, optional signing flags from the keyset's `db.key`/`db.pem`, the `--include-path` entries derived from `Hierarchies`, and `--service-reload` when `ServiceReload`.
3. Write the resulting `.raw` to `<OutputDir>/<name>.<type>.raw`. The handler computes `OutputDir = artifactsDir/extensions/<id>/`, namespaced by the extension's UUID so two same-name builds never collide on disk.
4. Update the DB record with `RawFile` and `ContainerImage`, transition `phase` to `Ready` or `Error` with `Message`.

The async entry mirrors `(*Builder).run` (`internal/builder/auroraboot/builder.go:209`) — the public `Build` registers state, persists the initial `ExtensionRecord` (phase `Pending`), then dispatches `go b.run(ctx, opts)`.

The log-streaming machinery is reused: build logs go through a `dbLogWriter` (`internal/builder/auroraboot/builder.go:78-114`) that buffers and appends to the extension's `Logs` field plus broadcasts chunks through the existing `LogBroadcaster` (UI WS fan-out). Logs are tagged with `kind=extension` and `id=…` so the UI subscribes by record id.

### Deletion policy

`DELETE /api/v1/extensions/:id` enforces:

- **Block** if any `artifact_extension_bundles` row references the extension's `name` (operator must remove from the bundle first; response is 409 Conflict with the list of referencing artifacts).
- **Allow** if only `node_extensions` rows reference it. Those rows are kept (left as stale records of past installs); the `.raw` already on the node survives until a `remove` command runs against it. The list page filters out extensions in `Error` phase from "delete-blocks-bundle" — only `Ready` extensions can be bundled, so an Error row deletion never collides.

Cancelling a build (`POST /api/v1/extensions/:id/cancel`) cancels the build context, cleans `<OutputDir>` (mirrors `ArtifactBuilder.Cancel`), and leaves the row in phase `Error` with a "cancelled by user" message rather than deleting it (consistent with how `cancelArtifact` behaves today).

## Signing keysets

The existing `SecureBootKeySet` record carries `keysDir`, which today holds the UKI secure-boot key material. AuroraBoot reads `db.key` and `db.pem` from that directory and passes them as `--private-key` and `--certificate` to the sysext CLI. If those files are missing, the build records `phase = Error` with a message that names the missing files.

Additionally, the signing keyset picker in the Extensions builder runs a pre-flight check: for each keyset it lists, the server reports whether `db.key`+`db.pem` exist; missing keysets show a `⚠ no sysext signing material` annotation in the dropdown and disable selection. Operators are told *before* hitting Build, not after.

No new key store is introduced.

## ArtifactBuilder integration

`ArtifactBuilder.tsx` gains two additions in Step 2:

**Hierarchies disclosure** (inside Access & Security): a `<details>` block "Pre-configure for system extensions · Optional · advanced". When open: chip input for additional `SYSTEMD_SYSEXT_HIERARCHIES` (`/usr` always included, surfaced as help text); nested `<details>` for `SYSTEMD_CONFEXT_HIERARCHIES`; nested `<details>` "What this bakes" showing the cloud-config snippet, default-open so the operator can see what AuroraBoot will add.

The hierarchies are submitted in the artifact create payload as `extensionHierarchies: { sysext: [...], confext: [...] }`. The backend stores the value and, at build time, appends a stage to the baked cloud-config:

```yaml
stages:
  initramfs:
    - files:
        - path: /etc/systemd/system/systemd-sysext.service.d/99-aurora-hierarchies.conf
          permissions: 0644
          content: |
            [Service]
            Environment=SYSTEMD_SYSEXT_HIERARCHIES=/usr:/opt:/srv
        - path: /etc/systemd/system/systemd-confext.service.d/99-aurora-hierarchies.conf
          permissions: 0644
          content: |
            [Service]
            Environment=SYSTEMD_CONFEXT_HIERARCHIES=/etc
```

(The confext drop-in is only emitted when the confext list is non-empty.)

**Bundled extensions card** (separate card after Access & Security): a multi-select of existing Ready extensions filtered to the artifact's arch. Each picked row shows `{extension chip, optional pinned-version input}`. Unpinned = "use latest Ready at upgrade time"; pinned = exact version. Submitted as `bundledExtensions: [{extensionId, pinnedVersion}]`. The card is empty by default — bundling is opt-in.

When the operator opens the artifact upgrade dialog later, the bundled extensions appear pre-selected in the "Also push these extensions" section, with the pinned version resolved on the server before the command is dispatched.

## Acceptance criteria (polish bar)

The implementation must satisfy these gates, derived from the design critique and polish passes. Each is grouped by surface.

**Spacing & alignment.** No raw pixel gaps in new files; all spacing uses Tailwind tokens. Card padding mirrors `ArtifactBuilder.tsx`. Step indicator is the same component used by ArtifactBuilder. Form rows follow the existing 6px label-to-input / 4px input-to-help rhythm.

**Typography.** Allowed sizes: `text-[11px]`, `text-xs`, `text-sm`, `text-base`, `text-xl`. Any deviation requires an inline comment. All card titles use the `CardTitle` component, all labels use `Label`, all buttons use `Button` — no new primitives. Monospace is restricted to paths, OCI refs, identifiers, JSON.

**Color discipline.** Orange `#EE5007` is reserved for primary brand action. Sky-500 family is `sysext`. Violet-500 family is `confext`. Green-500 family is Ready / supported / safe-default. Amber-500 family is Building / warning. Red-500 family is Error / destructive — including the new `extension` allowed-command chip. All borders go through `border-border`, all surfaces through `bg-muted/N` or equivalent tokens; no raw rgba values. Contrast verified against WCAG AA for both light and dark themes.

**Interaction states.** Every interactive element has default / hover / focus-visible / active / disabled states. Type pills on Step 2 are real `<button>` elements with `aria-pressed`. Hierarchy chips' `×` is a real `<button aria-label="Remove /opt">`. Quick-add chips have visible hover and press feedback. Template cards keyboard-select with Enter/Space. The Next button is disabled until per-step validation passes. The Build button shows the existing `Loader2` spinner during async submission. Disconnection during build surfaces a thin amber "Reconnecting…" banner above the logs.

**Micro-interactions.** Transitions limited to `transform` and `opacity`, 150–200ms, `ease-out`. `prefers-reduced-motion` honored — Building row progress bar transitions off. Chip insertion is 120ms fade + 4px translate-y; removal is instant.

**Copy.** Sentence case for labels ("Base image", not "Base Image"). No periods on single-line labels; help text gets periods. Imperative button verbs. "Reload systemd-sysext now" becomes "Activate immediately". Boot-scope buttons get `InfoTooltip` siblings explaining active/passive/recovery/common. Terminology locked: extension (noun), sysext/confext when type matters, install/upgrade/remove for actions, "replace" not "upgrade" in the version-comparison diff; never "deploy".

**Edge cases.** Empty `/extensions` shows a one-sentence value prop, primary Build button, and three template chips — Tailscale, Fluent-bit, Nvidia container toolkit. Error rows in the list show an inline excerpt of the build error and a Retry row action. Long names truncate with `title=` for full text on hover. Multi-arch mismatches surface a pre-build chip "Source image has no <arch> manifest" and disable Build until confirmed. Phase Building has a determinate progress bar; Pending has an indeterminate spinner.

**Forms.** Validation focuses and scrolls to the first invalid field (reuse ArtifactBuilder's pattern). Hierarchy chip validation enforces start-with-`/`, no `..`, not exactly `/` or `/usr`, length ≤ 256. `extraSteps` rejects lines starting with `FROM`. Server returns structured 400 errors.

**Bundling.** ArtifactBuilder's bundled-extensions card lists only Ready extensions matching the artifact's arch (cross-arch bundling is rejected client- and server-side). The artifact upgrade dialog's "Also push these extensions" section shows bundled extensions pre-selected and pinned-version resolved; pre-action diff renders three lines (OS, bundled changes with per-node replace/first-install counts, carry-forward summary). If any bundled extension is in phase `Error` at send time, the dialog blocks send with an inline explanation.

**Boot-scope semantics.** Manual-flow install dialog defaults to Common scope; picking Active reveals the amber "only enabled when booted in active mode" callout below the boot-scope row. Compound upgrade dispatch installs (overwrites) the `.raw` first; if the extension is not already enabled at any scope on the node, the agent then issues `<type> enable --active` (without `--now`) so the post-reboot active boot picks it up. There is no same-name remove step and no scope rotation in `node_extensions` — scope choices persist across upgrades because extensions are partition-independent.

**Accessibility.** All chips carry accessible names. Disclosures (`<details>` / Radix Collapsible) are keyboard-operable. Install dialog's payload preview has an `aria-label`. Focus returns to the row's Install trigger on dialog close. The cross-check strip differentiates green/amber/red also by glyph (`✓` / `⚠` / `✕`).

**Hygiene.** All icons from `lucide-react`, sized `h-4 w-4` inline / `h-5 w-5` in card headers. No TODOs in shipped code. Every interactive element has at least a render test under `ui/src/test/`. Validation rules have table-driven backend tests under `pkg/handlers/`.

## Out of scope (follow-up)

- Multi-arch builds in one record.
- Inline edit-and-rebuild of an existing extension (clone is the path).
- Plugin templates loaded from disk.
- Removing `--with-opt` (kept as a deprecated alias indefinitely).
- A dedicated "Extensions" tab on the Group detail page (the list page filter is enough for v1).
- Two-phase commit / rollback on the node for compound upgrade failures past the OS-upgrade boundary (covered by Kairos's existing dual-partition rollback).

## Test plan

Frontend:

- Render tests for ExtensionList, ExtensionBuilder (each step), InstallExtensionDialog, the artifact upgrade dialog's "Also push these extensions" section, ArtifactBuilder's new disclosure + bundled-extensions card, the node detail page's "Installed extensions" section.
- Per-step validation tests for ExtensionBuilder, including the `extraSteps` `^FROM` rule and the hierarchies normalization.
- A snapshot test of both payload shapes (standalone `extension` and compound `upgrade` with `extensions[]`).

Backend:

- Handler tests for `POST /api/v1/extensions` validating all rules.
- Handler tests for `PUT /api/v1/artifacts/:id/bundle-extensions` validating arch-matching at write time and resolution at dispatch time (pinned-version exact match, unpinned newest-by-`created_at`, structured error when no matching Ready row exists).
- Handler tests for `DELETE /api/v1/extensions/:id`: blocked when a bundle references the name, allowed when only `node_extensions` references it (rows left stale).
- Builder tests covering each source mode (artifact, image, dockerfile, artifact+steps).
- Phonehome handler tests for the `extension` command across all four actions and both types.
- Phonehome handler tests for the extended `upgrade` / `upgrade-recovery` commands: each `extensions[]` entry triggers an `install` then a conditional `enable --active` (only when not already enabled at any scope), the install order matches `extensions[]` order, an abort on extension install/enable failure does **not** invoke `kairos-agent upgrade`, and the parent upgrade still runs when `extensions[]` is empty/absent (backward-compat).
- Handler tests for the REST status-callback path (`PUT /api/v1/nodes/:nodeID/commands/:commandID/status`): on success for the manual `extension` command, the server upserts a `node_extensions` row keyed by `(node_id, name, type, scope)`; on `remove`, it deletes rows for that name on that node; on failure, rows are untouched.
- Migration test ensuring `extensions`, `artifact_extension_bundles`, `node_extensions` tables and `artifacts.extension_hierarchies` column are created on a fresh DB.

Agent:

- Existing `sysext.go` tests stay green.
- Phonehome dispatch test mirroring the existing `upgrade` test, covering both the new `extension` command and the extended `upgrade` (with `extensions[]`) dispatch + policy gating.

End-to-end:

- e2e/manual: build an extension from a base image, install it on a node booted from a paired artifact, verify the resulting symlink under `/var/lib/sysext/active`.
- e2e/bundled: build an OS artifact with a bundled extension, run the upgrade, verify the new OS *and* the new extension are both live after the single reboot.

## Phasing

Single PR, but landed in this internal order to keep diffs reviewable. **Step 0 is a separate PR in the `kairos-agent` repo** — the agent changes (new `extension` command + extended `upgrade` dispatch) ship there first, get tagged, and AuroraBoot vendors the new version before the rest of the PR is merged.

0. **kairos-agent** (separate, prerequisite PR): add `extension` phonehome command + handler, extend `handleUpgrade` to install `extensions[]` into the passive partition before the OS upgrade, add tests, tag a release.
1. AuroraBoot backend: `extensions` table, `artifact_extension_bundles` table, `node_extensions` table, `artifacts.extension_hierarchies` column on `store.ArtifactRecord`, stores, handlers, ExtensionBuilder under `pkg/builder` mirroring `internal/builder/auroraboot/builder.go`'s async `go b.run(...)` entry, download endpoint wired through the existing `auth.DownloadMiddleware`. Add `extension` to the command-type constants at `pkg/store/store.go:67-74`.
2. CLI: `--include-path` flag with `--with-opt` deprecation warning.
3. Vendor the new kairos-agent into AuroraBoot.
4. Frontend: API client, list page, builder wizard, detail page, install dialog, artifact upgrade dialog extension multi-select, ArtifactBuilder hierarchies disclosure + bundled-extensions card, node detail "Installed extensions" section.
5. Tests + docs (this spec lives alongside).
