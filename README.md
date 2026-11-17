<p align="center">
  <img src="docs/screenshots/dashboard-zero-state.png" alt="Kairos dashboard" width="820" />
</p>

<h1 align="center">AuroraBoot</h1>
<p align="center">
  <strong>The Kairos bootstrapper — build images, provision machines, manage the fleet.</strong>
</p>

<p align="center">
  <a href="https://opensource.org/licenses/"><img alt="License" src="https://img.shields.io/badge/license-Apache%202.0-blue" /></a>
  <a href="https://github.com/kairos-io/kairos/issues"><img alt="Issues" src="https://img.shields.io/github/issues/kairos-io/kairos"></a>
  <a href="https://kairos.io/docs/" target="_blank"><img alt="docs" src="https://img.shields.io/badge/Documentation-blue"></a>
  <img alt="Made with Go" src="https://img.shields.io/badge/made%20with-Go-blue">
  <img alt="Go report" src="https://goreportcard.com/badge/github.com/kairos-io/AuroraBoot" />
</p>

> **Found a bug, or want to request a feature?** Open it on
> [kairos-io/kairos](https://github.com/kairos-io/kairos/issues), including
> issues about this repository. Every Kairos issue lives in one place, so you
> never have to work out which repository to file against.

AuroraBoot is the official bootstrapper for [Kairos](https://kairos.io). With a single binary you can:

- **Build** bootable images — ISO, UKI, raw disks, sysextensions — from any Kairos flavor or container image.
- **Provision** machines over the network via PXE/netboot, Redfish, or plain USB.
- **Customize** installation media with cloud-configs so machines install themselves unattended.
- **Manage** a whole fleet from a browser once nodes come online.

Two ways to run it, same binary:

1. **One-shot CLI** — `auroraboot build-iso`, `auroraboot build-uki`, `auroraboot netboot`, … for quick builds and scripted pipelines.
2. **Fleet server** — `auroraboot web` (or `docker compose up`) gives you a self-hosted dashboard, REST API, node manager, SecureBoot key store and netboot server, all in one place.

---

## Getting started with the fleet server

```bash
git clone https://github.com/kairos-io/AuroraBoot
cd AuroraBoot
docker compose up --build -d
```

On first boot AuroraBoot generates an admin password and a node registration token. `docker-compose.yml` keeps `/data` in a named volume, not a bind mount, so read them from inside the container:

```bash
docker compose exec auroraboot cat /data/secrets/admin-password
docker compose exec auroraboot cat /data/secrets/registration-token
```

Open **http://localhost:9099**, sign in, and the welcome wizard walks you through the three steps: build an artifact, deploy it, manage the nodes that come online.

If you reach the UI from another machine (not `localhost`), set `AURORABOOT_URL` to that reachable address before starting the stack. Left unset, AuroraBoot falls back to the container's hostname, which nodes can't resolve, so they can't phone home. See `--url` below. Its host is also the address the Deploy dialog reports for the netboot server; with no `AURORABOOT_URL` that dialog reports a local interface address instead.

Behind a reverse proxy that rewrites the `Host` header, set `AURORABOOT_URL` (`--url`) to the address the browser uses. The server refuses sign-in and the live-update socket from a page whose origin matches neither the request `Host` nor that URL, and logs a `refused a request from Origin` line naming all three values.

### What you get

- **A guided Artifact Builder** with ready-made templates for Ubuntu, Fedora, Debian, openSUSE, Alpine, Rocky and Hadron. Pick architecture, model, variant — the UI filters the choices so you only see what Kairos actually supports.
- **A deployment surface** — download the image, PXE-boot a rack of machines into it, hand it off to a Redfish BMC, or upgrade a node you already registered.
- **A node manager** — every machine AuroraBoot builds an image for phones home automatically and shows up in the Nodes list. From there you can send commands: upgrade, reboot, reset, apply a cloud-config, or run arbitrary shell with captured output.
- **A SecureBoot key store** for full PK / KEK / db sets plus TPM PCR policy keys, generated on demand and referenced by name from UKI builds. Keys can be exported as a signed archive and imported on another instance.
- **A REST API and Go client** mirroring the UI one-to-one, with Swagger UI at `/api/docs` and a first-class client at [`pkg/client`](pkg/client).

### Common settings

| Flag / env var | What it does |
|---|---|
| `--listen :8080` | HTTP listen address |
| `--data-dir ./data` | Where the DB, artifacts, keys and secrets live |
| `--db <dsn>` | Override DB DSN (SQLite by default, Postgres supported) |
| `--url https://…` | External URL of this instance, injected into cloud-configs so nodes know where to phone home |
| `AURORABOOT_ADMIN_PASSWORD` | Override admin password |
| `AURORABOOT_REG_TOKEN` | Override registration token |
| `--disable-rate-limit` | Turn off per-identity rate limiting of the node-driven endpoints |
| `--reset-timeout 30m` / `AURORABOOT_RESET_TIMEOUT` | How long a node has to come back after a reset before the reset is reported failed. Defaults to 30 minutes; the clock restarts when the node picks the reset command up and again when it re-registers mid-reset, and a negative duration disables expiry |

See the full [AuroraBoot reference](https://kairos.io/docs/reference/auroraboot/) for everything else.

### Rate limiting

The node-driven endpoints — registration, heartbeat and command polling — are
rate-limited **on by default** so a single misbehaving node, or a leaked
registration token, can't flood the fleet server. Registration is limited per
client IP; heartbeat and command polling are limited per node. Admin/UI/API
traffic (including the CAPI infra provider, which authenticates as admin) is
**never** rate-limited.

The defaults are generous and only ever bite a runaway or a flood. Tune or
disable them if needed:

- `--node-rate-limit <rps>` / `AURORABOOT_NODE_RATE_LIMIT` — per-node requests/sec
- `--register-rate-limit <rps>` / `AURORABOOT_REGISTER_RATE_LIMIT` — per-IP requests/sec
- `--node-rate-limit-burst <n>` / `AURORABOOT_NODE_RATE_LIMIT_BURST` — per-node burst
- `--register-rate-limit-burst <n>` / `AURORABOOT_REGISTER_RATE_LIMIT_BURST` — per-IP burst
- `--disable-rate-limit` / `AURORABOOT_DISABLE_RATE_LIMIT` — turn it off entirely

Rate is the sustained refill; burst is how many requests one identity may make in
the same instant. Left unset, burst is the larger of 20 and one second of the
configured rate, so raising a rate raises the peak with it and the generous
defaults keep their 20. Set a burst explicitly to go either way — for example a
low `--register-rate-limit` paired with a low `--register-rate-limit-burst` to
leave a token brute-force no free instant allowance.

If a whole rack of nodes registers at once from behind a single NAT egress IP,
they share one per-IP registration bucket; raise `--register-rate-limit` or
disable rate limiting for that deployment. The per-IP key is the client address
as the server sees it (honouring `X-Forwarded-For` behind a trusted proxy), so
it's a flood speed-bump rather than a hard boundary — the registration token
remains the actual access control.

### Hadron builds

The Artifact Builder includes a **"Hadron custom"** template that composes a
Hadron OS image from a base release, per-vendor firmware layers and pre-built
software layers. Selecting that template opens an inline composer panel inside
the normal Kairos wizard:

1. **Base image** — pick a Hadron release tag from a live dropdown (fetched
   from GitHub Releases) or enter a fully-qualified custom image ref.
2. **Firmware** — browse and select from the published
   [hadron-firmware](https://kairos-io.github.io/hadron-firmware/data.json)
   catalog. Items can be reordered and removed.
3. **Software layers** — browse and select from the published
   [hadron-layers](https://kairos-io.github.io/hadron-layers/releases.json)
   catalog. Same drag-to-reorder, searchable list.
4. **Extra Dockerfile** — free-form lines appended after the generated
   `COPY` instructions, for site-specific customisations.
5. **Compose & continue** — renders a Dockerfile from the selection
   (layout-normalising `RUN`, one `COPY --from=` per firmware, one per layer,
   extras appended) and advances to the Configure step of the normal Kairos
   builder. From there the Dockerfile drives the standard Kairos build
   pipeline, producing ISOs, UKIs and raw disks exactly like any other
   artifact.

A live Dockerfile preview is always visible in the panel so you can verify the
rendered output before committing to a build.

### System extensions at build time

The Output step of the Artifact Builder lists the extensions a catalog
publishes and writes the ones you select into the built ISO, so the installed
system carries them without pulling anything on first boot. The catalog field
is pre-filled with the
[hadron-layers](https://kairos-io.github.io/hadron-layers/releases.json)
catalog, the same index a node reads, and can be pointed at your own. Only
extensions published for the architecture being built are offered, and each
one can be pinned to a version or left on the catalog's latest.

On a classic (non-UKI) ISO, the build also writes `extensions.yaml` to the ISO
root. It declares each image under `install.extensions`, which is what the
installer stages onto the installed system. This needs a kairos-agent that
reads `install.extensions`, which is newer than v4.3.0.

The same `iso.extensions` and `iso.extensions_catalogs` keys drive a raw disk
or cloud image build, so an artifact spec names its extensions once and each
artifact type decides where they go. A raw disk is assembled rather than
installed, so the images ride in the OEM partition and the first-boot reset
moves them onto the persistent one. Either way the extension is merged with no
network access at install time.

### Cloning Hadron artifacts as templates

Every artifact built from the Hadron composer stores the source composition
(`hadronBase`, `hadronFirmware`, `hadronLayers`, `hadronExtra`) alongside the
rendered Dockerfile. Clicking **Clone as Hadron** on such an artifact row
reopens the composer pre-filled with that composition so you can make targeted
changes — bump the Hadron version, swap a firmware, add a layer — and then
compose a new artifact without rebuilding the recipe from scratch. Non-Hadron
artifacts continue to clone through the standard path.

---

## Using the CLI

You don't need the fleet server to get value out of AuroraBoot. The historical one-shot CLI is fully preserved, and it's often all you need for a quick build or a one-off netboot.

### Netboot a machine from a Kairos release

Run this on a machine on the same network as the target, and let the target PXE-boot:

```bash
docker run --rm -ti --net host quay.io/kairos/auroraboot \
    --set "artifact_version=v2.4.2" \
    --set "release_version=v2.4.2" \
    --set "flavor=rockylinux" \
    --set "flavor_release=9" \
    --set "repository=kairos-io/kairos" \
    --cloud-config /path/to/cloud-config.yaml
```

This downloads the needed artifacts, bakes your cloud-config into a custom ISO, and serves it over the network.

> **Security note:** the netboot HTTP server is **unauthenticated by design**. A PXE/HTTP-booting machine can't present a credential, so everything it needs — kernel, initrd, squashfs, and the cloud-config baked into the ISO (which may carry a registration token, SSH keys or passwords) — is served over plain HTTP to anyone who can reach it. By default it binds all interfaces on `:8080`. Run it only on a **trusted, isolated provisioning network**, and prefer binding a specific interface with `--set listen_addr=<host>:8080` over exposing it everywhere. AuroraBoot logs a warning at startup to make this boundary explicit.

Supported architectures:
- `amd64` (default, matches x86_64)
- `arm64` (matches aarch64)
- `riscv64`

### Use a container image instead

Point AuroraBoot at any Kairos container image (or your own) and it will boot that:

```bash
docker run --rm -ti --net host quay.io/kairos/auroraboot \
    --set container_image=quay.io/kairos/rockylinux:9-core-amd64-generic-v2.4.2
```

Add `-v /var/run/docker.sock:/var/run/docker.sock` if you want to use an image already sitting in your local Docker daemon instead of pulling from a remote.

### Cross-architecture builds

When pulling images for a different architecture than the host, set `arch`:

```bash
docker run --rm -ti --net host quay.io/kairos/auroraboot \
    --set container_image=quay.io/kairos/alpine:3.21-standard-arm64-rpi4-v3.6.0 \
    --set arch=arm64
```

Supported: `amd64` (default), `arm64`, and `riscv64`.

### Offline ISO, no netboot

To generate an ISO without starting the PXE server, pass `disable_netboot=true`:

```bash
docker run -v /var/run/docker.sock:/var/run/docker.sock --rm -ti --net host \
    quay.io/kairos/auroraboot \
    --set container_image=quay.io/kairos/rockylinux:9-core-amd64-generic-v2.4.2 \
    --set disable_netboot=true
```

### Configuration file

Everything on the `--set` flag can also live in a YAML file:

```yaml
artifact_version: "v2.4.2"
release_version: "v2.4.2"
container_image: "..."
arch: "amd64"  # Optional: architecture to use when pulling container images (amd64, arm64, or riscv64)
flavor: "rockylinux"
flavor_release: "9"
repository: "kairos-io/kairos"

cloud_config: |
  #cloud-config
  install:
    device: "auto"
    auto: true
    reboot: true
  users:
    - name: kairos
      passwd: kairos
```

Passing `-` to `--cloud-config` reads it from stdin, which is handy in CI pipelines.

### Other subcommands

```bash
# Build an ISO from a container image. The source is a positional argument.
# --output defaults to the current directory. It only receives the ISO and its
# checksum. In Docker, pass --output or -w, because the image sets no working
# directory.
auroraboot build-iso quay.io/kairos/ubuntu:24.04-core-amd64-generic-v3.6.0 \
    --override-name kairos --output ./out

# Build a UKI from a container image
auroraboot build-uki quay.io/kairos/ubuntu:24.04-standard-amd64-generic-v3.6.0 \
    --output-dir ./out

# Build an ISO carrying system extensions from a catalog. Names resolve against
# the hadron-layers catalog unless --extensions-catalog names another one, and
# an extension can be pinned with name@version. Both flags are repeatable, and
# catalogs are searched in order, so your own index can shadow a published name.
auroraboot build-iso quay.io/kairos/ubuntu:24.04-core-amd64-generic-v3.6.0 \
    --extension nvidia --extension tailscale@v1.2.3 --output ./out

# Generate a SecureBoot key set
auroraboot genkey my-keys --output ./keys

# Generate a sysextension from a container image (sysext and confext must run as root)
auroraboot sysext my-ext quay.io/myorg/my-tool:latest

# Redfish-driven deploy to a BMC
auroraboot redfish deploy --endpoint https://bmc/redfish/v1 --username admin \
    --password-file ./bmc-password --image-url http://10.0.0.5:8090/kairos.iso

# Extract netboot artifacts from an ISO
auroraboot netboot kairos.iso ./netboot-out
```

Run `auroraboot help` for the full list.

### Experimental

- [Redfish deploy notes](./redfish.md)

---

## Under the hood

- **One binary, one container.** Go backend, React frontend bundled into the binary at build time and served by the same process. SQLite by default, Postgres optional.
- **CLI and fleet server share the same image factory.** Both call `deployer.Deploy`, `pkg/uki.Build` and `pkg/secureboot.GenerateKeySet` in-process — whatever the CLI builds, the server builds the same way, and streams the logs into the dashboard as they come out of the deployer.
- **Nodes auto-register** via the `phonehome:` cloud-config stage baked into every artifact AuroraBoot produces. First boot → node shows up in the UI. The artifact builder also bakes an explicit `allowed_commands` list (default: `upgrade`, `upgrade-recovery`, `reboot`, `unregister`); tick the destructive checkboxes — `exec`, `reset`, `apply-cloud-config` — only on fleets where you need them.
- **Deleting a node** runs a remote teardown: AuroraBoot sends an `unregister` command, the agent stops the phone-home service and drops its credentials + cloud-config files, then the DB record is removed. The UI shows live progress. For offline nodes, SSH in and run `kairos-agent phone-home uninstall` to do the same teardown by hand before force-deleting the record.

---

## Development

```bash
# Backend (Go 1.26+)
go build ./...
go test ./...
go run . web --listen :8080   # serves the API the frontend dev server proxies to

# Frontend (Node 22+) — in a second terminal, with the backend above running:
cd ui
npm install
npm run dev             # Vite dev server on localhost:5173, proxies /api to :8080
npm run dev -- --host   # same, but reachable from other machines (Vite binds localhost only by default)
VITE_ALLOWED_HOSTS=my-machine.local npm run dev -- --host   # also needed to reach it by a LAN/mDNS name
npm run build
npm test

# Regenerate the OpenAPI spec
make openapi

# Local Docker build
docker build -t quay.io/kairos/auroraboot:local .
# Skip the Swagger stage for faster iteration:
docker build --build-arg=SWAGGER_STAGE=without-swagger -t quay.io/kairos/auroraboot:local .
```

---

## Documentation & links

- [Kairos documentation](https://kairos.io/docs/) — the underlying OS
- [Getting started with Kairos](https://kairos.io/docs/getting-started)
- [AuroraBoot reference](https://kairos.io/docs/reference/auroraboot/) — full CLI reference
- [Examples](https://kairos.io/docs/examples)
- [Community](https://kairos.io/community/)

---

## License

Apache 2.0 — see [LICENSE](LICENSE).
