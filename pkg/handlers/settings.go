package handlers

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"

	"github.com/google/uuid"
	"github.com/kairos-io/AuroraBoot/pkg/auth"
	"github.com/kairos-io/AuroraBoot/pkg/isoserve"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
)

// Namespaced settings keys backing the runtime image-source configuration. They
// live in the SettingsStore (a plain key/value table); the handler owns typing
// and validation of the values.
const (
	// SettingDefaultImageURL is the global default image URL the BMC pulls the ISO
	// from when neither a per-deploy nor a per-BMC override is set (model a).
	SettingDefaultImageURL = "imageSource.defaultImageURL"
	// SettingLocalServeEnabled gates whether AuroraBoot serves the built artifact
	// ISO locally (model b). "true"/"false"; defaults to false.
	SettingLocalServeEnabled = "imageSource.localServeEnabled"
	// SettingLocalServeAdvertisedURL is the advertised base URL the BMC reaches the
	// locally-served ISO at, when set (model b). Advertised-only; the bind address
	// is a launch decision.
	SettingLocalServeAdvertisedURL = "imageSource.localServeAdvertisedURL"
)

// SettingsHandler handles settings-related REST endpoints.
type SettingsHandler struct {
	mu           sync.RWMutex
	rotateMu     sync.Mutex              // serializes the token file write with the in-memory swap
	regToken     *auth.RegistrationToken // shared with the registration middleware
	regTokenFile string                  // path to persist the token so rotations survive restarts

	// settings persists the runtime image-source settings. May be nil (e.g. in
	// tests or when no DB-backed store is wired), in which case the image-source
	// endpoints report unconfigured defaults and reject writes.
	settings store.SettingsStore
	// isoServe is the launch-configured local ISO-serve listener, or nil when none
	// was configured. Its presence is what makes local serving available at all;
	// the enable flag (a setting) only gates an already-available listener.
	//
	// TODO(P3-followup): the design's "shared mode" mounts isoServe.Handler() on
	// the main Echo server so local serving is available even without a dedicated
	// --redfish-serve-addr listener (the enable flag would gate the mounted
	// handler 404<->serve). That requires isoserve to mint capability URLs from a
	// runtime advertised base (rather than its fixed launch BaseURL), which is a
	// non-trivial isoserve change; deferred to keep this phase's blast radius
	// small. For now local serving requires a launch-configured listener.
	isoServe *isoserve.Server
	// seedAdvertisedURL is the advertised URL seeded from the launch
	// --redfish-serve-url flag, used as the default advertised URL until an
	// operator sets one at runtime.
	seedAdvertisedURL string
	// launchCatalogs are the extension catalogs given at launch with
	// --extensions-catalog.
	launchCatalogs []string
}

// NewSettingsHandler creates a new SettingsHandler.
// regToken is shared with the registration middleware so that rotations are
// visible to it.
// regTokenFile is the path where the token is persisted; pass "" to disable persistence.
func NewSettingsHandler(regToken *auth.RegistrationToken, regTokenFile string) *SettingsHandler {
	return &SettingsHandler{regToken: regToken, regTokenFile: regTokenFile}
}

// WithImageSource wires the dependencies for the image-source settings endpoints:
// the settings store, the launch-configured local ISO-serve (nil when none), and
// the advertised URL seeded from the launch --redfish-serve-url flag. Returns the
// handler for chaining.
func (h *SettingsHandler) WithImageSource(settings store.SettingsStore, isoServe *isoserve.Server, seedAdvertisedURL string) *SettingsHandler {
	h.settings = settings
	h.isoServe = isoServe
	h.seedAdvertisedURL = seedAdvertisedURL
	return h
}

// GetRegistrationToken handles GET /api/v1/settings/registration-token.
//
//	@Summary	Read the current registration token
//	@Tags		Settings
//	@Produce	json
//	@Security	AdminBearer
//	@Success	200	{object}	APIRegistrationTokenResponse
//	@Router		/api/v1/settings/registration-token [get]
func (h *SettingsHandler) GetRegistrationToken(c echo.Context) error {
	token := h.regToken.Get()
	return c.JSON(http.StatusOK, map[string]string{
		"registrationToken": token,
	})
}

// RotateRegistrationToken handles POST /api/v1/settings/registration-token/rotate.
//
//	@Summary		Rotate the registration token
//	@Description	Generates and persists a new token. Already-registered nodes keep working because they authenticate with their api-key.
//	@Tags			Settings
//	@Produce		json
//	@Security		AdminBearer
//	@Success		200	{object}	APIRegistrationTokenResponse
//	@Failure		413	{object}	APIError
//	@Failure		500	{object}	APIError
//	@Router			/api/v1/settings/registration-token/rotate [post]
func (h *SettingsHandler) RotateRegistrationToken(c echo.Context) error {
	token := uuid.New().String()
	h.rotateMu.Lock()
	defer h.rotateMu.Unlock()
	if h.regTokenFile != "" {
		if err := os.WriteFile(h.regTokenFile, []byte(token), 0600); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to save registration token"})
		}
	}
	h.regToken.Set(token)
	return c.JSON(http.StatusOK, map[string]string{
		"registrationToken": token,
	})
}

// imageSourceLocalServe is the local-serve (model b) view of the image-source
// settings.
type imageSourceLocalServe struct {
	// Configured reports whether a local ISO-serve listener was configured at
	// launch (--redfish-serve-addr). Without it, local serving cannot be enabled.
	Configured bool `json:"configured"`
	// Enabled is the runtime gate (only meaningful when Configured).
	Enabled bool `json:"enabled"`
	// AdvertisedURL is the advertised base URL the BMC reaches the served ISO at.
	AdvertisedURL string `json:"advertisedURL"`
	// UsesTLS reports whether the listener serves over HTTPS.
	UsesTLS bool `json:"usesTLS"`
}

// imageSourceResponse is the GET /settings/image-source body.
type imageSourceResponse struct {
	DefaultImageURL string                `json:"defaultImageURL"`
	LocalServe      imageSourceLocalServe `json:"localServe"`
}

// GetImageSource handles GET /api/v1/settings/image-source.
//
//	@Summary	Read the runtime image-source settings
//	@Tags		Settings
//	@Produce	json
//	@Security	AdminBearer
//	@Success	200	{object}	imageSourceResponse
//	@Router		/api/v1/settings/image-source [get]
func (h *SettingsHandler) GetImageSource(c echo.Context) error {
	ctx := c.Request().Context()

	h.mu.RLock()
	defer h.mu.RUnlock()

	settings := map[string]string{}
	if h.settings != nil {
		all, err := h.settings.GetAll(ctx)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read settings"})
		}
		settings = all
	}

	advertised := settings[SettingLocalServeAdvertisedURL]
	if advertised == "" {
		// Default to the URL seeded from the launch --redfish-serve-url flag until
		// an operator sets one at runtime.
		advertised = h.seedAdvertisedURL
	}

	resp := imageSourceResponse{
		DefaultImageURL: settings[SettingDefaultImageURL],
		LocalServe: imageSourceLocalServe{
			Configured:    h.isoServe != nil,
			Enabled:       settings[SettingLocalServeEnabled] == "true",
			AdvertisedURL: advertised,
		},
	}
	if h.isoServe != nil {
		resp.LocalServe.UsesTLS = h.isoServe.UsesTLS()
	}
	return c.JSON(http.StatusOK, resp)
}

// updateImageSourceRequest is the PUT /settings/image-source body. Fields are
// pointers so an absent field is left unchanged while an explicit empty string
// clears the value.
type updateImageSourceRequest struct {
	DefaultImageURL         *string `json:"defaultImageURL"`
	LocalServeEnabled       *bool   `json:"localServeEnabled"`
	LocalServeAdvertisedURL *string `json:"localServeAdvertisedURL"`
}

// UpdateImageSource handles PUT /api/v1/settings/image-source.
//
//	@Summary		Update the runtime image-source settings
//	@Description	Sets the global default image URL and the local-serve enable flag / advertised URL. Enabling local serve requires a launch-configured listener.
//	@Tags			Settings
//	@Accept			json
//	@Produce		json
//	@Security		AdminBearer
//	@Success		200	{object}	imageSourceResponse
//	@Failure		413	{object}	APIError
//	@Router			/api/v1/settings/image-source [put]
func (h *SettingsHandler) UpdateImageSource(c echo.Context) error {
	ctx := c.Request().Context()

	var req updateImageSourceRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.settings == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "settings store is not configured on this server"})
	}

	// Validate every operator-supplied URL (SSRF defense in depth). An empty
	// string is allowed to clear the value.
	if req.DefaultImageURL != nil && *req.DefaultImageURL != "" {
		if err := isoserve.ValidateMediaURL(*req.DefaultImageURL); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid defaultImageURL: %v", err)})
		}
	}
	if req.LocalServeAdvertisedURL != nil && *req.LocalServeAdvertisedURL != "" {
		if err := isoserve.ValidateMediaURL(*req.LocalServeAdvertisedURL); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid localServeAdvertisedURL: %v", err)})
		}
	}

	// Refuse to enable local serving when no listener was configured at launch:
	// the enable flag is a gate over an existing listener, never a way to silently
	// bind a socket.
	if req.LocalServeEnabled != nil && *req.LocalServeEnabled && h.isoServe == nil {
		return c.JSON(http.StatusConflict, map[string]string{
			"error": "cannot enable local serving: no --redfish-serve-addr configured at launch",
		})
	}

	if req.DefaultImageURL != nil {
		if err := h.settings.Set(ctx, SettingDefaultImageURL, *req.DefaultImageURL); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to persist defaultImageURL"})
		}
	}
	if req.LocalServeEnabled != nil {
		val := "false"
		if *req.LocalServeEnabled {
			val = "true"
		}
		if err := h.settings.Set(ctx, SettingLocalServeEnabled, val); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to persist localServeEnabled"})
		}
	}
	if req.LocalServeAdvertisedURL != nil {
		if err := h.settings.Set(ctx, SettingLocalServeAdvertisedURL, *req.LocalServeAdvertisedURL); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to persist localServeAdvertisedURL"})
		}
	}

	return h.getImageSourceLocked(c, ctx)
}

// getImageSourceLocked builds the image-source response under an already-held
// lock (Update calls it after persisting). It mirrors GetImageSource without
// re-locking.
func (h *SettingsHandler) getImageSourceLocked(c echo.Context, ctx context.Context) error {
	settings, err := h.settings.GetAll(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read settings"})
	}
	advertised := settings[SettingLocalServeAdvertisedURL]
	if advertised == "" {
		advertised = h.seedAdvertisedURL
	}
	resp := imageSourceResponse{
		DefaultImageURL: settings[SettingDefaultImageURL],
		LocalServe: imageSourceLocalServe{
			Configured:    h.isoServe != nil,
			Enabled:       settings[SettingLocalServeEnabled] == "true",
			AdvertisedURL: advertised,
		},
	}
	if h.isoServe != nil {
		resp.LocalServe.UsesTLS = h.isoServe.UsesTLS()
	}
	return c.JSON(http.StatusOK, resp)
}
