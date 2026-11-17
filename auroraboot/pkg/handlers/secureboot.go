package handlers

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kairos-io/AuroraBoot/pkg/secureboot"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
)

// keySetNameRe is the strict allowlist for key-set names. A name is used
// verbatim as the directory component under keysDir (and is passed to openssl
// as part of -keyout paths), so it must never contain path separators, parent
// references, or anything that could escape the keys directory. Letters,
// digits, dash and underscore only, 1-64 chars.
var keySetNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// validateKeySetName rejects any key-set name that could be used to traverse
// outside keysDir. It intentionally refuses "." and ".." (which the regexp
// already excludes via the missing dot) as well as empty, absolute and
// separator-bearing names.
func validateKeySetName(name string) error {
	if !keySetNameRe.MatchString(name) {
		return fmt.Errorf("invalid key set name %q: must match %s", name, keySetNameRe.String())
	}
	return nil
}

// maxImportSize caps the number of uncompressed bytes we'll extract from an
// imported key set tarball. Real SecureBoot key sets are tiny (a few tens of
// KB), so 16 MiB leaves generous headroom without opening a zip-bomb avenue.
const maxImportSize = 16 * 1024 * 1024

// keySetManifest is the small JSON blob we include at the root of an
// exported tarball so imports can recover the logical metadata that isn't
// derivable from the key files themselves.
type keySetManifest struct {
	Version          int    `json:"version"`
	Kind             string `json:"kind"`
	Name             string `json:"name"`
	SecureBootEnroll string `json:"secureBootEnroll,omitempty"`
}

const keySetManifestKind = "auroraboot.secureboot-keyset"
const keySetManifestName = "manifest.json"

// SecureBootHandler manages SecureBoot key sets.
type SecureBootHandler struct {
	store   store.SecureBootKeySetStore
	keysDir string // base directory where key sets are stored
}

// NewSecureBootHandler creates a new SecureBootHandler.
func NewSecureBootHandler(s store.SecureBootKeySetStore, keysDir string) *SecureBootHandler {
	return &SecureBootHandler{store: s, keysDir: keysDir}
}

// SecureBootKeySetResponse is a key set as the API returns it: the stored
// record, whose server paths do not serialize, and whether the set holds a
// TPM PCR signing key.
type SecureBootKeySetResponse struct {
	store.SecureBootKeySet
	HasTPMPCRKey bool `json:"hasTpmPcrKey"`
}

// keySetResponse builds the API view of ks. An imported set may lack the
// TPM PCR key, or carry a directory at its path, so the key counts only when
// a regular file is there.
func keySetResponse(ks *store.SecureBootKeySet) SecureBootKeySetResponse {
	has := false
	if ks.TPMPCRKeyPath != "" {
		if info, err := os.Stat(ks.TPMPCRKeyPath); err == nil && info.Mode().IsRegular() {
			has = true
		}
	}
	return SecureBootKeySetResponse{SecureBootKeySet: *ks, HasTPMPCRKey: has}
}

type generateKeysRequest struct {
	Name string `json:"name"`
}

// GenerateKeys handles POST /api/v1/secureboot-keys/generate.
// Keys are stored under <keysDir>/<name>/ — the user only provides a name.
//
//	@Summary	Generate a SecureBoot key set
//	@Tags		SecureBoot
//	@Accept		json
//	@Produce	json
//	@Security	AdminBearer
//	@Param		body	body		APIGenerateKeySetRequest	true	"Key set name + enroll mode"
//	@Success	201		{object}	handlers.SecureBootKeySetResponse
//	@Failure	413		{object}	APIError
//	@Router		/api/v1/secureboot-keys/generate [post]
func (h *SecureBootHandler) GenerateKeys(c echo.Context) error {
	var req generateKeysRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	if req.Name == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "name is required"})
	}
	if err := validateKeySetName(req.Name); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	outputDir := filepath.Join(h.keysDir, req.Name)

	ctx := c.Request().Context()

	if existing, _ := h.store.GetByName(ctx, req.Name); existing != nil {
		return c.JSON(http.StatusConflict, map[string]string{"error": fmt.Sprintf("a key set named %q already exists", req.Name)})
	}
	// A directory left empty or half filled by a failed run is fine to reuse;
	// only one that already holds key files is refused.
	for _, f := range []string{"PK.key", "KEK.key", "db.key", "tpm2-pcr-private.pem"} {
		if _, err := os.Stat(filepath.Join(outputDir, f)); err == nil {
			return c.JSON(http.StatusConflict, map[string]string{"error": fmt.Sprintf("directory %s already holds %s on disk", outputDir, f)})
		}
	}

	if err := secureboot.GenerateKeySet(secureboot.Options{
		Name:      req.Name,
		OutputDir: outputDir,
	}); err != nil {
		c.Logger().Errorf("generating key set %q: %v", req.Name, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to generate keys"})
	}

	ks := &store.SecureBootKeySet{
		Name:          req.Name,
		KeysDir:       outputDir,
		TPMPCRKeyPath: filepath.Join(outputDir, "tpm2-pcr-private.pem"),
	}
	if err := h.store.Create(ctx, ks); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to save key set record"})
	}

	return c.JSON(http.StatusCreated, keySetResponse(ks))
}

// ListKeys handles GET /api/v1/secureboot-keys.
//
//	@Summary	List SecureBoot key sets
//	@Tags		SecureBoot
//	@Produce	json
//	@Security	AdminBearer
//	@Success	200	{array}	handlers.SecureBootKeySetResponse
//	@Router		/api/v1/secureboot-keys [get]
func (h *SecureBootHandler) ListKeys(c echo.Context) error {
	keys, err := h.store.List(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to list key sets"})
	}
	out := make([]SecureBootKeySetResponse, 0, len(keys))
	for _, ks := range keys {
		out = append(out, keySetResponse(ks))
	}
	return c.JSON(http.StatusOK, out)
}

// DeleteKeys handles DELETE /api/v1/secureboot-keys/:id.
// Only removes the DB record; does NOT delete key files from disk.
//
//	@Summary		Delete a key set record
//	@Description	Removes the database record. Key files on disk are left in place.
//	@Tags			SecureBoot
//	@Security		AdminBearer
//	@Param			id	path	string	true	"Key set ID"
//	@Success		204
//	@Router			/api/v1/secureboot-keys/{id} [delete]
func (h *SecureBootHandler) DeleteKeys(c echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "id is required"})
	}

	if err := h.store.Delete(c.Request().Context(), id); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to delete key set"})
	}
	return c.NoContent(http.StatusNoContent)
}

// ExportKeys handles GET /api/v1/secureboot-keys/:id/export.
// It streams a gzipped tar of the key set's directory, with a small
// manifest.json at the root recording the logical name and enroll setting
// so ImportKeys can reconstitute the store record.
//
//	@Summary		Export a key set as a portable tar.gz
//	@Description	Streams a gzipped tar with manifest.json at the root and every key file under keys/. File permissions preserved so private keys remain 0600.
//	@Tags			SecureBoot
//	@Produce		application/gzip
//	@Security		AdminBearer
//	@Param			id	path		string	true	"Key set ID"
//	@Success		200	{file}		string
//	@Router			/api/v1/secureboot-keys/{id}/export [get]
func (h *SecureBootHandler) ExportKeys(c echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "id is required"})
	}

	ctx := c.Request().Context()
	ks, err := h.store.GetByID(ctx, id)
	if err != nil || ks == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "key set not found"})
	}

	info, err := os.Stat(ks.KeysDir)
	if err != nil || !info.IsDir() {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "key set directory missing on disk"})
	}

	filename := fmt.Sprintf("%s.secureboot-keyset.tar.gz", sanitizeFilename(ks.Name))
	c.Response().Header().Set(echo.HeaderContentType, "application/gzip")
	c.Response().Header().Set(echo.HeaderContentDisposition, fmt.Sprintf("attachment; filename=%q", filename))
	c.Response().WriteHeader(http.StatusOK)

	gz := gzip.NewWriter(c.Response().Writer)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	manifest := keySetManifest{
		Version:          1,
		Kind:             keySetManifestKind,
		Name:             ks.Name,
		SecureBootEnroll: ks.SecureBootEnroll,
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{
		Name:    keySetManifestName,
		Mode:    0o644,
		Size:    int64(len(manifestBytes)),
		ModTime: info.ModTime(),
	}); err != nil {
		return err
	}
	if _, err := tw.Write(manifestBytes); err != nil {
		return err
	}

	// Walk the keys dir and add every regular file under a "keys/" prefix.
	return filepath.Walk(ks.KeysDir, func(path string, fi os.FileInfo, werr error) error {
		if werr != nil {
			return werr
		}
		if fi.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(ks.KeysDir, path)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		hdr := &tar.Header{
			Name:    filepath.ToSlash(filepath.Join("keys", rel)),
			Mode:    int64(fi.Mode().Perm()),
			Size:    fi.Size(),
			ModTime: fi.ModTime(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		_, err = io.Copy(tw, f)
		return err
	})
}

// ImportKeys handles POST /api/v1/secureboot-keys/import.
// Accepts a multipart upload with a single "file" field containing a tar.gz
// produced by ExportKeys. The manifest's name becomes the new record's name;
// an ?name=<override> query parameter lets the caller pick a different name
// to avoid collisions with an existing local key set.
//
//	@Summary	Import a key set exported from another instance
//	@Tags		SecureBoot
//	@Accept		multipart/form-data
//	@Produce	json
//	@Security	AdminBearer
//	@Param		name	query		string	false	"Override the manifest name to avoid a collision"
//	@Param		file	formData	file	true	"tar.gz produced by the export endpoint"
//	@Success	201		{object}	handlers.SecureBootKeySetResponse
//	@Failure	409		{object}	APIError
//	@Failure	413		{object}	APIError
//	@Router		/api/v1/secureboot-keys/import [post]
func (h *SecureBootHandler) ImportKeys(c echo.Context) error {
	ctx := c.Request().Context()

	if ok, err := limitBody(c, maxImportRequestBytes); !ok {
		return err
	}
	fileHeader, err := c.FormFile("file")
	if err != nil {
		if isBodyTooLarge(err) {
			return bodyTooLarge(c)
		}
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "missing file field"})
	}
	src, err := fileHeader.Open()
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "cannot open upload"})
	}
	defer src.Close()

	gz, err := gzip.NewReader(src)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "not a gzip stream"})
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	var manifest *keySetManifest
	type pendingFile struct {
		rel  string
		mode os.FileMode
		data []byte
	}
	var pending []pendingFile
	var total int64

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			c.Logger().Errorf("reading key set archive: %v", err)
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "corrupt tar stream"})
		}
		// Reject anything but regular files — no symlinks, no dir entries.
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			continue
		}
		// Path hygiene: no absolute paths, no parent-dir escapes.
		cleaned := filepath.ToSlash(filepath.Clean(hdr.Name))
		if strings.HasPrefix(cleaned, "/") || strings.Contains(cleaned, "..") {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "unsafe path in archive", "path": hdr.Name})
		}

		total += hdr.Size
		if total > maxImportSize {
			return c.JSON(http.StatusRequestEntityTooLarge, map[string]string{"error": "archive exceeds size limit"})
		}

		buf := make([]byte, hdr.Size)
		if _, err := io.ReadFull(tr, buf); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "truncated file in archive", "path": hdr.Name})
		}

		if cleaned == keySetManifestName {
			var m keySetManifest
			if err := json.Unmarshal(buf, &m); err != nil {
				c.Logger().Errorf("parsing key set manifest: %v", err)
				return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid manifest"})
			}
			if m.Kind != keySetManifestKind {
				return c.JSON(http.StatusBadRequest, map[string]string{"error": "manifest kind mismatch", "kind": m.Kind})
			}
			if m.Version != 1 {
				return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("unsupported manifest version %d", m.Version)})
			}
			manifest = &m
			continue
		}

		// Everything else must live under keys/.
		if !strings.HasPrefix(cleaned, "keys/") {
			continue
		}
		rel := strings.TrimPrefix(cleaned, "keys/")
		if rel == "" {
			continue
		}
		pending = append(pending, pendingFile{
			rel:  rel,
			mode: os.FileMode(hdr.Mode).Perm(),
			data: buf,
		})
	}

	if manifest == nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "archive is missing manifest.json"})
	}
	if len(pending) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "archive has no key files"})
	}

	// Resolve the target name: manifest default, overridable via ?name=.
	name := manifest.Name
	if override := c.QueryParam("name"); override != "" {
		name = override
	}
	if name == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "manifest has no name and no ?name= override was provided"})
	}
	if err := validateKeySetName(name); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	if existing, _ := h.store.GetByName(ctx, name); existing != nil {
		return c.JSON(http.StatusConflict, map[string]string{"error": fmt.Sprintf("a key set named %q already exists", name)})
	}

	// Create an empty target dir. If the import fails halfway we clean up.
	targetDir := filepath.Join(h.keysDir, name)
	if _, err := os.Stat(targetDir); err == nil {
		return c.JSON(http.StatusConflict, map[string]string{"error": fmt.Sprintf("a key set directory named %q already exists", name)})
	}
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		c.Logger().Errorf("creating directory for key set %q: %v", name, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "cannot create key set directory"})
	}

	// Write files.
	writeErr := func() error {
		for _, pf := range pending {
			dest := filepath.Join(targetDir, filepath.FromSlash(pf.rel))
			if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
				return err
			}
			mode := pf.mode
			if mode == 0 {
				mode = 0o600
			}
			if err := os.WriteFile(dest, pf.data, mode); err != nil {
				return err
			}
		}
		return nil
	}()
	if writeErr != nil {
		_ = os.RemoveAll(targetDir)
		c.Logger().Errorf("writing files for key set %q: %v", name, writeErr)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed writing key files"})
	}

	ks := &store.SecureBootKeySet{
		Name:             name,
		KeysDir:          targetDir,
		TPMPCRKeyPath:    filepath.Join(targetDir, "tpm2-pcr-private.pem"),
		SecureBootEnroll: manifest.SecureBootEnroll,
	}
	if err := h.store.Create(ctx, ks); err != nil {
		_ = os.RemoveAll(targetDir)
		c.Logger().Errorf("saving record for key set %q: %v", name, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to save key set record"})
	}

	return c.JSON(http.StatusCreated, keySetResponse(ks))
}

// sanitizeFilename produces a tame filename fragment from a user-chosen name.
func sanitizeFilename(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
	}
	if b.Len() == 0 {
		return "keyset"
	}
	return b.String()
}
