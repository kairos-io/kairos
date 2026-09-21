# provider

> The Kairos p2p provider lives in the Kairos monorepo. See the
> [root README](../README.md) for the full repository layout. Import
> path: `github.com/kairos-io/kairos/v4/provider`.

The provider behind Kairos **standard** images: the p2p mesh that lets nodes
find each other, agree on roles, and bootstrap a Kubernetes cluster without a
control plane being named up front. Mesh support currently covers k3s only, and
tracks k3s releases.

Core images do not ship this binary at all. Everything below applies to
standard images.

## The two things this binary is

It is built once and installed at `/system/providers/agent-provider-kairos`,
where it answers to two different callers:

- **An agent plugin.** `kairos-agent` discovers executables named
  `agent-provider-*` under `/system/providers` and talks to them over the
  go-pluggable event protocol (`../sdk/bus`). When `argv[1]` is a known event
  name such as `agent.bootstrap` or `init.provider.info`, the binary reads a
  JSON payload on stdin and answers on stdout. This is the whole of the
  provider contract, and it is what makes other providers (`provider-rke2`,
  `provider-k3s`, ...) interchangeable with this one.
- **A CLI**, when `argv[1]` is anything else. `get-kubeconfig`, `role`,
  `bridge`, `register`, `generate-token` and friends. Nothing in the provider
  contract requires this half; it is specific to this provider.

## Reaching the CLI

`/system/providers` is not on `PATH`. The multi-call `kairos` binary finds the
installed provider and hands your arguments to it:

```sh
kairos provider get-kubeconfig
kairos provider role list
kairos provider --help
```

On a core image, or anywhere else with no provider installed, that prints an
error saying so. If more than one provider is ever installed, it refuses rather
than guessing which one you meant; running several side by side is not
supported yet (kairos-io/kairos#3926).

These commands were previously a separate `kairosctl` binary, downloaded onto a
workstation, plus a `/usr/bin/kairos` that was this provider rather than the
multi-call dispatcher. Both are gone; `kairos provider <cmd>` is the one way in
(kairos-io/kairos#4393).

## Development

Standalone build from the repo root:

```sh
go build ./provider
```

Or as part of the whole monorepo build pipeline:

```sh
make provider-kairos   # produces dist/linux-<arch>/provider-kairos
make binaries          # builds everything: kairos, kcrypt-challenger, kairos-installer, provider-kairos, kairos-init
```

`kairos-init` embeds the built binary into standard images at
`/system/providers/agent-provider-kairos`, in both the default and FIPS
variants. It reports the same version as every other binary in the tree; see
`../internal/version`.

## Using it

To run Kairos with mesh support, download a standard bootable medium from the
[Kairos releases](https://github.com/kairos-io/kairos/releases), then follow:

- https://kairos.io/docs/examples/single-node/
- https://kairos.io/docs/examples/multi-node/
- https://kairos.io/docs/examples/multi-node-p2p-ha-kubevip/

Upgrades go through Kubernetes or `kairos-agent upgrade --image <image>`;
`kairos-agent upgrade list-releases` lists what is available. See the
[image matrix](https://kairos.io/docs/reference/image_matrix/) for the
published images.
