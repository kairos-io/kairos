# UI revamp, pieces 2–4: fleet screens, build screens, agent metrics

Date: 2026-09-29
Status: written during an unattended run; review in the PR
Depends on: `2026-09-29-ui-revamp-foundations-design.md` (piece 1)

The direction is "Clarity" (direction A of the audit), plus the group board
from direction C. Every screen uses the piece 1 tokens, button variants, button
placement rules and app shell.

## Shared building blocks

These live in `ui/src/components/fleet/` and `ui/src/lib/`, and pages 2–4 use
them:

- `lib/time.ts`: `timeAgo(date)` and `formatBytes(n)`. `timeAgo` replaces the
  three copies in `Dashboard.tsx`, `NodeTable.tsx` and `NodeDetail.tsx`.
- `lib/phase.ts` (from piece 1): `phaseTone(phase)`, which returns
  `"success" | "warning" | "danger" | "info" | "neutral"`.
- `StatusDot`: a dot in a status tone.
- `StackBar`: a horizontal bar split by count per status. It has an accessible
  label such as "8 online, 3 offline".
- `StatTile`: label, value, optional sub-line, optional children. Used in
  summary bands.
- `PageHeader` gets optional `breadcrumb`, `status` (a node) and `meta` (a
  node). Its actions follow the placement rules.
- `EmptyState`: icon, title, text, optional action.

## Piece 2: fleet screens

### Dashboard (`pages/Dashboard.tsx`)

The zero-state wizard (`GetStartedHero`) stays as it is.

- **Summary band**, four `StatTile`s:
  - Fleet: the online count out of the total, with a `StackBar`.
  - Builds: the number of artifacts building. For the newest build, its name
    and how long ago it started. Also the count of artifacts that failed in the
    last 24 h.
  - Deployments: the number running, and the target of the newest one.
  - Extensions: the counts that are ready, building and in error.
- **Needs attention**: a list of rows. Each row has an icon, a title, a
  sub-line and one button. Rows are built from data the UI already loads:
  - A node with boot state `recovery` or `passive`: the button opens the node.
  - An Offline node: the button opens the node.
  - An artifact or extension in Error: the button opens its detail page.
  - A node that is not in a group: the button opens the Groups board.

  The list is empty when nothing needs attention, and then shows "Everything
  looks healthy".
- **Activity**: the 10 newest events, merged from artifacts (created, ready,
  failed), extensions (the same), deployments (started) and nodes (registered,
  last heartbeat for offline nodes). Each event has an icon in a status tone,
  one sentence and a relative time. A segmented filter chooses All, Builds or
  Nodes.
- **Groups**: one row per group, with a `StackBar` and the online count. A
  final row shows nodes that are not in a group, with an Assign button.
- **Image versions**: nodes counted by `osRelease.KAIROS_VERSION`, which main
  shows as "Image Version", drawn as horizontal bars.
- The header has "Import nodes" (outline) and "Build artifact" (primary).

### Node list (`pages/Nodes.tsx`, `components/NodeTable.tsx`)

- A **summary strip** with three cells: the status `StackBar` and legend, the
  image version bars, and a "Needs attention" list of up to 4 nodes (offline or
  in recovery).
- **Search** in one input. It matches hostname, remote IP, reported addresses,
  label keys and values, and image version. **Filter chips** for Status, Group
  and Label open small menus. An active chip shows its value and a remove
  button. The search and the filters are kept in the URL query (`q`, `phase`,
  `group`, `label`, `groupBy`, `view`), so a filtered view can be shared as a
  link.
- **Group by**: None, Group, or a label key (the default key is `site`).
  Grouped tables show a header row per group, with a count, a `StackBar`, and a
  collapse toggle.
- **Columns**: checkbox; node (a status dot, the hostname, and the remote IP
  under it); status (only when it is not Online, plus a recovery pill); image
  version; Kairos version (`agentVersion`); hardware (`CPU_COUNT` vCPU and
  `MEM_TOTAL` shown as GiB); last heartbeat; labels (the first label plus a
  "+N" count). A row click opens the node. A checkbox click does not.
- **Selection**: a checkbox on each row, one per group header, and one in the
  table header. When any row is selected, a dark action bar appears above the
  table: "N selected", then Send command (primary), Move to group, Add label,
  Clear. Send command targets the selected node IDs. When nothing is selected,
  the page header's "Send command" targets the visible nodes, as in piece 1 B4.
- **Tile view**: a toggle between list and tiles. Tiles are grouped the same
  way. Each tile has a top stripe in the status tone, the hostname, IP, image
  version, and a hardware or heartbeat line. Offline tiles are dimmed.

### Node detail (`pages/NodeDetail.tsx`)

- **Header**: breadcrumb (Nodes › group › hostname); the hostname with a status
  pill; a meta line with remote IP, group, image version and Kairos version.
  Actions: Reboot and Upgrade (outline; each opens the command dialog with that
  command chosen), Send command (primary), and ⋯ (Decommission, Clear command
  history).
- **Health strip**, five `StatTile`s: Connection (status and last heartbeat),
  Boot (boot state, and a warning tone unless it is `active`), Image, Kairos,
  Reset (`resetState`, `lastReset`).
- **Hardware card**: CPU count, memory in GiB, architecture, kernel. With agent
  metrics (piece 4), this card is replaced by the Resources card.
- **Commands** as a timeline. Each command shows an icon in its status tone, the
  command name, a status pill, its arguments on one line, and a relative time.
  The newest running command shows its output as live terminal text. A failed
  command shows the last line of its result in an inline error box. A pending
  or queued command shows no output box. A segmented filter chooses All,
  Running or Failed. The delete and clear actions stay.
- **Aside**:
  - Details: machine ID (shortened, with a copy button); remote IP ("Seen
    from"); reported addresses with their type; group selector; claim status;
    registered date.
  - Extensions: the existing "Installed extensions" card, restyled.
  - Labels as chips: each chip has a remove button, and an "Add label" chip
    opens an inline `key=value` input. It validates that the key is not empty
    and is not already used. It saves through `setLabels`.
- The existing behavior covered by the `nodeDetail.*` tests is kept: hostname
  display, label saving, version labels.

### Groups board (`pages/Groups.tsx`)

- A Board and Table switch. Table is today's table, with the real node count
  and a `StackBar` column.
- **Board**: the first column is "Not in a group", then one column per group,
  then a "New group" column that opens the create dialog. Each column header
  has the name, the online count out of the total, a `StackBar`, the
  description, and a ⋯ menu (Open, Send command, Rename, Delete).
- **Node cards** show a status dot, the hostname, and the image version or IP.
  A card can be dragged (HTML5 drag and drop) onto another column, which calls
  `setGroup(nodeID, groupID)`. Dropping on "Not in a group" calls it with `""`.
  During the drag, the target column shows a drop outline. The change appears
  on the board immediately, and is rolled back with an error toast if the API
  call fails.
- **Keyboard**: a card is focusable. Enter opens the node. The `m` key, or the
  card's ⋯ button, opens a "Move to…" menu that lists the groups.

### Group detail (`pages/GroupDetail.tsx`)

- **Header**: breadcrumb, name, description. Actions: Import nodes (outline),
  Send command (primary; opens the dialog for the whole group), and ⋯ (Edit
  name and description, Delete).
- **Summary band**: online out of total, the number of image versions, the
  count of boot issues, and total capacity (vCPU and GiB).
- An alert row for each node in recovery or passive boot.
- The node table from the node list, with selection, without the Group column.

## Piece 3: build screens

### Wizard shell (`components/wizard/WizardShell.tsx`)

This is one component, used by the artifact builder and the extension builder.

- **Props**:
  - `steps: { key: string; label: string; summary?: string; state: "todo" | "current" | "done" | "error" }[]`
  - `current: string`
  - `onStepChange(key)`
  - `aside?: ReactNode`
  - `footer: { onBack?, backLabel?, status?: { tone, text }, secondary?: ReactNode, primary: { label, onClick, disabled?, loading? } }`
  - `children`
- **Layout**: a vertical stepper on the left (it becomes a grid above the form
  below 1024px), the form in the middle, and the aside on the right (it moves
  below the form below 1024px). The footer stays at the bottom of the viewport
  (`sticky bottom-0`), across the content column.
- **Steps**: a step with state `done` or `error`, or the current step, can be
  clicked. A `todo` step cannot be clicked until every earlier step is valid.

### Artifact builder (`pages/ArtifactBuilder.tsx`)

- **Six steps**, one topic each. The existing sections are moved, not
  rewritten:
  1. **Base**: the name (now required), the templates, and the custom image or
     Dockerfile.
  2. **System**: architecture, model, variant, Kubernetes, version, and the
     Hadron advanced options.
  3. **Extensions**: the bundled-extensions card ("Install after boot"), the
     catalog picker from Output ("Bake into the image"), and "Pre-configure for
     system extensions". It shows one short explanation of the difference.
  4. **Access**: user setup, SSH keys, provisioning (auto-install, register,
     target group), remote commands, and security (FIPS, Trusted Boot).
  5. **Output**: output formats, overlay files, and Advanced (cloud-config,
     init image).
  6. **Review**.
- **Templates**: clicking a template selects it and does not change the step.
  The Custom card is not highlighted until it is selected.
- **Validation**: `computeErrors` maps each error to the new step keys. An error
  is removed as soon as its field becomes valid, so an error never stays on
  screen for a field that is now correct. Next validates only the current step.
- **Controls**:
  - Architecture, variant and user setup use a segmented control instead of
    full-width cards.
  - Remote commands use three presets:
    - Safe: `upgrade`, `upgrade-recovery`, `reboot`, `unregister`.
    - Full: Safe plus `exec`, `reset`, `apply-cloud-config`, `extension`.
    - Custom: shows the existing checkbox picker.
  - When "Default user" is selected, a warning says the password is `kairos`.
- **Summary aside**: a `BuildSummary` component
  (`components/wizard/BuildSummary.tsx`). It takes the builder state and lists
  Base, System, Extensions, Access, Output and Security, each with an Edit link
  to its step. Rows with a warning (default password, destructive commands) use
  the warning tone.
- **Review**: the same `BuildSummary`, full width, plus the cloud-config preview
  that exists today.
- **Footer**: Back, then a status ("All required fields set", or "N issues on
  this step"), then Save as template (outline), then Next: <step>, or Start
  build on the last step. Import and Export config move to the header ⋯ menu.
- The existing `artifactBuilder.*` tests keep passing. They are updated only
  where they assert which step a section is on.

### Extension builder (`pages/ExtensionBuilder.tsx`)

- It uses `WizardShell` with its three steps (Source, Configure, Review), and
  the orange primary without "→" arrows.
- **Source validation**: the name is required. The image mode requires an
  image, and the Dockerfile mode requires a Dockerfile. From-artifact mode
  requires an artifact.
- **Signing**: a select of the key sets from `listSecureBootKeySets()`, with
  "Unsigned" as the first option, instead of free text.
- Review lists every field, including hierarchies, signing and service reload.

### Extensions list and detail (`pages/Extensions.tsx`, `pages/ExtensionDetail.tsx`, `components/InstallExtensionDialog.tsx`)

- **List columns**: extension (a type pill, the name, and "from <source> ·
  <arch>" under it); status (a pill, and a progress bar in its own cell while
  building); version; installed on (the count of nodes from
  `listNodesForExtension`, loaded per row); signed (the key-set name, or
  "unsigned"); updated; actions (Install for Ready extensions, and ⋯).
- A from-artifact source shows the artifact's name, not its UUID.
- Extensions in Error get an alert above the table, with View log.
- **Detail**: the subtitle is the extension name and type, not "Extension
  <uuid>". An "Installed on" card lists nodes from `listNodesForExtension`,
  with their phase and install time.
- **Install dialog**: the shared `Select` component replaces the native
  `<select>`. The help text says: "Installing an extension with the same name
  again upgrades it."

### Artifact detail (`pages/ArtifactDetail.tsx`)

- **Header**: a "Saved as template" chip when saved; Clone (outline); Deploy as
  a split button (primary), whose menu lists the deploy methods; ⋯ (Save as
  template or remove it, Export config, Delete).
- The success banner no longer has its own Deploy button.
- **Downloads**: each file row has a Copy link button next to the download.
- **Configuration**: a read-only `BuildSummary` built from the stored artifact,
  instead of the raw block.

### Deploy dialog (`components/DeployDialog.tsx`)

- The dialog always shows the PXE and RedFish tabs. A tab whose output the
  artifact does not have is disabled, and its tooltip gives the reason, for
  example: "This artifact has no Netboot output. Clone it and enable Netboot."
  The description names only the methods that are available.

## Piece 4: agent metrics

### Contract (agent → server)

The heartbeat (the WebSocket `heartbeat` message and `POST
/api/v1/nodes/:id/heartbeat`) accepts an optional `metrics` object:

```json
{
  "sampledAt": "2026-09-29T09:41:07Z",
  "uptimeSeconds": 3895200,
  "load": [13.9, 12.1, 10.4],
  "cpu": { "usedPercent": 87.2 },
  "memory": { "totalBytes": 33501757440, "availableBytes": 7086696448 },
  "disks": [
    { "label": "COS_PERSISTENT", "mount": "/usr/local",
      "totalBytes": 257698037760, "usedBytes": 169651208192 }
  ],
  "temperatureC": 71
}
```

Every field is optional. An agent that sends no `metrics` gets today's behavior.
The agent-side change belongs in kairos-io/kairos
(`agent/internal/phonehome`). It is a follow-up and is not part of this
branch.

### Server

- `store.NodeMetrics` holds the struct above (`pkg/store/metrics.go`).
- `pkg/metrics.Buffer` keeps, in memory, the last 120 samples per node, and
  the latest sample. Its methods:
  - `Record(nodeID string, m store.NodeMetrics)`
  - `Latest(nodeID string) (store.NodeMetrics, bool)`
  - `Samples(nodeID string) []store.NodeMetrics`
  - `AllLatest() map[string]store.NodeMetrics`
  - `Forget(nodeID string)`

  It is safe for concurrent use. Samples are lost on restart, which is
  acceptable for live telemetry. A zero `sampledAt` is replaced by the time the
  server received the sample.
- The REST and WS heartbeat handlers record `metrics` when it is present.
  Deleting a node calls `Forget`.
- `GET /api/v1/nodes/:nodeID/metrics` (admin) returns
  `{ "latest": NodeMetrics | null, "samples": NodeMetrics[] }`.
- `GET /api/v1/metrics/latest` (admin) returns `{ "<nodeID>": NodeMetrics }`.
- Both are in the OpenAPI spec.

### UI

- `api/metrics.ts`: `getNodeMetrics(id)` and `getLatestMetrics()`.
  `lib/metrics.ts` computes `cpuPercent`, `memPercent`, `diskPercent` (the
  highest-used disk, and per disk), and `formatUptime`.
- Components (`components/fleet/`):
  - `Gauge`: an SVG ring, in a tone by threshold: warning at ≥ 80 %, danger at
    ≥ 90 %.
  - `Sparkline`: an SVG line and area, from `number[]`.
  - `MeterBar`: a bar with the percentage.
- **Node detail**: when `latest` exists, a Resources card replaces the Hardware
  card. It has gauges for CPU and memory with sparklines from `samples`, a bar
  per disk, and load, uptime and temperature. A CPU of 85 % or more across the
  last 10 samples shows a warning alert under the header. The card polls every
  10 s.
- **Node list**: when any node has metrics, the Kairos and Hardware columns are
  replaced by CPU, Memory and Disk `MeterBar`s. The tile view gets "Color by
  Status, CPU, Memory, Disk".
- **Dashboard**: a "Fleet resources" card with average CPU and memory gauges and
  the 3 busiest nodes. Nodes above the thresholds are added to "Needs attention".
- **Groups board**: node cards show three small bars (CPU, memory, disk).
- When no node reports metrics, every screen looks exactly as it does without
  piece 4.
