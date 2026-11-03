package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
)

// SettingExtensionCatalogs holds the extension catalogs an operator saved from
// the UI, as a JSON array of URLs.
const SettingExtensionCatalogs = "extensions.catalogs"

// maxSavedExtensionCatalogs bounds the saved list. The builder offers every
// entry as a quick pick, so a list longer than this is a mistake, not a use.
const maxSavedExtensionCatalogs = 32

// WithExtensionCatalogs sets the catalogs given at launch with
// --extensions-catalog. The UI offers them for every flavor, and they cannot
// be removed at runtime. Returns the handler for chaining.
func (h *SettingsHandler) WithExtensionCatalogs(launch []string) *SettingsHandler {
	h.launchCatalogs = append([]string(nil), launch...)
	return h
}

// extensionCatalogsResponse is the GET /settings/extension-catalogs body.
type extensionCatalogsResponse struct {
	// Launch are the catalogs given with --extensions-catalog. Read-only.
	Launch []string `json:"launch"`
	// Saved are the catalogs an operator added at runtime.
	Saved []string `json:"saved"`
}

// updateExtensionCatalogsRequest is the PUT /settings/extension-catalogs body.
// It replaces the saved list.
type updateExtensionCatalogsRequest struct {
	Saved []string `json:"saved"`
}

// GetExtensionCatalogs handles GET /api/v1/settings/extension-catalogs.
//
//	@Summary	Read the configured extension catalogs
//	@Tags		Settings
//	@Produce	json
//	@Security	AdminBearer
//	@Success	200	{object}	extensionCatalogsResponse
//	@Router		/api/v1/settings/extension-catalogs [get]
func (h *SettingsHandler) GetExtensionCatalogs(c echo.Context) error {
	h.mu.RLock()
	defer h.mu.RUnlock()

	saved, err := h.savedExtensionCatalogs(c)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read extension catalogs"})
	}
	return c.JSON(http.StatusOK, h.extensionCatalogsResponse(saved))
}

// UpdateExtensionCatalogs handles PUT /api/v1/settings/extension-catalogs.
//
//	@Summary		Replace the saved extension catalogs
//	@Description	Replaces the catalogs saved from the UI. The catalogs given at launch with --extensions-catalog are not affected.
//	@Tags			Settings
//	@Accept			json
//	@Produce		json
//	@Security		AdminBearer
//	@Success		200	{object}	extensionCatalogsResponse
//	@Failure		413	{object}	APIError
//	@Router			/api/v1/settings/extension-catalogs [put]
func (h *SettingsHandler) UpdateExtensionCatalogs(c echo.Context) error {
	var req updateExtensionCatalogsRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	saved, err := normalizeExtensionCatalogs(req.Saved)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.settings == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "settings store is not configured on this server"})
	}
	encoded, err := json.Marshal(saved)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to encode extension catalogs"})
	}
	if err := h.settings.Set(c.Request().Context(), SettingExtensionCatalogs, string(encoded)); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to persist extension catalogs"})
	}
	return c.JSON(http.StatusOK, h.extensionCatalogsResponse(saved))
}

// savedExtensionCatalogs reads the saved list. The caller holds h.mu.
func (h *SettingsHandler) savedExtensionCatalogs(c echo.Context) ([]string, error) {
	if h.settings == nil {
		return []string{}, nil
	}
	raw, found, err := h.settings.Get(c.Request().Context(), SettingExtensionCatalogs)
	if err != nil {
		return nil, err
	}
	saved := []string{}
	if !found || raw == "" {
		return saved, nil
	}
	if err := json.Unmarshal([]byte(raw), &saved); err != nil {
		return nil, fmt.Errorf("decode %s: %w", SettingExtensionCatalogs, err)
	}
	return saved, nil
}

func (h *SettingsHandler) extensionCatalogsResponse(saved []string) extensionCatalogsResponse {
	launch := h.launchCatalogs
	if launch == nil {
		launch = []string{}
	}
	return extensionCatalogsResponse{Launch: launch, Saved: saved}
}

// normalizeExtensionCatalogs trims, validates and de-duplicates the catalogs
// an operator saves, keeping their order. The browser reads these catalogs
// directly and the build reads them again, so only absolute http(s) URLs are
// accepted: a local file path means nothing to the browser.
func normalizeExtensionCatalogs(values []string) ([]string, error) {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if err := validateCatalogURL(value); err != nil {
			return nil, err
		}
		if _, dup := seen[value]; dup {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) > maxSavedExtensionCatalogs {
		return nil, fmt.Errorf("too many extension catalogs: %d, at most %d", len(out), maxSavedExtensionCatalogs)
	}
	return out, nil
}

// validateCatalogURL accepts only an absolute http(s) URL. The extension
// catalog loader opens anything else as a file on this server.
func validateCatalogURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("invalid extension catalog %q: use an http or https URL", value)
	}
	return nil
}
