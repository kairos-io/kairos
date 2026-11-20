# Operator-backed ArtifactBuilder

## Goal

Add a second implementation of `builder.ArtifactBuilder` (defined in
`pkg/builder/builder.go`) that delegates artifact building to the
[kairos-operator](https://github.com/kairos-io/kairos-operator) running in a
Kubernetes cluster via its `OSArtifact` CRD (`api/v1alpha2`).

When AuroraBoot is started with a cluster configured (in-cluster or via a
user-supplied kubeconfig), the new implementation is wired into the web server
in place of the existing local builder (`internal/builder/auroraboot`).

Work proceeds **TDD-style**, in small, independently-mergeable steps.

## Implementation status

The bulk of the plan has shipped on branch `4055-webui-operator-builds` across
22 commits (`git log --oneline main..HEAD` on that branch). Each step listed
below carries a **[LANDED]** or **[OUTSTANDING]** tag inline in its heading.
The commits do not map one-to-one to plan subsections; several rounds of
workflow-backed code review added targeted fix commits on top of each feature
commit.

### At a glance

| Step | State | Notes |
|------|-------|-------|
| 1.1 - 1.5 (scaffold + flags) | LANDED | `4710a7f` (`ErrNotSupported` sentinel), `2fbe054` (scaffold + flags) |
| 1.6 (`/api/v1/system/builder`) | LANDED | `6a83ae8`; DTO consolidated with the wire type (see Departures) |
| 2 (kind harness) | LANDED | `38f3a9d`, refined by `9c24d17` |
| 3.1 - 3.2 (translate + secrets) | LANDED | `94e1f8f`; further hardened by `9ad7324` (grouped/flat field bugs) |
| 3.3 (Status) | LANDED | Folded into `a2dc339`; `Artifacts` still empty (retrieval deferred) |
| 3.4 (List + Cancel) | LANDED | `a2dc339`; cluster spec in `9c24d17` |
| 3.5 (log streaming) | LANDED | `215c2fb` (hoist), `3bf3e62` (streamer), `67168fb` (e2e), `71ebec3` (transient tolerance) |
| Store phase writeback | LANDED (not originally in plan) | `40c10ab` - see Departures |
| 3.6 (nightly slow-ISO e2e) | OUTSTANDING | Not started; see task list |

### Departures from the plan as written

- **Store phase writeback is NOT optional.** The plan assumed the operator
  builder could report progress through `Status` alone. In production the
  `ArtifactHandler` reads from `store.ArtifactStore` (not the builder) for
  Get/List, so an operator build sat at `Pending` in the DB forever while
  the CR advanced. `40c10ab` adds a `watchCRPhase` goroutine, spawned
  alongside `streamPodLogs`, that mirrors the CR's phase and message into
  the store row until the CR reaches `Ready`/`Error`, is deleted, or the
  build context is cancelled. `Cancel` also updates the store row to
  `BuildError`/`"cancelled"` after the CR is deleted.
- **SystemInfo / APISystemBuilder are one type**, not the domain/DTO split
  Step 1.6 originally sketched. The two structs had identical fields in the
  same package and shadowed `hardware.SystemInfo` and `redfish.SystemInfo`.
  `c8b5e8e` collapses them; the swag-tagged DTO is the sole carrier.
- **The `k8s` client on `operator.Builder` was ripped then re-added.**
  Step 1.5 landed with a full controller-runtime client construction the
  scaffold never touched (`2fbe054`). A code review flagged the dead
  wiring, so `137e25e` reverted it to a pure validator; `a2dc339` restored
  the real client alongside the code that finally used it, this time with
  both `clientgoscheme` and `buildv1alpha2` registered so future
  Get/List/Watch on `Pod`, `Job`, `Secret` cannot silently miss a kind.
- **Log streaming hoists the `LogBroadcaster` interface, not `dbLogWriter`.**
  Step 3.5 as written suggested hoisting `dbLogWriter` to a shared spot.
  In the shipped code the concrete writer stayed local to
  `internal/builder/auroraboot`; only the interface moved to
  `pkg/builder/broadcaster.go` (`215c2fb`). The operator streams via its
  own line-oriented sink that fans out to `store.AppendLog` + the shared
  `LogBroadcaster`.
- **`List` does not merge with the store.** Section 3.4 mentioned merging
  operator CRs with store records to catch orphans. That responsibility is
  the handler's, not the builder's. `operator.Builder.List` reports what
  the cluster sees; the handler already prefers `store.List` when a store
  is wired and falls back to `builder.List` otherwise. Merging remains a
  follow-up if orphan visibility becomes an operational need.
- **Cancel is idempotent on both backends now.** The local builder's
  `Cancel` used to return `"build %q not found"` when its in-memory map
  had no record of the id (routine post-restart). `aeb6a18` makes it
  return `nil` for unknown ids, matching the operator backend's
  `NotFound`-tolerates-no-op contract. That let the handler treat Cancel
  as a pure side-effect and simplified the response matrix.
- **`Delete` and `ClearFailed` forward `Cancel` to the builder.** Step 3
  did not address cluster-side cleanup of terminal-phase records. Without
  forwarding, deleting a `Ready` operator artifact wiped the DB row but
  left the OSArtifact CR and its PVC alive forever. `aeb6a18` fixes both
  paths.
- **Pre-built vs from-scratch is discriminated by more than `BaseImage`.**
  The translator initially routed `Spec.Image.Ref` on any `BaseImage` with no
  Dockerfile, which silently skipped kairos-init when the caller provided a
  raw distro image (bare `ubuntu:24.04` and similar). `preBuilt` now also
  requires `KairosVersion == ""` and `KairosInitImage == ""`; any signal
  that the caller wants build-time work drops us onto `Spec.Image.BuildOptions`
  so the operator's kairos-init flow runs. An empty `KairosVersion` in the
  from-scratch branch falls back to `"latest"`, mirroring the local backend's
  `ensureKairosified` (`internal/builder/auroraboot/builder.go:522-526`).
  Threaded via `internal/builder/operator/translate.go`; the harness pin is
  now `kairos-operator v0.1.1`, which is the first release to expose
  `BuildOptions.KairosInitImage` on the CRD.
- **`sanitizeClusterURL` strips more than userinfo.** The Step 1.6
  hardening initially only stripped `user:pass@`. A review flagged that
  a kubeconfig can also carry credentials in a query parameter
  (`?token=SECRET`) or fragment; `0104ba8` reduces the sanitizer output to
  scheme + host + path.
- **Handler Cancel matrix was reworked twice.** First to check the store
  before calling the builder (`a73213b`), then to call the builder first
  because the store-first design masked transient DB errors as 404 and
  stranded backend state when a manual DB edit removed the row (`aeb6a18`).
  The final matrix is documented on the handler itself.

### Deferred / follow-up items

- **Artifact retrieval from cluster storage** remains the open question
  from 3.3. `BuildStatus.Artifacts` is empty for the operator backend;
  `/api/v1/system/builder` reports `downloadSupported: false`; the UI
  hides download UI. Restoring parity with the local backend needs a
  design pass (nginx exporter proxy vs S3 sink vs sidecar - see the three
  options in 3.3).
- **Cross-backend field alignment.** The operator translator honours both
  the grouped `Source.*` fields and the legacy flat fields (`9ad7324`),
  auto-routes to kairos-init when the caller signals build-time intent (see
  the pre-built-discriminator note above), and threads `KairosInitImage`.
  The local backend still only reads the flat fields for the non-KairosVersion
  knobs. Same `BuildOptions` input can therefore produce different builds
  across backends when the caller uses only the grouped shape for those
  knobs. Fixing this is a local-builder refactor that is out of scope for
  the operator branch.
- **`Step 3.6`**: full-ISO e2e (real Kairos build → `Phase == Ready` → ISO
  on PVC) under a separate build tag, for nightly CI. Not yet started.
- **`auroraboot.Builder.Cancel` holds a write lock across DB I/O.** A
  preexisting concurrency wart in the local backend; a slow DB stalls
  every `Status`/`List` reader for the duration of a Cancel. Not touched
  in this branch.
- **UI badge consumption of `/api/v1/system/builder`.** The endpoint
  ships (`6a83ae8`) but the React frontend does not yet render the
  "Builds via cluster X" badge; separate frontend PR.

## Background — what already exists

- Interface: `pkg/builder/builder.go` defines `ArtifactBuilder` with
  `Build / Status / List / Cancel` and `BuildOptions`, `BuildStatus`.
- Current impl: `internal/builder/auroraboot.Builder` runs the build locally
  (Docker, `pkg/uki`, `deployer`). Selected by `--builder=local` (default) in
  the switch inside `runWeb` (`internal/cmd/web.go`).
- Operator API: `github.com/kairos-io/kairos-operator/api/v1alpha2.OSArtifact`,
  with `ImageSpec`, `ArtifactSpec`, `OSArtifactStatus.Phase` (`Pending` /
  `Building` / `Exporting` / `Ready` / `Error`).
- Operator e2e harness: `kairos-operator/test/e2e/e2e_suite_test.go` spins a kind
  cluster, builds & loads images, installs CertManager + operator. We will
  re-use this pattern, **not** the operator's exact code (different repo).
- AuroraBoot already uses Ginkgo v2 + Gomega. No new test framework needed.

## Naming

- Package: `internal/builder/operator` (sibling to `internal/builder/auroraboot`).
- Type: `operator.Builder` implementing `builder.ArtifactBuilder`.
- CLI flag(s): see Step 1.

## Step 1 — Config flags + stub implementation [LANDED]

**Goal:** wire the selection logic end-to-end with a builder that compiles,
satisfies the interface, and returns "not implemented" from every method. No
real cluster interaction yet.

### 1.1 — CLI flags on `web`

Add to `internal/cmd/web.go` `WebCMD.Flags`:

- `--builder` (string, default `local`): `local` | `operator`. Explicit so we
  never silently switch backends.
- `--kubeconfig` (string, env `KUBECONFIG`): path to kubeconfig file.
  Optional — when empty and `--builder=operator`, we try in-cluster config.
- `--builder-namespace` (string, default `default`): namespace where
  `OSArtifact` CRs are created.

Rationale for not using a config file: AuroraBoot's `web` command is
flag/env-only today. Match that.

### 1.2 — Selection logic

In `runWeb`:

```go
var artifactBuilder builder.ArtifactBuilder
switch c.String("builder") {
case "local", "":
    artifactBuilder = auroraboot.New(artifactsDir, nil, artifactStore).
        WithLogBroadcaster(wsHub.UI)
case "operator":
    cfg, err := loadKubeConfig(c.String("kubeconfig")) // in-cluster fallback
    if err != nil { return err }
    artifactBuilder, err = operator.New(operator.Config{
        RESTConfig: cfg,
        Namespace:  c.String("builder-namespace"),
        Store:      artifactStore,
    })
    if err != nil { return err }
default:
    return fmt.Errorf("--builder must be 'local' or 'operator'")
}
```

`loadKubeConfig`: try the explicit path; if empty, try `rest.InClusterConfig()`;
if that fails, fall back to `clientcmd.NewDefaultClientConfigLoadingRules()` (so
local dev with `~/.kube/config` works without flags).

### 1.3 — Stub `internal/builder/operator/builder.go`

```go
package operator

type Config struct {
    RESTConfig *rest.Config
    Namespace  string
    Store      store.ArtifactStore
}

type Builder struct { /* fields */ }

func New(cfg Config) (*Builder, error) { /* validate, build controller-runtime client */ }

func (b *Builder) Build(ctx context.Context, opts builder.BuildOptions) (*builder.BuildStatus, error) {
    return nil, errors.New("operator builder: not implemented")
}
// Status, List, Cancel: same.
```

Compile-time assertion: `var _ builder.ArtifactBuilder = (*Builder)(nil)`.

### 1.4 — go.mod additions

Required:

- `github.com/kairos-io/kairos-operator` (for `api/v1alpha2.OSArtifact` types)
- `sigs.k8s.io/controller-runtime/pkg/client` (typed client)
- `k8s.io/client-go/rest`, `k8s.io/client-go/tools/clientcmd`
- `k8s.io/apimachinery` (already pulled transitively, make it explicit)

Both repos are on `go 1.26.4` now (AuroraBoot `go.mod`, operator `go.mod`), so
no version-skew concern. We only import the operator's `api/v1alpha2` types,
not its controllers, so the transitive surface stays small.

### 1.5 — Unit tests (no cluster)

`internal/builder/operator/builder_test.go` (Ginkgo):

- `New` rejects nil REST config.
- `New` rejects empty namespace.
- All four methods return a "not implemented" error so this commit is a
  pure scaffold — easy to diff-review.

A separate `internal/cmd/web_builder_test.go` (or extend the existing test) for
the flag → constructor selection: build a fake `cli.Context`, assert that
`--builder=operator` reaches `operator.New` and `local` reaches `auroraboot.New`.
Skip if that's awkward in `urfave/cli` — selection is thin enough that the e2e
tests in Step 2 will cover it.

**Deliverable:** `--builder=operator --kubeconfig=...` boots, errors cleanly
from any build action, with all unit tests green. **No real cluster needed.**

### 1.6 — `/api/v1/system/builder` (backend introspection)

Add a small read-only endpoint so the UI can render a "Builds via cluster X"
badge and behave sensibly when download features are unavailable (see 3.3).

Response shape:

```json
{
  "backend": "local" | "operator",
  "cluster": "kind-auroraboot",          // omitted when backend=local
  "namespace": "kairos-builds",          // omitted when backend=local
  "downloadSupported": true              // false when backend=operator, until 3.3 lands
}
```

- `cluster` is derived from the REST config's `Host` (or the current-context
  name when we loaded a kubeconfig file).
- `downloadSupported=false` is a hint to the UI to hide the "Download ISO"
  button and instead show the artifact list with a message pointing the user
  at whatever retrieval mechanism the ops team configured out-of-band.

Registered next to the existing `/api/v1/artifacts` routes in
`pkg/server/server.go`. Handler lives in `pkg/handlers/system.go` (new file).
No auth beyond the existing admin bearer.

**Explicitly out of scope:** a settings page or any UI-driven way to point at
a different cluster. That would require persisting a kubeconfig (secret at
rest, RBAC preflight, credential rotation) which is more work than the whole
delegation itself. If a hosted / multi-tenant deployment mode surfaces later,
that becomes its own plan.

## Step 2 — Test environment for the operator builder [LANDED]

**Goal:** a Ginkgo suite that can run a real `OSArtifact` build against a real
kind cluster, with reusable helpers, used by every Step-3 test.

### 2.1 — Suite layout

New directory: `test/operator/` (sibling to `test/integration/` and `test/e2e/`).

```
test/operator/
  suite_test.go        # BeforeSuite: kind up + operator install. AfterSuite: teardown.
  helpers.go           # CreateArtifact, WaitForPhase, Cleanup, GetCRStatus
  builder_test.go      # (added in Step 3) actual test specs
  kind.yaml            # 1- or 2-node kind config
```

### 2.2 — `BeforeSuite` responsibilities

Lifted in spirit from `kairos-operator/test/e2e/e2e_suite_test.go`:

1. `createCluster()` — `kind create cluster --name auroraboot-builder-e2e-<ts>
   --config kind.yaml --kubeconfig <tmp>`. Tolerate pre-existing cluster via
   env var `KEEP_CLUSTER=1` for fast iteration.
2. `installOperator()` — clone-free install: `kubectl apply -k
   github.com/kairos-io/kairos-operator/config/default?ref=<pinned-tag>` (or a
   pinned manifest URL). Wait for the operator deployment rollout. Decide tag
   pinning in code review — probably pin a known-good operator release tag
   committed into this repo so CI is reproducible.
3. (Optional) install cert-manager only if the chosen operator version requires
   it. The operator's own suite installs it; we should mirror.
4. Export `KUBECONFIG` to the spawned suite so kubectl-shelling helpers work.

### 2.3 — Skip on missing prerequisites

Tests must skip cleanly (not fail) when:

- `kind` is not on `PATH`
- Docker isn't running

Use a top-level `BeforeSuite` `Skip(...)` so `go test ./...` stays green on
contributor laptops without docker/kind. The existing pattern in the repo for
test-skipping (check `test/e2e/suite_test.go` to confirm convention) should be
followed.

### 2.4 — Helpers (`helpers.go`)

Inspired by `kairos-operator/test/e2e/e2e_suite_test.go::TestClients`:

- `setupTestClients(ctx)` → controller-runtime `client.Client` keyed on the
  test kubeconfig + `v1alpha2` scheme registered.
- `createOSArtifact(name, spec) *OSArtifact`
- `waitForPhase(name, phase, timeout)` — polls `.status.phase`
- `collectDebugLogs(artifactName)` — `kubectl describe` + builder pod logs.
  Called from `AfterEach` on failure, mirroring the operator's helper.
- `cleanupArtifact(name)` — delete + wait for GC.

These mirror the operator helpers but live in our repo so we don't take a test
dependency on operator internals.

### 2.5 — Smoke spec

A single spec in `suite_test.go` itself (or a placeholder in `builder_test.go`
to be replaced in Step 3):

```go
It("the operator is installed and reconciling", func() {
    art := createOSArtifact("smoke", minimalSpec()) // pre-built image, ISO=true
    waitForPhase(art.Name, v1alpha2.Building, 2*time.Minute)
    cleanupArtifact(art.Name)
})
```

This validates the harness independently of any AuroraBoot code paths. If this
passes in CI on a fresh kind cluster, the harness is good to go.

### 2.6 — Make target

Add to `Makefile`:

```
.PHONY: test-operator-e2e
test-operator-e2e:
	go test -tags=operator_e2e ./test/operator/... -v -timeout=30m
```

Build tag so it doesn't run under `make test`. CI workflow change is out of
scope for this plan — note in PR description that a workflow needs adding once
the suite is stable.

**Deliverable:** `make test-operator-e2e` brings up kind, installs operator,
runs smoke spec green, tears down. Step 3 fills in the real tests.

## Step 3 — TDD implementation of the operator builder [3.1-3.5 LANDED, 3.6 OUTSTANDING]

Each sub-step: write the test first, watch it fail, implement, watch it pass.

### 3.1 — `Build`: translate `BuildOptions` → `OSArtifact` spec

**Tests first** (`builder_test.go`):

- **3.1.a (unit, no cluster):** `translateBuildOptions(opts)` → expected
  `OSArtifactSpec`. Table-driven, covering:
    - Pre-built image (`opts.BaseImage` set, no `Dockerfile`) → `Spec.Image.Ref
      = opts.BaseImage`, no `BuildOptions`, no `OCISpec`.
    - From-scratch Kairos build (`KairosVersion`, `Model`, `KubernetesDistro`,
      `KubernetesVersion`, `FIPS`, `TrustedBoot`) → `Spec.Image.BuildOptions`
      populated; `Spec.Image.BuildImage` defaulted from the AuroraBoot build
      ID.
    - Dockerfile-provided (`opts.Dockerfile != ""`) → `Spec.Image.OCISpec` with
      a Secret reference; we create the Secret in 3.2.
    - Outputs map: `ISO` → `Artifacts.ISO`, `CloudImage` → `Artifacts.CloudImage`,
      `Outputs.GCE` → `Artifacts.GCEImage`, `Outputs.VHD` → `Artifacts.AzureImage`,
      `Netboot`, etc. Document any AuroraBoot output type that the operator
      can't currently produce — fail loudly in `Build`, don't silently drop.
    - `CloudConfig` → Secret + `Artifacts.CloudConfigRef`.
    - `Source.Arch` → `Artifacts.Arch` with the same `amd64`/`arm64` validation
      the CRD enforces.
- **3.1.b (cluster):** `Builder.Build(ctx, opts)` creates a real `OSArtifact`
  in the test namespace; assert via `client.Get` that the CR exists with the
  expected spec. Return `BuildStatus{ID, Phase: Pending}`.

**Implementation sketch:**

```go
func (b *Builder) Build(ctx, opts) (*BuildStatus, error) {
    id := opts.ID
    if id == "" { id = uuid.NewString() }

    // Pre-create any Secrets we reference (cloud-config, OCI Dockerfile).
    if err := b.materializeSecrets(ctx, id, opts); err != nil { return nil, err }

    art := &v1alpha2.OSArtifact{
        ObjectMeta: metav1.ObjectMeta{Name: id, Namespace: b.namespace,
            Labels: map[string]string{"auroraboot.io/build-id": id}},
        Spec: translateBuildOptions(id, opts),
    }
    if err := b.k8s.Create(ctx, art); err != nil { return nil, err }
    // ... store record exactly like auroraboot.Builder.Build does ...
    return &BuildStatus{ID: id, Phase: BuildPending}, nil
}
```

We deliberately use the AuroraBoot build ID as the CR name. One-to-one mapping,
no extra ID table.

**Decision flagged for review:** the operator does not currently surface
build *logs* the way the local builder does (via `dbLogWriter` + `wsHub`). For
parity we'll need to tail the builder Pod logs and forward chunks to
`logBroadcaster` + `store.AppendLog`. **Cost is non-trivial** — propose
deferring to a 3.5 sub-step rather than blocking 3.1.

### 3.2 — Secret materialization for non-trivial inputs

Tests:

- A `BuildOptions` with `CloudConfig` set creates a Secret containing the
  config, referenced from `Spec.Artifacts.CloudConfigRef`.
- A `BuildOptions` with `Dockerfile` set creates a Secret containing the
  Dockerfile, referenced from `Spec.Image.OCISpec.Ref`.
- Secrets are owned by the OSArtifact CR (ownerReferences), so they GC when
  the CR is deleted.

Why: the operator reads cloud-config and OCI build definitions from Secrets,
not inline strings (see `OCISpec.Ref *SecretKeySelector`, `CloudConfigRef
*SecretKeySelector`). The local builder takes them inline.

### 3.3 — `Status`

Tests:

- Status of an unknown ID → not-found error.
- Status returns translation of `OSArtifactStatus.Phase` to
  `builder.BuildPending/Building/Ready/Error`. Map `Exporting` → `Building`
  (the UI only knows the four AuroraBoot phases).
- `BuildStatus.Message` mirrors `OSArtifactStatus.Message`.
- `BuildStatus.Artifacts` lists artifact file paths/URLs from the operator-
  managed PVC. **Open question for review:** the operator stores artifacts on
  a PVC inside the cluster; how does AuroraBoot's UI download them? Options:
    1. Operator-side exporter (Job) uploads to S3-compatible storage —
       AuroraBoot serves signed URLs. Out of scope for v1.
    2. AuroraBoot proxies through the API server (`kubectl cp`-style). Works
       for kind/dev but doesn't scale.
    3. Run an in-cluster sidecar that NFS/HTTP-serves the PVC. Most work,
       cleanest separation.
  **For Step 3 we keep `Artifacts` empty** when the backend is operator, and
  surface a clear UI message. Treat artifact retrieval as a follow-up project
  with its own plan.

### 3.4 — `List` and `Cancel`

- `List`: query OSArtifacts with our label selector, translate each to
  `BuildStatus`. Merge with `store.ArtifactStore` records so phantom DB
  entries (CR deleted out of band) still appear with `Error/orphaned`.
- `Cancel`: `client.Delete(ctx, art)`. Update the DB record to `Error /
  cancelled`, matching `auroraboot.Builder.Cancel`.

Tests cover happy paths plus the orphan case.

### 3.5 — Log streaming

Watch the build Pod (label selector `build.kairos.io/artifact=<id>`); on
`PodRunning` start a `corev1.Pods().GetLogs(...).Stream()` for each container;
write chunks to `dbLogWriter` (already package-internal but we may need to
hoist it to a shared place) and `logBroadcaster`. Tests assert log lines arrive
in the store after a minimal build.

This is the largest sub-step; consider splitting it into its own PR.

### 3.6 — Full e2e build (kind) [OUTSTANDING]

A real Kairos ISO build through the operator backend. This is slow (~10–15
min) so tag it `// +build operator_e2e_slow` and run only in nightly CI.

The success criterion is `BuildStatus.Phase == Ready` and the OSArtifact's
PVC contains an `.iso` file. Artifact retrieval over HTTP is **not** asserted
(see 3.3).

## Out of scope (track separately)

- Artifact retrieval / download from the cluster (see 3.3).
- Multi-tenant namespace handling (we use a single `--builder-namespace`).
- RBAC bundling — assume the kubeconfig has CRUD on `osartifacts`, `secrets`,
  `pods/log` in the target namespace. Document required permissions in the
  README change accompanying Step 1.
- Settings-page UI for pointing at an arbitrary cluster at runtime. Locked
  in as flag-only for v1 (see 1.6). Revisit only if a multi-tenant deployment
  mode emerges.
- Existing e2e / integration suite (`test/e2e/`, `test/integration/`) coverage
  of the operator backend. The new kind-based suite in Step 2 tests the
  operator builder in isolation. Whether the existing suite should ALSO carry
  a `--builder=operator` case is a follow-up: decide once Step 3 is stable,
  based on how much shared setup pays for itself vs. the reproducibility win
  of a dedicated harness.
- HA / leader-election for AuroraBoot itself.

## Review checklist before kicking off Step 1 [historical, resolved]

Retained for context; every item below was decided before Step 1 landed.

1. Confirm flag names (`--builder`, `--kubeconfig`, `--builder-namespace`).
   Shipped as sketched.
2. Confirm namespace strategy (single namespace per AuroraBoot instance).
   Shipped as sketched.
3. Confirm operator version to pin in `test/operator/`. Pinned at `v0.1.0`
   (`operatorKustomize` constant in `test/operator/suite_test.go`).
4. Confirm artifact retrieval is out of scope for v1. Confirmed;
   `downloadSupported=false` in `/api/v1/system/builder` reflects this and
   the UI hides download UI on the operator backend.
5. Confirm we're OK adding `controller-runtime` to AuroraBoot's deps. Yes;
   `sigs.k8s.io/controller-runtime`, `github.com/kairos-io/kairos-operator`,
   `k8s.io/apimachinery`, `k8s.io/client-go` are all direct requires.
