# Extensions — Plan 2a of 3: AuroraBoot backend data model & stores

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land all DB tables, GORM models, and store interfaces/adapters that the rest of the AuroraBoot backend will consume. Each model auto-migrates on a fresh DB; each store interface ships with a GORM adapter and CRUD specs.

**Architecture:** GORM structs live in `pkg/store/store.go` next to the existing `ArtifactRecord`. Store interfaces live in the same file. GORM adapters live in `internal/store/gorm/adapters.go`. `Store.AutoMigrate` (in `internal/store/gorm/store.go`) is extended to include the new tables. All new code is testable through the existing `internal/store/gorm/store_test.go` patterns (SQLite in-memory).

**Tech Stack:** Go 1.23+, GORM, ginkgo v2 / gomega, SQLite for tests.

**Repository:** `/home/mudler/_git/AuroraBoot`

**Prerequisites:** none — this plan can land before Plan 1 (kairos-agent) ships.

---

## Reference — read these before starting

| File | What it does |
|---|---|
| `pkg/store/store.go:67-74` | Existing command-type constants. `extension` joins this list. |
| `pkg/store/store.go:123-158` | `ArtifactRecord` — model to extend with `ExtensionHierarchies`. |
| `pkg/store/store.go:181-189` | `SecureBootKeySet` shape (for cross-reference; not modified here). |
| `internal/store/gorm/store.go:1-50` | `Store` struct, `New`, `AutoMigrate` line that lists all current models. |
| `internal/store/gorm/adapters.go` | Existing adapters — copy the pattern. |
| `internal/store/gorm/store_test.go` | Ginkgo SQLite test patterns. |
| `internal/store/gorm/suite_test.go` | The ginkgo entry point for the package. |

---

## Task 1: Add `extension` to the command-type constants

**Files:**
- Modify: `pkg/store/store.go` (around lines 67-74)
- Modify: `pkg/store/store_test.go` (if it exists; otherwise create — check first)

- [ ] **Step 1: Inspect the existing constants block**

```
grep -n "CommandTypeUpgrade\|CommandType\|cmd.*=.*\"upgrade\"" pkg/store/store.go
```

Expected: a const block defining string constants like `CommandTypeUpgrade = "upgrade"`, `CommandTypeReset = "reset"`, etc. Confirm naming convention before adding.

- [ ] **Step 2: Write the failing spec**

If `pkg/store/store_test.go` exists, append to its `Describe("command type constants", …)` block. Otherwise create `pkg/store/store_test.go`:

```go
package store_test

import (
	"github.com/kairos-io/AuroraBoot/pkg/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("command type constants", func() {
	It("includes extension", func() {
		Expect(store.CommandTypeExtension).To(Equal("extension"))
	})
})
```

If no `suite_test.go` exists in `pkg/store`, create one:

```go
package store_test

import (
	"testing"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestStore(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "store suite")
}
```

- [ ] **Step 3: Run the spec, verify it fails**

```
cd ~/_git/AuroraBoot && go test ./pkg/store/... -v
```

Expected: BUILD FAIL — `CommandTypeExtension` undefined.

- [ ] **Step 4: Add the constant**

In `pkg/store/store.go`, append to the const block that holds the existing command types:

```go
	CommandTypeExtension       = "extension"
```

(Match the existing naming convention exactly. If the block uses lowercase string literals only — i.e. no exported constants — add the constant anyway at the end of that block; existing handler code may reference unexported strings but new code referencing the constant is preferable.)

- [ ] **Step 5: Run the spec, verify it passes**

```
cd ~/_git/AuroraBoot && go test ./pkg/store/... -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/store/store.go pkg/store/store_test.go pkg/store/suite_test.go 2>/dev/null
git commit -m "store: add CommandTypeExtension constant"
```

---

## Task 2: Add `ExtensionHierarchies` column to `ArtifactRecord`

**Files:**
- Modify: `pkg/store/store.go` (`ArtifactRecord` struct around L123-158)
- Modify: `internal/store/gorm/store_test.go`

JSON-serialized field. Existing schema uses GORM's `serializer:json` tag for similar use cases (see `ArtifactFiles` and `Labels`).

- [ ] **Step 1: Write the failing spec**

Append to `internal/store/gorm/store_test.go`:

```go
var _ = Describe("ArtifactRecord.ExtensionHierarchies", func() {
	It("persists and reloads the hierarchies map", func() {
		s := newTestStore() // existing helper that opens :memory: sqlite + auto-migrates
		ctx := context.Background()
		rec := &store.ArtifactRecord{
			ID:    "a-1",
			Phase: "Ready",
			ExtensionHierarchies: store.ExtensionHierarchies{
				Sysext:  []string{"/opt", "/srv"},
				Confext: []string{},
			},
		}
		Expect(s.Artifacts().Save(ctx, rec)).To(Succeed())
		got, err := s.Artifacts().Get(ctx, "a-1")
		Expect(err).ToNot(HaveOccurred())
		Expect(got.ExtensionHierarchies.Sysext).To(Equal([]string{"/opt", "/srv"}))
		Expect(got.ExtensionHierarchies.Confext).To(BeEmpty())
	})

	It("defaults to a zero value for legacy rows", func() {
		s := newTestStore()
		ctx := context.Background()
		Expect(s.Artifacts().Save(ctx, &store.ArtifactRecord{ID: "a-2", Phase: "Ready"})).To(Succeed())
		got, _ := s.Artifacts().Get(ctx, "a-2")
		Expect(got.ExtensionHierarchies.Sysext).To(BeNil())
		Expect(got.ExtensionHierarchies.Confext).To(BeNil())
	})
})
```

If `newTestStore` doesn't already exist in `store_test.go`, confirm the equivalent fixture (e.g. an inline `gorm.New(":memory:")`) used by neighbouring specs and use that instead.

- [ ] **Step 2: Run the spec, verify it fails**

```
cd ~/_git/AuroraBoot && go test ./internal/store/gorm/... -v
```

Expected: BUILD FAIL — `ExtensionHierarchies` type and field undefined.

- [ ] **Step 3: Add the type and field**

In `pkg/store/store.go`, near the existing `ArtifactRecord` (above is fine), add:

```go
// ExtensionHierarchies records the SYSTEMD_{SYSEXT,CONFEXT}_HIERARCHIES paths
// declared at artifact build time so the Extensions UI can cross-check what
// scopes an OS image supports. /usr (sysext) and /etc (confext) are implicit
// and never stored in either slice.
type ExtensionHierarchies struct {
	Sysext  []string `json:"sysext"`
	Confext []string `json:"confext"`
}
```

Then, inside `ArtifactRecord`, add the column:

```go
	ExtensionHierarchies ExtensionHierarchies `gorm:"serializer:json" json:"extensionHierarchies"`
```

Place it near the other `serializer:json` fields (e.g. `ArtifactFiles`) so the file stays scannable.

- [ ] **Step 4: Run the specs**

```
cd ~/_git/AuroraBoot && go test ./internal/store/gorm/... -v
```

Expected: PASS, including any pre-existing artifact specs.

- [ ] **Step 5: Commit**

```bash
git add pkg/store/store.go internal/store/gorm/store_test.go
git commit -m "store: add ExtensionHierarchies column to ArtifactRecord"
```

---

## Task 3: `ExtensionRecord` GORM model + AutoMigrate

**Files:**
- Modify: `pkg/store/store.go`
- Modify: `internal/store/gorm/store.go` (extend `AutoMigrate`)
- Modify: `internal/store/gorm/store_test.go`

- [ ] **Step 1: Write the failing spec**

Append to `internal/store/gorm/store_test.go`:

```go
var _ = Describe("ExtensionRecord schema", func() {
	It("creates the extensions table on AutoMigrate", func() {
		s := newTestStore()
		var count int64
		// Use GORM's underlying DB to count rows — table existence is implied.
		Expect(s.UnsafeDB().Model(&store.ExtensionRecord{}).Count(&count).Error).To(Succeed())
		Expect(count).To(BeZero())
	})

	It("round-trips Hierarchies", func() {
		s := newTestStore()
		rec := &store.ExtensionRecord{
			ID:          "e-1",
			Name:        "tailscale-agent",
			Type:        "sysext",
			Phase:       "Ready",
			Arch:        "amd64",
			Version:     "v1.74.0",
			SourceMode:  "image",
			SourceImage: "quay.io/myorg/tailscale:1.74",
			Hierarchies: []string{"/opt", "/srv"},
		}
		Expect(s.UnsafeDB().Create(rec).Error).To(Succeed())
		var got store.ExtensionRecord
		Expect(s.UnsafeDB().First(&got, "id = ?", "e-1").Error).To(Succeed())
		Expect(got.Hierarchies).To(Equal([]string{"/opt", "/srv"}))
	})
})
```

If `s.UnsafeDB()` doesn't exist on `Store`, add a small test-only helper in `internal/store/gorm/export_test.go`:

```go
package gorm

import "gorm.io/gorm"

// UnsafeDB exposes the GORM handle for tests that need to drive the
// database directly. Production code must use the store interfaces.
func (s *Store) UnsafeDB() *gorm.DB { return s.db }
```

- [ ] **Step 2: Run the specs, verify they fail**

```
cd ~/_git/AuroraBoot && go test ./internal/store/gorm/... -v
```

Expected: BUILD FAIL — `ExtensionRecord` undefined.

- [ ] **Step 3: Add the model**

In `pkg/store/store.go`, add near the other records:

```go
// ExtensionRecord is one sysext or confext build managed by AuroraBoot.
// .raw output lives at <artifactsDir>/extensions/<ID>/<Name>.<Type>.raw.
type ExtensionRecord struct {
	ID    string `gorm:"primaryKey" json:"id"`
	Name  string `gorm:"index"      json:"name"`
	Type  string `                  json:"type"`  // "sysext" | "confext"
	Phase string `                  json:"phase"` // Pending | Building | Ready | Error
	Message string `                json:"message"`

	Arch    string `json:"arch"`
	Version string `json:"version"`

	SourceMode       string `json:"sourceMode"`     // artifact | image | dockerfile
	SourceArtifactID string `json:"sourceArtifactId"`
	SourceImage      string `json:"sourceImage"`
	Dockerfile       string `gorm:"type:text" json:"dockerfile,omitempty"`
	ExtraSteps       string `gorm:"type:text" json:"extraSteps,omitempty"`

	SigningKeySetID string   `json:"signingKeySetId"`
	Hierarchies     []string `gorm:"serializer:json" json:"hierarchies"`
	ServiceReload   bool     `json:"serviceReload"`

	ContainerImage string `json:"containerImage"`
	RawFilename    string `json:"rawFilename"`

	Logs string `gorm:"type:text" json:"-"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
```

(Ensure `time` is imported.)

- [ ] **Step 4: Extend `AutoMigrate`**

In `internal/store/gorm/store.go`, find the existing `db.AutoMigrate(...)` call and append `&store.ExtensionRecord{}` to its arg list, preserving line layout.

- [ ] **Step 5: Run the specs**

```
cd ~/_git/AuroraBoot && go test ./internal/store/gorm/... -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/store/store.go internal/store/gorm/store.go internal/store/gorm/store_test.go internal/store/gorm/export_test.go 2>/dev/null
git commit -m "store: add ExtensionRecord model + AutoMigrate"
```

---

## Task 4: `ArtifactExtensionBundle` model + AutoMigrate

**Files:**
- Modify: `pkg/store/store.go`
- Modify: `internal/store/gorm/store.go`
- Modify: `internal/store/gorm/store_test.go`

Bundle entries are keyed by `(ArtifactID, ExtensionName)` — surviving rebuilds of the underlying extension. `PinnedVersion` is nullable.

- [ ] **Step 1: Write the failing spec**

Append to `internal/store/gorm/store_test.go`:

```go
var _ = Describe("ArtifactExtensionBundle schema", func() {
	It("creates the table and round-trips an entry", func() {
		s := newTestStore()
		Expect(s.UnsafeDB().Create(&store.ArtifactExtensionBundle{
			ArtifactID:     "a-1",
			ExtensionName:  "tailscale-agent",
			ExtensionType:  "sysext",
			PinnedVersion:  "",
			Order:          0,
		}).Error).To(Succeed())
		var got store.ArtifactExtensionBundle
		Expect(s.UnsafeDB().First(&got, "artifact_id = ? AND extension_name = ?", "a-1", "tailscale-agent").Error).To(Succeed())
		Expect(got.ExtensionType).To(Equal("sysext"))
	})

	It("rejects duplicate (artifact, name) pairs", func() {
		s := newTestStore()
		row := store.ArtifactExtensionBundle{ArtifactID: "a-1", ExtensionName: "x", ExtensionType: "sysext"}
		Expect(s.UnsafeDB().Create(&row).Error).To(Succeed())
		Expect(s.UnsafeDB().Create(&row).Error).To(HaveOccurred())
	})
})
```

- [ ] **Step 2: Verify the spec fails**

```
cd ~/_git/AuroraBoot && go test ./internal/store/gorm/... -v
```

Expected: BUILD FAIL — `ArtifactExtensionBundle` undefined.

- [ ] **Step 3: Add the model**

In `pkg/store/store.go`:

```go
// ArtifactExtensionBundle links an artifact to an extension that should ride
// with every upgrade to that artifact. Entries are by (ArtifactID,
// ExtensionName) — the actual extension UUID is resolved at dispatch time so
// the bundle survives rebuilds of the named extension.
type ArtifactExtensionBundle struct {
	ArtifactID    string `gorm:"primaryKey" json:"artifactId"`
	ExtensionName string `gorm:"primaryKey" json:"extensionName"`
	ExtensionType string `                  json:"extensionType"` // sysext | confext
	PinnedVersion string `                  json:"pinnedVersion,omitempty"`
	Order         int    `                  json:"order"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
```

- [ ] **Step 4: Extend AutoMigrate**

Append `&store.ArtifactExtensionBundle{}` to the `db.AutoMigrate(...)` list in `internal/store/gorm/store.go`.

- [ ] **Step 5: Verify the specs pass**

```
cd ~/_git/AuroraBoot && go test ./internal/store/gorm/... -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/store/store.go internal/store/gorm/store.go internal/store/gorm/store_test.go
git commit -m "store: add ArtifactExtensionBundle model + AutoMigrate"
```

---

## Task 5: `NodeExtensionRow` model + AutoMigrate

**Files:**
- Modify: `pkg/store/store.go`
- Modify: `internal/store/gorm/store.go`
- Modify: `internal/store/gorm/store_test.go`

Tracks what's actually installed on each node, keyed by `(NodeID, Name, Type, BootState)`.

- [ ] **Step 1: Write the failing spec**

Append to `internal/store/gorm/store_test.go`:

```go
var _ = Describe("NodeExtensionRow schema", func() {
	It("round-trips a row", func() {
		s := newTestStore()
		row := store.NodeExtensionRow{
			NodeID:       "n-1",
			Name:         "tailscale-agent",
			Type:         "sysext",
			Version:      "v1.74.0",
			BootState:    "common",
			InstalledAt:  time.Now().UTC().Truncate(time.Second),
			ExtensionID:  "e-1",
		}
		Expect(s.UnsafeDB().Create(&row).Error).To(Succeed())
		var got store.NodeExtensionRow
		Expect(s.UnsafeDB().First(&got, "node_id = ? AND name = ? AND type = ? AND boot_state = ?",
			"n-1", "tailscale-agent", "sysext", "common").Error).To(Succeed())
		Expect(got.Version).To(Equal("v1.74.0"))
	})

	It("rejects duplicate composite keys", func() {
		s := newTestStore()
		row := store.NodeExtensionRow{NodeID: "n-1", Name: "x", Type: "sysext", BootState: "common"}
		Expect(s.UnsafeDB().Create(&row).Error).To(Succeed())
		Expect(s.UnsafeDB().Create(&row).Error).To(HaveOccurred())
	})
})
```

- [ ] **Step 2: Verify the spec fails**

```
cd ~/_git/AuroraBoot && go test ./internal/store/gorm/... -v
```

Expected: BUILD FAIL — `NodeExtensionRow` undefined.

- [ ] **Step 3: Add the model**

In `pkg/store/store.go`:

```go
// NodeExtensionRow is the per-node tracking that drives the Install dialog's
// pre-action diff and the node detail page's "Installed extensions" section.
// The agent's REST status callback writes/deletes rows on each successful
// install / disable / remove.
type NodeExtensionRow struct {
	NodeID      string    `gorm:"primaryKey" json:"nodeId"`
	Name        string    `gorm:"primaryKey" json:"name"`
	Type        string    `gorm:"primaryKey" json:"type"`      // sysext | confext
	BootState   string    `gorm:"primaryKey" json:"bootState"` // active | passive | recovery | common
	ExtensionID string    `                  json:"extensionId,omitempty"` // best-effort link
	Version     string    `                  json:"version"`
	InstalledAt time.Time `                  json:"installedAt"`
	UpdatedAt   time.Time `                  json:"updatedAt"`
}
```

- [ ] **Step 4: Extend AutoMigrate**

Append `&store.NodeExtensionRow{}` to the `db.AutoMigrate(...)` list.

- [ ] **Step 5: Verify the specs pass**

```
cd ~/_git/AuroraBoot && go test ./internal/store/gorm/... -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/store/store.go internal/store/gorm/store.go internal/store/gorm/store_test.go
git commit -m "store: add NodeExtensionRow model + AutoMigrate"
```

---

## Task 6: `ExtensionStore` interface + GORM adapter

**Files:**
- Modify: `pkg/store/store.go` (interface)
- Modify: `internal/store/gorm/adapters.go` (adapter)
- Modify: `internal/store/gorm/store.go` (accessor)
- Modify: `internal/store/gorm/store_test.go`

- [ ] **Step 1: Inspect the existing adapter pattern**

```
grep -n "ArtifactStoreAdapter\|func.*ArtifactStore" internal/store/gorm/adapters.go pkg/store/store.go | head -20
```

Expected: `ArtifactStoreAdapter` exists in `adapters.go` with methods `Save`, `Get`, `List`, `Delete`. Mirror this shape.

- [ ] **Step 2: Write the failing spec**

Append to `internal/store/gorm/store_test.go`:

```go
var _ = Describe("ExtensionStoreAdapter", func() {
	var (
		s   *Store
		ctx = context.Background()
	)
	BeforeEach(func() { s = newTestStore() })

	It("Save / Get round-trips", func() {
		ext := &store.ExtensionRecord{ID: "e-1", Name: "tailscale-agent", Type: "sysext", Phase: "Ready", Arch: "amd64", Version: "v1.74.0"}
		Expect(s.Extensions().Save(ctx, ext)).To(Succeed())
		got, err := s.Extensions().Get(ctx, "e-1")
		Expect(err).ToNot(HaveOccurred())
		Expect(got.Name).To(Equal("tailscale-agent"))
	})

	It("List orders by created_at descending", func() {
		Expect(s.Extensions().Save(ctx, &store.ExtensionRecord{ID: "old", Name: "x", Type: "sysext", Phase: "Ready", CreatedAt: time.Now().Add(-1 * time.Hour)})).To(Succeed())
		Expect(s.Extensions().Save(ctx, &store.ExtensionRecord{ID: "new", Name: "y", Type: "sysext", Phase: "Ready", CreatedAt: time.Now()})).To(Succeed())
		list, err := s.Extensions().List(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(list[0].ID).To(Equal("new"))
		Expect(list[1].ID).To(Equal("old"))
	})

	It("Delete removes one record", func() {
		Expect(s.Extensions().Save(ctx, &store.ExtensionRecord{ID: "e-1", Name: "x", Type: "sysext", Phase: "Ready"})).To(Succeed())
		Expect(s.Extensions().Delete(ctx, "e-1")).To(Succeed())
		_, err := s.Extensions().Get(ctx, "e-1")
		Expect(err).To(HaveOccurred())
	})

	It("FindLatestReadyByName returns the newest Ready row with that name", func() {
		old := &store.ExtensionRecord{ID: "old", Name: "tailscale-agent", Type: "sysext", Version: "v1.72", Phase: "Ready", CreatedAt: time.Now().Add(-1 * time.Hour)}
		new := &store.ExtensionRecord{ID: "new", Name: "tailscale-agent", Type: "sysext", Version: "v1.74", Phase: "Ready", CreatedAt: time.Now()}
		err := &store.ExtensionRecord{ID: "err", Name: "tailscale-agent", Type: "sysext", Version: "v2.0", Phase: "Error", CreatedAt: time.Now().Add(1 * time.Hour)}
		for _, r := range []*store.ExtensionRecord{old, new, err} {
			Expect(s.Extensions().Save(ctx, r)).To(Succeed())
		}
		got, derr := s.Extensions().FindLatestReadyByName(ctx, "sysext", "tailscale-agent")
		Expect(derr).ToNot(HaveOccurred())
		Expect(got.ID).To(Equal("new"))
	})

	It("FindByNameAndVersion returns an exact match", func() {
		Expect(s.Extensions().Save(ctx, &store.ExtensionRecord{ID: "v74", Name: "ts", Type: "sysext", Version: "v1.74", Phase: "Ready"})).To(Succeed())
		got, err := s.Extensions().FindByNameAndVersion(ctx, "sysext", "ts", "v1.74")
		Expect(err).ToNot(HaveOccurred())
		Expect(got.ID).To(Equal("v74"))
	})
})
```

- [ ] **Step 3: Verify it fails**

```
cd ~/_git/AuroraBoot && go test ./internal/store/gorm/... -v
```

Expected: BUILD FAIL.

- [ ] **Step 4: Add the interface**

In `pkg/store/store.go`, near the other store interfaces:

```go
type ExtensionStore interface {
	Save(ctx context.Context, e *ExtensionRecord) error
	Get(ctx context.Context, id string) (*ExtensionRecord, error)
	List(ctx context.Context) ([]ExtensionRecord, error)
	Delete(ctx context.Context, id string) error
	FindLatestReadyByName(ctx context.Context, extType, name string) (*ExtensionRecord, error)
	FindByNameAndVersion(ctx context.Context, extType, name, version string) (*ExtensionRecord, error)
}
```

- [ ] **Step 5: Add the adapter**

In `internal/store/gorm/adapters.go`:

```go
type ExtensionStoreAdapter struct{ S *Store }

func (a *ExtensionStoreAdapter) Save(ctx context.Context, e *store.ExtensionRecord) error {
	return a.S.db.WithContext(ctx).Save(e).Error
}
func (a *ExtensionStoreAdapter) Get(ctx context.Context, id string) (*store.ExtensionRecord, error) {
	var rec store.ExtensionRecord
	if err := a.S.db.WithContext(ctx).First(&rec, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &rec, nil
}
func (a *ExtensionStoreAdapter) List(ctx context.Context) ([]store.ExtensionRecord, error) {
	var out []store.ExtensionRecord
	if err := a.S.db.WithContext(ctx).Order("created_at DESC").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}
func (a *ExtensionStoreAdapter) Delete(ctx context.Context, id string) error {
	return a.S.db.WithContext(ctx).Delete(&store.ExtensionRecord{}, "id = ?", id).Error
}
func (a *ExtensionStoreAdapter) FindLatestReadyByName(ctx context.Context, extType, name string) (*store.ExtensionRecord, error) {
	var rec store.ExtensionRecord
	q := a.S.db.WithContext(ctx).
		Where("type = ? AND name = ? AND phase = ?", extType, name, "Ready").
		Order("created_at DESC").Limit(1)
	if err := q.First(&rec).Error; err != nil {
		return nil, err
	}
	return &rec, nil
}
func (a *ExtensionStoreAdapter) FindByNameAndVersion(ctx context.Context, extType, name, version string) (*store.ExtensionRecord, error) {
	var rec store.ExtensionRecord
	q := a.S.db.WithContext(ctx).
		Where("type = ? AND name = ? AND version = ? AND phase = ?", extType, name, version, "Ready").
		Limit(1)
	if err := q.First(&rec).Error; err != nil {
		return nil, err
	}
	return &rec, nil
}
```

- [ ] **Step 6: Add the `Extensions()` accessor on `Store`**

In `internal/store/gorm/store.go`, near similar accessors:

```go
func (s *Store) Extensions() *ExtensionStoreAdapter { return &ExtensionStoreAdapter{S: s} }
```

(If the existing pattern is method-free direct field access, mirror that instead — check how `s.Artifacts()` is invoked elsewhere.)

- [ ] **Step 7: Verify the specs pass**

```
cd ~/_git/AuroraBoot && go test ./internal/store/gorm/... -v
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add pkg/store/store.go internal/store/gorm/adapters.go internal/store/gorm/store.go internal/store/gorm/store_test.go
git commit -m "store: ExtensionStore interface + GORM adapter"
```

---

## Task 7: `ArtifactExtensionBundleStore` interface + adapter

**Files:**
- Modify: `pkg/store/store.go`
- Modify: `internal/store/gorm/adapters.go`
- Modify: `internal/store/gorm/store.go`
- Modify: `internal/store/gorm/store_test.go`

- [ ] **Step 1: Write the failing spec**

Append to `internal/store/gorm/store_test.go`:

```go
var _ = Describe("ArtifactExtensionBundleStoreAdapter", func() {
	var (
		s   *Store
		ctx = context.Background()
	)
	BeforeEach(func() { s = newTestStore() })

	It("ReplaceForArtifact replaces the entire set atomically", func() {
		Expect(s.Bundles().ReplaceForArtifact(ctx, "a-1", []store.ArtifactExtensionBundle{
			{ArtifactID: "a-1", ExtensionName: "tailscale", ExtensionType: "sysext", Order: 0},
			{ArtifactID: "a-1", ExtensionName: "fluent-bit", ExtensionType: "confext", Order: 1},
		})).To(Succeed())

		got, err := s.Bundles().ListForArtifact(ctx, "a-1")
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(HaveLen(2))

		// Replace with just one entry; the other should be dropped.
		Expect(s.Bundles().ReplaceForArtifact(ctx, "a-1", []store.ArtifactExtensionBundle{
			{ArtifactID: "a-1", ExtensionName: "tailscale", ExtensionType: "sysext"},
		})).To(Succeed())
		got, _ = s.Bundles().ListForArtifact(ctx, "a-1")
		Expect(got).To(HaveLen(1))
		Expect(got[0].ExtensionName).To(Equal("tailscale"))
	})

	It("ArtifactsReferencingExtension lists artifacts that bundle a given name", func() {
		Expect(s.Bundles().ReplaceForArtifact(ctx, "a-1", []store.ArtifactExtensionBundle{
			{ArtifactID: "a-1", ExtensionName: "tailscale", ExtensionType: "sysext"},
		})).To(Succeed())
		Expect(s.Bundles().ReplaceForArtifact(ctx, "a-2", []store.ArtifactExtensionBundle{
			{ArtifactID: "a-2", ExtensionName: "tailscale", ExtensionType: "sysext"},
			{ArtifactID: "a-2", ExtensionName: "fluent-bit", ExtensionType: "confext"},
		})).To(Succeed())

		refs, err := s.Bundles().ArtifactsReferencingExtension(ctx, "tailscale")
		Expect(err).ToNot(HaveOccurred())
		Expect(refs).To(ConsistOf("a-1", "a-2"))
	})
})
```

- [ ] **Step 2: Verify failure**

```
cd ~/_git/AuroraBoot && go test ./internal/store/gorm/... -v
```

Expected: BUILD FAIL.

- [ ] **Step 3: Add the interface**

In `pkg/store/store.go`:

```go
type ArtifactExtensionBundleStore interface {
	ListForArtifact(ctx context.Context, artifactID string) ([]ArtifactExtensionBundle, error)
	ReplaceForArtifact(ctx context.Context, artifactID string, entries []ArtifactExtensionBundle) error
	ArtifactsReferencingExtension(ctx context.Context, extensionName string) ([]string, error)
}
```

- [ ] **Step 4: Add the adapter**

In `internal/store/gorm/adapters.go`:

```go
type ArtifactExtensionBundleStoreAdapter struct{ S *Store }

func (a *ArtifactExtensionBundleStoreAdapter) ListForArtifact(ctx context.Context, artifactID string) ([]store.ArtifactExtensionBundle, error) {
	var out []store.ArtifactExtensionBundle
	err := a.S.db.WithContext(ctx).
		Where("artifact_id = ?", artifactID).
		Order(`"order" ASC`).
		Find(&out).Error
	return out, err
}

func (a *ArtifactExtensionBundleStoreAdapter) ReplaceForArtifact(ctx context.Context, artifactID string, entries []store.ArtifactExtensionBundle) error {
	return a.S.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("artifact_id = ?", artifactID).Delete(&store.ArtifactExtensionBundle{}).Error; err != nil {
			return err
		}
		if len(entries) == 0 {
			return nil
		}
		for i := range entries {
			entries[i].ArtifactID = artifactID
		}
		return tx.Create(&entries).Error
	})
}

func (a *ArtifactExtensionBundleStoreAdapter) ArtifactsReferencingExtension(ctx context.Context, extensionName string) ([]string, error) {
	var ids []string
	err := a.S.db.WithContext(ctx).
		Model(&store.ArtifactExtensionBundle{}).
		Where("extension_name = ?", extensionName).
		Distinct("artifact_id").
		Pluck("artifact_id", &ids).Error
	return ids, err
}
```

(Ensure `"gorm.io/gorm"` is in the import list at the top of `adapters.go`.)

- [ ] **Step 5: Add the accessor**

In `internal/store/gorm/store.go`:

```go
func (s *Store) Bundles() *ArtifactExtensionBundleStoreAdapter { return &ArtifactExtensionBundleStoreAdapter{S: s} }
```

- [ ] **Step 6: Verify the specs pass**

```
cd ~/_git/AuroraBoot && go test ./internal/store/gorm/... -v
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add pkg/store/store.go internal/store/gorm/adapters.go internal/store/gorm/store.go internal/store/gorm/store_test.go
git commit -m "store: ArtifactExtensionBundleStore interface + adapter"
```

---

## Task 8: `NodeExtensionStore` interface + adapter

**Files:**
- Modify: `pkg/store/store.go`
- Modify: `internal/store/gorm/adapters.go`
- Modify: `internal/store/gorm/store.go`
- Modify: `internal/store/gorm/store_test.go`

- [ ] **Step 1: Write the failing spec**

Append to `internal/store/gorm/store_test.go`:

```go
var _ = Describe("NodeExtensionStoreAdapter", func() {
	var (
		s   *Store
		ctx = context.Background()
	)
	BeforeEach(func() { s = newTestStore() })

	It("Upsert inserts a new row and updates an existing one", func() {
		Expect(s.NodeExtensions().Upsert(ctx, &store.NodeExtensionRow{
			NodeID: "n-1", Name: "ts", Type: "sysext", BootState: "common",
			Version: "v1.72", ExtensionID: "e-old",
		})).To(Succeed())
		Expect(s.NodeExtensions().Upsert(ctx, &store.NodeExtensionRow{
			NodeID: "n-1", Name: "ts", Type: "sysext", BootState: "common",
			Version: "v1.74", ExtensionID: "e-new",
		})).To(Succeed())
		rows, err := s.NodeExtensions().ListForNode(ctx, "n-1")
		Expect(err).ToNot(HaveOccurred())
		Expect(rows).To(HaveLen(1))
		Expect(rows[0].Version).To(Equal("v1.74"))
		Expect(rows[0].ExtensionID).To(Equal("e-new"))
	})

	It("DeleteByName drops all rows for a name on that node", func() {
		_ = s.NodeExtensions().Upsert(ctx, &store.NodeExtensionRow{NodeID: "n-1", Name: "ts", Type: "sysext", BootState: "common"})
		_ = s.NodeExtensions().Upsert(ctx, &store.NodeExtensionRow{NodeID: "n-1", Name: "ts", Type: "sysext", BootState: "active"})
		Expect(s.NodeExtensions().DeleteByName(ctx, "n-1", "sysext", "ts")).To(Succeed())
		rows, _ := s.NodeExtensions().ListForNode(ctx, "n-1")
		Expect(rows).To(BeEmpty())
	})

	It("DeleteByScope drops the row for a specific scope only", func() {
		_ = s.NodeExtensions().Upsert(ctx, &store.NodeExtensionRow{NodeID: "n-1", Name: "ts", Type: "sysext", BootState: "common"})
		_ = s.NodeExtensions().Upsert(ctx, &store.NodeExtensionRow{NodeID: "n-1", Name: "ts", Type: "sysext", BootState: "active"})
		Expect(s.NodeExtensions().DeleteByScope(ctx, "n-1", "sysext", "ts", "active")).To(Succeed())
		rows, _ := s.NodeExtensions().ListForNode(ctx, "n-1")
		Expect(rows).To(HaveLen(1))
		Expect(rows[0].BootState).To(Equal("common"))
	})

	It("ListForExtensionByName aggregates rows across nodes", func() {
		for _, n := range []string{"n-1", "n-2", "n-3"} {
			_ = s.NodeExtensions().Upsert(ctx, &store.NodeExtensionRow{NodeID: n, Name: "ts", Type: "sysext", BootState: "common"})
		}
		rows, err := s.NodeExtensions().ListForExtensionByName(ctx, "sysext", "ts")
		Expect(err).ToNot(HaveOccurred())
		Expect(rows).To(HaveLen(3))
	})
})
```

- [ ] **Step 2: Verify failure**

```
cd ~/_git/AuroraBoot && go test ./internal/store/gorm/... -v
```

Expected: BUILD FAIL.

- [ ] **Step 3: Add the interface**

In `pkg/store/store.go`:

```go
type NodeExtensionStore interface {
	Upsert(ctx context.Context, row *NodeExtensionRow) error
	ListForNode(ctx context.Context, nodeID string) ([]NodeExtensionRow, error)
	ListForExtensionByName(ctx context.Context, extType, name string) ([]NodeExtensionRow, error)
	DeleteByScope(ctx context.Context, nodeID, extType, name, bootState string) error
	DeleteByName(ctx context.Context, nodeID, extType, name string) error
}
```

- [ ] **Step 4: Add the adapter**

In `internal/store/gorm/adapters.go`:

```go
type NodeExtensionStoreAdapter struct{ S *Store }

func (a *NodeExtensionStoreAdapter) Upsert(ctx context.Context, row *store.NodeExtensionRow) error {
	row.UpdatedAt = time.Now().UTC()
	if row.InstalledAt.IsZero() {
		row.InstalledAt = row.UpdatedAt
	}
	return a.S.db.WithContext(ctx).Save(row).Error
}

func (a *NodeExtensionStoreAdapter) ListForNode(ctx context.Context, nodeID string) ([]store.NodeExtensionRow, error) {
	var out []store.NodeExtensionRow
	err := a.S.db.WithContext(ctx).Where("node_id = ?", nodeID).Find(&out).Error
	return out, err
}

func (a *NodeExtensionStoreAdapter) ListForExtensionByName(ctx context.Context, extType, name string) ([]store.NodeExtensionRow, error) {
	var out []store.NodeExtensionRow
	err := a.S.db.WithContext(ctx).Where("type = ? AND name = ?", extType, name).Find(&out).Error
	return out, err
}

func (a *NodeExtensionStoreAdapter) DeleteByScope(ctx context.Context, nodeID, extType, name, bootState string) error {
	return a.S.db.WithContext(ctx).Where(
		"node_id = ? AND type = ? AND name = ? AND boot_state = ?",
		nodeID, extType, name, bootState,
	).Delete(&store.NodeExtensionRow{}).Error
}

func (a *NodeExtensionStoreAdapter) DeleteByName(ctx context.Context, nodeID, extType, name string) error {
	return a.S.db.WithContext(ctx).Where(
		"node_id = ? AND type = ? AND name = ?",
		nodeID, extType, name,
	).Delete(&store.NodeExtensionRow{}).Error
}
```

(Add `"time"` to the imports.)

- [ ] **Step 5: Add the accessor**

In `internal/store/gorm/store.go`:

```go
func (s *Store) NodeExtensions() *NodeExtensionStoreAdapter { return &NodeExtensionStoreAdapter{S: s} }
```

- [ ] **Step 6: Verify the specs pass**

```
cd ~/_git/AuroraBoot && go test ./internal/store/gorm/... -v
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add pkg/store/store.go internal/store/gorm/adapters.go internal/store/gorm/store.go internal/store/gorm/store_test.go
git commit -m "store: NodeExtensionStore interface + adapter"
```

---

## Self-review checks

- **Spec coverage:** data model (extensions, artifact_extension_bundles, node_extensions, artifacts.extension_hierarchies), command-type constant, all three store interfaces — present.
- **Type consistency:** `ExtensionRecord.Hierarchies []string`, `ExtensionHierarchies struct{Sysext, Confext []string}` — distinct types, intentionally. The first is the per-extension list; the second is the per-artifact `{sysext: [], confext: []}` shape.
- **Placeholder scan:** none.

## What lands at the end of this plan

- `go test ./internal/store/gorm/...` is green.
- A fresh `auroraboot.db` opened by `gorm.New(...)` carries the new tables.
- No handler/builder/wire code changes yet — these are isolated to Plans 2b–2e.

## Out of scope here

- Builder interface and implementation → Plan 2b.
- Handlers and validation → Plan 2c.
- Artifact integration (bundles, hierarchies bake) → Plan 2d.
- Status callback, CLI, wiring, vendor → Plan 2e.
