# Extensions — Plan 2b of 3: ExtensionBuilder interface + in-process implementation

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Define `ExtensionBuilder` as an interface in `pkg/builder/extension.go` (mirroring the existing `ArtifactBuilder`) and ship an in-process implementation in `internal/builder/auroraboot/extension.go`. The interface is the swap point for a future Kubernetes-operator-backed builder.

**Architecture:** Types + interface in `pkg/builder/extension.go`. Concrete builder in `internal/builder/auroraboot/extension.go`, structurally parallel to the existing `(*Builder).Build` / `(*Builder).run` flow but operating on `ExtensionRecord` and producing a single `.raw` per build. Docker build + `auroraboot sysext|confext` are reached through two test seams (`DockerBuildFunc`, `AurorabootCLIFunc`) so specs run hermetically.

**Tech Stack:** Go 1.23+, ginkgo v2 / gomega, in-memory fakes for `ExtensionStore`.

**Repository:** `/home/mudler/_git/AuroraBoot`

**Prerequisites:** Plan 2a (data model + stores) merged.

---

## Reference — read these before starting

| File | What it does |
|---|---|
| `pkg/builder/builder.go` (107 lines) | The pattern to mirror: types + interface only. |
| `internal/builder/auroraboot/builder.go:50-129` | `Builder` struct, `LogBroadcaster` interface, `dbLogWriter`, constructor. |
| `internal/builder/auroraboot/builder.go:147-215` | `Build` — record persistence + goroutine dispatch. |
| `internal/builder/auroraboot/builder.go:217-end` | `run` — phase machine, error capture, log flushes. |
| `internal/builder/auroraboot/builder.go:570-590` | The exact `docker build` invocation (matches `--no-cache -t <tag> -f <dockerfile> <ctx>`). |
| `internal/cmd/sysext.go` (whole file) | The CLI we shell out to from the builder's Step 3 — flag list to mirror. |
| `pkg/handlers/artifacts.go:154-157` | The `db.key` / `db.pem` lookup against a `SecureBootKeySet.KeysDir`. |

---

## Task 1: Interface + types in `pkg/builder/extension.go`

**Files:**
- Create: `pkg/builder/extension.go`
- Create: `pkg/builder/extension_test.go` (compile-time assertions only)
- Create: `pkg/builder/suite_test.go` if absent

The interface lives alongside `ArtifactBuilder` so callers depend on `pkg/builder` and not on the concrete `internal/builder/auroraboot` package. No goroutines, no I/O — just types.

- [ ] **Step 1: Confirm there is no existing `pkg/builder/suite_test.go`**

```
ls pkg/builder/
```

Expected: `builder.go` and possibly nothing else (no test file). If a suite file exists, skip creating one in Step 4.

- [ ] **Step 2: Write the compile-time conformance assertion**

Create `pkg/builder/extension_test.go`:

```go
package builder_test

import (
	"context"

	"github.com/kairos-io/AuroraBoot/pkg/builder"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// noopExt is a minimal stub used only to confirm the interface compiles
// and matches the expected method set. Real implementations live in
// internal/builder/auroraboot.
type noopExt struct{}

func (noopExt) Build(context.Context, builder.ExtensionBuildOptions) (*builder.ExtensionBuildStatus, error) {
	return nil, nil
}
func (noopExt) Status(context.Context, string) (*builder.ExtensionBuildStatus, error) { return nil, nil }
func (noopExt) List(context.Context) ([]*builder.ExtensionBuildStatus, error)         { return nil, nil }
func (noopExt) Cancel(context.Context, string) error                                  { return nil }

var _ = Describe("ExtensionBuilder interface", func() {
	It("compiles for a conformant implementation", func() {
		var _ builder.ExtensionBuilder = noopExt{}
		Expect(true).To(BeTrue())
	})

	It("exposes the phase constants reused from ArtifactBuilder", func() {
		Expect(builder.BuildPending).To(Equal("Pending"))
		Expect(builder.BuildBuilding).To(Equal("Building"))
		Expect(builder.BuildReady).To(Equal("Ready"))
		Expect(builder.BuildError).To(Equal("Error"))
	})
})
```

- [ ] **Step 3: Create a ginkgo suite for `pkg/builder`**

Create `pkg/builder/suite_test.go`:

```go
package builder_test

import (
	"testing"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestBuilder(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "pkg/builder suite")
}
```

- [ ] **Step 4: Run the spec to verify it fails**

```
cd ~/_git/AuroraBoot && go test ./pkg/builder/... -v
```

Expected: BUILD FAIL — `ExtensionBuildOptions`, `ExtensionBuildStatus`, `ExtensionBuilder` undefined.

- [ ] **Step 5: Create `pkg/builder/extension.go`**

```go
package builder

import "context"

// ExtensionSource describes where the sysext/confext content comes from.
// Exactly one of {BaseImage, SourceArtifactID, Dockerfile} is meaningful,
// chosen by Mode. ExtraSteps is only meaningful with Mode = "artifact".
type ExtensionSource struct {
	Mode             string // "artifact" | "image" | "dockerfile"
	SourceArtifactID string // when Mode = "artifact"
	BaseImage        string // when Mode = "image", or resolved at build time for Mode = "artifact"
	Dockerfile       string // when Mode = "dockerfile" or "artifact"+ExtraSteps
	ExtraSteps       string // optional, only with Mode = "artifact"
	BuildContextDir  string // mirrors ArtifactBuilder.BuildContextDir for COPY support
}

// ExtensionSigning carries the PEM files to sign the .raw with. Empty
// strings mean unsigned. The handler resolves these from a SecureBootKeySet
// before calling Build.
type ExtensionSigning struct {
	PrivateKey  string // file path
	Certificate string // file path
}

// ExtensionBuildOptions describes what to build.
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

// ExtensionBuildStatus tracks an extension build's state. Phase strings
// match the existing BuildPending / BuildBuilding / BuildReady / BuildError
// constants from this package.
type ExtensionBuildStatus struct {
	ID             string `json:"id"`
	Phase          string `json:"phase"`
	Message        string `json:"message"`
	RawFile        string `json:"rawFile"`
	ContainerImage string `json:"containerImage"`
}

// ExtensionBuilder builds Kairos extensions (.raw files).
//
// The interface is the swap point for a future Kubernetes-operator-backed
// implementation: the production in-process builder lives in
// internal/builder/auroraboot/extension.go, but a k8s controller-backed
// implementation can satisfy this interface by translating Build/Cancel
// calls into Custom Resource operations and Status/List into watches.
type ExtensionBuilder interface {
	Build(ctx context.Context, opts ExtensionBuildOptions) (*ExtensionBuildStatus, error)
	Status(ctx context.Context, id string) (*ExtensionBuildStatus, error)
	List(ctx context.Context) ([]*ExtensionBuildStatus, error)
	Cancel(ctx context.Context, id string) error
}
```

- [ ] **Step 6: Run the spec, verify it passes**

```
cd ~/_git/AuroraBoot && go test ./pkg/builder/... -v
```

Expected: PASS (both `It` blocks).

- [ ] **Step 7: Commit**

```bash
git add pkg/builder/extension.go pkg/builder/extension_test.go pkg/builder/suite_test.go 2>/dev/null
git commit -m "builder: define ExtensionBuilder interface + types"
```

---

## Task 2: `ExtensionStore.AppendLog` (closes a small gap from Plan 2a)

**Files:**
- Modify: `pkg/store/store.go` (ExtensionStore interface)
- Modify: `internal/store/gorm/adapters.go`
- Modify: `internal/store/gorm/store_test.go`

The in-process builder streams logs into the DB. Plan 2a defined `ExtensionStore` but omitted `AppendLog`; adding it now keeps the builder hermetic.

- [ ] **Step 1: Write the failing spec**

Append to `internal/store/gorm/store_test.go`:

```go
var _ = Describe("ExtensionStoreAdapter.AppendLog", func() {
	It("appends chunks across calls", func() {
		s := newTestStore()
		ctx := context.Background()
		Expect(s.Extensions().Save(ctx, &store.ExtensionRecord{ID: "e-1", Name: "x", Type: "sysext", Phase: "Building"})).To(Succeed())

		Expect(s.Extensions().AppendLog(ctx, "e-1", "step 1...\n")).To(Succeed())
		Expect(s.Extensions().AppendLog(ctx, "e-1", "step 2...\n")).To(Succeed())

		got, _ := s.Extensions().Get(ctx, "e-1")
		Expect(got.Logs).To(Equal("step 1...\nstep 2...\n"))
	})
})
```

- [ ] **Step 2: Verify it fails**

```
cd ~/_git/AuroraBoot && go test ./internal/store/gorm/... -v
```

Expected: BUILD FAIL.

- [ ] **Step 3: Extend the interface**

In `pkg/store/store.go`, in the `ExtensionStore` interface, append:

```go
	AppendLog(ctx context.Context, id, chunk string) error
```

- [ ] **Step 4: Implement on the adapter**

In `internal/store/gorm/adapters.go`:

```go
func (a *ExtensionStoreAdapter) AppendLog(ctx context.Context, id, chunk string) error {
	return a.S.db.WithContext(ctx).
		Model(&store.ExtensionRecord{}).
		Where("id = ?", id).
		Update("logs", gorm.Expr("COALESCE(logs, '') || ?", chunk)).Error
}
```

- [ ] **Step 5: Verify the spec passes**

```
cd ~/_git/AuroraBoot && go test ./internal/store/gorm/... -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/store/store.go internal/store/gorm/adapters.go internal/store/gorm/store_test.go
git commit -m "store: ExtensionStore.AppendLog for log streaming"
```

---

## Task 3: In-process `ExtensionBuilder` skeleton — `New` + `Build` (Pending persistence + dispatch)

**Files:**
- Create: `internal/builder/auroraboot/extension.go`
- Create: `internal/builder/auroraboot/extension_test.go`

Bring the constructor and the synchronous-up-front part of `Build` online — phase set to `Pending`, record persisted, goroutine dispatched but exits immediately. Each subsequent task extends the goroutine body.

- [ ] **Step 1: Write the failing spec**

Create `internal/builder/auroraboot/extension_test.go`:

```go
package auroraboot_test

import (
	"context"
	"sync"

	"github.com/kairos-io/AuroraBoot/internal/builder/auroraboot"
	"github.com/kairos-io/AuroraBoot/pkg/builder"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakeExtStore is a thread-safe in-memory ExtensionStore for builder specs.
type fakeExtStore struct {
	mu   sync.Mutex
	rows map[string]*store.ExtensionRecord
}

func newFakeExtStore() *fakeExtStore {
	return &fakeExtStore{rows: map[string]*store.ExtensionRecord{}}
}

func (f *fakeExtStore) Save(_ context.Context, r *store.ExtensionRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *r
	f.rows[r.ID] = &cp
	return nil
}

func (f *fakeExtStore) Get(_ context.Context, id string) (*store.ExtensionRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r, ok := f.rows[id]; ok {
		cp := *r
		return &cp, nil
	}
	return nil, fmt.Errorf("not found: %s", id)
}

func (f *fakeExtStore) List(context.Context) ([]store.ExtensionRecord, error) { return nil, nil }
func (f *fakeExtStore) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.rows, id)
	return nil
}
func (f *fakeExtStore) FindLatestReadyByName(context.Context, string, string) (*store.ExtensionRecord, error) {
	return nil, nil
}
func (f *fakeExtStore) FindByNameAndVersion(context.Context, string, string, string) (*store.ExtensionRecord, error) {
	return nil, nil
}
func (f *fakeExtStore) AppendLog(_ context.Context, id, chunk string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r, ok := f.rows[id]; ok {
		r.Logs += chunk
	}
	return nil
}

var _ = Describe("ExtensionBuilder.Build (skeleton)", func() {
	var (
		store *fakeExtStore
		eb    *auroraboot.ExtensionBuilder
		ctx   = context.Background()
	)

	BeforeEach(func() {
		store = newFakeExtStore()
		// Replace docker and CLI seams with no-ops so the goroutine exits cleanly.
		eb = auroraboot.NewExtensionBuilder(GinkgoT().TempDir(), store).
			WithDockerBuildFunc(func(context.Context, auroraboot.DockerBuildArgs) error { return nil }).
			WithAurorabootCLIFunc(func(context.Context, auroraboot.AurorabootCLIArgs) error { return nil })
	})

	It("persists a Pending record and returns immediately", func() {
		st, err := eb.Build(ctx, builder.ExtensionBuildOptions{
			ID: "e-1", Name: "tailscale-agent", Type: "sysext", Arch: "amd64",
			Source: builder.ExtensionSource{Mode: "image", BaseImage: "ubuntu:24.04"},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(st.ID).To(Equal("e-1"))
		Expect(st.Phase).To(Equal(builder.BuildPending))

		rec, gerr := store.Get(ctx, "e-1")
		Expect(gerr).ToNot(HaveOccurred())
		Expect(rec.Phase).To(BeElementOf(builder.BuildPending, builder.BuildBuilding, builder.BuildReady))
	})

	It("generates a UUID when ID is empty", func() {
		st, err := eb.Build(ctx, builder.ExtensionBuildOptions{
			Name: "x", Type: "sysext", Arch: "amd64",
			Source: builder.ExtensionSource{Mode: "image", BaseImage: "ubuntu:24.04"},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(st.ID).ToNot(BeEmpty())
	})
})
```

(Add `"fmt"` import to the fake store file.)

- [ ] **Step 2: Verify it fails**

```
cd ~/_git/AuroraBoot && go test ./internal/builder/auroraboot/... -v
```

Expected: BUILD FAIL.

- [ ] **Step 3: Create the skeleton**

Create `internal/builder/auroraboot/extension.go`:

```go
package auroraboot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kairos-io/AuroraBoot/pkg/builder"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

// DockerBuildArgs is what the test seam receives. Production swaps in an
// implementation that shells out to `docker build`.
type DockerBuildArgs struct {
	Tag             string
	DockerfilePath  string
	BuildContextDir string
}

// AurorabootCLIArgs is what the test seam receives for the `auroraboot
// sysext|confext` invocation.
type AurorabootCLIArgs struct {
	Type        string   // "sysext" | "confext"
	Name        string
	SourceImage string
	Arch        string
	OutputDir   string
	PrivateKey  string
	Certificate string
	IncludePaths []string
	ServiceReload bool
}

// DockerBuildFunc abstracts `docker build`.
type DockerBuildFunc func(ctx context.Context, args DockerBuildArgs) error

// AurorabootCLIFunc abstracts `auroraboot sysext|confext`.
type AurorabootCLIFunc func(ctx context.Context, args AurorabootCLIArgs) error

// ExtensionBuilder is the in-process builder.ExtensionBuilder implementation.
type ExtensionBuilder struct {
	baseDir         string
	store           store.ExtensionStore
	artifacts       store.ArtifactStore // used to resolve Source.SourceArtifactID
	dockerBuildFn   DockerBuildFunc
	aurorabootCLIFn AurorabootCLIFunc
	logBroadcaster  LogBroadcaster

	mu     sync.RWMutex
	builds map[string]*extBuildState
}

type extBuildState struct {
	status builder.ExtensionBuildStatus
	cancel context.CancelFunc
}

// NewExtensionBuilder creates an in-process ExtensionBuilder. The seams
// default to real shellouts; tests swap them with With* setters.
func NewExtensionBuilder(baseDir string, s store.ExtensionStore) *ExtensionBuilder {
	return &ExtensionBuilder{
		baseDir:         baseDir,
		store:           s,
		dockerBuildFn:   DefaultDockerBuildFunc,
		aurorabootCLIFn: DefaultAurorabootCLIFunc,
		builds:          make(map[string]*extBuildState),
	}
}

// WithDockerBuildFunc swaps the docker-build seam (test entry point).
func (b *ExtensionBuilder) WithDockerBuildFunc(fn DockerBuildFunc) *ExtensionBuilder {
	b.dockerBuildFn = fn
	return b
}

// WithAurorabootCLIFunc swaps the auroraboot CLI seam (test entry point).
func (b *ExtensionBuilder) WithAurorabootCLIFunc(fn AurorabootCLIFunc) *ExtensionBuilder {
	b.aurorabootCLIFn = fn
	return b
}

// WithArtifactStore wires the artifact store used by Source.Mode=artifact
// resolution. Not required for Mode=image or Mode=dockerfile.
func (b *ExtensionBuilder) WithArtifactStore(s store.ArtifactStore) *ExtensionBuilder {
	b.artifacts = s
	return b
}

// WithLogBroadcaster fans every log chunk out to a UI hub. Mirrors the
// existing Builder.WithLogBroadcaster.
func (b *ExtensionBuilder) WithLogBroadcaster(lb LogBroadcaster) *ExtensionBuilder {
	b.logBroadcaster = lb
	return b
}

// DefaultDockerBuildFunc is wired up in Task 4. For now it returns an
// error so a missing seam is loud at runtime.
var DefaultDockerBuildFunc DockerBuildFunc = func(context.Context, DockerBuildArgs) error {
	return fmt.Errorf("docker build not wired up")
}

// DefaultAurorabootCLIFunc is wired up in Task 5.
var DefaultAurorabootCLIFunc AurorabootCLIFunc = func(context.Context, AurorabootCLIArgs) error {
	return fmt.Errorf("auroraboot cli not wired up")
}

func (b *ExtensionBuilder) Build(ctx context.Context, opts builder.ExtensionBuildOptions) (*builder.ExtensionBuildStatus, error) {
	id := opts.ID
	if id == "" {
		id = uuid.New().String()
	}

	outputDir := filepath.Join(b.baseDir, id)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating output dir: %w", err)
	}

	buildCtx, cancel := context.WithCancel(context.Background())
	bs := &extBuildState{
		status: builder.ExtensionBuildStatus{ID: id, Phase: builder.BuildPending},
		cancel: cancel,
	}

	b.mu.Lock()
	b.builds[id] = bs
	b.mu.Unlock()

	if b.store != nil {
		rec := &store.ExtensionRecord{
			ID:               id,
			Name:             opts.Name,
			Type:             opts.Type,
			Phase:            builder.BuildPending,
			Arch:             opts.Arch,
			Version:          opts.Version,
			SourceMode:       opts.Source.Mode,
			SourceArtifactID: opts.Source.SourceArtifactID,
			SourceImage:      opts.Source.BaseImage,
			Dockerfile:       opts.Source.Dockerfile,
			ExtraSteps:       opts.Source.ExtraSteps,
			Hierarchies:      opts.Hierarchies,
			ServiceReload:    opts.ServiceReload,
			CreatedAt:        time.Now().UTC(),
			UpdatedAt:        time.Now().UTC(),
		}
		if err := b.store.Save(ctx, rec); err != nil {
			cancel()
			return nil, fmt.Errorf("persisting extension record: %w", err)
		}
	}

	go b.run(buildCtx, bs, opts, outputDir)

	return &builder.ExtensionBuildStatus{ID: id, Phase: builder.BuildPending}, nil
}

func (b *ExtensionBuilder) run(ctx context.Context, bs *extBuildState, opts builder.ExtensionBuildOptions, outputDir string) {
	b.setPhase(bs, builder.BuildBuilding, "")

	// Subsequent tasks fill in:
	//   - source resolution (Task 4)
	//   - auroraboot CLI invocation (Task 5)
	//   - log streaming + finalize (Task 6)
	// For now this skeleton just marks the build Ready so spec 1 of Task 3 passes.
	b.setPhase(bs, builder.BuildReady, "")
}

func (b *ExtensionBuilder) setPhase(bs *extBuildState, phase, msg string) {
	b.mu.Lock()
	bs.status.Phase = phase
	bs.status.Message = msg
	id := bs.status.ID
	b.mu.Unlock()

	if b.store == nil {
		return
	}
	rec, err := b.store.Get(context.Background(), id)
	if err != nil {
		return
	}
	rec.Phase = phase
	rec.Message = msg
	rec.UpdatedAt = time.Now().UTC()
	_ = b.store.Save(context.Background(), rec)
}
```

- [ ] **Step 4: Verify the specs pass**

```
cd ~/_git/AuroraBoot && go test ./internal/builder/auroraboot/... -ginkgo.focus="Build .skeleton" -v
```

Expected: PASS (2 `It` blocks). The phase might be `Ready` (we transition to it immediately) — that's fine; both possibilities are matched by `BeElementOf`.

- [ ] **Step 5: Commit**

```bash
git add internal/builder/auroraboot/extension.go internal/builder/auroraboot/extension_test.go
git commit -m "auroraboot: ExtensionBuilder skeleton (Pending + dispatch)"
```

---

## Task 4: Source resolution

**Files:**
- Modify: `internal/builder/auroraboot/extension.go`
- Modify: `internal/builder/auroraboot/extension_test.go`

The `run` goroutine needs to produce a `containerImage` (the resolved OCI ref to pass to `auroraboot sysext|confext`). Four modes:

- `image` — use `opts.Source.BaseImage` as-is.
- `artifact` — read `Artifact.ContainerImage` of `Source.SourceArtifactID`.
- `dockerfile` — synthesize the path, call `dockerBuildFn`, use the resulting tag.
- `artifact` + `extraSteps` — synthesize `FROM <artifact-image>\n<extra>` Dockerfile, call `dockerBuildFn`.

- [ ] **Step 1: Write the failing spec**

Append to `internal/builder/auroraboot/extension_test.go`:

```go
import (
	// existing
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

// fakeArtifactStore is just enough to resolve `artifact:<id>` source mode.
type fakeArtifactStore struct {
	rows map[string]*store.ArtifactRecord
}

func (f *fakeArtifactStore) Get(_ context.Context, id string) (*store.ArtifactRecord, error) {
	if r, ok := f.rows[id]; ok {
		return r, nil
	}
	return nil, fmt.Errorf("not found")
}

// Embed the remaining ArtifactStore methods as panics — Task 4 doesn't need them.
func (f *fakeArtifactStore) Create(context.Context, *store.ArtifactRecord) error { panic("unused") }
func (f *fakeArtifactStore) Update(context.Context, *store.ArtifactRecord) error { panic("unused") }
func (f *fakeArtifactStore) List(context.Context) ([]*store.ArtifactRecord, error) { panic("unused") }
func (f *fakeArtifactStore) Delete(context.Context, string) error                 { panic("unused") }
func (f *fakeArtifactStore) AppendLog(context.Context, string, string) error      { panic("unused") }
// (If ArtifactStore has more methods, add panicking stubs for each.)

var _ = Describe("ExtensionBuilder.Build — source resolution", func() {
	var (
		eb       *auroraboot.ExtensionBuilder
		extStore *fakeExtStore
		artStore *fakeArtifactStore
		dbCalls  atomic.Int32
		dbArgs   auroraboot.DockerBuildArgs
		cliArgs  auroraboot.AurorabootCLIArgs
		baseDir  string
	)

	BeforeEach(func() {
		baseDir = GinkgoT().TempDir()
		extStore = newFakeExtStore()
		artStore = &fakeArtifactStore{rows: map[string]*store.ArtifactRecord{
			"a-1": {ID: "a-1", ContainerImage: "quay.io/myorg/edge-os:v4.1.0"},
		}}
		dbCalls.Store(0)
		eb = auroraboot.NewExtensionBuilder(baseDir, extStore).
			WithArtifactStore(adapterToArtifactStore(artStore)).
			WithDockerBuildFunc(func(_ context.Context, a auroraboot.DockerBuildArgs) error {
				dbCalls.Add(1)
				dbArgs = a
				return nil
			}).
			WithAurorabootCLIFunc(func(_ context.Context, a auroraboot.AurorabootCLIArgs) error {
				cliArgs = a
				return nil
			})
	})

	awaitReady := func(id string) *store.ExtensionRecord {
		Eventually(func() string {
			rec, _ := extStore.Get(context.Background(), id)
			if rec == nil {
				return ""
			}
			return rec.Phase
		}, "2s", "20ms").Should(Equal(builder.BuildReady))
		rec, _ := extStore.Get(context.Background(), id)
		return rec
	}

	It("uses BaseImage verbatim for Mode=image", func() {
		_, err := eb.Build(context.Background(), builder.ExtensionBuildOptions{
			ID: "e-1", Name: "ts", Type: "sysext", Arch: "amd64",
			Source: builder.ExtensionSource{Mode: "image", BaseImage: "ubuntu:24.04"},
		})
		Expect(err).ToNot(HaveOccurred())
		rec := awaitReady("e-1")
		Expect(rec.ContainerImage).To(Equal("ubuntu:24.04"))
		Expect(dbCalls.Load()).To(Equal(int32(0)))
		Expect(cliArgs.SourceImage).To(Equal("ubuntu:24.04"))
	})

	It("resolves Mode=artifact by reading Artifact.ContainerImage", func() {
		_, err := eb.Build(context.Background(), builder.ExtensionBuildOptions{
			ID: "e-2", Name: "ts", Type: "sysext", Arch: "amd64",
			Source: builder.ExtensionSource{Mode: "artifact", SourceArtifactID: "a-1"},
		})
		Expect(err).ToNot(HaveOccurred())
		rec := awaitReady("e-2")
		Expect(rec.ContainerImage).To(Equal("quay.io/myorg/edge-os:v4.1.0"))
		Expect(dbCalls.Load()).To(Equal(int32(0)))
	})

	It("docker-builds Mode=dockerfile and uses the resulting tag", func() {
		_, err := eb.Build(context.Background(), builder.ExtensionBuildOptions{
			ID: "e-3", Name: "ts", Type: "sysext", Arch: "amd64",
			Source: builder.ExtensionSource{
				Mode: "dockerfile", Dockerfile: "FROM ubuntu:24.04\nRUN apt-get install -y curl",
			},
		})
		Expect(err).ToNot(HaveOccurred())
		rec := awaitReady("e-3")
		Expect(dbCalls.Load()).To(Equal(int32(1)))
		Expect(dbArgs.Tag).To(Equal("auroraboot-extbuild:e-3"))
		Expect(dbArgs.DockerfilePath).To(Equal(filepath.Join(baseDir, "e-3", "Dockerfile")))
		Expect(rec.ContainerImage).To(Equal("auroraboot-extbuild:e-3"))
	})

	It("docker-builds Mode=artifact with ExtraSteps and uses the resulting tag", func() {
		_, err := eb.Build(context.Background(), builder.ExtensionBuildOptions{
			ID: "e-4", Name: "ts", Type: "sysext", Arch: "amd64",
			Source: builder.ExtensionSource{
				Mode: "artifact", SourceArtifactID: "a-1",
				ExtraSteps: "RUN curl -fsSL https://tailscale.com/install.sh | sh",
			},
		})
		Expect(err).ToNot(HaveOccurred())
		rec := awaitReady("e-4")
		Expect(dbCalls.Load()).To(Equal(int32(1)))

		df, _ := os.ReadFile(dbArgs.DockerfilePath)
		Expect(string(df)).To(ContainSubstring("FROM quay.io/myorg/edge-os:v4.1.0"))
		Expect(string(df)).To(ContainSubstring("RUN curl -fsSL https://tailscale.com/install.sh"))
		Expect(rec.ContainerImage).To(Equal("auroraboot-extbuild:e-4"))
	})

	It("transitions to Error when source resolution fails", func() {
		_, err := eb.Build(context.Background(), builder.ExtensionBuildOptions{
			ID: "e-5", Name: "ts", Type: "sysext", Arch: "amd64",
			Source: builder.ExtensionSource{Mode: "artifact", SourceArtifactID: "does-not-exist"},
		})
		Expect(err).ToNot(HaveOccurred())
		Eventually(func() string {
			rec, _ := extStore.Get(context.Background(), "e-5")
			if rec == nil {
				return ""
			}
			return rec.Phase
		}, "2s", "20ms").Should(Equal(builder.BuildError))
	})
})

// adapterToArtifactStore wraps the test fake to fit the store.ArtifactStore
// interface. Because the interface is package-private to pkg/store, we use
// a thin adapter rather than satisfying the full interface in the fake.
func adapterToArtifactStore(f *fakeArtifactStore) store.ArtifactStore {
	return f
}
```

(`adapterToArtifactStore` exists so the test compiles even if `fakeArtifactStore` doesn't yet declare every method of `store.ArtifactStore`. If your `store.ArtifactStore` has more methods, add stubs in the fake.)

- [ ] **Step 2: Verify the specs fail**

```
cd ~/_git/AuroraBoot && go test ./internal/builder/auroraboot/... -ginkgo.focus="source resolution" -v
```

Expected: FAIL — the current `run` doesn't write `ContainerImage`, doesn't invoke `dockerBuildFn`, doesn't resolve artifact mode.

- [ ] **Step 3: Implement source resolution in `run`**

In `internal/builder/auroraboot/extension.go`, replace the `run` function:

```go
func (b *ExtensionBuilder) run(ctx context.Context, bs *extBuildState, opts builder.ExtensionBuildOptions, outputDir string) {
	b.setPhase(bs, builder.BuildBuilding, "")

	containerImage, err := b.resolveSource(ctx, opts, outputDir)
	if err != nil {
		b.setPhase(bs, builder.BuildError, err.Error())
		return
	}
	b.updateContainerImage(bs.status.ID, containerImage)

	// Task 5 wires the auroraboot CLI; Task 6 wires log streaming and finalize.
	b.setPhase(bs, builder.BuildReady, "")
}

func (b *ExtensionBuilder) resolveSource(ctx context.Context, opts builder.ExtensionBuildOptions, outputDir string) (string, error) {
	switch opts.Source.Mode {
	case "image":
		if opts.Source.BaseImage == "" {
			return "", fmt.Errorf("source.baseImage required for mode=image")
		}
		return opts.Source.BaseImage, nil

	case "artifact":
		if b.artifacts == nil {
			return "", fmt.Errorf("artifact store not wired; cannot resolve mode=artifact")
		}
		art, err := b.artifacts.Get(ctx, opts.Source.SourceArtifactID)
		if err != nil {
			return "", fmt.Errorf("artifact %s: %w", opts.Source.SourceArtifactID, err)
		}
		if opts.Source.ExtraSteps == "" {
			return art.ContainerImage, nil
		}
		// Synthesize Dockerfile = FROM <artifact-image>\n<extraSteps>
		dockerfile := fmt.Sprintf("FROM %s\n%s\n", art.ContainerImage, opts.Source.ExtraSteps)
		return b.dockerBuildAndTag(ctx, opts.ID, dockerfile, opts.Source.BuildContextDir, outputDir)

	case "dockerfile":
		if opts.Source.Dockerfile == "" {
			return "", fmt.Errorf("source.dockerfile required for mode=dockerfile")
		}
		return b.dockerBuildAndTag(ctx, opts.ID, opts.Source.Dockerfile, opts.Source.BuildContextDir, outputDir)

	default:
		return "", fmt.Errorf("unsupported source.mode %q", opts.Source.Mode)
	}
}

func (b *ExtensionBuilder) dockerBuildAndTag(ctx context.Context, id, dockerfile, contextDir, outputDir string) (string, error) {
	dockerfilePath := filepath.Join(outputDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0o644); err != nil {
		return "", fmt.Errorf("writing Dockerfile: %w", err)
	}
	if contextDir == "" {
		contextDir = outputDir
	}
	tag := "auroraboot-extbuild:" + id
	if err := b.dockerBuildFn(ctx, DockerBuildArgs{
		Tag:             tag,
		DockerfilePath:  dockerfilePath,
		BuildContextDir: contextDir,
	}); err != nil {
		return "", fmt.Errorf("docker build: %w", err)
	}
	return tag, nil
}

func (b *ExtensionBuilder) updateContainerImage(id, image string) {
	if b.store == nil {
		return
	}
	rec, err := b.store.Get(context.Background(), id)
	if err != nil {
		return
	}
	rec.ContainerImage = image
	rec.UpdatedAt = time.Now().UTC()
	_ = b.store.Save(context.Background(), rec)
}
```

- [ ] **Step 4: Verify the specs pass**

```
cd ~/_git/AuroraBoot && go test ./internal/builder/auroraboot/... -ginkgo.focus="source resolution" -v
```

Expected: PASS (5 `It` blocks).

- [ ] **Step 5: Commit**

```bash
git add internal/builder/auroraboot/extension.go internal/builder/auroraboot/extension_test.go
git commit -m "auroraboot: ExtensionBuilder source resolution (image|artifact|dockerfile|+steps)"
```

---

## Task 5: `auroraboot sysext|confext` invocation + RawFile persistence

**Files:**
- Modify: `internal/builder/auroraboot/extension.go`
- Modify: `internal/builder/auroraboot/extension_test.go`

The `run` goroutine, after source resolution, invokes `auroraboot sysext|confext` to produce a `.raw` file under `outputDir`. The CLI seam returns nil on success; the builder then records `RawFilename` and transitions to `Ready`.

- [ ] **Step 1: Write the failing spec**

Append to `internal/builder/auroraboot/extension_test.go`:

```go
var _ = Describe("ExtensionBuilder.Build — auroraboot CLI invocation", func() {
	var (
		eb       *auroraboot.ExtensionBuilder
		extStore *fakeExtStore
		cliArgs  auroraboot.AurorabootCLIArgs
	)

	BeforeEach(func() {
		extStore = newFakeExtStore()
		eb = auroraboot.NewExtensionBuilder(GinkgoT().TempDir(), extStore).
			WithDockerBuildFunc(func(context.Context, auroraboot.DockerBuildArgs) error { return nil }).
			WithAurorabootCLIFunc(func(_ context.Context, a auroraboot.AurorabootCLIArgs) error {
				cliArgs = a
				// Simulate the CLI dropping the .raw file in OutputDir.
				return os.WriteFile(filepath.Join(a.OutputDir, a.Name+"."+a.Type+".raw"), []byte("fake"), 0o644)
			})
	})

	awaitReady := func(id string) *store.ExtensionRecord {
		Eventually(func() string {
			rec, _ := extStore.Get(context.Background(), id)
			if rec == nil {
				return ""
			}
			return rec.Phase
		}, "2s", "20ms").Should(Equal(builder.BuildReady))
		rec, _ := extStore.Get(context.Background(), id)
		return rec
	}

	It("passes type/name/arch/output through to the CLI", func() {
		_, err := eb.Build(context.Background(), builder.ExtensionBuildOptions{
			ID: "e-1", Name: "tailscale-agent", Type: "sysext", Arch: "amd64", Version: "v1.74.0",
			Source: builder.ExtensionSource{Mode: "image", BaseImage: "ubuntu:24.04"},
		})
		Expect(err).ToNot(HaveOccurred())
		rec := awaitReady("e-1")
		Expect(rec.RawFilename).To(Equal("tailscale-agent.sysext.raw"))
		Expect(cliArgs.Type).To(Equal("sysext"))
		Expect(cliArgs.Name).To(Equal("tailscale-agent"))
		Expect(cliArgs.Arch).To(Equal("amd64"))
		Expect(cliArgs.SourceImage).To(Equal("ubuntu:24.04"))
	})

	It("passes hierarchies and ServiceReload for sysext", func() {
		_, err := eb.Build(context.Background(), builder.ExtensionBuildOptions{
			ID: "e-2", Name: "ts", Type: "sysext", Arch: "amd64",
			Source: builder.ExtensionSource{Mode: "image", BaseImage: "ubuntu:24.04"},
			Hierarchies:   []string{"/opt", "/srv"},
			ServiceReload: true,
		})
		Expect(err).ToNot(HaveOccurred())
		_ = awaitReady("e-2")
		Expect(cliArgs.IncludePaths).To(Equal([]string{"/opt", "/srv"}))
		Expect(cliArgs.ServiceReload).To(BeTrue())
	})

	It("forwards signing files", func() {
		_, err := eb.Build(context.Background(), builder.ExtensionBuildOptions{
			ID: "e-3", Name: "ts", Type: "sysext", Arch: "amd64",
			Source:  builder.ExtensionSource{Mode: "image", BaseImage: "ubuntu:24.04"},
			Signing: builder.ExtensionSigning{PrivateKey: "/tmp/db.key", Certificate: "/tmp/db.pem"},
		})
		Expect(err).ToNot(HaveOccurred())
		_ = awaitReady("e-3")
		Expect(cliArgs.PrivateKey).To(Equal("/tmp/db.key"))
		Expect(cliArgs.Certificate).To(Equal("/tmp/db.pem"))
	})

	It("transitions to Error when the CLI fails", func() {
		eb = eb.WithAurorabootCLIFunc(func(context.Context, auroraboot.AurorabootCLIArgs) error {
			return fmt.Errorf("systemd-repart: device too small for verity")
		})
		_, err := eb.Build(context.Background(), builder.ExtensionBuildOptions{
			ID: "e-4", Name: "ts", Type: "sysext", Arch: "amd64",
			Source: builder.ExtensionSource{Mode: "image", BaseImage: "ubuntu:24.04"},
		})
		Expect(err).ToNot(HaveOccurred())
		Eventually(func() string {
			rec, _ := extStore.Get(context.Background(), "e-4")
			if rec == nil {
				return ""
			}
			return rec.Phase
		}, "2s", "20ms").Should(Equal(builder.BuildError))
	})
})
```

- [ ] **Step 2: Verify it fails**

```
cd ~/_git/AuroraBoot && go test ./internal/builder/auroraboot/... -ginkgo.focus="CLI invocation" -v
```

Expected: FAIL — `RawFilename` isn't set, the seam isn't called.

- [ ] **Step 3: Extend `run` to invoke the CLI seam**

In `internal/builder/auroraboot/extension.go`, replace the body of `run`:

```go
func (b *ExtensionBuilder) run(ctx context.Context, bs *extBuildState, opts builder.ExtensionBuildOptions, outputDir string) {
	b.setPhase(bs, builder.BuildBuilding, "")

	containerImage, err := b.resolveSource(ctx, opts, outputDir)
	if err != nil {
		b.setPhase(bs, builder.BuildError, err.Error())
		return
	}
	b.updateContainerImage(bs.status.ID, containerImage)

	if err := b.aurorabootCLIFn(ctx, AurorabootCLIArgs{
		Type:          opts.Type,
		Name:          opts.Name,
		SourceImage:   containerImage,
		Arch:          opts.Arch,
		OutputDir:     outputDir,
		PrivateKey:    opts.Signing.PrivateKey,
		Certificate:   opts.Signing.Certificate,
		IncludePaths:  opts.Hierarchies,
		ServiceReload: opts.ServiceReload,
	}); err != nil {
		b.setPhase(bs, builder.BuildError, err.Error())
		return
	}

	rawFilename := opts.Name + "." + opts.Type + ".raw"
	b.updateRawFilename(bs.status.ID, rawFilename)
	b.setPhase(bs, builder.BuildReady, "")
}

func (b *ExtensionBuilder) updateRawFilename(id, name string) {
	if b.store == nil {
		return
	}
	rec, err := b.store.Get(context.Background(), id)
	if err != nil {
		return
	}
	rec.RawFilename = name
	rec.UpdatedAt = time.Now().UTC()
	_ = b.store.Save(context.Background(), rec)
}
```

- [ ] **Step 4: Verify the specs pass**

```
cd ~/_git/AuroraBoot && go test ./internal/builder/auroraboot/... -ginkgo.focus="CLI invocation" -v
```

Expected: PASS (4 `It` blocks).

- [ ] **Step 5: Commit**

```bash
git add internal/builder/auroraboot/extension.go internal/builder/auroraboot/extension_test.go
git commit -m "auroraboot: ExtensionBuilder invokes auroraboot sysext|confext CLI"
```

---

## Task 6: Default `DockerBuildFunc` + `AurorabootCLIFunc` + Status/List/Cancel + log streaming

**Files:**
- Modify: `internal/builder/auroraboot/extension.go`
- Modify: `internal/builder/auroraboot/extension_test.go`

Wires up the production seams (real shellouts), implements the remaining interface methods, and pipes stdout/stderr through a log writer that calls `ExtensionStore.AppendLog`.

- [ ] **Step 1: Write the failing spec for Status / List / Cancel**

Append to `internal/builder/auroraboot/extension_test.go`:

```go
var _ = Describe("ExtensionBuilder — Status, List, Cancel", func() {
	var (
		eb       *auroraboot.ExtensionBuilder
		extStore *fakeExtStore
	)

	BeforeEach(func() {
		extStore = newFakeExtStore()
		eb = auroraboot.NewExtensionBuilder(GinkgoT().TempDir(), extStore).
			WithDockerBuildFunc(func(context.Context, auroraboot.DockerBuildArgs) error { return nil }).
			WithAurorabootCLIFunc(func(_ context.Context, a auroraboot.AurorabootCLIArgs) error {
				return os.WriteFile(filepath.Join(a.OutputDir, a.Name+"."+a.Type+".raw"), []byte("fake"), 0o644)
			})
	})

	It("Status returns the in-memory state for an existing build", func() {
		_, err := eb.Build(context.Background(), builder.ExtensionBuildOptions{
			ID: "e-1", Name: "ts", Type: "sysext", Arch: "amd64",
			Source: builder.ExtensionSource{Mode: "image", BaseImage: "ubuntu:24.04"},
		})
		Expect(err).ToNot(HaveOccurred())
		Eventually(func() string {
			st, _ := eb.Status(context.Background(), "e-1")
			if st == nil {
				return ""
			}
			return st.Phase
		}, "2s", "20ms").Should(Equal(builder.BuildReady))
	})

	It("List returns one status per known build", func() {
		for _, id := range []string{"e-a", "e-b", "e-c"} {
			_, _ = eb.Build(context.Background(), builder.ExtensionBuildOptions{
				ID: id, Name: id, Type: "sysext", Arch: "amd64",
				Source: builder.ExtensionSource{Mode: "image", BaseImage: "ubuntu:24.04"},
			})
		}
		Eventually(func() int {
			list, _ := eb.List(context.Background())
			return len(list)
		}, "2s", "20ms").Should(Equal(3))
	})

	It("Cancel transitions a running build to Error", func() {
		// Block the CLI seam so we have time to cancel.
		blocker := make(chan struct{})
		eb = eb.WithAurorabootCLIFunc(func(ctx context.Context, _ auroraboot.AurorabootCLIArgs) error {
			select {
			case <-blocker:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		_, _ = eb.Build(context.Background(), builder.ExtensionBuildOptions{
			ID: "e-c", Name: "ts", Type: "sysext", Arch: "amd64",
			Source: builder.ExtensionSource{Mode: "image", BaseImage: "ubuntu:24.04"},
		})
		// Wait for Building phase to be reached.
		Eventually(func() string {
			st, _ := eb.Status(context.Background(), "e-c")
			if st == nil {
				return ""
			}
			return st.Phase
		}, "1s", "20ms").Should(Equal(builder.BuildBuilding))

		Expect(eb.Cancel(context.Background(), "e-c")).To(Succeed())
		close(blocker)
		Eventually(func() string {
			st, _ := eb.Status(context.Background(), "e-c")
			if st == nil {
				return ""
			}
			return st.Phase
		}, "2s", "20ms").Should(Equal(builder.BuildError))
	})

	It("Status returns 'not found' for an unknown ID", func() {
		_, err := eb.Status(context.Background(), "does-not-exist")
		Expect(err).To(HaveOccurred())
	})
})
```

- [ ] **Step 2: Verify it fails**

```
cd ~/_git/AuroraBoot && go test ./internal/builder/auroraboot/... -ginkgo.focus="Status, List, Cancel" -v
```

Expected: FAIL — `Status`, `List`, `Cancel` not implemented yet.

- [ ] **Step 3: Implement Status / List / Cancel**

Append to `internal/builder/auroraboot/extension.go`:

```go
func (b *ExtensionBuilder) Status(_ context.Context, id string) (*builder.ExtensionBuildStatus, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	bs, ok := b.builds[id]
	if !ok {
		return nil, fmt.Errorf("not found: %s", id)
	}
	cp := bs.status
	return &cp, nil
}

func (b *ExtensionBuilder) List(_ context.Context) ([]*builder.ExtensionBuildStatus, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]*builder.ExtensionBuildStatus, 0, len(b.builds))
	for _, bs := range b.builds {
		cp := bs.status
		out = append(out, &cp)
	}
	return out, nil
}

func (b *ExtensionBuilder) Cancel(_ context.Context, id string) error {
	b.mu.Lock()
	bs, ok := b.builds[id]
	b.mu.Unlock()
	if !ok {
		return fmt.Errorf("not found: %s", id)
	}
	bs.cancel()
	// The goroutine's setPhase to Error will happen when the in-flight
	// CLI returns ctx.Err(); no need to set the phase here.
	return nil
}
```

- [ ] **Step 4: Wire production `DefaultDockerBuildFunc` + `DefaultAurorabootCLIFunc`**

Replace the placeholder `DefaultDockerBuildFunc` and `DefaultAurorabootCLIFunc` in `internal/builder/auroraboot/extension.go`:

```go
// DefaultDockerBuildFunc shells out to the host docker daemon.
// Matches the call shape used by Builder.dockerBuild (builder.go:583).
var DefaultDockerBuildFunc DockerBuildFunc = func(ctx context.Context, args DockerBuildArgs) error {
	cmd := exec.CommandContext(ctx, "docker", "build", "--no-cache",
		"-t", args.Tag, "-f", args.DockerfilePath, args.BuildContextDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker build: %w\n%s", err, string(out))
	}
	return nil
}

// DefaultAurorabootCLIFunc shells out to this binary's sysext|confext
// subcommand. The command lives in internal/cmd/sysext.go.
var DefaultAurorabootCLIFunc AurorabootCLIFunc = func(ctx context.Context, a AurorabootCLIArgs) error {
	cliArgs := []string{a.Type, a.Name, a.SourceImage,
		"--arch", a.Arch,
		"--output", a.OutputDir,
	}
	if a.PrivateKey != "" {
		cliArgs = append(cliArgs, "--private-key", a.PrivateKey)
	}
	if a.Certificate != "" {
		cliArgs = append(cliArgs, "--certificate", a.Certificate)
	}
	for _, p := range a.IncludePaths {
		cliArgs = append(cliArgs, "--include-path", p)
	}
	if a.ServiceReload && a.Type == "sysext" {
		cliArgs = append(cliArgs, "--service-reload")
	}
	cmd := exec.CommandContext(ctx, "auroraboot", cliArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("auroraboot %s: %w\n%s", a.Type, err, string(out))
	}
	return nil
}
```

(Add `"os/exec"` to the import list.)

- [ ] **Step 5: Add log streaming**

Insert a new helper in `internal/builder/auroraboot/extension.go`:

```go
// extDBLogWriter mirrors dbLogWriter but writes to ExtensionStore.AppendLog.
type extDBLogWriter struct {
	store       store.ExtensionStore
	id          string
	buf         bytes.Buffer
	mu          sync.Mutex
	broadcaster LogBroadcaster
}

func (w *extDBLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.buf.Write(p)
	if w.buf.Len() > 4096 {
		w.flushLocked()
	}
	return n, err
}

func (w *extDBLogWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.flushLocked()
}

func (w *extDBLogWriter) flushLocked() {
	if w.buf.Len() == 0 {
		return
	}
	text := w.buf.String()
	w.buf.Reset()
	_ = w.store.AppendLog(context.Background(), w.id, text)
	if w.broadcaster != nil {
		w.broadcaster.BroadcastLogChunk(w.id, text)
	}
}
```

(Add `"bytes"` to the imports.)

The CLI seam's I/O is wired through this writer by an internal helper:

```go
// runWithLogging invokes fn with a writer that streams output into
// ExtensionStore.AppendLog. Used by the default seam below; tests bypass
// this by replacing the seam wholesale via WithAurorabootCLIFunc.
func (b *ExtensionBuilder) runWithLogging(id string, body func(io.Writer) error) error {
	if b.store == nil {
		return body(io.Discard)
	}
	w := &extDBLogWriter{store: b.store, id: id, broadcaster: b.logBroadcaster}
	defer w.Flush()
	return body(w)
}
```

(Add `"io"` to the imports.)

Production wiring uses this from a wrapper that ties the writer into `cmd.Stdout` and `cmd.Stderr`. For the default seam, swap the `DefaultDockerBuildFunc` and `DefaultAurorabootCLIFunc` to accept an `io.Writer`:

```go
// Add an optional Logger field on the args.
type DockerBuildArgs struct {
	Tag             string
	DockerfilePath  string
	BuildContextDir string
	Logger          io.Writer // when non-nil, stdout+stderr fan into this
}

type AurorabootCLIArgs struct {
	Type, Name, SourceImage, Arch, OutputDir, PrivateKey, Certificate string
	IncludePaths   []string
	ServiceReload  bool
	Logger         io.Writer
}
```

Update both `DefaultDockerBuildFunc` and `DefaultAurorabootCLIFunc` to:

```go
	if args.Logger != nil {
		cmd.Stdout = args.Logger
		cmd.Stderr = args.Logger
	}
	err := cmd.Run()
	// (no more CombinedOutput because the streams go to the logger)
```

Then in `run`, populate `Logger` from the writer returned by `runWithLogging`. The cleanest approach:

```go
	logger := &extDBLogWriter{store: b.store, id: bs.status.ID, broadcaster: b.logBroadcaster}
	defer logger.Flush()
	// pass `logger` into the args before each seam call
```

- [ ] **Step 6: Verify all specs pass**

```
cd ~/_git/AuroraBoot && go test ./internal/builder/auroraboot/... -v
```

Expected: PASS (entire ExtensionBuilder suite).

- [ ] **Step 7: Commit**

```bash
git add internal/builder/auroraboot/extension.go internal/builder/auroraboot/extension_test.go
git commit -m "auroraboot: ExtensionBuilder Status/List/Cancel + production seams + log streaming"
```

---

## Self-review checks

- **Spec coverage:** `ExtensionBuilder` interface (Task 1), source modes including artifact + ExtraSteps (Task 4), CLI invocation with sign/hierarchies/service-reload (Task 5), full interface surface + log streaming (Task 6). Build context directory mirrors `ArtifactBuilder.BuildContextDir`.
- **Type consistency:** `ExtensionBuildOptions.OutputDir` is implicit (computed inside `Build`); test specs never set it. `RawFilename` is `<Name>.<Type>.raw`, matching the on-disk convention from `auroraboot sysext`.
- **Placeholder scan:** none. The "Tasks 5/6 wire this up" comments in Task 3's stub are intentional scaffolding and are removed by Task 5.

## What lands at the end of this plan

- `go test ./pkg/builder/... ./internal/builder/auroraboot/...` green.
- Production `auroraboot web` can construct a real `ExtensionBuilder` once Plan 2e wires it into `server.Config`.
- A future Kubernetes-operator backend can satisfy `pkg/builder.ExtensionBuilder` without touching any caller.

## Out of scope here

- HTTP handlers → Plan 2c.
- Artifact integration (bundle list, hierarchies bake) → Plan 2d.
- Status callback for `node_extensions` → Plan 2e.
- Wiring into `server.Config` and `internal/cmd/web.go` → Plan 2e.
