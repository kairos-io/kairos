# Extensions — Plan 2d of 3: Artifact integration (bundles, hierarchies bake, resolve)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the loop on the ArtifactBuilder ↔ Extensions relationship: artifacts can carry bundled extensions, can declare additional sysext/confext hierarchies that get baked into the image's cloud-config, and the upgrade dialog can resolve a bundle into the concrete `extensions` arg the phonehome `upgrade` command will carry.

**Architecture:** Three new REST endpoints on `ArtifactHandler` (`GET`/`PUT` bundle-extensions, `POST` bundle-resolve). Two new fields on `createArtifactRequest` (`extensionHierarchies`, `bundledExtensions`). One new section appended to the existing `buildCloudConfig` document. All wiring runs through the stores defined in Plan 2a; the bundle store and extension store get injected into `ArtifactHandler` via constructor changes.

**Tech Stack:** echo v4, gorm, ginkgo v2 / gomega, `gopkg.in/yaml.v3` (already in use by `buildCloudConfig`).

**Repository:** `/home/mudler/_git/AuroraBoot`

**Prerequisites:** Plans 2a (data model + stores), 2b (ExtensionBuilder interface), 2c (ExtensionHandler) merged.

---

## Reference — read these before starting

| File | What it does |
|---|---|
| `pkg/handlers/artifacts.go:20-41` | `ArtifactHandler` struct + `NewArtifactHandler` — extended in Task 1. |
| `pkg/handlers/artifacts.go:44-97` | `createArtifactRequest` DTO — extended in Tasks 3–4. |
| `pkg/handlers/artifacts.go:124-294` | `Create` handler — site of the cloud-config bake (Task 5) and bundle persistence (Task 4). |
| `pkg/handlers/artifacts.go:670-end` | `buildCloudConfig` — where the SYSTEMD_SYSEXT_HIERARCHIES drop-in is appended. |
| `pkg/store/store.go` (post-2a) | `ExtensionHierarchies`, `ArtifactExtensionBundle`, `ExtensionStore`, `ArtifactExtensionBundleStore`. |
| `pkg/auth/middleware.go:57-79` | `DownloadMiddleware` — referenced when computing the `source` URL the agent uses to fetch the .raw (Task 6). |

---

## Task 1: Extend `ArtifactHandler` with the bundle store + `GET` bundle-extensions

**Files:**
- Modify: `pkg/handlers/artifacts.go` (constructor + handler struct)
- Modify: `pkg/handlers/artifacts_test.go` (constructor call sites)
- Modify: `pkg/server/server.go` (constructor call site)
- Modify: `pkg/handlers/artifacts_test.go` (add bundle spec)

Constructor gets two new args (`store.ExtensionStore`, `store.ArtifactExtensionBundleStore`) so all subsequent tasks can read/write through `h.bundles` and `h.extensions`. `GET /api/v1/artifacts/:id/bundle-extensions` returns the raw stored entries.

- [ ] **Step 1: Write the failing spec**

Append to `pkg/handlers/artifacts_test.go`:

```go
var _ = Describe("ArtifactHandler — bundle endpoints", func() {
	var (
		e       *echo.Echo
		fb      *fakeBuilder
		es      *fakeExtensionStore
		bs      *fakeBundleStore
		handler *handlers.ArtifactHandler
		ctx     = context.Background()
	)

	BeforeEach(func() {
		e = echo.New()
		fb = &fakeBuilder{}
		es = newFakeExtensionStore()
		bs = newFakeBundleStore()
		handler = handlers.NewArtifactHandler(fb, nil, nil, nil, es, bs, "", "reg-token", "http://localhost:8080")
	})

	withParam := func(method, path, id, body string) (echo.Context, *httptest.ResponseRecorder) {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues(id)
		return c, rec
	}

	It("GET /bundle-extensions returns an empty list when none configured", func() {
		c, rec := withParam(http.MethodGet, "/api/v1/artifacts/a-1/bundle-extensions", "a-1", "")
		Expect(handler.ListBundleExtensions(c)).To(Succeed())
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Body.String()).To(MatchRegexp(`^\[\]\s*$`))
	})

	It("GET /bundle-extensions returns the stored entries", func() {
		Expect(bs.ReplaceForArtifact(ctx, "a-1", []store.ArtifactExtensionBundle{
			{ArtifactID: "a-1", ExtensionName: "tailscale-agent", ExtensionType: "sysext", Order: 0},
		})).To(Succeed())
		c, rec := withParam(http.MethodGet, "/api/v1/artifacts/a-1/bundle-extensions", "a-1", "")
		Expect(handler.ListBundleExtensions(c)).To(Succeed())
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Body.String()).To(ContainSubstring("tailscale-agent"))
	})
})
```

(Update the existing artifact specs' constructor calls to the new signature — add `nil, nil` for `extensions` and `bundles` where these tasks don't exercise them. Use grep + sed:

```
grep -l "handlers.NewArtifactHandler(" pkg/handlers/
```

— and add the two `nil`s in the same slot you'll wire above.)

- [ ] **Step 2: Verify it fails**

```
cd ~/_git/AuroraBoot && go test ./pkg/handlers/... -v
```

Expected: BUILD FAIL — the constructor signature is wrong and `ListBundleExtensions` doesn't exist.

- [ ] **Step 3: Extend the handler struct + constructor**

In `pkg/handlers/artifacts.go`, change `ArtifactHandler` and `NewArtifactHandler`:

```go
type ArtifactHandler struct {
	builder        builder.ArtifactBuilder
	store          store.ArtifactStore
	groups         store.GroupStore
	secureBootKeys store.SecureBootKeySetStore
	extensions     store.ExtensionStore                 // new
	bundles        store.ArtifactExtensionBundleStore   // new
	regToken       string
	aurorabootURL  string
	artifactsDir   string
}

func NewArtifactHandler(
	b builder.ArtifactBuilder,
	artifactStore store.ArtifactStore,
	groups store.GroupStore,
	secureBootKeys store.SecureBootKeySetStore,
	extensions store.ExtensionStore,                  // new
	bundles store.ArtifactExtensionBundleStore,       // new
	artifactsDir string,
	regToken string,
	aurorabootURL string,
) *ArtifactHandler {
	return &ArtifactHandler{
		builder:        b,
		store:          artifactStore,
		groups:         groups,
		secureBootKeys: secureBootKeys,
		extensions:     extensions,
		bundles:        bundles,
		artifactsDir:   artifactsDir,
		regToken:       regToken,
		aurorabootURL:  aurorabootURL,
	}
}
```

- [ ] **Step 4: Update the server wiring**

In `pkg/server/server.go`, find the `handlers.NewArtifactHandler(...)` call (around line 82) and add two new fields to `server.Config`:

```go
	ArtifactStore                 store.ArtifactStore
	SecureBootKeySetStore         store.SecureBootKeySetStore
	ExtensionStore                store.ExtensionStore                    // new
	ArtifactExtensionBundleStore  store.ArtifactExtensionBundleStore      // new
```

Then update the constructor call:

```go
	artifactHandler := handlers.NewArtifactHandler(
		cfg.Builder, cfg.ArtifactStore, cfg.GroupStore, cfg.SecureBootKeySetStore,
		cfg.ExtensionStore, cfg.ArtifactExtensionBundleStore,
		cfg.ArtifactsDir, regToken, cfg.AuroraBootURL,
	)
```

(In `pkg/server/server_test.go`, the existing `server.New(server.Config{...})` literal will compile because both new fields are nil-valued by default. Verify with `go vet ./pkg/server/...`.)

- [ ] **Step 5: Add `ListBundleExtensions`**

Append to `pkg/handlers/artifacts.go`:

```go
// ListBundleExtensions handles GET /api/v1/artifacts/:id/bundle-extensions.
//
//	@Summary		List bundled extensions
//	@Tags			Artifacts
//	@Produce		json
//	@Security		AdminBearer
//	@Success		200		{array}		store.ArtifactExtensionBundle
//	@Router			/api/v1/artifacts/{id}/bundle-extensions [get]
func (h *ArtifactHandler) ListBundleExtensions(c echo.Context) error {
	if h.bundles == nil {
		return c.JSON(http.StatusOK, []store.ArtifactExtensionBundle{})
	}
	entries, err := h.bundles.ListForArtifact(c.Request().Context(), c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "list failed"})
	}
	if entries == nil {
		entries = []store.ArtifactExtensionBundle{}
	}
	return c.JSON(http.StatusOK, entries)
}
```

- [ ] **Step 6: Verify the specs pass**

```
cd ~/_git/AuroraBoot && go test ./pkg/handlers/... ./pkg/server/... -v
```

Expected: every spec green.

- [ ] **Step 7: Commit**

```bash
git add pkg/handlers/artifacts.go pkg/handlers/artifacts_test.go pkg/server/server.go
git commit -m "handlers: ArtifactHandler.ListBundleExtensions + extension/bundle stores"
```

---

## Task 2: `PUT /api/v1/artifacts/:id/bundle-extensions` with arch-matching

**Files:**
- Modify: `pkg/handlers/artifacts.go`
- Modify: `pkg/handlers/artifacts_test.go`

Replace-the-set semantics. Each entry's `(name, type)` must match an existing Ready extension whose `arch` equals the artifact's `arch`.

- [ ] **Step 1: Write the failing specs**

Append to `pkg/handlers/artifacts_test.go`:

```go
var _ = Describe("ArtifactHandler.SetBundleExtensions", func() {
	var (
		e       *echo.Echo
		es      *fakeExtensionStore
		bs      *fakeBundleStore
		ars     *fakeArtifactStore // already used by other specs
		handler *handlers.ArtifactHandler
		ctx     = context.Background()
	)

	BeforeEach(func() {
		e = echo.New()
		es = newFakeExtensionStore()
		bs = newFakeBundleStore()
		ars = newFakeArtifactStore() // helper that satisfies store.ArtifactStore
		_ = ars.Create(ctx, &store.ArtifactRecord{ID: "a-1", Arch: "amd64", Phase: "Ready"})
		_ = es.Save(ctx, &store.ExtensionRecord{ID: "e-1", Name: "tailscale-agent", Type: "sysext", Arch: "amd64", Phase: "Ready"})
		_ = es.Save(ctx, &store.ExtensionRecord{ID: "e-2", Name: "armthing", Type: "sysext", Arch: "arm64", Phase: "Ready"})
		handler = handlers.NewArtifactHandler(&fakeBuilder{}, ars, nil, nil, es, bs, "", "tok", "http://x")
	})

	put := func(id, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/artifacts/"+id+"/bundle-extensions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues(id)
		Expect(handler.SetBundleExtensions(c)).To(Succeed())
		return rec
	}

	It("accepts entries whose arch matches the artifact", func() {
		rec := put("a-1", `[{"extensionName":"tailscale-agent","extensionType":"sysext"}]`)
		Expect(rec.Code).To(Equal(http.StatusOK))
		got, _ := bs.ListForArtifact(ctx, "a-1")
		Expect(got).To(HaveLen(1))
	})

	It("rejects entries whose arch does NOT match the artifact", func() {
		rec := put("a-1", `[{"extensionName":"armthing","extensionType":"sysext"}]`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring("arch"))
	})

	It("rejects entries whose name has no matching Ready extension", func() {
		rec := put("a-1", `[{"extensionName":"does-not-exist","extensionType":"sysext"}]`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring("does-not-exist"))
	})

	It("replaces the entire set atomically", func() {
		put("a-1", `[{"extensionName":"tailscale-agent","extensionType":"sysext"}]`)
		_ = es.Save(ctx, &store.ExtensionRecord{ID: "e-3", Name: "newthing", Type: "sysext", Arch: "amd64", Phase: "Ready"})
		rec := put("a-1", `[{"extensionName":"newthing","extensionType":"sysext"}]`)
		Expect(rec.Code).To(Equal(http.StatusOK))
		got, _ := bs.ListForArtifact(ctx, "a-1")
		Expect(got).To(HaveLen(1))
		Expect(got[0].ExtensionName).To(Equal("newthing"))
	})

	It("returns 404 when the artifact does not exist", func() {
		rec := put("missing", `[]`)
		Expect(rec.Code).To(Equal(http.StatusNotFound))
	})

	It("validates pinnedVersion against a Ready extension of that version", func() {
		_ = es.Save(ctx, &store.ExtensionRecord{ID: "e-4", Name: "tailscale-agent", Type: "sysext", Arch: "amd64", Version: "v1.74", Phase: "Ready"})
		// pinned to v1.74 — must succeed.
		rec := put("a-1", `[{"extensionName":"tailscale-agent","extensionType":"sysext","pinnedVersion":"v1.74"}]`)
		Expect(rec.Code).To(Equal(http.StatusOK))
		// pinned to a missing version — must fail.
		rec = put("a-1", `[{"extensionName":"tailscale-agent","extensionType":"sysext","pinnedVersion":"v99"}]`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
	})
})
```

(`newFakeArtifactStore` is a helper to add to `pkg/handlers/fakes_test.go` if not already present — track the `Create`/`Get`/`Update`/`List`/`Delete`/`AppendLog` methods of `store.ArtifactStore`.)

- [ ] **Step 2: Verify it fails**

```
cd ~/_git/AuroraBoot && go test ./pkg/handlers/... -ginkgo.focus="SetBundleExtensions" -v
```

Expected: BUILD FAIL.

- [ ] **Step 3: Implement `SetBundleExtensions`**

Append to `pkg/handlers/artifacts.go`:

```go
type setBundleEntry struct {
	ExtensionName string `json:"extensionName"`
	ExtensionType string `json:"extensionType"`
	PinnedVersion string `json:"pinnedVersion,omitempty"`
	Order         int    `json:"order,omitempty"`
}

// SetBundleExtensions handles PUT /api/v1/artifacts/:id/bundle-extensions.
//
//	@Summary		Replace bundled extensions for an artifact
//	@Tags			Artifacts
//	@Accept			json
//	@Produce		json
//	@Security		AdminBearer
//	@Param			body	body		[]setBundleEntry	true	"Replacement set"
//	@Success		200		{array}		store.ArtifactExtensionBundle
//	@Failure		400		{object}	APIError
//	@Failure		404		{object}	APIError
//	@Router			/api/v1/artifacts/{id}/bundle-extensions [put]
func (h *ArtifactHandler) SetBundleExtensions(c echo.Context) error {
	if h.bundles == nil || h.extensions == nil || h.store == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "bundles not configured"})
	}
	id := c.Param("id")
	ctx := c.Request().Context()

	artifact, err := h.store.Get(ctx, id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
	}

	var entries []setBundleEntry
	if err := c.Bind(&entries); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}

	out := make([]store.ArtifactExtensionBundle, 0, len(entries))
	for i, e := range entries {
		if e.ExtensionName == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("[%d]: extensionName required", i)})
		}
		if e.ExtensionType != "sysext" && e.ExtensionType != "confext" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("[%d]: extensionType must be sysext or confext", i)})
		}

		// Resolve to a Ready extension to validate arch + (if pinned) version.
		var ext *store.ExtensionRecord
		var rErr error
		if e.PinnedVersion != "" {
			ext, rErr = h.extensions.FindByNameAndVersion(ctx, e.ExtensionType, e.ExtensionName, e.PinnedVersion)
		} else {
			ext, rErr = h.extensions.FindLatestReadyByName(ctx, e.ExtensionType, e.ExtensionName)
		}
		if rErr != nil || ext == nil {
			return c.JSON(http.StatusBadRequest, map[string]string{
				"error": fmt.Sprintf("[%d]: no Ready %s extension matches name=%q version=%q",
					i, e.ExtensionType, e.ExtensionName, e.PinnedVersion),
			})
		}
		if ext.Arch != artifact.Arch {
			return c.JSON(http.StatusBadRequest, map[string]string{
				"error": fmt.Sprintf("[%d]: extension arch %q does not match artifact arch %q",
					i, ext.Arch, artifact.Arch),
			})
		}
		out = append(out, store.ArtifactExtensionBundle{
			ArtifactID:    id,
			ExtensionName: e.ExtensionName,
			ExtensionType: e.ExtensionType,
			PinnedVersion: e.PinnedVersion,
			Order:         e.Order,
		})
	}

	if err := h.bundles.ReplaceForArtifact(ctx, id, out); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "replace failed"})
	}
	return c.JSON(http.StatusOK, out)
}
```

- [ ] **Step 4: Verify the specs pass**

```
cd ~/_git/AuroraBoot && go test ./pkg/handlers/... -ginkgo.focus="SetBundleExtensions" -v
```

Expected: PASS (6 `It` blocks).

- [ ] **Step 5: Commit**

```bash
git add pkg/handlers/artifacts.go pkg/handlers/artifacts_test.go pkg/handlers/fakes_test.go
git commit -m "handlers: ArtifactHandler.SetBundleExtensions with arch-matching"
```

---

## Task 3: `extensionHierarchies` field on `createArtifactRequest`

**Files:**
- Modify: `pkg/handlers/artifacts.go`
- Modify: `pkg/handlers/artifacts_test.go`

Add the JSON field to the create request, validate the hierarchies, and persist them onto `ArtifactRecord.ExtensionHierarchies`.

- [ ] **Step 1: Write the failing spec**

Append to `pkg/handlers/artifacts_test.go`:

```go
var _ = Describe("ArtifactHandler.Create — extensionHierarchies", func() {
	var (
		e       *echo.Echo
		ars     *fakeArtifactStore
		handler *handlers.ArtifactHandler
	)

	BeforeEach(func() {
		e = echo.New()
		ars = newFakeArtifactStore()
		handler = handlers.NewArtifactHandler(&fakeBuilder{}, ars, nil, nil, nil, nil, "", "tok", "http://x")
	})

	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		Expect(handler.Create(c)).To(Succeed())
		return rec
	}

	It("persists sysext + confext hierarchies onto the artifact record", func() {
		rec := post(`{"baseImage":"ubuntu:24.04","arch":"amd64","outputs":{"iso":true},
			"extensionHierarchies":{"sysext":["/opt","/srv"],"confext":[]}}`)
		Expect(rec.Code).To(Equal(http.StatusCreated))

		// The latest create persists the record before kicking off the build,
		// so the fake store has one entry.
		Expect(ars.lastSaved).ToNot(BeNil())
		Expect(ars.lastSaved.ExtensionHierarchies.Sysext).To(Equal([]string{"/opt", "/srv"}))
		Expect(ars.lastSaved.ExtensionHierarchies.Confext).To(BeEmpty())
	})

	It("validates each sysext hierarchy entry", func() {
		rec := post(`{"baseImage":"ubuntu:24.04","arch":"amd64","outputs":{"iso":true},
			"extensionHierarchies":{"sysext":["/usr"]}}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring("/usr"))
	})

	It("validates each confext hierarchy entry — /etc is implicit", func() {
		rec := post(`{"baseImage":"ubuntu:24.04","arch":"amd64","outputs":{"iso":true},
			"extensionHierarchies":{"sysext":[], "confext":["/etc"]}}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring("/etc"))
	})

	It("accepts an absent field", func() {
		rec := post(`{"baseImage":"ubuntu:24.04","arch":"amd64","outputs":{"iso":true}}`)
		Expect(rec.Code).To(Equal(http.StatusCreated))
		Expect(ars.lastSaved.ExtensionHierarchies.Sysext).To(BeNil())
	})
})
```

(`fakeArtifactStore.lastSaved` should be the most recent `*store.ArtifactRecord` passed to `Create`/`Update` — add the field to the fake if absent.)

- [ ] **Step 2: Verify it fails**

```
cd ~/_git/AuroraBoot && go test ./pkg/handlers/... -ginkgo.focus="extensionHierarchies" -v
```

Expected: BUILD FAIL.

- [ ] **Step 3: Add the request DTO field + validation**

In `pkg/handlers/artifacts.go`, add to the `createArtifactRequest` struct (near the bottom of the existing field list):

```go
	ExtensionHierarchies *extensionHierarchiesReq `json:"extensionHierarchies,omitempty"`
```

Add the nested DTO:

```go
type extensionHierarchiesReq struct {
	Sysext  []string `json:"sysext"`
	Confext []string `json:"confext"`
}
```

In `Create`, after the existing body-bind, insert:

```go
	// Hierarchies validation: sysext list cannot include /usr or /; confext list
	// cannot include /etc or /. validateHierarchies (added in Plan 2c) covers /usr;
	// we add the /etc rule inline for the confext branch.
	var sysHierarchies, conHierarchies []string
	if req.ExtensionHierarchies != nil {
		var verr error
		sysHierarchies, verr = validateHierarchies(req.ExtensionHierarchies.Sysext)
		if verr != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "sysext " + verr.Error()})
		}
		for i, p := range req.ExtensionHierarchies.Confext {
			p = strings.TrimRight(p, "/")
			if p == "/etc" || p == "/" {
				return c.JSON(http.StatusBadRequest, map[string]string{
					"error": fmt.Sprintf("confext hierarchies[%d]: %q is implicit and cannot be listed", i, p),
				})
			}
		}
		// Reuse validateHierarchies for confext — same shape rules; /usr/etc check happens above.
		// The /usr check inside validateHierarchies is redundant here but harmless (confext
		// would never want /usr listed anyway).
		conHierarchies, verr = validateHierarchies(req.ExtensionHierarchies.Confext)
		if verr != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "confext " + verr.Error()})
		}
	}
```

Where the artifact record is persisted (around line 260), add:

```go
		ExtensionHierarchies: store.ExtensionHierarchies{
			Sysext:  sysHierarchies,
			Confext: conHierarchies,
		},
```

- [ ] **Step 4: Verify the specs pass**

```
cd ~/_git/AuroraBoot && go test ./pkg/handlers/... -ginkgo.focus="extensionHierarchies" -v
```

Expected: PASS (4 `It` blocks).

- [ ] **Step 5: Commit**

```bash
git add pkg/handlers/artifacts.go pkg/handlers/artifacts_test.go
git commit -m "handlers: persist extensionHierarchies on ArtifactRecord"
```

---

## Task 4: `bundledExtensions` field on `createArtifactRequest`

**Files:**
- Modify: `pkg/handlers/artifacts.go`
- Modify: `pkg/handlers/artifacts_test.go`

The Create payload can carry the initial bundle list. The handler validates each entry the same way `SetBundleExtensions` does, then persists via `ReplaceForArtifact` after the record is saved.

- [ ] **Step 1: Write the failing spec**

Append to `pkg/handlers/artifacts_test.go`:

```go
var _ = Describe("ArtifactHandler.Create — bundledExtensions", func() {
	var (
		e       *echo.Echo
		ars     *fakeArtifactStore
		es      *fakeExtensionStore
		bs      *fakeBundleStore
		handler *handlers.ArtifactHandler
		ctx     = context.Background()
	)

	BeforeEach(func() {
		e = echo.New()
		ars = newFakeArtifactStore()
		es = newFakeExtensionStore()
		bs = newFakeBundleStore()
		_ = es.Save(ctx, &store.ExtensionRecord{ID: "e-1", Name: "tailscale-agent", Type: "sysext", Arch: "amd64", Phase: "Ready"})
		handler = handlers.NewArtifactHandler(&fakeBuilder{}, ars, nil, nil, es, bs, "", "tok", "http://x")
	})

	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		Expect(handler.Create(c)).To(Succeed())
		return rec
	}

	It("persists bundledExtensions when arch matches", func() {
		rec := post(`{"baseImage":"ubuntu:24.04","arch":"amd64","outputs":{"iso":true},
			"bundledExtensions":[{"name":"tailscale-agent","type":"sysext"}]}`)
		Expect(rec.Code).To(Equal(http.StatusCreated))
		entries, _ := bs.ListForArtifact(ctx, ars.lastSaved.ID)
		Expect(entries).To(HaveLen(1))
	})

	It("rejects bundled entries whose arch differs from the artifact", func() {
		rec := post(`{"baseImage":"ubuntu:24.04","arch":"arm64","outputs":{"iso":true},
			"bundledExtensions":[{"name":"tailscale-agent","type":"sysext"}]}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		// No record should have been persisted because validation runs before Build/Save.
		Expect(ars.lastSaved).To(BeNil())
	})
})
```

- [ ] **Step 2: Verify it fails**

```
cd ~/_git/AuroraBoot && go test ./pkg/handlers/... -ginkgo.focus="bundledExtensions" -v
```

Expected: BUILD FAIL.

- [ ] **Step 3: Add the request field + persistence**

In `pkg/handlers/artifacts.go`, append to `createArtifactRequest`:

```go
	BundledExtensions []createBundleEntry `json:"bundledExtensions,omitempty"`
```

```go
type createBundleEntry struct {
	Name          string `json:"name"`
	Type          string `json:"type"`
	PinnedVersion string `json:"pinnedVersion,omitempty"`
	Order         int    `json:"order,omitempty"`
}
```

In `Create`, **before** kicking off the build (i.e. before the `h.builder.Build(...)` call), validate the bundle entries:

```go
	bundleRows := make([]store.ArtifactExtensionBundle, 0, len(req.BundledExtensions))
	if len(req.BundledExtensions) > 0 {
		if h.extensions == nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "extensions store not configured"})
		}
		for i, b := range req.BundledExtensions {
			if b.Name == "" {
				return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("bundledExtensions[%d]: name required", i)})
			}
			if b.Type != "sysext" && b.Type != "confext" {
				return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("bundledExtensions[%d]: type must be sysext or confext", i)})
			}
			var ext *store.ExtensionRecord
			var rErr error
			if b.PinnedVersion != "" {
				ext, rErr = h.extensions.FindByNameAndVersion(ctx, b.Type, b.Name, b.PinnedVersion)
			} else {
				ext, rErr = h.extensions.FindLatestReadyByName(ctx, b.Type, b.Name)
			}
			if rErr != nil || ext == nil {
				return c.JSON(http.StatusBadRequest, map[string]string{
					"error": fmt.Sprintf("bundledExtensions[%d]: no Ready %s extension matches name=%q version=%q",
						i, b.Type, b.Name, b.PinnedVersion),
				})
			}
			if ext.Arch != req.Arch {
				return c.JSON(http.StatusBadRequest, map[string]string{
					"error": fmt.Sprintf("bundledExtensions[%d]: arch %q != artifact arch %q",
						i, ext.Arch, req.Arch),
				})
			}
			bundleRows = append(bundleRows, store.ArtifactExtensionBundle{
				// ArtifactID filled in below once we know the ID.
				ExtensionName: b.Name, ExtensionType: b.Type, PinnedVersion: b.PinnedVersion, Order: b.Order,
			})
		}
	}
```

After the existing `h.store.Create(ctx, rec)` call (i.e. once `rec.ID` is final), persist the bundle rows:

```go
	if h.bundles != nil && len(bundleRows) > 0 {
		for i := range bundleRows {
			bundleRows[i].ArtifactID = rec.ID
		}
		if err := h.bundles.ReplaceForArtifact(ctx, rec.ID, bundleRows); err != nil {
			// Don't fail the Create — log and move on; operator can SetBundleExtensions later.
			c.Logger().Errorf("persist bundle for %s: %v", rec.ID, err)
		}
	}
```

- [ ] **Step 4: Verify the specs pass**

```
cd ~/_git/AuroraBoot && go test ./pkg/handlers/... -ginkgo.focus="bundledExtensions" -v
```

Expected: PASS (2 `It` blocks).

- [ ] **Step 5: Commit**

```bash
git add pkg/handlers/artifacts.go pkg/handlers/artifacts_test.go
git commit -m "handlers: bundledExtensions field on createArtifactRequest"
```

---

## Task 5: Cloud-config bake — `SYSTEMD_SYSEXT_HIERARCHIES` drop-in

**Files:**
- Modify: `pkg/handlers/artifacts.go`
- Modify: `pkg/handlers/artifacts_test.go`

`buildCloudConfig` gets two new params, `sysextHierarchies` and `confextHierarchies`. When either is non-empty, a `stages.initramfs.files` entry is appended that writes the systemd drop-in.

- [ ] **Step 1: Write the failing spec**

Append to `pkg/handlers/artifacts_test.go`:

```go
var _ = Describe("ArtifactHandler.Create — extension hierarchies cloud-config bake", func() {
	var (
		e       *echo.Echo
		fb      *fakeBuilder
		ars     *fakeArtifactStore
		handler *handlers.ArtifactHandler
	)

	BeforeEach(func() {
		e = echo.New()
		fb = &fakeBuilder{}
		ars = newFakeArtifactStore()
		handler = handlers.NewArtifactHandler(fb, ars, nil, nil, nil, nil, "", "tok", "http://x")
	})

	post := func(body string) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		Expect(handler.Create(c)).To(Succeed())
		Expect(rec.Code).To(Equal(http.StatusCreated))
	}

	It("bakes a SYSTEMD_SYSEXT_HIERARCHIES drop-in when sysext hierarchies are listed", func() {
		post(`{"baseImage":"ubuntu:24.04","arch":"amd64","outputs":{"iso":true},
			"extensionHierarchies":{"sysext":["/opt","/srv"]}}`)
		Expect(fb.lastOpts.CloudConfig).To(ContainSubstring("99-aurora-hierarchies.conf"))
		Expect(fb.lastOpts.CloudConfig).To(ContainSubstring("SYSTEMD_SYSEXT_HIERARCHIES=/usr:/opt:/srv"))
		Expect(fb.lastOpts.CloudConfig).ToNot(ContainSubstring("SYSTEMD_CONFEXT_HIERARCHIES"))
	})

	It("bakes a SYSTEMD_CONFEXT_HIERARCHIES drop-in when confext hierarchies are listed", func() {
		post(`{"baseImage":"ubuntu:24.04","arch":"amd64","outputs":{"iso":true},
			"extensionHierarchies":{"sysext":[],"confext":["/srv/configs"]}}`)
		Expect(fb.lastOpts.CloudConfig).To(ContainSubstring("SYSTEMD_CONFEXT_HIERARCHIES=/etc:/srv/configs"))
		Expect(fb.lastOpts.CloudConfig).ToNot(ContainSubstring("SYSTEMD_SYSEXT_HIERARCHIES"))
	})

	It("omits both drop-ins when no extension hierarchies are listed", func() {
		post(`{"baseImage":"ubuntu:24.04","arch":"amd64","outputs":{"iso":true}}`)
		Expect(fb.lastOpts.CloudConfig).ToNot(ContainSubstring("99-aurora-hierarchies.conf"))
	})
})
```

- [ ] **Step 2: Verify it fails**

```
cd ~/_git/AuroraBoot && go test ./pkg/handlers/... -ginkgo.focus="cloud-config bake" -v
```

Expected: FAIL.

- [ ] **Step 3: Extend `buildCloudConfig`**

In `pkg/handlers/artifacts.go`, add two fields to `cloudConfigParams`:

```go
	sysextHierarchies  []string // appended to /usr in the drop-in if non-empty
	confextHierarchies []string // appended to /etc in the drop-in if non-empty
```

In `buildCloudConfig`, after the existing `stages` block is built but before `extraYAML` is appended, insert:

```go
	files := []interface{}{}
	if len(p.sysextHierarchies) > 0 {
		all := append([]string{"/usr"}, p.sysextHierarchies...)
		files = append(files, map[string]interface{}{
			"path":        "/etc/systemd/system/systemd-sysext.service.d/99-aurora-hierarchies.conf",
			"permissions": 0o644,
			"content":     "[Service]\nEnvironment=SYSTEMD_SYSEXT_HIERARCHIES=" + strings.Join(all, ":") + "\n",
		})
	}
	if len(p.confextHierarchies) > 0 {
		all := append([]string{"/etc"}, p.confextHierarchies...)
		files = append(files, map[string]interface{}{
			"path":        "/etc/systemd/system/systemd-confext.service.d/99-aurora-hierarchies.conf",
			"permissions": 0o644,
			"content":     "[Service]\nEnvironment=SYSTEMD_CONFEXT_HIERARCHIES=" + strings.Join(all, ":") + "\n",
		})
	}
	if len(files) > 0 {
		// Merge under stages.initramfs, preserving any existing entries the user block already wrote.
		stages, _ := doc["stages"].(map[string]interface{})
		if stages == nil {
			stages = map[string]interface{}{}
			doc["stages"] = stages
		}
		initramfs, _ := stages["initramfs"].([]interface{})
		// Append a new entry with a "files" key. Existing entries (e.g. the users block)
		// stay untouched.
		initramfs = append(initramfs, map[string]interface{}{"files": files})
		stages["initramfs"] = initramfs
	}
```

In `Create`, when invoking `buildCloudConfig`, pass the hierarchies through:

```go
		sysextHierarchies:  sysHierarchies,
		confextHierarchies: conHierarchies,
```

- [ ] **Step 4: Verify the specs pass**

```
cd ~/_git/AuroraBoot && go test ./pkg/handlers/... -ginkgo.focus="cloud-config bake" -v
```

Expected: PASS (3 `It` blocks).

- [ ] **Step 5: Commit**

```bash
git add pkg/handlers/artifacts.go pkg/handlers/artifacts_test.go
git commit -m "handlers: bake SYSTEMD_{SYSEXT,CONFEXT}_HIERARCHIES drop-in into cloud-config"
```

---

## Task 6: Bundle-resolve endpoint

**Files:**
- Modify: `pkg/handlers/artifacts.go`
- Modify: `pkg/handlers/artifacts_test.go`

`POST /api/v1/artifacts/:id/bundle-resolve` returns the bundle entries with their `source` URLs filled in — the shape the agent will consume inside the `extensions` arg. The UI calls this when opening the upgrade dialog so it can pre-select the entries with concrete download URLs.

- [ ] **Step 1: Write the failing spec**

Append to `pkg/handlers/artifacts_test.go`:

```go
var _ = Describe("ArtifactHandler.ResolveBundle", func() {
	var (
		e       *echo.Echo
		es      *fakeExtensionStore
		bs      *fakeBundleStore
		ars     *fakeArtifactStore
		handler *handlers.ArtifactHandler
		ctx     = context.Background()
	)

	BeforeEach(func() {
		e = echo.New()
		es = newFakeExtensionStore()
		bs = newFakeBundleStore()
		ars = newFakeArtifactStore()
		_ = ars.Create(ctx, &store.ArtifactRecord{ID: "a-1", Arch: "amd64", Phase: "Ready"})
		// Two ready extensions named "tailscale-agent": newer wins for unpinned resolution.
		_ = es.Save(ctx, &store.ExtensionRecord{
			ID: "e-old", Name: "tailscale-agent", Type: "sysext", Arch: "amd64", Version: "v1.72",
			Phase: "Ready", CreatedAt: time.Now().Add(-1 * time.Hour),
			RawFilename: "tailscale-agent.sysext.raw",
		})
		_ = es.Save(ctx, &store.ExtensionRecord{
			ID: "e-new", Name: "tailscale-agent", Type: "sysext", Arch: "amd64", Version: "v1.74",
			Phase: "Ready", CreatedAt: time.Now(),
			RawFilename: "tailscale-agent.sysext.raw",
		})
		_ = bs.ReplaceForArtifact(ctx, "a-1", []store.ArtifactExtensionBundle{
			{ArtifactID: "a-1", ExtensionName: "tailscale-agent", ExtensionType: "sysext"},
		})
		handler = handlers.NewArtifactHandler(&fakeBuilder{}, ars, nil, nil, es, bs, "", "tok", "http://aurora.local")
	})

	post := func(id string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/"+id+"/bundle-resolve", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues(id)
		Expect(handler.ResolveBundle(c)).To(Succeed())
		return rec
	}

	It("resolves unpinned entries to the newest Ready extension by created_at", func() {
		rec := post("a-1")
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Body.String()).To(ContainSubstring("v1.74"))
		Expect(rec.Body.String()).ToNot(ContainSubstring("v1.72"))
	})

	It("emits a source URL pointing at the extensions download endpoint", func() {
		rec := post("a-1")
		Expect(rec.Body.String()).To(ContainSubstring("/api/v1/extensions/e-new/download/tailscale-agent.sysext.raw"))
		Expect(rec.Body.String()).To(ContainSubstring("http://aurora.local"))
	})

	It("returns 404 when the artifact is unknown", func() {
		rec := post("missing")
		Expect(rec.Code).To(Equal(http.StatusNotFound))
	})

	It("returns 400 when a bundle entry cannot resolve", func() {
		_ = bs.ReplaceForArtifact(ctx, "a-1", []store.ArtifactExtensionBundle{
			{ArtifactID: "a-1", ExtensionName: "does-not-exist", ExtensionType: "sysext"},
		})
		rec := post("a-1")
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring("does-not-exist"))
	})
})
```

(`time` import — add if not already there.)

- [ ] **Step 2: Verify it fails**

```
cd ~/_git/AuroraBoot && go test ./pkg/handlers/... -ginkgo.focus="ResolveBundle" -v
```

Expected: BUILD FAIL.

- [ ] **Step 3: Implement `ResolveBundle`**

Append to `pkg/handlers/artifacts.go`:

```go
// ResolvedBundleEntry is what the UI feeds back into the upgrade command's
// `extensions` arg. The agent will Parse this same shape on the node.
type ResolvedBundleEntry struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Version string `json:"version"`
	Source  string `json:"source"`
}

// ResolveBundle handles POST /api/v1/artifacts/:id/bundle-resolve.
//
//	@Summary		Resolve bundled extensions for upgrade dispatch
//	@Description	Returns the bundle entries with concrete download URLs and
//	                resolved versions, ready to be passed as the `extensions`
//	                arg of an `upgrade` phonehome command.
//	@Tags			Artifacts
//	@Produce		json
//	@Security		AdminBearer
//	@Success		200		{array}		ResolvedBundleEntry
//	@Failure		400		{object}	APIError
//	@Failure		404		{object}	APIError
//	@Router			/api/v1/artifacts/{id}/bundle-resolve [post]
func (h *ArtifactHandler) ResolveBundle(c echo.Context) error {
	if h.bundles == nil || h.extensions == nil || h.store == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "bundles not configured"})
	}
	id := c.Param("id")
	ctx := c.Request().Context()

	if _, err := h.store.Get(ctx, id); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
	}

	entries, err := h.bundles.ListForArtifact(ctx, id)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "list failed"})
	}

	out := make([]ResolvedBundleEntry, 0, len(entries))
	for i, e := range entries {
		var ext *store.ExtensionRecord
		var rErr error
		if e.PinnedVersion != "" {
			ext, rErr = h.extensions.FindByNameAndVersion(ctx, e.ExtensionType, e.ExtensionName, e.PinnedVersion)
		} else {
			ext, rErr = h.extensions.FindLatestReadyByName(ctx, e.ExtensionType, e.ExtensionName)
		}
		if rErr != nil || ext == nil {
			return c.JSON(http.StatusBadRequest, map[string]string{
				"error": fmt.Sprintf("bundle[%d]: no Ready %s extension matches name=%q version=%q",
					i, e.ExtensionType, e.ExtensionName, e.PinnedVersion),
			})
		}
		source := fmt.Sprintf("%s/api/v1/extensions/%s/download/%s",
			strings.TrimRight(h.aurorabootURL, "/"), ext.ID, ext.RawFilename)
		out = append(out, ResolvedBundleEntry{
			Name:    ext.Name,
			Type:    ext.Type,
			Version: ext.Version,
			Source:  source,
		})
	}
	return c.JSON(http.StatusOK, out)
}
```

The agent appends `?token=<api-key>` to this URL when it actually downloads — the URL we emit here is template-shaped; the agent stamps the token from its own credentials at dispatch time.

- [ ] **Step 4: Verify the specs pass**

```
cd ~/_git/AuroraBoot && go test ./pkg/handlers/... -ginkgo.focus="ResolveBundle" -v
```

Expected: PASS (4 `It` blocks).

- [ ] **Step 5: Full handler suite green**

```
cd ~/_git/AuroraBoot && go test ./pkg/handlers/... -v
```

Expected: every spec passes. No regression against the existing artifact specs.

- [ ] **Step 6: Commit**

```bash
git add pkg/handlers/artifacts.go pkg/handlers/artifacts_test.go
git commit -m "handlers: ArtifactHandler.ResolveBundle for upgrade dispatch"
```

---

## Self-review checks

- **Spec coverage:** Bundle list (Task 1), bundle replace with arch-matching (Task 2), `extensionHierarchies` on Create (Task 3), `bundledExtensions` on Create (Task 4), cloud-config bake of the systemd drop-in (Task 5), bundle resolve for dispatch (Task 6). Together these cover every artifact-side requirement in *ArtifactBuilder integration* and *Two delivery flows / Bundled flow* in the spec.
- **Type consistency:** `BundleEntry` JSON tag on `ListForArtifact` returns the GORM struct directly; the `SetBundleExtensions` request DTO (`setBundleEntry`) and `Create`'s `createBundleEntry` use different tag spellings (`extensionName`/`name`) intentionally — UI works with the human-readable `name` on create, but `extensionName` on the bundle-set surface since that mirrors the underlying column. If you'd rather unify them, do so in one pass and update the UI in Plan 3.
- **Placeholder scan:** none. Token-stamping (`?token=`) is delegated to the agent because that's where the credentials live.

## What lands at the end of this plan

- Operators can attach a bundle list to an artifact at create time or after the fact via `PUT`.
- Artifacts can declare extension hierarchies that AuroraBoot bakes into the image automatically.
- The UI can resolve a bundle into the concrete `extensions` payload to feed the upgrade command.
- `go test ./pkg/handlers/... ./pkg/server/... -v` is green.

## Out of scope here

- Status callback writing into `node_extensions` → Plan 2e.
- `--include-path` CLI flag → Plan 2e.
- Route registration → Plan 2e.
- Vendoring the new kairos-agent → Plan 2e.
- All frontend work → Plan 3.
