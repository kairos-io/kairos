# Redfish Deployment

> **Status: EXPERIMENTAL.** The Redfish path has been validated against
> [sushy-tools](https://docs.openstack.org/sushy-tools/latest/) (a spec-compliant BMC
> emulator) and the generic/DMTF profile. It has **NOT** been tested against real iLO,
> Supermicro, or Dell hardware. Treat the vendor-specific profiles as a best-effort match
> to public documentation until catalogue item
> [#7](https://github.com/kairos-io/kairos/issues/4115) is closed.

AuroraBoot can provision a server over its BMC using the
[Redfish](https://www.dmtf.org/standards/redfish) standard. Two modes are available:

- **CLI** (`auroraboot redfish deploy`) — for scripted, one-shot provisioning.
- **Fleet server** (`auroraboot web`) — for provisioning managed nodes via the REST API.

## How it works

Redfish virtual-media deployment is a URL-pull: **the BMC fetches the ISO, not AuroraBoot.**
The `VirtualMedia.InsertMedia` action takes an HTTP(S) URL; the BMC's internal firmware
downloads the image from it and mounts it as a virtual CD/DVD. There is no byte-upload path.

The full deployment flow, as executed by `pkg/redfish.Deployer`:

1. Authenticate: POST a JSON credential body to create a Redfish session; keep the
   `X-Auth-Token` for all subsequent requests.
2. Discover: list the `Systems` collection and take the first `ComputerSystem`. No
   resource IDs are hardcoded.
3. Find virtual media: search `VirtualMedia` on the `ComputerSystem` first, then on each
   `Manager`, for a CD/DVD-capable slot. (HPE iLO exposes it under the Manager; the
   generic path finds it either way.)
4. Insert media (`VirtualMedia.InsertMedia`): pass the ISO URL and set
   `TransferProtocolType` to HTTP or HTTPS. The BMC begins fetching.
5. Set one-time boot: PATCH `ComputerSystem.Boot` with `BootSourceOverrideEnabled: Once`,
   `BootSourceOverrideTarget: Cd`, and `BootSourceOverrideMode: UEFI`.
6. Reset: POST `ComputerSystem.Actions.#ComputerSystem.Reset` with an appropriate
   `ResetType` (`On` when the system is off; `ForceRestart` otherwise), selected from the
   system's advertised allowable values.
7. Poll: if the BMC returned a `202` with a `Task` location, GET that task every three
   seconds until it reaches a terminal state (`Completed`, `Exception`, `Killed`, or
   `Cancelled`).
8. Clean up: DELETE the Redfish session. This happens on both success and error paths.

AuroraBoot does not stay in the loop after step 6/7. Once the boot override and reset are
confirmed, the node boots the ISO and runs the Kairos installer autonomously.

---

## CLI usage

### Prerequisites

- The BMC Redfish endpoint must be network-reachable from the machine running AuroraBoot.
- The ISO must be network-reachable from the BMC (see "Serving the ISO" below).
- You need a Redfish username and password with enough privilege to manage virtual media
  and reset the system.

### Serving the ISO

Because InsertMedia is URL-pull, you must provide a URL the BMC can reach. Two options:

**Option 1 — operator-hosted:** host the ISO yourself (HTTP server, S3, NFS, etc.) and
pass the URL with `--image-url`. AuroraBoot SSRF-validates the URL but otherwise hands it
straight to the BMC.

**Option 2 — AuroraBoot-served:** pass a local ISO path as the positional argument and
set `--redfish-serve-url` to the base URL the BMC will use to fetch it. AuroraBoot starts
a one-shot tokenized HTTP server on that address, registers the ISO under an opaque
32-byte random token, passes the resulting URL to InsertMedia, and shuts the server down
once the deployment returns. The BMC-side URL looks like:

```
http://10.0.0.5:8090/redfish/iso/<token>/kairos.iso
```

The token is the only capability; the server never lists files or accepts directory
traversal.

### Minimal examples

**With an operator-hosted URL:**

```bash
auroraboot redfish deploy \
  --endpoint   https://bmc.example.com \
  --username   admin \
  --password-file /run/secrets/bmc-password \
  --image-url  http://fileserver.example.com/kairos.iso
```

**With a local ISO served by AuroraBoot:**

```bash
auroraboot redfish deploy \
  --endpoint           https://bmc.example.com \
  --username           admin \
  --password-file      /run/secrets/bmc-password \
  --redfish-serve-url  http://10.0.0.5:8090 \
  --redfish-serve-addr 10.0.0.5:8090 \
  /path/to/kairos.iso
```

`--redfish-serve-addr` defaults to the `host:port` of `--redfish-serve-url`, so you can
omit it when the two match.

### Credential options

Pass exactly one of:

| Option | Notes |
|---|---|
| `--password-file /path` | Recommended. File content is read; trailing newline trimmed. |
| `AURORABOOT_REDFISH_PASSWORD` | Environment variable. Useful in container runtimes. |
| `--password-stdin` | Reads from standard input. Works with `echo \| auroraboot …` or a prompt. |
| `--password <value>` | Insecure: the password appears in the process list (`ps aux`). Avoid on shared hosts. |

Precedence when multiple are set: `--password` > `--password-file` > env var > `--password-stdin`.

### Full flag reference

| Flag | Default | Description |
|---|---|---|
| `--endpoint` | (required) | Redfish endpoint URL (`https://bmc.example.com`). |
| `--username` | (required) | Redfish username. |
| `--password-file` | | File holding the password (recommended). |
| `--password-stdin` | | Read password from stdin. |
| `--password` | | Inline password (insecure; visible in process list). |
| `--image-url` | | URL the BMC fetches the ISO from. Mutually exclusive with a local ISO path. |
| `--redfish-serve-url` | | Advertised base URL for the local ISO server (e.g. `http://10.0.0.5:8090`). Required when passing a local ISO path. |
| `--redfish-serve-addr` | derived from `--redfish-serve-url` | Bind address for the local server. |
| `--serve-tls` | false | Use HTTPS for the local ISO server. Requires `--serve-tls-cert`, `--serve-tls-key`, and an `https://` `--redfish-serve-url`. Not valid with `--image-url`, which bypasses the local server. |
| `--serve-tls-cert` | | TLS certificate file for the local ISO server. Only valid with `--serve-tls`. |
| `--serve-tls-key` | | TLS key file for the local ISO server. Only valid with `--serve-tls`. |
| `--vendor` | `generic` | Hardware profile: `generic`, `dmtf`, `ilo`, `supermicro`. |
| `--verify-ssl` | true | Verify TLS certificates when connecting to the BMC endpoint. |
| `--min-memory` | 4 | Minimum required system memory in GiB. Deploy aborts if below this. |
| `--min-cpus` | 2 | Minimum required CPU count. Deploy aborts if below this. |
| `--required-features` | `UEFI` | Hardware features the system must support. Detectable values: `UEFI`, `SecureBoot`. An unknown or undetectable required feature fails the deploy. |
| `--timeout` | 30m | Overall operation timeout. |

The positional argument (if given) is the local ISO file path. Provide either
`--image-url` or a local path plus `--redfish-serve-url`; providing neither or both is an
error.

### TLS and network posture

`--verify-ssl` defaults to `true`; the BMC endpoint certificate is verified against the
system trust store. Disable it only in isolated lab environments.

The local ISO server defaults to **plain HTTP**. This is acceptable when both AuroraBoot
and the BMC are on an isolated, trusted management network (a common data-centre
topology). Integrity of the payload is delegated to the Kairos image signature and
SecureBoot rather than the transport. If your environment requires encryption on the
management network, set `--serve-tls` and provide a certificate the BMC trusts.

The `TransferProtocolType` AuroraBoot advertises on InsertMedia is derived from the URL
the BMC is handed, never from a flag: the scheme of `--image-url` in URL-pull mode, and
whether the local ISO server has a certificate in local-serve mode. A `--serve-tls` that
disagreed with `--redfish-serve-url` would leave the BMC fetching the wrong scheme, so
the deploy refuses to start instead.

### Vendor profiles

| Value | Notes |
|---|---|
| `generic` (default) | Spec-compliant DMTF Redfish. Targets sushy-tools, PiKVM, and any conformant BMC. |
| `dmtf` | Identical to `generic`. |
| `ilo` | Searches for virtual media under the Manager (where HPE iLO exposes it) before falling back to the System. InsertMedia parameters and reset type follow the spec default. **Not verified on real iLO hardware (#7).** |
| `supermicro` | Currently identical to `generic`. Known firmware sensitivities are documented in the source but no quirk has been added without hardware confirmation. **Not verified on real Supermicro hardware (#7).** |

### Hardware gate

Before deploying, AuroraBoot inspects the system via Redfish and validates the
requirements set by `--min-memory`, `--min-cpus`, and `--required-features`. The deploy
aborts with a clear error if any requirement is not met.

The `--required-features` gate fails closed: if a required feature cannot be positively
detected from the Redfish data (e.g. the BMC does not advertise it), the deploy is
rejected. The default requirement is `UEFI`; add `SecureBoot` if your image requires it.

Features AuroraBoot can currently detect:

- `UEFI` — derived from the `BootSourceOverrideMode` allowable values advertised by the
  `ComputerSystem`, with secondary fallbacks.
- `SecureBoot` — derived from the presence of the `SecureBoot` link on the
  `ComputerSystem`.

Anything else (e.g. `TPM`) is not detectable from standard Redfish and will always fail
the gate — do not add it to `--required-features` unless you know the BMC advertises it.

---

## Fleet server usage

The `auroraboot web` fleet server exposes Redfish deployment through its REST API (admin
bearer auth required: `Authorization: Bearer <admin-password>`).

### Enabling local ISO serving

To let the server automatically serve artifact ISOs to BMCs, start it with a bind address
and an advertised URL:

```bash
auroraboot web \
  --listen              :8080 \
  --redfish-serve-addr  10.0.0.5:8090 \
  --redfish-serve-url   http://10.0.0.5:8090
```

`--redfish-serve-addr` is required to activate the ISO server. If `--redfish-serve-url`
is omitted, AuroraBoot falls back to `--url` (the external server URL). Set
`--redfish-serve-url` explicitly when the BMC management network uses a different address
than the UI network.

Without `--redfish-serve-addr`, the server has no ISO server and every Redfish deploy
request must supply an explicit `imageUrl`.

The `--redfish-serve-tls-cert` and `--redfish-serve-tls-key` flags activate HTTPS on the
ISO server (same posture as the CLI `--serve-tls`). The advertised URL carries the scheme
the BMC reads, so it must agree: pass both TLS flags together with an `https://`
`--redfish-serve-url`, and leave the URL `http://` when they are not set. AuroraBoot
refuses to start on a mismatch, because the BMC would otherwise be told to fetch over one
protocol from a URL naming the other. Note that `--redfish-serve-url` falls back to
`--url`, so enabling TLS means setting one of the two to an `https://` URL.

Environment variable equivalents: `AURORABOOT_REDFISH_SERVE_URL`,
`AURORABOOT_REDFISH_SERVE_ADDR`.

### BMC target management

Save BMC credentials as named targets so they can be reused across deploys.

```bash
# Create a target
curl -sX POST http://localhost:8080/api/v1/bmc-targets \
  -H "Authorization: Bearer <password>" \
  -H "Content-Type: application/json" \
  -d '{
    "name":      "rack1-node3",
    "endpoint":  "https://bmc.rack1.example.com",
    "username":  "admin",
    "password":  "secret",
    "vendor":    "generic",
    "verifySSL": true
  }'

# List targets (passwords are never returned)
curl -s http://localhost:8080/api/v1/bmc-targets \
  -H "Authorization: Bearer <password>"

# Inspect hardware on a target
curl -sX POST "http://localhost:8080/api/v1/bmc-targets/<id>/inspect" \
  -H "Authorization: Bearer <password>"
```

BMC passwords are encrypted at rest with AES-256-GCM (a per-server data encryption key
stored at `data/secrets/bmc-key`). Passwords are never returned by the API (`"password"`
is omitted from all responses).

The hardware inspection endpoint (`POST /api/v1/bmc-targets/:id/inspect`) connects to the
BMC, reads the `ComputerSystem`, and returns:

```json
{
  "memoryGiB": 64,
  "processorCount": 2,
  "model": "ProLiant DL380 Gen10",
  "manufacturer": "HPE",
  "serialNumber": "MXQ...",
  "supportedFeatures": ["UEFI"]
}
```

`supportedFeatures` lists only what AuroraBoot positively detected. It is informational;
the API does not gate on required features (the CLI does).

### Deploying an artifact

```bash
# Deploy using a saved BMC target (server serves the ISO automatically)
curl -sX POST "http://localhost:8080/api/v1/artifacts/<artifact-id>/deploy/redfish" \
  -H "Authorization: Bearer <password>" \
  -H "Content-Type: application/json" \
  -d '{"bmcTargetId": "<bmc-target-id>"}'

# Deploy using inline credentials and an operator-supplied image URL
curl -sX POST "http://localhost:8080/api/v1/artifacts/<artifact-id>/deploy/redfish" \
  -H "Authorization: Bearer <password>" \
  -H "Content-Type: application/json" \
  -d '{
    "endpoint":  "https://bmc.example.com",
    "username":  "admin",
    "password":  "secret",
    "vendor":    "generic",
    "verifySSL": true,
    "imageUrl":  "http://fileserver.example.com/kairos.iso"
  }'
```

The server responds immediately with `202 Accepted` and a deployment record:

```json
{
  "id":          "3fa85f64-...",
  "artifactId":  "...",
  "method":      "redfish",
  "status":      "Active",
  "message":     "Deployment initiated",
  "progress":    0,
  "startedAt":   "2026-06-03T10:00:00Z"
}
```

The deployment runs asynchronously. Poll or list to follow progress:

```bash
# Get a single deployment
curl -s "http://localhost:8080/api/v1/deployments/<deployment-id>" \
  -H "Authorization: Bearer <password>"

# List all deployments
curl -s "http://localhost:8080/api/v1/deployments" \
  -H "Authorization: Bearer <password>"
```

The `status` field transitions: `Active` → `Completed` or `Failed`. The `message` field
carries the current step label (`discovering`, `inserting media`, `setting boot`,
`resetting`, `polling task`) while the deploy is in progress, and a final summary on
completion. The `progress` field is an integer 0–100 that only advances, never regresses.

If the server restarts while a deployment is `Active`, the orphaned row is flipped to
`Failed` with the message `"interrupted by server restart"` on the next startup.

### Web UI (dashboard)

The AuroraBoot web dashboard (`auroraboot web`) provides a point-and-click Redfish deploy
workflow with two features that are not available through the REST API alone: a pre-flight
hardware gate in the deploy dialog, and live per-step progress in the Deployments view.

#### Opening the deploy dialog

Navigate to an artifact's detail page and click **Deploy**. If the artifact includes an
ISO file the dialog opens with a **RedFish** tab alongside the PXE tab. Select a saved BMC
target from the dropdown (targets must be created first — see
[BMC target management](#bmc-target-management) or use **+ Add new target** inline).

#### Pre-flight hardware gate

After selecting a saved BMC target, click **Pre-flight inspect**. The dialog calls
`POST /api/v1/bmc-targets/:id/inspect` and displays the hardware facts the server reads
from the BMC:

- Model, manufacturer, and serial number
- Memory (GiB) and processor count
- Supported features (e.g. `UEFI`, `SecureBoot`)

The UI enforces minimum thresholds: **2 GiB memory** and **1 processor**. A node that
reports less than either value is highlighted in red and a warning banner blocks the
**Deploy via RedFish** button. The operator must tick **Deploy anyway (override)** to
unblock the button; the warning is intentional friction, not a hard server-side rejection.

Limitation: pre-flight inspection requires a saved BMC target. There is no
by-credentials inspect endpoint, so the gate is skipped when a deploy uses inline
credentials supplied directly in the form. Deploy is still allowed in that case — the
button is not blocked.

#### Starting a deploy

Click **Deploy via RedFish** to call
`POST /api/v1/artifacts/:id/deploy/redfish` with the selected target's ID. The server
responds immediately with `202 Accepted`; the actual Redfish flow (InsertMedia → boot
override → reset → optional task poll) runs asynchronously. The dialog shows a success
banner when the request is accepted. Switch to the **Deployments** view to follow
progress.

#### Live deploy-step progress (Deployments view)

The Deployments page subscribes to the UI WebSocket channel. The server broadcasts a
`deploy-progress` event after each step of the deploy flow, in the envelope:

```json
{
  "type": "deploy-progress",
  "data": {
    "deploymentId": "<uuid>",
    "status":       "Active",
    "progress":     40,
    "step":         "inserting media",
    "message":      "inserting media"
  }
}
```

The page patches the matching row in-place (status badge, progress bar, step text) on
every event without a full refetch. The `progress` field is an integer 0–100 that only
advances; the `message` field shows the current step label (`discovering`,
`inserting media`, `setting boot`, `resetting`, `polling task`, `completed`, or
`failed`).

`GET /api/v1/deployments` remains authoritative. The page also runs a REST poll every
**15 seconds** while any deployment is `Active` or `Running` as a fallback for events
missed during a WebSocket reconnect. If the hub is not wired (e.g. in test builds) all
updates arrive via this poll path.

Not yet in the UI: a dedicated BMC-management page, deployment-to-node linkage, and
per-deployment error drill-down are planned follow-on work.

### REST API summary

All Redfish-related endpoints require admin bearer authentication.

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/v1/bmc-targets` | Create a saved BMC target. |
| `GET` | `/api/v1/bmc-targets` | List all BMC targets (passwords omitted). |
| `PUT` | `/api/v1/bmc-targets/:id` | Update a BMC target. |
| `DELETE` | `/api/v1/bmc-targets/:id` | Delete a BMC target. |
| `POST` | `/api/v1/bmc-targets/:id/inspect` | Inspect hardware on a BMC target. |
| `POST` | `/api/v1/artifacts/:id/deploy/redfish` | Start a Redfish deployment for an artifact. |
| `GET` | `/api/v1/deployments` | List all deployments. |
| `GET` | `/api/v1/deployments/:id` | Get a single deployment. |

---

## Status and limitations

- **EXPERIMENTAL.** The API and CLI flags may change before a stable release.
- Validated against sushy-tools (spec-compliant DMTF emulator) and the generic profile.
  The `ilo` and `supermicro` vendor profiles are derived from public documentation and have
  **not** been tested on real hardware (tracking: #7).
- The multipart HTTP push path (an alternative to URL-pull on some newer BMCs) is not
  implemented. InsertMedia URL-pull is the only media insertion method.
- The local ISO server uses plain HTTP by default. Use `--serve-tls` (CLI) or
  `--redfish-serve-tls-cert`/`--redfish-serve-tls-key` (server) to opt in to HTTPS. When
  running on HTTP, deploy only on trusted, isolated management networks.
- Feature detection (`UEFI`, `SecureBoot`) relies on the BMC advertising the relevant
  fields in the `ComputerSystem` response. A BMC that omits those fields will fail
  `--required-features UEFI` even if the hardware supports UEFI; in that case pass
  `--required-features ""` (no required features) to bypass the gate.
- Session cleanup (Redfish session DELETE) runs on both success and error paths. If the
  process is killed hard (e.g. `kill -9`), the session is leaked and must be cleaned up
  on the BMC manually.
- The dashboard pre-flight gate (2 GiB / 1 CPU thresholds) is UI-side only and applies
  only to saved BMC targets. It is not enforced by the server on the deploy endpoint.
  Inline-credential deploys from the UI skip the gate entirely. The CLI enforces
  `--min-memory` and `--min-cpus` server-side regardless.
- The Deployments view receives live progress via the UI WebSocket (`deploy-progress`
  events) and falls back to a 15-second REST poll when the WebSocket is unavailable or
  reconnecting. Either path converges on the same deployment store state.
