> ## This repository has been archived pending migration into kairos-io/kairos
>
> provider-kairos is being absorbed into the
> [kairos-io/kairos](https://github.com/kairos-io/kairos) monorepo as
> part of the plan tracked in
> [kairos-io/kairos#4301](https://github.com/kairos-io/kairos/issues/4301).
>
> Every existing release stays published and downloadable from this
> repository's releases page, so anything that pins a specific
> provider-kairos version, including the binary that kairos-init
> fetches when building an image, keeps working. Existing Go module
> tags also stay resolvable via the Go module proxy.
>
> New development happens at
> [github.com/kairos-io/kairos/provider](https://github.com/kairos-io/kairos/tree/master/provider).
> If that link returns 404, the subtree import is not yet complete;
> see the tracking issue above for the current state and timeline.
>
> **If you only run Kairos "standard" images, nothing changes.** The
> provider binary is still installed at
> `/system/providers/agent-provider-kairos` and still offers the same
> commands. It is simply built out of the monorepo from now on.
>
> **There are no imports to update.** Unlike the other repositories
> absorbed into the monorepo, this one never exposed an importable
> package: every package here is either a `main` package or lives
> under `internal/`. If you depend on provider-kairos, you depend on
> the binary and not on the code, so no import rewrite is needed. To
> build the binary from source after the move, clone the monorepo and
> run `go build ./provider`.
>
> **Open PRs on this repository will not be merged here.** The
> branches remain reachable via `git fetch` from the archived repo, so
> nothing is lost. To carry unfinished work forward, fetch the branch,
> `git format-patch`, and `git am --directory=provider` against the
> monorepo.
>
> **On backports to older lines.** The default answer is: consume the
> latest release. We will only unarchive this repository for fixes we
> judge important enough (typically security fixes or breakage with no
> reasonable workaround). Convenience backports and feature backports
> do not qualify. To request one, open an issue on
> [kairos-io/kairos](https://github.com/kairos-io/kairos/issues)
> describing the provider-kairos version, the fix, and why moving to
> the latest release is not viable. If we agree the fix meets the bar,
> we will unarchive temporarily to land it and cut a new patch tag.

<h1 align="center">
  <br>
     <img width="184" alt="kairos-white-column 5bc2fe34" src="https://user-images.githubusercontent.com/2420543/193010398-72d4ba6e-7efe-4c2e-b7ba-d3a826a55b7d.png"><br>
    Kairos standard provider
<br>
</h1>

<h3 align="center">Kairos standard provider</h3>
<p align="center">
  <a href="https://opensource.org/licenses/">
    <img src="https://img.shields.io/badge/licence-APL2-brightgreen"
         alt="license">
  </a>
  <a href="https://github.com/kairos-io/provider-kairos/issues"><img src="https://img.shields.io/github/issues/kairos-io/provider-kairos"></a>
  <a href="https://kairos.io/docs/" target=_blank> <img src="https://img.shields.io/badge/Documentation-blue"
         alt="docs"></a>
  <img src="https://img.shields.io/badge/made%20with-Go-blue">
  <img src="https://goreportcard.com/badge/github.com/kairos-io/provider-kairos" alt="go report card" />
</p>


With Kairos you can build immutable, bootable Kubernetes and OS images for your edge devices as easily as writing a Dockerfile. Optional P2P mesh with distributed ledger automates node bootstrapping and coordination. Updating nodes is as easy as CI/CD: push a new image to your container registry and let secure, risk-free A/B atomic upgrades do the rest.


<table>
<tr>
<th align="center">
<img width="640" height="1px">
<p> 
<small>
Documentation
</small>
</p>
</th>
<th align="center">
<img width="640" height="1">
<p> 
<small>
Contribute
</small>
</p>
</th>
</tr>
<tr>
<td>

 📚 [Getting started with Kairos](https://kairos.io/docs/getting-started) <br> :bulb: [Examples](https://kairos.io/docs/examples) <br> :movie_camera: [Video](https://kairos.io/docs/media/) <br> :open_hands:[Engage with the Community](https://kairos.io/community/)
  
</td>
<td>
  
🙌[ CONTRIBUTING.md ]( https://github.com/kairos-io/kairos/blob/master/CONTRIBUTING.md ) <br> :raising_hand: [ GOVERNANCE ]( https://github.com/kairos-io/kairos/blob/master/GOVERNANCE.md ) <br>:construction_worker:[Code of conduct](https://github.com/kairos-io/kairos/blob/master/CODE_OF_CONDUCT.md) 
  
</td>
</tr>
</table>

## Provider kairos

This repository hosts the code for provider binary used in Kairos "standard" images which offer full-mesh support.
full-mesh support currently is available only with k3s, and the provider follows strictly k3s releases.

> [!NOTE] 
> The provider-kairos release pipelines have been merged with the kairos ones from version `2.4.0` onward. All image artifacts are released from the kairos repository, both core images and standard images (those with the provider).

To use Kairos with mesh support, download the bootable medium form the [kairos releases](https://github.com/kairos-io/kairos/releases).

Follow up the examples in our documentation on how to get started:
- https://kairos.io/docs/examples/single-node/
- https://kairos.io/docs/examples/multi-node/
- https://kairos.io/docs/examples/multi-node-p2p-ha-kubevip/

## Upgrades

Upgrading can be done either via Kubernetes or manually with `kairos-agent upgrade --image <image>`, or you can list available versions with `kairos-agent upgrade list-releases`. 

Container images available for upgrades are pushed to quay, you can check out the [image matrix in our documentation](https://kairos.io/docs/reference/image_matrix/).
