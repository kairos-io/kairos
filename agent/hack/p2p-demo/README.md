# p2p image source demo (EXPERIMENTAL)

A seed, a rogue seeder, eight hostile peers and five leechers on a docker network that has **no
gateway and no internet** (`internal: true`). No keys anywhere. The seed makes a
256 MiB fake `active.img` and a 16 MiB `passive.img` and runs
`kairos agent p2p seed` with no per-image arguments: it prints the source, with the
pin (`?pin=`, the hash of the image's manifest), to use on the other nodes. The rogue seeder announces itself and
serves *another* image.

| node | source | what it shows |
|---|---|---|
| leech1 | `p2p://active?pin=<hex>` | mDNS discovery plus the pin; the rogue serves another image and is ignored |
| leech2 | `p2p://seed,leech1,leech3,rogue/active?pin=<hex>` | static list plus the pin |
| leech3 | the line the seed printed, as it is | the "update one node, copy the printed source" workflow |
| leech4 | `p2p://active` | no pin: **refused** before any peer is contacted; then with `--insecure`, which logs a loud warning |
| leech5 | `p2p://stall,lie,fake1..fake6,seed/active?pin=<hex>` | the hostile case, eight hostile addresses listed before the seed: one answers the manifest and then never a chunk, one claims the image hash with a 12 GiB size, six declare the **true** size and image hash and serve chunks that match their own manifest. None of their manifests is the pinned one, so every one is discarded after a tiny manifest download and not a single chunk is fetched from them (the fakes' logs show no chunk request). Bounded time, no hang |

`?pin=` is the form to use. The weaker `?sha256=<image hash>` (optionally `&size=<bytes>`)
is also accepted, but it only authenticates the image at the end: a peer that declares
the true size and hash can make a node download the whole image before the check
fails, so fakes like the six above cost it bandwidth (bounded, then it gives up).

No `--bind` or `--allow-cidr` anywhere: the bind address is the interface
address and the allowlist defaults to the listed peers (static) or the local
subnet (mDNS). The seed is rate limited to 12 MiB/s on purpose, so the finished
leechers visibly take part of the load of the later ones: a node that completed
and verified the image offers it to the others.

```sh
./agent/hack/p2p-demo/run.sh      # needs docker + compose plugin, ~3 minutes
```

It builds the agent, proves the network is isolated, starts the nodes and prints
each node's log: discovery, which manifests were ignored and why, what the
download was checked against, and which peer served how many bytes and chunks.
All nodes run as uid 1000. A real captured run is in `sample-output.txt`.

Notes: in real life the printed source (it contains the pin) is copied into the
cloud-config or the command of the other nodes; here a shared volume stands in
for that. Leech4's run with `--insecure` shows what the opt out means: nothing
stops it from picking the rogue's manifest if that one is listed first. mDNS only
works inside one L2 segment, and the discovery is unauthenticated.
