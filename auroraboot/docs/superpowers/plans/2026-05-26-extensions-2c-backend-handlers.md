# Extensions — Plan 2c of 3: ExtensionHandler + validation

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the REST surface for extensions — Create, Get, List, PATCH, Delete, GetLogs, Cancel, Download — wired against `builder.ExtensionBuilder` and the stores from Plan 2a. All validation rules from the spec land here.

**Architecture:** New `pkg/handlers/extensions.go` mirroring `artifacts.go`'s shape. `NewExtensionHandler(builder, ext store, bundle store, secureBoot store, artifactsDir)` is the constructor. Routes are wired in Plan 2e. Tests use `httptest` + `echo.New()` + the existing `fakeBuilder` pattern from `server_test.go`. Path validation, `^FROM` rejection, and bundle-blocks-delete all unit-tested through the handler.

**Tech Stack:** echo v4, ginkgo v2 / gomega.

**Repository:** `/home/mudler/_git/AuroraBoot`

**Prerequisites:** Plans 2a (data model + stores) and 2b (builder interface) merged.

---

## Reference — read these before starting

| File | What it does |
|---|---|
| `pkg/handlers/artifacts.go:31-41` | `NewArtifactHandler` — the constructor shape to mirror. |
| `pkg/handlers/artifacts.go:44-97` | `createArtifactRequest` + nested DTOs — the JSON-tag style to follow. |
| `pkg/handlers/artifacts.go:124-294` | `Create` handler — pattern for kicking off async builds. |
| `pkg/handlers/artifacts.go:319-355` | `List` and `Get`. |
| `pkg/handlers/artifacts.go:357-410` | `GetLogs` (streams from store) and `Cancel`. |
| `pkg/handlers/artifacts.go:416-454` | `Download` (path-traversal guard). |
| `pkg/handlers/artifacts.go:154-157` | The `db.key` / `db.pem` lookup against a `SecureBootKeySet.KeysDir`. |
| `pkg/handlers/artifacts_test.go` | Ginkgo + httptest pattern. `fakeBuilder` lives in `server_test.go`. |
| `pkg/auth/middleware.go:57-79` | `DownloadMiddleware` — already accepts admin password or node API key. We just attach it to the new route. |

---

## Task 1: `ExtensionHandler` scaffolding + `Create` minimal happy path

**Files:**
- Create: `pkg/handlers/extensions.go`
- Create: `pkg/handlers/extensions_test.go`
- Modify: `pkg/handlers/fakes_test.go` (add `fakeExtensionBuilder`, `fakeExtensionStore`, `fakeBundleStore`)

Ship the constructor, the create-build endpoint with the minimum validation (required fields), and a single happy-path spec. Subsequent tasks add validation and the other endpoints.

- [ ] **Step 1: Add the test fakes**

In `pkg/handlers/fakes_test.go` (create if absent — the existing `fakes_test.go` already holds shared fakes for `pkg/handlers` specs), append:

```go
import (
	"context"
	"sync"

	"github.com/kairos-io/AuroraBoot/pkg/builder"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

// --- fakeExtensionBuilder -----------------------------------------------

type fakeExtensionBuilder struct {
	lastOpts builder.ExtensionBuildOptions
	buildErr error
	cancels  []string
	mu       sync.Mutex
}

func (f *fakeExtensionBuilder) Build(_ context.Context, opts builder.ExtensionBuildOptions) (*builder.ExtensionBuildStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastOpts = opts
	if f.buildErr != nil {
		return nil, f.buildErr
	}
	return &builder.ExtensionBuildStatus{ID: opts.ID, Phase: builder.BuildPending}, nil
}
func (f *fakeExtensionBuilder) Status(_ context.Context, id string) (*builder.ExtensionBuildStatus, error) {
	return &builder.ExtensionBuildStatus{ID: id, Phase: builder.BuildReady}, nil
}
func (f *fakeExtensionBuilder) List(_ context.Context) ([]*builder.ExtensionBuildStatus, error) {
	return nil, nil
}
func (f *fakeExtensionBuilder) Cancel(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancels = append(f.cancels, id)
	return nil
}

// --- fakeExtensionStore -------------------------------------------------

type fakeExtensionStore struct {
	mu   sync.Mutex
	rows map[string]*store.ExtensionRecord
}

func newFakeExtensionStore() *fakeExtensionStore {
	return &fakeExtensionStore{rows: map[string]*store.ExtensionRecord{}}
}

func (f *fakeExtensionStore) Save(_ context.Context, r *store.ExtensionRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *r
	f.rows[r.ID] = &cp
	return nil
}
func (f *fakeExtensionStore) Get(_ context.Context, id string) (*store.ExtensionRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r, ok := f.rows[id]; ok {
		cp := *r
		return &cp, nil
	}
	return nil, errNotFound
}
func (f *fakeExtensionStore) List(context.Context) ([]store.ExtensionRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]store.ExtensionRecord, 0, len(f.rows))
	for _, r := range f.rows {
		out = append(out, *r)
	}
	return out, nil
}
func (f *fakeExtensionStore) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.rows, id)
	return nil
}
func (f *fakeExtensionStore) FindLatestReadyByName(context.Context, string, string) (*store.ExtensionRecord, error) {
	return nil, errNotFound
}
func (f *fakeExtensionStore) FindByNameAndVersion(context.Context, string, string, string) (*store.ExtensionRecord, error) {
	return nil, errNotFound
}
func (f *fakeExtensionStore) AppendLog(_ context.Context, id, chunk string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r, ok := f.rows[id]; ok {
		r.Logs += chunk
	}
	return nil
}

// --- fakeBundleStore ----------------------------------------------------

type fakeBundleStore struct {
	mu      sync.Mutex
	rowsBy  map[string][]store.ArtifactExtensionBundle // keyed by artifact id
	byName  map[string][]string                        // extension name → artifact ids
}

func newFakeBundleStore() *fakeBundleStore {
	return &fakeBundleStore{
		rowsBy: map[string][]store.ArtifactExtensionBundle{},
		byName: map[string][]string{},
	}
}

func (f *fakeBundleStore) ListForArtifact(_ context.Context, artifactID string) ([]store.ArtifactExtensionBundle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]store.ArtifactExtensionBundle(nil), f.rowsBy[artifactID]...), nil
}
func (f *fakeBundleStore) ReplaceForArtifact(_ context.Context, artifactID string, entries []store.ArtifactExtensionBundle) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	// First drop any name→artifact mappings the old rows carried.
	for _, old := range f.rowsBy[artifactID] {
		f.removeNameMapLocked(old.ExtensionName, artifactID)
	}
	f.rowsBy[artifactID] = append([]store.ArtifactExtensionBundle(nil), entries...)
	for _, e := range entries {
		f.byName[e.ExtensionName] = append(f.byName[e.ExtensionName], artifactID)
	}
	return nil
}
func (f *fakeBundleStore) ArtifactsReferencingExtension(_ context.Context, name string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.byName[name]...), nil
}
func (f *fakeBundleStore) removeNameMapLocked(name, artifactID string) {
	ids := f.byName[name]
	out := ids[:0]
	for _, id := range ids {
		if id != artifactID {
			out = append(out, id)
		}
	}
	f.byName[name] = out
}

var errNotFound = errors.New("not found")
```

(Add `"errors"` import.)

- [ ] **Step 2: Write the failing spec**

Create `pkg/handlers/extensions_test.go`:

```go
package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/builder"
	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/labstack/echo/v4"
)

var _ = Describe("ExtensionHandler.Create", func() {
	var (
		e        *echo.Echo
		fb       *fakeExtensionBuilder
		es       *fakeExtensionStore
		bs       *fakeBundleStore
		handler  *handlers.ExtensionHandler
	)

	BeforeEach(func() {
		e = echo.New()
		fb = &fakeExtensionBuilder{}
		es = newFakeExtensionStore()
		bs = newFakeBundleStore()
		handler = handlers.NewExtensionHandler(fb, es, bs, nil, "")
	})

	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/extensions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		Expect(handler.Create(c)).To(Succeed())
		return rec
	}

	It("creates a sysext build and returns 201 with a Pending status", func() {
		rec := post(`{"name":"tailscale-agent","type":"sysext","arch":"amd64","version":"v1.74.0",
			"source":{"mode":"image","baseImage":"ubuntu:24.04"}}`)
		Expect(rec.Code).To(Equal(http.StatusCreated))

		var status builder.ExtensionBuildStatus
		Expect(json.Unmarshal(rec.Body.Bytes(), &status)).To(Succeed())
		Expect(status.Phase).To(Equal(builder.BuildPending))
		Expect(status.ID).ToNot(BeEmpty())
		Expect(fb.lastOpts.Type).To(Equal("sysext"))
		Expect(fb.lastOpts.Source.Mode).To(Equal("image"))
		Expect(fb.lastOpts.Source.BaseImage).To(Equal("ubuntu:24.04"))
	})
})
```

- [ ] **Step 3: Verify it fails**

```
cd ~/_git/AuroraBoot && go test ./pkg/handlers/... -v
```

Expected: BUILD FAIL — `ExtensionHandler` undefined.

- [ ] **Step 4: Implement the scaffolding**

Create `pkg/handlers/extensions.go`:

```go
package handlers

import (
	"net/http"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/kairos-io/AuroraBoot/pkg/builder"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
)

type ExtensionHandler struct {
	builder        builder.ExtensionBuilder
	store          store.ExtensionStore
	bundles        store.ArtifactExtensionBundleStore
	secureBootKeys store.SecureBootKeySetStore
	artifactsDir   string
}

func NewExtensionHandler(
	b builder.ExtensionBuilder,
	s store.ExtensionStore,
	bs store.ArtifactExtensionBundleStore,
	sb store.SecureBootKeySetStore,
	artifactsDir string,
) *ExtensionHandler {
	return &ExtensionHandler{
		builder:        b,
		store:          s,
		bundles:        bs,
		secureBootKeys: sb,
		artifactsDir:   artifactsDir,
	}
}

// createExtensionRequest is the JSON shape POSTed to /api/v1/extensions.
// Mirror of the Extensions builder Step 1–3 wizard payload.
type createExtensionRequest struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Arch    string `json:"arch"`
	Version string `json:"version"`

	Source extensionSourceReq `json:"source"`

	SigningKeySetID string   `json:"signingKeySetId,omitempty"`
	Hierarchies     []string `json:"hierarchies,omitempty"`
	ServiceReload   bool     `json:"serviceReload,omitempty"`
}

type extensionSourceReq struct {
	Mode             string `json:"mode"`
	SourceArtifactID string `json:"artifactId,omitempty"`
	BaseImage        string `json:"baseImage,omitempty"`
	Dockerfile       string `json:"dockerfile,omitempty"`
	ExtraSteps       string `json:"extraSteps,omitempty"`
	BuildContextDir  string `json:"buildContextDir,omitempty"`
}

// Create handles POST /api/v1/extensions.
//
//	@Summary		Start an extension build
//	@Description	Kicks off an async sysext/confext build. Subscribe to /api/v1/ws/ui or poll GET /api/v1/extensions/{id}.
//	@Tags			Extensions
//	@Accept			json
//	@Produce		json
//	@Security		AdminBearer
//	@Param			body	body		createExtensionRequest	true	"Build specification"
//	@Success		201		{object}	builder.ExtensionBuildStatus
//	@Failure		400		{object}	APIError
//	@Router			/api/v1/extensions [post]
func (h *ExtensionHandler) Create(c echo.Context) error {
	var req createExtensionRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if req.Type != "sysext" && req.Type != "confext" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": `type must be "sysext" or "confext"`,
		})
	}
	if req.Name == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "name is required"})
	}

	ctx := c.Request().Context()

	// Signing key resolution (mirrors artifacts.go:149-158).
	signing := builder.ExtensionSigning{}
	if req.SigningKeySetID != "" && h.secureBootKeys != nil {
		ks, err := h.secureBootKeys.GetByID(ctx, req.SigningKeySetID)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "key set not found"})
		}
		signing.PrivateKey = filepath.Join(ks.KeysDir, "db.key")
		signing.Certificate = filepath.Join(ks.KeysDir, "db.pem")
	}

	opts := builder.ExtensionBuildOptions{
		ID:      uuid.New().String(),
		Name:    req.Name,
		Type:    req.Type,
		Arch:    req.Arch,
		Version: req.Version,
		Source: builder.ExtensionSource{
			Mode:             req.Source.Mode,
			SourceArtifactID: req.Source.SourceArtifactID,
			BaseImage:        req.Source.BaseImage,
			Dockerfile:       req.Source.Dockerfile,
			ExtraSteps:       req.Source.ExtraSteps,
			BuildContextDir:  req.Source.BuildContextDir,
		},
		Signing:       signing,
		Hierarchies:   req.Hierarchies,
		ServiceReload: req.ServiceReload,
	}

	status, err := h.builder.Build(ctx, opts)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to start build"})
	}
	return c.JSON(http.StatusCreated, status)
}
```

- [ ] **Step 5: Verify the spec passes**

```
cd ~/_git/AuroraBoot && go test ./pkg/handlers/... -ginkgo.focus="ExtensionHandler.Create" -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/handlers/extensions.go pkg/handlers/extensions_test.go pkg/handlers/fakes_test.go
git commit -m "handlers: ExtensionHandler scaffolding + Create"
```

---

## Task 2: Hierarchies validation + normalization

**Files:**
- Modify: `pkg/handlers/extensions.go` (add `validateHierarchies` + integrate into Create)
- Modify: `pkg/handlers/extensions_test.go`

Spec rules: must start with `/`, no `..`, not exactly `/` or `/usr`, length ≤ 256. Normalization: trailing-slash strip, dedup, alphabetical sort before persistence.

- [ ] **Step 1: Write the failing specs**

Append to `pkg/handlers/extensions_test.go`:

```go
var _ = Describe("ExtensionHandler.Create — hierarchies validation", func() {
	var (
		e       *echo.Echo
		fb      *fakeExtensionBuilder
		handler *handlers.ExtensionHandler
	)

	BeforeEach(func() {
		e = echo.New()
		fb = &fakeExtensionBuilder{}
		handler = handlers.NewExtensionHandler(fb, newFakeExtensionStore(), newFakeBundleStore(), nil, "")
	})

	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/extensions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		Expect(handler.Create(c)).To(Succeed())
		return rec
	}

	base := `"name":"x","type":"sysext","arch":"amd64","source":{"mode":"image","baseImage":"ubuntu:24.04"}`

	It("rejects a path without a leading slash", func() {
		rec := post(`{` + base + `,"hierarchies":["opt"]}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring("hierarchies[0]"))
	})

	It("rejects a path containing ..", func() {
		rec := post(`{` + base + `,"hierarchies":["/opt/../etc"]}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring(".."))
	})

	It("rejects exactly /usr", func() {
		rec := post(`{` + base + `,"hierarchies":["/usr"]}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring("/usr"))
	})

	It("rejects exactly /", func() {
		rec := post(`{` + base + `,"hierarchies":["/"]}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
	})

	It("rejects a path longer than 256 chars", func() {
		long := "/" + strings.Repeat("a", 256)
		rec := post(`{` + base + `,"hierarchies":["` + long + `"]}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring("256"))
	})

	It("normalizes: trims trailing slashes, dedupes, sorts alphabetically", func() {
		rec := post(`{` + base + `,"hierarchies":["/srv/","/opt","/srv","/opt/"]}`)
		Expect(rec.Code).To(Equal(http.StatusCreated))
		Expect(fb.lastOpts.Hierarchies).To(Equal([]string{"/opt", "/srv"}))
	})

	It("accepts nil hierarchies for a confext", func() {
		rec := post(`{"name":"fb","type":"confext","arch":"amd64","source":{"mode":"image","baseImage":"alpine:3"}}`)
		Expect(rec.Code).To(Equal(http.StatusCreated))
		Expect(fb.lastOpts.Hierarchies).To(BeNil())
	})
})
```

- [ ] **Step 2: Verify it fails**

```
cd ~/_git/AuroraBoot && go test ./pkg/handlers/... -ginkgo.focus="hierarchies validation" -v
```

Expected: FAIL — `Create` currently passes hierarchies through unchecked.

- [ ] **Step 3: Implement `validateHierarchies` + integrate**

In `pkg/handlers/extensions.go`, append:

```go
import (
	// existing
	"fmt"
	"sort"
)

// validateHierarchies enforces the spec rules and returns a normalized list:
// trailing slashes stripped, duplicates removed, alphabetically sorted.
// The returned list is what the builder + store should see.
func validateHierarchies(in []string) ([]string, error) {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for i, raw := range in {
		p := strings.TrimRight(raw, "/")
		if p == "" {
			return nil, fmt.Errorf("hierarchies[%d]: empty path", i)
		}
		if !strings.HasPrefix(p, "/") {
			return nil, fmt.Errorf("hierarchies[%d]: must start with /", i)
		}
		if strings.Contains(p, "..") {
			return nil, fmt.Errorf("hierarchies[%d]: must not contain ..", i)
		}
		if p == "/" || p == "/usr" {
			return nil, fmt.Errorf("hierarchies[%d]: %q is implicit and cannot be listed", i, p)
		}
		if len(p) > 256 {
			return nil, fmt.Errorf("hierarchies[%d]: exceeds 256 chars", i)
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}
```

(Add `"strings"` to the import list if not already there.)

Then in `Create`, immediately after the `req.Name == ""` check, add:

```go
	normalized, err := validateHierarchies(req.Hierarchies)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	req.Hierarchies = normalized
```

- [ ] **Step 4: Verify the specs pass**

```
cd ~/_git/AuroraBoot && go test ./pkg/handlers/... -ginkgo.focus="hierarchies validation" -v
```

Expected: PASS (7 `It` blocks).

- [ ] **Step 5: Commit**

```bash
git add pkg/handlers/extensions.go pkg/handlers/extensions_test.go
git commit -m "handlers: validate + normalize extension hierarchies"
```

---

## Task 3: `extraSteps` `^FROM` rejection + mode/source validation

**Files:**
- Modify: `pkg/handlers/extensions.go`
- Modify: `pkg/handlers/extensions_test.go`

`extraSteps` must not introduce its own `FROM` line — that would override the artifact base the operator chose. Also: enforce that each source mode has the required fields.

- [ ] **Step 1: Write the failing specs**

Append to `pkg/handlers/extensions_test.go`:

```go
var _ = Describe("ExtensionHandler.Create — source/mode validation", func() {
	var (
		e       *echo.Echo
		handler *handlers.ExtensionHandler
	)

	BeforeEach(func() {
		e = echo.New()
		handler = handlers.NewExtensionHandler(&fakeExtensionBuilder{}, newFakeExtensionStore(), newFakeBundleStore(), nil, "")
	})

	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/extensions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		Expect(handler.Create(c)).To(Succeed())
		return rec
	}

	common := `"name":"x","type":"sysext","arch":"amd64"`

	It("rejects an unsupported source.mode", func() {
		rec := post(`{` + common + `,"source":{"mode":"voodoo"}}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring("mode"))
	})

	It("requires source.baseImage for mode=image", func() {
		rec := post(`{` + common + `,"source":{"mode":"image"}}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring("baseImage"))
	})

	It("requires source.artifactId for mode=artifact", func() {
		rec := post(`{` + common + `,"source":{"mode":"artifact"}}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring("artifactId"))
	})

	It("requires source.dockerfile for mode=dockerfile", func() {
		rec := post(`{` + common + `,"source":{"mode":"dockerfile"}}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring("dockerfile"))
	})

	It("rejects extraSteps with a FROM line", func() {
		rec := post(`{` + common + `,"source":{"mode":"artifact","artifactId":"a-1","extraSteps":"FROM ubuntu:24.04\nRUN ls"}}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring("FROM"))
	})

	It("rejects extraSteps with a FROM line preceded by whitespace and case-insensitive", func() {
		rec := post(`{` + common + `,"source":{"mode":"artifact","artifactId":"a-1","extraSteps":"  from ubuntu:24.04"}}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
	})

	It("accepts extraSteps without any FROM line", func() {
		rec := post(`{` + common + `,"source":{"mode":"artifact","artifactId":"a-1","extraSteps":"RUN curl -fsSL https://tailscale.com/install.sh | sh"}}`)
		Expect(rec.Code).To(Equal(http.StatusCreated))
	})

	It("rejects unsupported arch", func() {
		rec := post(`{` + common[:strings.Index(common, "amd64")] + `i386"` + `,"source":{"mode":"image","baseImage":"ubuntu:24.04"}}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring("arch"))
	})
})
```

- [ ] **Step 2: Verify it fails**

```
cd ~/_git/AuroraBoot && go test ./pkg/handlers/... -ginkgo.focus="source/mode validation" -v
```

Expected: FAIL.

- [ ] **Step 3: Implement the validators**

In `pkg/handlers/extensions.go`, append:

```go
// validateExtensionRequest checks the type/arch/source invariants the spec
// pins. Returns the first failing error message.
func validateExtensionRequest(req createExtensionRequest) error {
	switch req.Arch {
	case "amd64", "arm64", "riscv64":
	default:
		return fmt.Errorf("arch must be amd64, arm64, or riscv64")
	}

	switch req.Source.Mode {
	case "image":
		if req.Source.BaseImage == "" {
			return fmt.Errorf("source.baseImage is required for mode=image")
		}
	case "artifact":
		if req.Source.SourceArtifactID == "" {
			return fmt.Errorf("source.artifactId is required for mode=artifact")
		}
		if err := rejectFromInExtraSteps(req.Source.ExtraSteps); err != nil {
			return err
		}
	case "dockerfile":
		if req.Source.Dockerfile == "" {
			return fmt.Errorf("source.dockerfile is required for mode=dockerfile")
		}
	default:
		return fmt.Errorf("source.mode must be artifact, image, or dockerfile")
	}
	return nil
}

// rejectFromInExtraSteps refuses lines that begin with FROM (case-insensitive,
// allowing leading whitespace). The "From artifact" mode pins the base; user
// steps must not override it.
func rejectFromInExtraSteps(extra string) error {
	for i, line := range strings.Split(extra, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if len(trimmed) >= 5 && strings.EqualFold(trimmed[:5], "FROM ") {
			return fmt.Errorf("extraSteps line %d must not start with FROM (the artifact image is the implicit FROM)", i+1)
		}
		if strings.EqualFold(trimmed, "FROM") {
			return fmt.Errorf("extraSteps line %d must not start with FROM", i+1)
		}
	}
	return nil
}
```

Then in `Create`, immediately after the `req.Name == ""` check (and before `validateHierarchies`), call:

```go
	if err := validateExtensionRequest(req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
```

- [ ] **Step 4: Verify the specs pass**

```
cd ~/_git/AuroraBoot && go test ./pkg/handlers/... -ginkgo.focus="source/mode validation" -v
```

Expected: PASS (8 `It` blocks).

- [ ] **Step 5: Commit**

```bash
git add pkg/handlers/extensions.go pkg/handlers/extensions_test.go
git commit -m "handlers: validate extension source/mode/arch + reject FROM in extraSteps"
```

---

## Task 4: Get / List / PATCH / GetLogs / Cancel

**Files:**
- Modify: `pkg/handlers/extensions.go`
- Modify: `pkg/handlers/extensions_test.go`

`Get` and `List` read from the store. `PATCH` updates only the `Name` field (mirror of `Artifacts.PATCH` shape). `GetLogs` returns plain text. `Cancel` delegates to the builder.

- [ ] **Step 1: Write the failing specs**

Append to `pkg/handlers/extensions_test.go`:

```go
var _ = Describe("ExtensionHandler — Get / List / PATCH / GetLogs / Cancel", func() {
	var (
		e       *echo.Echo
		fb      *fakeExtensionBuilder
		es      *fakeExtensionStore
		handler *handlers.ExtensionHandler
		ctx     = context.Background()
	)

	BeforeEach(func() {
		e = echo.New()
		fb = &fakeExtensionBuilder{}
		es = newFakeExtensionStore()
		handler = handlers.NewExtensionHandler(fb, es, newFakeBundleStore(), nil, "")
		_ = es.Save(ctx, &store.ExtensionRecord{ID: "e-1", Name: "ts", Type: "sysext", Phase: "Ready", Arch: "amd64", Logs: "step 1\nstep 2"})
	})

	withParam := func(method, path, id, body string) echo.Context {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues(id)
		// Stash the recorder so the spec can fetch it.
		c.Set("rec", rec)
		return c
	}

	rec := func(c echo.Context) *httptest.ResponseRecorder { return c.Get("rec").(*httptest.ResponseRecorder) }

	It("Get returns the extension record", func() {
		c := withParam(http.MethodGet, "/api/v1/extensions/e-1", "e-1", "")
		Expect(handler.Get(c)).To(Succeed())
		Expect(rec(c).Code).To(Equal(http.StatusOK))
		Expect(rec(c).Body.String()).To(ContainSubstring(`"name":"ts"`))
	})

	It("Get returns 404 when the extension does not exist", func() {
		c := withParam(http.MethodGet, "/api/v1/extensions/missing", "missing", "")
		Expect(handler.Get(c)).To(Succeed())
		Expect(rec(c).Code).To(Equal(http.StatusNotFound))
	})

	It("List returns every record", func() {
		_ = es.Save(ctx, &store.ExtensionRecord{ID: "e-2", Name: "x", Type: "confext", Phase: "Ready"})
		req := httptest.NewRequest(http.MethodGet, "/api/v1/extensions", nil)
		rr := httptest.NewRecorder()
		c := e.NewContext(req, rr)
		Expect(handler.List(c)).To(Succeed())
		Expect(rr.Code).To(Equal(http.StatusOK))
		Expect(rr.Body.String()).To(ContainSubstring("e-1"))
		Expect(rr.Body.String()).To(ContainSubstring("e-2"))
	})

	It("PATCH renames the extension", func() {
		c := withParam(http.MethodPatch, "/api/v1/extensions/e-1", "e-1", `{"name":"tailscale-renamed"}`)
		Expect(handler.Update(c)).To(Succeed())
		Expect(rec(c).Code).To(Equal(http.StatusOK))
		got, _ := es.Get(ctx, "e-1")
		Expect(got.Name).To(Equal("tailscale-renamed"))
	})

	It("PATCH rejects an empty body", func() {
		c := withParam(http.MethodPatch, "/api/v1/extensions/e-1", "e-1", `{}`)
		Expect(handler.Update(c)).To(Succeed())
		Expect(rec(c).Code).To(Equal(http.StatusBadRequest))
	})

	It("GetLogs returns the appended log buffer as text/plain", func() {
		c := withParam(http.MethodGet, "/api/v1/extensions/e-1/logs", "e-1", "")
		Expect(handler.GetLogs(c)).To(Succeed())
		Expect(rec(c).Code).To(Equal(http.StatusOK))
		Expect(rec(c).Body.String()).To(Equal("step 1\nstep 2"))
	})

	It("Cancel delegates to the builder", func() {
		c := withParam(http.MethodPost, "/api/v1/extensions/e-1/cancel", "e-1", "")
		Expect(handler.Cancel(c)).To(Succeed())
		Expect(rec(c).Code).To(Equal(http.StatusNoContent))
		Expect(fb.cancels).To(Equal([]string{"e-1"}))
	})
})
```

(Add `"context"` and the store import to the test file if not already present.)

- [ ] **Step 2: Verify it fails**

```
cd ~/_git/AuroraBoot && go test ./pkg/handlers/... -ginkgo.focus="Get / List / PATCH / GetLogs / Cancel" -v
```

Expected: BUILD FAIL.

- [ ] **Step 3: Implement the handlers**

Append to `pkg/handlers/extensions.go`:

```go
// Get handles GET /api/v1/extensions/:id.
func (h *ExtensionHandler) Get(c echo.Context) error {
	rec, err := h.store.Get(c.Request().Context(), c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
	}
	return c.JSON(http.StatusOK, rec)
}

// List handles GET /api/v1/extensions.
func (h *ExtensionHandler) List(c echo.Context) error {
	list, err := h.store.List(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "list failed"})
	}
	return c.JSON(http.StatusOK, list)
}

type extensionPatch struct {
	Name string `json:"name"`
}

// Update handles PATCH /api/v1/extensions/:id. Only `name` is mutable.
func (h *ExtensionHandler) Update(c echo.Context) error {
	var patch extensionPatch
	if err := c.Bind(&patch); err != nil || patch.Name == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "name is required"})
	}
	ctx := c.Request().Context()
	rec, err := h.store.Get(ctx, c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
	}
	rec.Name = patch.Name
	if err := h.store.Save(ctx, rec); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "save failed"})
	}
	return c.JSON(http.StatusOK, rec)
}

// GetLogs handles GET /api/v1/extensions/:id/logs.
func (h *ExtensionHandler) GetLogs(c echo.Context) error {
	rec, err := h.store.Get(c.Request().Context(), c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
	}
	return c.String(http.StatusOK, rec.Logs)
}

// Cancel handles POST /api/v1/extensions/:id/cancel.
func (h *ExtensionHandler) Cancel(c echo.Context) error {
	if err := h.builder.Cancel(c.Request().Context(), c.Param("id")); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
	}
	return c.NoContent(http.StatusNoContent)
}
```

- [ ] **Step 4: Verify the specs pass**

```
cd ~/_git/AuroraBoot && go test ./pkg/handlers/... -ginkgo.focus="Get / List / PATCH / GetLogs / Cancel" -v
```

Expected: PASS (7 `It` blocks).

- [ ] **Step 5: Commit**

```bash
git add pkg/handlers/extensions.go pkg/handlers/extensions_test.go
git commit -m "handlers: ExtensionHandler Get/List/PATCH/GetLogs/Cancel"
```

---

## Task 5: `Delete` with bundle-blocks-delete policy

**Files:**
- Modify: `pkg/handlers/extensions.go`
- Modify: `pkg/handlers/extensions_test.go`

`DELETE /api/v1/extensions/:id` enforces:
- **409 Conflict** if any bundle references the extension's `Name`.
- **204 No Content** otherwise; orphans `node_extensions` rows by design.

- [ ] **Step 1: Write the failing specs**

Append to `pkg/handlers/extensions_test.go`:

```go
var _ = Describe("ExtensionHandler.Delete — bundle-blocks-delete", func() {
	var (
		e       *echo.Echo
		es      *fakeExtensionStore
		bs      *fakeBundleStore
		handler *handlers.ExtensionHandler
		ctx     = context.Background()
	)

	BeforeEach(func() {
		e = echo.New()
		es = newFakeExtensionStore()
		bs = newFakeBundleStore()
		handler = handlers.NewExtensionHandler(&fakeExtensionBuilder{}, es, bs, nil, "")
		_ = es.Save(ctx, &store.ExtensionRecord{ID: "e-1", Name: "tailscale-agent", Type: "sysext", Phase: "Ready"})
	})

	doDelete := func(id string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/extensions/"+id, nil)
		rr := httptest.NewRecorder()
		c := e.NewContext(req, rr)
		c.SetParamNames("id")
		c.SetParamValues(id)
		Expect(handler.Delete(c)).To(Succeed())
		return rr
	}

	It("allows deletion when no bundle references the name", func() {
		rec := doDelete("e-1")
		Expect(rec.Code).To(Equal(http.StatusNoContent))
		_, err := es.Get(ctx, "e-1")
		Expect(err).To(HaveOccurred())
	})

	It("returns 409 when a bundle references the extension by name", func() {
		_ = bs.ReplaceForArtifact(ctx, "a-1", []store.ArtifactExtensionBundle{
			{ArtifactID: "a-1", ExtensionName: "tailscale-agent", ExtensionType: "sysext"},
		})
		rec := doDelete("e-1")
		Expect(rec.Code).To(Equal(http.StatusConflict))
		Expect(rec.Body.String()).To(ContainSubstring("a-1"))
		// Record must NOT be deleted.
		_, err := es.Get(ctx, "e-1")
		Expect(err).ToNot(HaveOccurred())
	})

	It("returns 404 for a missing extension", func() {
		rec := doDelete("nope")
		Expect(rec.Code).To(Equal(http.StatusNotFound))
	})
})
```

- [ ] **Step 2: Verify it fails**

```
cd ~/_git/AuroraBoot && go test ./pkg/handlers/... -ginkgo.focus="bundle-blocks-delete" -v
```

Expected: BUILD FAIL.

- [ ] **Step 3: Implement `Delete`**

Append to `pkg/handlers/extensions.go`:

```go
// Delete handles DELETE /api/v1/extensions/:id. Blocked with 409 when any
// artifact bundle references the extension by name; the operator must
// remove the bundle entry first.
func (h *ExtensionHandler) Delete(c echo.Context) error {
	ctx := c.Request().Context()
	rec, err := h.store.Get(ctx, c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
	}
	if h.bundles != nil {
		artifacts, err := h.bundles.ArtifactsReferencingExtension(ctx, rec.Name)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "bundle lookup failed"})
		}
		if len(artifacts) > 0 {
			return c.JSON(http.StatusConflict, map[string]any{
				"error":     "extension is referenced by one or more artifact bundles",
				"artifacts": artifacts,
			})
		}
	}
	if err := h.store.Delete(ctx, rec.ID); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "delete failed"})
	}
	return c.NoContent(http.StatusNoContent)
}
```

- [ ] **Step 4: Verify the specs pass**

```
cd ~/_git/AuroraBoot && go test ./pkg/handlers/... -ginkgo.focus="bundle-blocks-delete" -v
```

Expected: PASS (3 `It` blocks).

- [ ] **Step 5: Commit**

```bash
git add pkg/handlers/extensions.go pkg/handlers/extensions_test.go
git commit -m "handlers: ExtensionHandler.Delete with bundle-blocks-delete policy"
```

---

## Task 6: `Download` endpoint (raw `.raw` file)

**Files:**
- Modify: `pkg/handlers/extensions.go`
- Modify: `pkg/handlers/extensions_test.go`

`GET /api/v1/extensions/:id/download/:filename` serves the `.raw` file from `artifactsDir/extensions/<id>/<filename>`. Path traversal is rejected. Auth is handled by `DownloadMiddleware` (wired in Plan 2e), so this handler is unauthenticated by design — the middleware sits in front.

- [ ] **Step 1: Write the failing specs**

Append to `pkg/handlers/extensions_test.go`:

```go
var _ = Describe("ExtensionHandler.Download", func() {
	var (
		e       *echo.Echo
		tmp     string
		handler *handlers.ExtensionHandler
	)

	BeforeEach(func() {
		e = echo.New()
		tmp = GinkgoT().TempDir()
		// Seed a fake .raw under tmp/extensions/e-1/.
		extDir := filepath.Join(tmp, "extensions", "e-1")
		Expect(os.MkdirAll(extDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(extDir, "tailscale-agent.sysext.raw"), []byte("RAW-BYTES"), 0o644)).To(Succeed())
		handler = handlers.NewExtensionHandler(&fakeExtensionBuilder{}, newFakeExtensionStore(), newFakeBundleStore(), nil, tmp)
	})

	do := func(id, filename string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/extensions/"+id+"/download/"+filename, nil)
		rr := httptest.NewRecorder()
		c := e.NewContext(req, rr)
		c.SetParamNames("id", "filename")
		c.SetParamValues(id, filename)
		Expect(handler.Download(c)).To(Succeed())
		return rr
	}

	It("serves the .raw file", func() {
		rec := do("e-1", "tailscale-agent.sysext.raw")
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Body.String()).To(Equal("RAW-BYTES"))
	})

	It("returns 404 for a missing file", func() {
		rec := do("e-1", "missing.raw")
		Expect(rec.Code).To(Equal(http.StatusNotFound))
	})

	It("rejects path traversal in filename", func() {
		rec := do("e-1", "../../etc/passwd")
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
	})

	It("rejects path traversal in id", func() {
		rec := do("../e-1", "foo.raw")
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
	})
})
```

(Add `"os"`, `"path/filepath"` to the imports.)

- [ ] **Step 2: Verify it fails**

```
cd ~/_git/AuroraBoot && go test ./pkg/handlers/... -ginkgo.focus="ExtensionHandler.Download" -v
```

Expected: BUILD FAIL.

- [ ] **Step 3: Implement `Download`**

Append to `pkg/handlers/extensions.go`:

```go
import (
	// existing
	"os"
)

// Download handles GET /api/v1/extensions/:id/download/:filename. Auth is
// applied by DownloadMiddleware in the router (admin password OR node API
// key via Authorization header or ?token=). This handler only enforces
// path-traversal safety and streams the file.
func (h *ExtensionHandler) Download(c echo.Context) error {
	id := c.Param("id")
	filename := c.Param("filename")
	if !isSafePathSegment(id) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}
	if !isSafePathSegment(filename) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid filename"})
	}
	path := filepath.Join(h.artifactsDir, "extensions", id, filename)
	// Defence-in-depth: confirm the resolved path is still inside artifactsDir.
	resolvedDir := filepath.Clean(filepath.Join(h.artifactsDir, "extensions"))
	resolvedPath := filepath.Clean(path)
	if !strings.HasPrefix(resolvedPath, resolvedDir+string(filepath.Separator)) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid path"})
	}
	f, err := os.Open(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "open failed"})
	}
	defer f.Close()
	return c.Stream(http.StatusOK, "application/octet-stream", f)
}

// isSafePathSegment rejects empty, `..`, or `/`-containing segments.
func isSafePathSegment(s string) bool {
	if s == "" || s == "." || s == ".." {
		return false
	}
	if strings.ContainsAny(s, "/\\") {
		return false
	}
	if strings.Contains(s, "..") {
		return false
	}
	return true
}
```

- [ ] **Step 4: Verify the specs pass**

```
cd ~/_git/AuroraBoot && go test ./pkg/handlers/... -ginkgo.focus="ExtensionHandler.Download" -v
```

Expected: PASS (4 `It` blocks).

- [ ] **Step 5: Run the full handlers suite to confirm no regressions**

```
cd ~/_git/AuroraBoot && go test ./pkg/handlers/... -v
```

Expected: every spec passes, including the existing artifact specs.

- [ ] **Step 6: Commit**

```bash
git add pkg/handlers/extensions.go pkg/handlers/extensions_test.go
git commit -m "handlers: ExtensionHandler.Download with path-traversal guards"
```

---

## Self-review checks

- **Spec coverage:** `POST /extensions` with full validation (Tasks 1–3), `GET`/`List`/`PATCH`/`logs`/`cancel` (Task 4), `DELETE` with 409 on bundle reference (Task 5), `Download` for the .raw (Task 6). DownloadMiddleware wiring lives in Plan 2e.
- **Type consistency:** `createExtensionRequest.Source.Mode` matches the spec's `"artifact" | "image" | "dockerfile"`. `Hierarchies` JSON tag is plural to mirror the spec.
- **Placeholder scan:** none.

## What lands at the end of this plan

- `go test ./pkg/handlers/... -v` green (existing artifact specs untouched).
- A future router-wiring task (Plan 2e) can simply call `NewExtensionHandler(...)` and bind the routes.
- Behaviour is hermetic — no docker, no real ExtensionBuilder; specs use the `fakeExtensionBuilder` + fake stores.

## Out of scope here

- Bundle CRUD on the ArtifactHandler → Plan 2d.
- `extension_hierarchies` field on the artifact create payload + cloud-config bake → Plan 2d.
- Bundle resolution at upgrade dispatch → Plan 2d.
- Status-callback `node_extensions` writes → Plan 2e.
- `--include-path` CLI flag → Plan 2e.
- Route registration in `pkg/server/server.go` → Plan 2e.
