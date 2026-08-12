# Kairos monorepo single-binary spike

A working proof that `immucore`, `kairos-agent`, and `kcrypt-discovery-challenger`
can share one source tree and one Go module and ship as one binary that
dispatches to a sub-tool based on `argv[0]`. Follow-up discussion belongs
on kairos-io/kairos#4301.

## Layout

```
kairos/
  sub/immucore/                       submodule, spike/monorepo-extract-root branch
  sub/kairos-agent/                   submodule, spike/monorepo-extract-root branch
  sub/kcrypt-discovery-challenger/    submodule, spike/monorepo-extract-root branch
  sub/kairos-sdk/                     submodule, pinned at v0.25.2 (main tracking)
  spike/
    go.mod                            module github.com/kairos-io/kairos/spike
    go.work                           unifies spike + the three subs
    main.go                           argv[0] dispatcher
    register_immucore.go              always linked
    register_kcrypt.go                always linked
    register_agent.go                 build tag !initramfs
```

Each sub-repo's `main.go` was reduced to a thin stub that calls into a new
`pkg/cmd` (or `pkg/discovery` for kcrypt) package. Standalone builds of each
sub-repo still work unchanged.

`sub/kairos-sdk` is included in the workspace as a sibling so all three
sub-tools resolve `github.com/kairos-io/kairos-sdk` to the same source
tree, instead of each pulling a possibly-different tagged release via the
module proxy. Bumping the sdk in a monorepo world would be one commit
touching one directory.

## Clone and build

```
git clone --recursive -b spike/monorepo-single-binary git@github.com:jimmykarily/kairos.git
cd kairos/spike

go build -o kairos .                              # full: immucore + agent + kcrypt
go build -tags initramfs -o kairos-slim .         # slim: immucore + kcrypt only
```

Symlink dispatch:

```
ln -s kairos immucore
./immucore version                                # runs the immucore sub
./kairos kairos-agent --version                   # same via explicit sub name
```

## Sizes observed on this machine

Stripped with `-trimpath -ldflags="-s -w"`, Linux amd64, Go 1.26.5:

| Binary                                          | Size  |
| ----------------------------------------------- | ----- |
| immucore standalone                             |  23M  |
| kairos-agent standalone                         |  31M  |
| kcrypt-discovery-challenger standalone          |  12M  |
| Sum of standalones                              |  66M  |
| kairos (all three subs)                         |  33M  |
| kairos-slim (immucore + kcrypt, initramfs tag)  |  25M  |

Reproduce with the commands above; add `-trimpath -ldflags="-s -w"` to strip.

## What this proves

- One Go module can host all three sub-tools with no source duplication.
- The linker keeps only symbols reachable from the main packages actually
  linked in a given build. Adding kcrypt to the initramfs variant costs
  2M over today's immucore-only footprint (25M vs 23M).
- Bundling all three on the rootfs replaces three binaries totalling 66M
  with one at 33M.

## What this does not cover

- Test suites in each sub-repo were only smoke-checked, not run in full.
- The kairos-agent CLI has ~1600 lines of command wiring inline in main.go;
  the extract moved the file wholesale into pkg/cmd and did not restructure.
- AuroraBoot and kairos-init are not in the spike. If the monorepo direction
  is chosen, they should live in the same repo as separate build targets
  (not linked into the multi-call binary).
- yip is not in the spike. It lives at github.com/mudler/yip and would need
  either its owner's agreement to move, or a fork under kairos-io.
- The challenger server (kcrypt-discovery-challenger repo root main.go) runs
  in-cluster and is out of scope for the initramfs binary. It would live in
  the monorepo as a separate build target.

## Bumping a submodule

To pick up new commits from a sub-repo's tracked branch:

```
git -C sub/immucore pull
git add sub/immucore
git commit -m "bump sub/immucore"
```

For kairos-sdk, the same command bumps every sub-tool at once, since they
all resolve through the workspace to `sub/kairos-sdk`.
