package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/kairos-io/AuroraBoot/pkg/builder"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
)

// extensionDownloadTokenBytes sizes the per-extension download bearer. 32
// bytes of crypto/rand is the same budget as the artifact upload token.
const extensionDownloadTokenBytes = 32

// mintExtensionDownloadToken returns the bearer that authorizes downloading
// one extension's .raw. See store.ExtensionRecord.DownloadToken for why the
// install command's source URL carries this instead of the admin password.
func mintExtensionDownloadToken() (string, error) {
	b := make([]byte, extensionDownloadTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("mint extension download token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// ExtensionHandler exposes the REST surface for sysext/confext extension
// builds: create, get, list, patch, delete, logs, cancel, download.
type ExtensionHandler struct {
	builder        builder.ExtensionBuilder
	store          store.ExtensionStore
	bundles        store.ArtifactExtensionBundleStore
	secureBootKeys store.SecureBootKeySetStore
	nodeExtensions store.NodeExtensionStore
	artifactsDir   string
}

// NewExtensionHandler constructs a handler. Any of the dependencies may be
// nil to opt out of the corresponding behaviour (e.g. pass nil for
// secureBootKeys when signing isn't configured). The builder is wrapped so
// every request's source image is validated before any backend sees it.
func NewExtensionHandler(
	b builder.ExtensionBuilder,
	s store.ExtensionStore,
	bs store.ArtifactExtensionBundleStore,
	sb store.SecureBootKeySetStore,
	nodeExtensions store.NodeExtensionStore,
	artifactsDir string,
) *ExtensionHandler {
	return &ExtensionHandler{
		builder:        builder.NewValidatingExtensionBuilder(b),
		store:          s,
		bundles:        bs,
		secureBootKeys: sb,
		nodeExtensions: nodeExtensions,
		artifactsDir:   artifactsDir,
	}
}

// createExtensionRequest is the JSON shape POSTed to /api/v1/extensions.
// Mirror of the Extensions builder Step 1-3 wizard payload.
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
//	@Failure		413		{object}	APIError
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

	if err := validateExtensionRequest(req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	// Both fields are sysext-only, and the CLI enforces that: `auroraboot
	// confext` declares no --include-path or --service-reload flag, so
	// forwarding either one ends the build in phase Error with
	// "flag provided but not defined". Reject them here instead, so an API
	// caller gets the reason rather than a failed build. The UI already only
	// sends them for sysext, so this is reachable via REST and swagger only.
	if req.Type == "confext" {
		if len(req.Hierarchies) > 0 {
			return c.JSON(http.StatusBadRequest, map[string]string{
				"error": "hierarchies are sysext-only; /etc is the implicit confext hierarchy",
			})
		}
		if req.ServiceReload {
			return c.JSON(http.StatusBadRequest, map[string]string{
				"error": "serviceReload is sysext-only",
			})
		}
	}

	normalized, err := validateHierarchies(req.Hierarchies)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	req.Hierarchies = normalized

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

	downloadToken, err := mintExtensionDownloadToken()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to start build"})
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
		},
		Signing:       signing,
		Hierarchies:   req.Hierarchies,
		ServiceReload: req.ServiceReload,
		// Passed to the builder rather than attached to the row afterwards.
		// Build persists the record and only then starts its goroutine, whose
		// first act is a full-row upsert of the phase; a second
		// read-modify-write from here races that write and can drop the
		// keyset linkage permanently.
		SigningKeySetID: req.SigningKeySetID,
		// Minted here, and carried in the record from the first write, so the
		// install command's source URL never needs the admin password.
		DownloadToken: downloadToken,
	}

	status, err := h.builder.Build(ctx, opts)
	if err != nil {
		if errors.Is(err, builder.ErrInvalidBuildOptions) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to start build"})
	}
	return c.JSON(http.StatusCreated, status)
}

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

// validateHierarchies enforces the spec rules and returns a normalized list:
// trailing slashes stripped, duplicates removed, alphabetically sorted.
// The returned list is what the builder + store should see. Nil input
// produces a nil output (no normalization for an unset field).
func validateHierarchies(in []string) ([]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
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

// Get handles GET /api/v1/extensions/:id.
func (h *ExtensionHandler) Get(c echo.Context) error {
	rec, err := h.store.GetByID(c.Request().Context(), c.Param("id"))
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
// Renaming is blocked with 409 when any artifact bundle references the
// current name, same as Delete.
func (h *ExtensionHandler) Update(c echo.Context) error {
	var patch extensionPatch
	if err := c.Bind(&patch); err != nil || patch.Name == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "name is required"})
	}
	ctx := c.Request().Context()
	rec, err := h.store.GetByID(ctx, c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
	}
	if patch.Name != rec.Name && h.bundles != nil {
		artifacts, err := h.bundles.ArtifactsReferencingExtension(ctx, rec.Name)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "bundle lookup failed"})
		}
		if len(artifacts) > 0 {
			return c.JSON(http.StatusConflict, map[string]any{
				"error":     "extension is referenced by one or more artifact bundles and cannot be renamed",
				"artifacts": artifacts,
			})
		}
	}
	rec.Name = patch.Name
	if err := h.store.Create(ctx, rec); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "save failed"})
	}
	return c.JSON(http.StatusOK, rec)
}

// GetLogs handles GET /api/v1/extensions/:id/logs.
func (h *ExtensionHandler) GetLogs(c echo.Context) error {
	rec, err := h.store.GetByID(c.Request().Context(), c.Param("id"))
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

// Delete handles DELETE /api/v1/extensions/:id. Blocked with 409 when any
// artifact bundle references the extension by name; the operator must
// remove the bundle entry first.
func (h *ExtensionHandler) Delete(c echo.Context) error {
	ctx := c.Request().Context()
	rec, err := h.store.GetByID(ctx, c.Param("id"))
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
	// Remove the build output after the record, so a download racing the
	// delete either finds the record gone or the file gone.
	if h.artifactsDir != "" && isSafePathSegment(rec.ID) {
		if err := os.RemoveAll(filepath.Join(h.artifactsDir, "extensions", rec.ID)); err != nil {
			fmt.Fprintf(os.Stderr, "extension delete: remove output of %q failed: %v\n", rec.ID, err)
		}
	}
	return c.NoContent(http.StatusNoContent)
}

// Download handles GET /api/v1/extensions/:id/download/:filename. Auth is
// applied by the router's download middleware (admin credential, node API
// key from the Authorization header, or the extension's own download token
// in ?token=). This handler enforces path-traversal safety, returns 404 when
// the extension record no longer exists, and streams the file.
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
	if _, err := h.store.GetByID(c.Request().Context(), id); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
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

// ListNodeExtensions handles GET /api/v1/nodes/:nodeID/extensions. Returns
// the per-node tracking rows the agent populates via the status callback.
//
//	@Summary	List extensions installed on a node
//	@Tags		Extensions
//	@Produce	json
//	@Security	AdminBearer
//	@Param		nodeID	path	string	true	"Node ID"
//	@Success	200		{array}	store.NodeExtensionRow
//	@Router		/api/v1/nodes/{nodeID}/extensions [get]
func (h *ExtensionHandler) ListNodeExtensions(c echo.Context) error {
	if h.nodeExtensions == nil {
		return c.JSON(http.StatusOK, []store.NodeExtensionRow{})
	}
	rows, err := h.nodeExtensions.ListForNode(c.Request().Context(), c.Param("nodeID"))
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "list failed"})
	}
	if rows == nil {
		rows = []store.NodeExtensionRow{}
	}
	return c.JSON(http.StatusOK, rows)
}

// ListNodesForExtension handles GET /api/v1/extensions/:id/nodes. Returns
// every node tracking row that references the extension by name.
//
//	@Summary	List nodes that have a given extension installed
//	@Tags		Extensions
//	@Produce	json
//	@Security	AdminBearer
//	@Param		id	path	string	true	"Extension ID"
//	@Success	200	{array}	store.NodeExtensionRow
//	@Router		/api/v1/extensions/{id}/nodes [get]
func (h *ExtensionHandler) ListNodesForExtension(c echo.Context) error {
	if h.nodeExtensions == nil || h.store == nil {
		return c.JSON(http.StatusOK, []store.NodeExtensionRow{})
	}
	rec, err := h.store.GetByID(c.Request().Context(), c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
	}
	rows, err := h.nodeExtensions.ListForExtensionByName(c.Request().Context(), rec.Type, rec.Name)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "list failed"})
	}
	if rows == nil {
		rows = []store.NodeExtensionRow{}
	}
	return c.JSON(http.StatusOK, rows)
}

// isSafePathSegment rejects empty, `.`/`..`, `/`-containing, or NUL-containing
// segments. A NUL byte can arrive as an escaped "%00" and would make os.Open fail
// with an error other than "not exist".
func isSafePathSegment(s string) bool {
	if s == "" || s == "." || s == ".." {
		return false
	}
	if strings.ContainsAny(s, "/\\\x00") {
		return false
	}
	if strings.Contains(s, "..") {
		return false
	}
	return true
}
