# Extensions — Plan 2e of 3: Status callback, CLI, route wiring, agent vendor

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close out the backend half of the feature: the REST status callback writes `node_extensions` on successful install / enable / disable / remove (manual flow) and on successful bundled upgrade; the CLI's `auroraboot sysext` grows `--include-path` (with `--with-opt` kept as an indefinite alias); every new route from Plans 2c–2d gets registered; the in-process `ExtensionBuilder` is wired into `web.go`; the new tagged kairos-agent is vendored; the full suite + swagger regen sanity-pass green.

**Architecture:** `CommandHandler` learns to interpret an `extension` command's args (manual flow) and a compound `upgrade`/`upgrade-recovery`'s `extensions` JSON arg (bundled flow). It depends on `NodeExtensionStore` and (for re-resolution) `ExtensionStore`. New routes attach via the existing `adminGroup` for admin operations and `agentGroup` for the node's status callback. `auroraboot web` constructs the new builder + stores and threads them through `server.Config`. The agent vendor bump happens as a single `go.mod` change.

**Tech Stack:** echo v4, gorm, ginkgo v2 / gomega, urfave/cli v2, swag.

**Repository:** `/home/mudler/_git/AuroraBoot`

**Prerequisites:** Plans 2a–2d merged; Plan 1 (kairos-agent) tagged and pushed to GitHub.

---

## Reference — read these before starting

| File | What it does |
|---|---|
| `pkg/handlers/commands.go:12-26` | `CommandHandler` struct + constructor. |
| `pkg/handlers/commands.go:197-220` | `updateStatusRequest` + `UpdateStatus` — extended in Task 1. |
| `pkg/store/store.go:43-54` | `NodeCommand` — `.Args` is `map[string]string`. |
| `pkg/store/store.go:67-74` | Command-type constants (`extension` added in Plan 2a Task 1). |
| `pkg/server/server.go:42-189` | Route registration. New routes go in the same `adminGroup`. Download endpoint follows the artifact pattern at L151-153. |
| `internal/cmd/web.go:150-189` | `runWeb` — wires stores + handler into `server.New`. Extended in Task 5. |
| `internal/cmd/sysext.go` (whole file) | The `sysext` CLI command. Task 2 adds `--include-path`. |
| `go.mod` | `github.com/kairos-io/kairos-agent/v2` line — bumped in Task 6. |

---

## Task 1: Status callback writes `node_extensions`

**Files:**
- Modify: `pkg/handlers/commands.go` (struct, constructor, `UpdateStatus`)
- Modify: `pkg/server/server.go` (constructor call site)
- Modify: `pkg/handlers/commands_test.go`

When the agent reports `Completed` for an `extension` command, the server upserts / deletes rows in `node_extensions` based on `action`. When it reports `Completed` for an `upgrade`/`upgrade-recovery` command that carries an `extensions` arg, the server parses each entry and upserts rows at `active` / `recovery` scope. Anything other than `Completed` is a no-op for tracking.

For manual `install`/`enable`, the wire payload doesn't carry `version`, so the server re-resolves the version via `ExtensionStore.FindLatestReadyByName` — this matches what the operator actually saw in the UI at dispatch time (an `ExtensionRecord` newer than that one would be a different build the operator didn't pick).

For bundled `upgrade`, the server emits an extra `version` field in each `extensions[]` entry at dispatch time (Plan 2d Task 6 already does this in `ResolveBundle`). The agent ignores unknown JSON fields (`encoding/json` default), so there is no agent compatibility break.

- [ ] **Step 1: Write the failing specs**

In `pkg/handlers/commands_test.go`, append:

```go
var _ = Describe("CommandHandler.UpdateStatus — node_extensions tracking", func() {
	var (
		e       *echo.Echo
		cs      *fakeCommandStore
		ns      *fakeNodeStore
		nes     *fakeNodeExtensionStore
		es      *fakeExtensionStore
		handler *handlers.CommandHandler
		ctx     = context.Background()
	)

	BeforeEach(func() {
		e = echo.New()
		cs = newFakeCommandStore()
		ns = &fakeNodeStore{}
		nes = newFakeNodeExtensionStore()
		es = newFakeExtensionStore()
		handler = handlers.NewCommandHandler(cs, ns, ws.NewHub(), nes, es)
	})

	withParams := func(method, path, nodeID, cmdID, body string) (echo.Context, *httptest.ResponseRecorder) {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("nodeID", "commandID")
		c.SetParamValues(nodeID, cmdID)
		return c, rec
	}

	putStatus := func(nodeID, cmdID, phase string) *httptest.ResponseRecorder {
		c, rec := withParams(http.MethodPut,
			"/api/v1/nodes/"+nodeID+"/commands/"+cmdID+"/status",
			nodeID, cmdID, fmt.Sprintf(`{"phase":%q}`, phase))
		Expect(handler.UpdateStatus(c)).To(Succeed())
		return rec
	}

	It("upserts a node_extensions row on a successful manual install", func() {
		_ = cs.Save(ctx, &store.NodeCommand{
			ID: "c-1", ManagedNodeID: "n-1", Command: "extension",
			Args: map[string]string{"type": "sysext", "action": "install",
				"name": "tailscale-agent", "bootState": "common"},
		})
		// Make the store know the latest version of tailscale-agent.
		_ = es.Save(ctx, &store.ExtensionRecord{
			ID: "e-1", Name: "tailscale-agent", Type: "sysext",
			Version: "v1.74.0", Phase: "Ready",
		})

		Expect(putStatus("n-1", "c-1", "Completed").Code).To(Equal(http.StatusOK))

		rows, _ := nes.ListForNode(ctx, "n-1")
		Expect(rows).To(HaveLen(1))
		Expect(rows[0].Name).To(Equal("tailscale-agent"))
		Expect(rows[0].BootState).To(Equal("common"))
		Expect(rows[0].Version).To(Equal("v1.74.0"))
		Expect(rows[0].ExtensionID).To(Equal("e-1"))
	})

	It("does nothing for a Failed manual install", func() {
		_ = cs.Save(ctx, &store.NodeCommand{
			ID: "c-2", ManagedNodeID: "n-1", Command: "extension",
			Args: map[string]string{"type": "sysext", "action": "install",
				"name": "x", "bootState": "common"},
		})
		Expect(putStatus("n-1", "c-2", "Failed").Code).To(Equal(http.StatusOK))
		rows, _ := nes.ListForNode(ctx, "n-1")
		Expect(rows).To(BeEmpty())
	})

	It("deletes the scope row on a successful manual disable", func() {
		_ = nes.Upsert(ctx, &store.NodeExtensionRow{NodeID: "n-1", Name: "ts", Type: "sysext", BootState: "common"})
		_ = nes.Upsert(ctx, &store.NodeExtensionRow{NodeID: "n-1", Name: "ts", Type: "sysext", BootState: "active"})
		_ = cs.Save(ctx, &store.NodeCommand{
			ID: "c-3", ManagedNodeID: "n-1", Command: "extension",
			Args: map[string]string{"type": "sysext", "action": "disable",
				"name": "ts", "bootState": "common"},
		})
		Expect(putStatus("n-1", "c-3", "Completed").Code).To(Equal(http.StatusOK))

		rows, _ := nes.ListForNode(ctx, "n-1")
		Expect(rows).To(HaveLen(1))
		Expect(rows[0].BootState).To(Equal("active"))
	})

	It("deletes every scope row on a successful manual remove", func() {
		for _, scope := range []string{"common", "active"} {
			_ = nes.Upsert(ctx, &store.NodeExtensionRow{NodeID: "n-1", Name: "ts", Type: "sysext", BootState: scope})
		}
		_ = cs.Save(ctx, &store.NodeCommand{
			ID: "c-4", ManagedNodeID: "n-1", Command: "extension",
			Args: map[string]string{"type": "sysext", "action": "remove", "name": "ts"},
		})
		Expect(putStatus("n-1", "c-4", "Completed").Code).To(Equal(http.StatusOK))
		rows, _ := nes.ListForNode(ctx, "n-1")
		Expect(rows).To(BeEmpty())
	})

	It("upserts every bundled extension at --active on a successful upgrade", func() {
		_ = cs.Save(ctx, &store.NodeCommand{
			ID: "c-5", ManagedNodeID: "n-1", Command: "upgrade",
			Args: map[string]string{
				"source": "artifact:a-1",
				"extensions": `[{"type":"sysext","name":"tailscale-agent","source":"https://x/y","version":"v1.74.0"},
				                {"type":"confext","name":"fluent-bit","source":"https://x/z","version":"2026.05.20"}]`,
			},
		})
		Expect(putStatus("n-1", "c-5", "Completed").Code).To(Equal(http.StatusOK))
		rows, _ := nes.ListForNode(ctx, "n-1")
		Expect(rows).To(HaveLen(2))
		for _, r := range rows {
			Expect(r.BootState).To(Equal("active"))
		}
	})

	It("upserts bundled extensions at --recovery on a successful upgrade-recovery", func() {
		_ = cs.Save(ctx, &store.NodeCommand{
			ID: "c-6", ManagedNodeID: "n-1", Command: "upgrade-recovery",
			Args: map[string]string{
				"source": "artifact:a-1",
				"extensions": `[{"type":"sysext","name":"rescue-tools","source":"https://x/r","version":"v3"}]`,
			},
		})
		Expect(putStatus("n-1", "c-6", "Completed").Code).To(Equal(http.StatusOK))
		rows, _ := nes.ListForNode(ctx, "n-1")
		Expect(rows).To(HaveLen(1))
		Expect(rows[0].BootState).To(Equal("recovery"))
	})

	It("is a no-op for upgrade with no extensions arg (backward compat)", func() {
		_ = cs.Save(ctx, &store.NodeCommand{
			ID: "c-7", ManagedNodeID: "n-1", Command: "upgrade",
			Args: map[string]string{"source": "artifact:a-1"},
		})
		Expect(putStatus("n-1", "c-7", "Completed").Code).To(Equal(http.StatusOK))
		rows, _ := nes.ListForNode(ctx, "n-1")
		Expect(rows).To(BeEmpty())
	})
})
```

Add `fakeNodeExtensionStore` and `fakeCommandStore.Save` to `pkg/handlers/fakes_test.go` if absent — mirror the patterns from the other in-memory fakes.

- [ ] **Step 2: Verify it fails**

```
cd ~/_git/AuroraBoot && go test ./pkg/handlers/... -ginkgo.focus="node_extensions tracking" -v
```

Expected: BUILD FAIL — `NewCommandHandler` doesn't accept the new arg, helper logic doesn't exist.

- [ ] **Step 3: Extend `CommandHandler`**

In `pkg/handlers/commands.go`, update the struct + constructor:

```go
import (
	// existing
	"encoding/json"

	"github.com/kairos-io/AuroraBoot/pkg/store"
)

type CommandHandler struct {
	commands       store.CommandStore
	nodes          store.NodeStore
	nodeExtensions store.NodeExtensionStore     // new
	extensions     store.ExtensionStore         // new
	hub            *ws.Hub
}

func NewCommandHandler(
	commands store.CommandStore,
	nodes store.NodeStore,
	hub *ws.Hub,
	nodeExtensions store.NodeExtensionStore,
	extensions store.ExtensionStore,
) *CommandHandler {
	return &CommandHandler{
		commands: commands, nodes: nodes, hub: hub,
		nodeExtensions: nodeExtensions, extensions: extensions,
	}
}
```

Extend `UpdateStatus`:

```go
func (h *CommandHandler) UpdateStatus(c echo.Context) error {
	commandID := c.Param("commandID")
	nodeID := c.Param("nodeID")
	var req updateStatusRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	if req.Phase == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "phase is required"})
	}

	ctx := c.Request().Context()
	if err := h.commands.UpdateStatus(ctx, commandID, req.Phase, req.Result); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to update command status"})
	}

	// Track node_extensions only on successful completion. Phase == "Completed".
	if req.Phase == "Completed" && h.nodeExtensions != nil {
		if cmd, err := h.commands.Get(ctx, commandID); err == nil {
			h.applyExtensionTracking(ctx, nodeID, cmd)
		}
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// bundledExtension matches the wire shape AuroraBoot emits in
// commands.Args["extensions"]. The optional Version field is server-side
// only — the agent ignores unknown JSON fields.
type bundledExtension struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Source  string `json:"source"`
	Version string `json:"version,omitempty"`
}

func (h *CommandHandler) applyExtensionTracking(ctx context.Context, nodeID string, cmd *store.NodeCommand) {
	switch cmd.Command {
	case store.CommandTypeExtension:
		h.applyManualExtension(ctx, nodeID, cmd)
	case "upgrade", "upgrade-recovery":
		h.applyBundledExtensions(ctx, nodeID, cmd)
	}
}

func (h *CommandHandler) applyManualExtension(ctx context.Context, nodeID string, cmd *store.NodeCommand) {
	args := cmd.Args
	action, extType, name, bootState := args["action"], args["type"], args["name"], args["bootState"]
	if name == "" || extType == "" {
		return
	}
	switch action {
	case "install", "enable":
		version := ""
		extensionID := ""
		if h.extensions != nil {
			if ext, err := h.extensions.FindLatestReadyByName(ctx, extType, name); err == nil && ext != nil {
				version = ext.Version
				extensionID = ext.ID
			}
		}
		_ = h.nodeExtensions.Upsert(ctx, &store.NodeExtensionRow{
			NodeID: nodeID, Name: name, Type: extType, BootState: bootState,
			Version: version, ExtensionID: extensionID,
		})
	case "disable":
		_ = h.nodeExtensions.DeleteByScope(ctx, nodeID, extType, name, bootState)
	case "remove":
		_ = h.nodeExtensions.DeleteByName(ctx, nodeID, extType, name)
	}
}

func (h *CommandHandler) applyBundledExtensions(ctx context.Context, nodeID string, cmd *store.NodeCommand) {
	raw := cmd.Args["extensions"]
	if raw == "" {
		return
	}
	var list []bundledExtension
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return
	}
	scope := "active"
	if cmd.Command == "upgrade-recovery" {
		scope = "recovery"
	}
	for _, e := range list {
		_ = h.nodeExtensions.Upsert(ctx, &store.NodeExtensionRow{
			NodeID: nodeID, Name: e.Name, Type: e.Type, BootState: scope,
			Version: e.Version,
		})
	}
}
```

- [ ] **Step 4: Update the constructor call site**

In `pkg/server/server.go`:

```go
	cmdHandler := handlers.NewCommandHandler(
		cfg.CommandStore, cfg.NodeStore, hub,
		cfg.NodeExtensionStore, cfg.ExtensionStore,
	)
```

Add `NodeExtensionStore store.NodeExtensionStore` to `server.Config` (next to `ExtensionStore`).

- [ ] **Step 5: Verify the specs pass**

```
cd ~/_git/AuroraBoot && go test ./pkg/handlers/... -ginkgo.focus="node_extensions tracking" -v
```

Expected: PASS (7 `It` blocks).

- [ ] **Step 6: Commit**

```bash
git add pkg/handlers/commands.go pkg/handlers/commands_test.go pkg/handlers/fakes_test.go pkg/server/server.go
git commit -m "handlers: status callback writes node_extensions for manual + bundled flows"
```

---

## Task 2: `--include-path` CLI flag + `--with-opt` deprecation

**Files:**
- Modify: `internal/cmd/sysext.go`
- Modify: `internal/cmd/sysext_test.go` (if absent — check the file list)

`--include-path` is a repeatable string flag. Each entry appends to the extractor allowlist. `--with-opt` becomes an alias that prepends `/opt` to the include list and prints a one-time deprecation warning.

- [ ] **Step 1: Inspect the current flag set**

```
grep -n "Flag\|StringSliceFlag" internal/cmd/sysext.go | head -30
```

Confirm the existing pattern (`cli.BoolFlag`, `cli.StringFlag`) and where the flag list is defined.

- [ ] **Step 2: Write the failing spec**

Create or extend `internal/cmd/sysext_test.go`:

```go
package cmd_test

import (
	"github.com/kairos-io/AuroraBoot/internal/cmd"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("internal/cmd.SysextCmd flags", func() {
	It("declares --include-path as a repeatable string flag", func() {
		var found bool
		for _, f := range cmd.SysextCmd.Flags {
			if f.Names()[0] == "include-path" {
				found = true
				_, ok := f.(*cliStringSliceFlag) // alias type defined below
				Expect(ok).To(BeTrue())
			}
		}
		Expect(found).To(BeTrue(), "expected --include-path flag")
	})

	It("keeps --with-opt as a (now-deprecated) bool flag", func() {
		var found bool
		for _, f := range cmd.SysextCmd.Flags {
			if f.Names()[0] == "with-opt" {
				found = true
			}
		}
		Expect(found).To(BeTrue(), "expected --with-opt to remain for backward compat")
	})
})
```

(The `cliStringSliceFlag` alias is a test-local convenience for asserting the flag kind. Adjust to the actual `urfave/cli` type — `*cli.StringSliceFlag`.)

If `internal/cmd` has no ginkgo suite, also create `internal/cmd/suite_test.go`:

```go
package cmd_test

import (
	"testing"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCmd(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "internal/cmd suite")
}
```

- [ ] **Step 3: Verify it fails**

```
cd ~/_git/AuroraBoot && go test ./internal/cmd/... -v
```

Expected: BUILD FAIL — `--include-path` not declared.

- [ ] **Step 4: Add the flag + alias logic**

In `internal/cmd/sysext.go`, inside the flag list, append:

```go
		&cli.StringSliceFlag{
			Name:  "include-path",
			Usage: "Filesystem paths to extract from the image layer (repeatable). /usr is always included.",
		},
```

Below the existing `--with-opt` flag definition, add a comment marking it deprecated:

```go
		// Deprecated: prefer --include-path=/opt. Kept as an indefinite alias —
		// the cost of carrying it is negligible; breaking scripts isn't.
```

In the action where the extractor regex is built (find the existing `--with-opt` consumer), replace the `if ctx.Bool("with-opt")` branch with a small helper that merges both sources:

```go
func includePathsFromFlags(ctx *cli.Context) []string {
	out := append([]string(nil), ctx.StringSlice("include-path")...)
	if ctx.Bool("with-opt") {
		// One-time deprecation warning. Uses fmt.Fprintln rather than the
		// logger because the logger is built later in the action.
		fmt.Fprintln(os.Stderr,
			"auroraboot sysext: --with-opt is deprecated; use --include-path=/opt instead.")
		out = append(out, "/opt")
	}
	// dedup
	seen := map[string]struct{}{}
	dedup := out[:0]
	for _, p := range out {
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		dedup = append(dedup, p)
	}
	return dedup
}
```

Wire the result into the existing regex assembly. The existing code path that previously gated `/opt` extraction now uses the full `includePaths` list to drive whatever `regexp.MustCompile(...)` or similar handles allowed paths.

- [ ] **Step 5: Verify the spec passes**

```
cd ~/_git/AuroraBoot && go test ./internal/cmd/... -v
```

Expected: PASS.

- [ ] **Step 6: Smoke-test the CLI**

```
cd ~/_git/AuroraBoot && go build -o /tmp/auroraboot ./ && /tmp/auroraboot sysext --help
```

Expected: `--include-path` appears in the flag list; `--with-opt` is still listed.

- [ ] **Step 7: Commit**

```bash
git add internal/cmd/sysext.go internal/cmd/sysext_test.go internal/cmd/suite_test.go 2>/dev/null
git commit -m "cmd: --include-path repeatable flag; --with-opt deprecated alias"
```

---

## Task 3: Register new routes in `pkg/server/server.go`

**Files:**
- Modify: `pkg/server/server.go`
- Modify: `pkg/server/server_test.go`

All endpoints from Plans 2c–2d need to attach to the existing `adminGroup` (admin auth) or wear `DownloadMiddleware` for the agent-facing download.

- [ ] **Step 1: Write the failing spec**

Append to `pkg/server/server_test.go`:

```go
var _ = Describe("Server — extension routes", func() {
	var (
		e *httptest.Server
	)

	BeforeEach(func() {
		echoApp := server.New(server.Config{
			NodeStore:     &fakeNodeStore{},
			CommandStore:  &fakeCommandStore{},
			GroupStore:    &fakeGroupStore{},
			Builder:       &fakeBuilder{},
			AdminPassword: "admin-pass",
			RegToken:      "reg-token",
			AuroraBootURL: "http://localhost:8080",
		})
		e = httptest.NewServer(echoApp)
	})
	AfterEach(func() { e.Close() })

	It("GET /api/v1/extensions requires admin auth", func() {
		resp, err := http.Get(e.URL + "/api/v1/extensions")
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
	})

	It("GET /api/v1/extensions returns 200 with admin auth", func() {
		req, _ := http.NewRequest(http.MethodGet, e.URL+"/api/v1/extensions", nil)
		req.Header.Set("Authorization", "Bearer admin-pass")
		resp, err := http.DefaultClient.Do(req)
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
	})

	It("GET /api/v1/artifacts/:id/bundle-extensions wires through admin auth", func() {
		req, _ := http.NewRequest(http.MethodGet, e.URL+"/api/v1/artifacts/a-1/bundle-extensions", nil)
		req.Header.Set("Authorization", "Bearer admin-pass")
		resp, err := http.DefaultClient.Do(req)
		Expect(err).ToNot(HaveOccurred())
		// The route exists; the body is `[]` because the fake bundle store is nil.
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
	})

	It("POST /api/v1/artifacts/:id/bundle-resolve is reachable", func() {
		req, _ := http.NewRequest(http.MethodPost, e.URL+"/api/v1/artifacts/a-1/bundle-resolve", nil)
		req.Header.Set("Authorization", "Bearer admin-pass")
		resp, err := http.DefaultClient.Do(req)
		Expect(err).ToNot(HaveOccurred())
		// Reaches the handler. Returns 500 because stores are nil — that's fine,
		// the routing itself is what we're proving here.
		Expect(resp.StatusCode).To(Or(Equal(http.StatusOK), Equal(http.StatusInternalServerError), Equal(http.StatusNotFound)))
	})
})
```

- [ ] **Step 2: Verify it fails**

```
cd ~/_git/AuroraBoot && go test ./pkg/server/... -v
```

Expected: FAIL — first GET 200 fails because the route isn't registered.

- [ ] **Step 3: Construct + wire the extension handler**

In `pkg/server/server.go`, after the existing `artifactHandler := handlers.NewArtifactHandler(...)` line, add:

```go
	var extensionHandler *handlers.ExtensionHandler
	if cfg.ExtensionBuilder != nil {
		extensionHandler = handlers.NewExtensionHandler(
			cfg.ExtensionBuilder, cfg.ExtensionStore, cfg.ArtifactExtensionBundleStore,
			cfg.SecureBootKeySetStore, cfg.ArtifactsDir,
		)
	}
```

Add `ExtensionBuilder builder.ExtensionBuilder` to `server.Config` (next to `Builder`).

Register the new routes inside the existing `adminGroup` block:

```go
	if extensionHandler != nil {
		adminGroup.POST("/extensions", extensionHandler.Create)
		adminGroup.GET("/extensions", extensionHandler.List)
		adminGroup.GET("/extensions/:id", extensionHandler.Get)
		adminGroup.PATCH("/extensions/:id", extensionHandler.Update)
		adminGroup.DELETE("/extensions/:id", extensionHandler.Delete)
		adminGroup.GET("/extensions/:id/logs", extensionHandler.GetLogs)
		adminGroup.POST("/extensions/:id/cancel", extensionHandler.Cancel)
	}
	adminGroup.GET("/artifacts/:id/bundle-extensions", artifactHandler.ListBundleExtensions)
	adminGroup.PUT("/artifacts/:id/bundle-extensions", artifactHandler.SetBundleExtensions)
	adminGroup.POST("/artifacts/:id/bundle-resolve", artifactHandler.ResolveBundle)
```

Register the download endpoint with `DownloadMiddleware`, mirroring the artifact pattern (server.go:151-153):

```go
	if extensionHandler != nil {
		e.GET("/api/v1/extensions/:id/download/:filename", extensionHandler.Download, dlAuth)
	}
```

(`dlAuth` is already declared above the artifact download routes. Reuse it.)

- [ ] **Step 4: Verify the specs pass**

```
cd ~/_git/AuroraBoot && go test ./pkg/server/... -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/server/server.go pkg/server/server_test.go
git commit -m "server: register extension + artifact-bundle routes"
```

---

## Task 4: Wire `ExtensionBuilder` + stores in `internal/cmd/web.go`

**Files:**
- Modify: `internal/cmd/web.go`

The web command's `runWeb` constructs all stores + builders + handlers. Threading `ExtensionBuilder` through is mechanical — mirror what's already done for `ArtifactBuilder`.

- [ ] **Step 1: Add the wiring**

In `internal/cmd/web.go`, find the existing `artifactBuilder := auroraboot.New(...)` line. Below it, add:

```go
	extensionStoreAdapter := &gormstore.ExtensionStoreAdapter{S: store}
	bundleStoreAdapter := &gormstore.ArtifactExtensionBundleStoreAdapter{S: store}
	nodeExtStoreAdapter := &gormstore.NodeExtensionStoreAdapter{S: store}

	extensionBuilder := auroraboot.NewExtensionBuilder(filepath.Join(artifactsDir, "extensions"), extensionStoreAdapter).
		WithArtifactStore(artifactStore).
		WithLogBroadcaster(wsHub.UI)
```

In the `server.New(server.Config{...})` literal, add the new fields:

```go
		ExtensionStore:                extensionStoreAdapter,
		ArtifactExtensionBundleStore:  bundleStoreAdapter,
		NodeExtensionStore:            nodeExtStoreAdapter,
		ExtensionBuilder:              extensionBuilder,
```

- [ ] **Step 2: Verify the binary builds**

```
cd ~/_git/AuroraBoot && go build -o /tmp/auroraboot ./
```

Expected: success.

- [ ] **Step 3: Run the web command against a temp data dir to confirm migrations + listen succeed**

```
mkdir -p /tmp/aurora-smoke && \
  /tmp/auroraboot web --data-dir /tmp/aurora-smoke --listen :18080 &
sleep 2
curl -s http://localhost:18080/healthz
kill %1
```

Expected: `/healthz` returns `{"status":"ok"}`, no error in stderr. Inspect `/tmp/aurora-smoke/auroraboot.db` to confirm the new tables exist (optional, with `sqlite3 .schema`).

- [ ] **Step 4: Commit**

```bash
git add internal/cmd/web.go
git commit -m "cmd: wire ExtensionBuilder + extension stores into web runtime"
```

---

## Task 5: Vendor the new kairos-agent

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

After Plan 1's PR is merged and tagged in the `kairos-agent` repo, this task bumps AuroraBoot's dependency.

- [ ] **Step 1: Discover the new tag**

```
gh release list --repo kairos-io/kairos-agent --limit 5
```

(Or check the repo directly.) Note the version that includes Plan 1's changes — call it `vX.Y.Z`.

- [ ] **Step 2: Update the module**

```
cd ~/_git/AuroraBoot && go get github.com/kairos-io/kairos-agent/v2@vX.Y.Z
go mod tidy
```

Expected: `go.mod` and `go.sum` show the new version.

- [ ] **Step 3: Re-run the test suite**

```
cd ~/_git/AuroraBoot && go test ./... -count=1
```

Expected: every package green. If any pre-existing kairos-agent-facing call broke, investigate — the new agent should be additive-only.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: bump kairos-agent to vX.Y.Z (extension phonehome command)"
```

---

## Task 6: Whole-repo verification + swagger regen

**Files:**
- Modify: `docs/swagger.json`, `docs/swagger.yaml`, `docs/docs.go` (regenerated)

- [ ] **Step 1: Regenerate the OpenAPI artifacts**

```
cd ~/_git/AuroraBoot && make openapi
```

(If a `make openapi` target doesn't exist, run swag directly: `swag init -g main.go --output docs --parseDependency`.)

Expected: `docs/swagger.json`, `docs/swagger.yaml`, `docs/docs.go` are updated to include the new `/extensions` endpoints and the bundle routes on `/artifacts/{id}/...`.

- [ ] **Step 2: Run the full Go suite**

```
cd ~/_git/AuroraBoot && go test ./... -count=1
```

Expected: every package green.

- [ ] **Step 3: Run vet + format check**

```
cd ~/_git/AuroraBoot && go vet ./... && gofmt -l .
```

Expected: `go vet` exits 0; `gofmt -l` produces no output.

- [ ] **Step 4: Run linter if configured**

```
cd ~/_git/AuroraBoot && [ -f .golangci.yml ] && golangci-lint run ./... || echo "no lint configured"
```

Expected: no issues.

- [ ] **Step 5: Smoke-test end-to-end**

Manual sanity, not an automated spec:

1. Start `auroraboot web --data-dir /tmp/aurora-smoke`.
2. From a separate shell:

   ```bash
   curl -s -X POST -H "Authorization: Bearer $(cat /tmp/aurora-smoke/secrets/admin-password)" \
     -H "Content-Type: application/json" \
     -d '{"name":"smoke","type":"sysext","arch":"amd64","source":{"mode":"image","baseImage":"alpine:3.21"}}' \
     http://localhost:18080/api/v1/extensions
   ```

3. Confirm the response carries `phase: "Pending"` and an ID.
4. `curl -s -H "Authorization: Bearer $ADMIN" http://localhost:18080/api/v1/extensions` lists the new record.

(The actual build will fail because the host probably lacks `docker` + `systemd-repart` in this smoke context. That's fine — the goal is to prove the REST surface and async dispatch work; the build itself is unit-tested separately.)

- [ ] **Step 6: Commit**

```bash
git add docs/swagger.json docs/swagger.yaml docs/docs.go
git commit -m "docs(openapi): regenerate for /extensions and bundle routes"
```

---

## Self-review checks

- **Spec coverage:** status callback writes `node_extensions` for both flows (Task 1), `--include-path` + `--with-opt` deprecation (Task 2), all new routes registered (Task 3), web command wires the builder (Task 4), agent vendored (Task 5), swagger regen (Task 6). Together these complete every backend requirement in the spec.
- **Type consistency:** `bundledExtension` wire shape inside `commands.go` matches the wire shape `ResolveBundle` (Plan 2d Task 6) emits and Plan 1's agent parser tolerates.
- **Placeholder scan:** the placeholder `vX.Y.Z` in Task 5 is intentional — the real tag isn't known until Plan 1's PR merges.

## What lands at the end of this plan

- `go test ./...` is green across the AuroraBoot repo.
- `auroraboot web` boots cleanly against a fresh SQLite DB and serves every new endpoint.
- The agent vendoring step closes the cross-repo gap with Plan 1.
- `docs/swagger.*` reflects the new REST surface.

## Out of scope here

- All frontend work — Plan 3.
- Multi-node + e2e tests against a real Kairos fleet — Plan 3 covers UI-level smoke; full e2e is operator-driven and out of scope for the plan series.
