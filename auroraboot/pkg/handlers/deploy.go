package handlers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	netbootmgr "github.com/kairos-io/AuroraBoot/internal/netbootmgr"
	"github.com/kairos-io/AuroraBoot/pkg/hardware"
	"github.com/kairos-io/AuroraBoot/pkg/isoserve"
	"github.com/kairos-io/AuroraBoot/pkg/redfish"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/kairos-io/AuroraBoot/pkg/ws"
	"github.com/labstack/echo/v4"
)

// redfishDeployTimeout bounds the full virtual-media deploy flow (InsertMedia ->
// boot override -> reset -> Task poll).
const redfishDeployTimeout = 30 * time.Minute

// DeployHandler handles deployment-related REST endpoints.
// netbootController is the part of netbootmgr.Manager the netboot endpoints
// drive.
type netbootController interface {
	Start(artifactsDir, artifactID string) error
	Stop() error
	GetStatus() netbootmgr.Status
	GetLogs() string
}

type DeployHandler struct {
	artifacts    store.ArtifactStore
	deployments  store.DeploymentStore
	bmcTargets   store.BMCTargetStore
	netboot      netbootController
	artifactsDir string
	// isoServe serves a local artifact ISO over a tokenized, BMC-reachable URL.
	// May be nil, in which case a Redfish deploy must supply an explicit imageUrl.
	isoServe *isoserve.Server

	// settings exposes the runtime image-source settings (global default image URL,
	// local-serve enable flag, advertised URL). May be nil (e.g. in tests or when
	// no settings store is wired), in which case the deploy falls back to the
	// request/per-BMC imageUrl or the configured local-serve.
	settings store.SettingsStore

	// hub is the WebSocket hub used to broadcast live deploy-step progress to UI
	// clients. May be nil (e.g. in tests or when no hub is wired), in which case
	// progress is only persisted to the Deployment row and surfaced via polling.
	hub *ws.Hub

	// baseCtx is the parent context for every background deploy goroutine. It is
	// tied to the server lifecycle so a shutdown cancels in-flight deploys. When
	// unset it defaults to context.Background().
	baseCtx context.Context

	// runs holds a cancel func per in-flight deployment so a caller (or shutdown)
	// can abort it. Guarded by runsMu.
	runsMu sync.Mutex
	runs   map[string]context.CancelFunc

	// servedISOs holds the serve token of each completed deployment whose BMC may
	// still be reading the locally served ISO. The token is revoked when the
	// deployment is finalized, or after servedISOHold if it never is. Guarded by
	// runsMu.
	servedISOs map[string]string

	// refreshAll enforces a single in-flight "refresh all" BMC-status sweep: a
	// concurrent RefreshAllBMCTargets call returns 409 rather than launching a
	// second parallel sweep that would double the BMC-ping load.
	refreshAll refreshGuard

	// finalizerFn builds the redfishFinalizer used by the eject/finalize path. nil
	// uses the real gofish-backed Deployer; tests inject a fake via
	// WithFinalizerFactory.
	finalizerFn finalizerFactory
}

// NewDeployHandler creates a new DeployHandler.
func NewDeployHandler(
	artifacts store.ArtifactStore,
	deployments store.DeploymentStore,
	bmcTargets store.BMCTargetStore,
	nb *netbootmgr.Manager,
	artifactsDir string,
	isoServe *isoserve.Server,
	hub *ws.Hub,
) *DeployHandler {
	h := &DeployHandler{
		artifacts:    artifacts,
		deployments:  deployments,
		bmcTargets:   bmcTargets,
		artifactsDir: artifactsDir,
		isoServe:     isoServe,
		hub:          hub,
		baseCtx:      context.Background(),
		runs:         make(map[string]context.CancelFunc),
		servedISOs:   make(map[string]string),
	}
	// Leave the interface nil rather than holding a typed nil pointer.
	if nb != nil {
		h.netboot = nb
	}
	return h
}

// WithBaseContext sets the parent context for background deploy goroutines so a
// server shutdown cancels any in-flight Redfish deploy. Returns the handler for
// chaining.
func (h *DeployHandler) WithBaseContext(ctx context.Context) *DeployHandler {
	if ctx != nil {
		h.baseCtx = ctx
	}
	return h
}

// WithSettings wires the runtime settings store so DeployRedfish can resolve the
// global default image URL and the local-serve enable flag. Passing nil leaves
// the handler using only the request/per-BMC imageUrl and the launch-configured
// local-serve. Returns the handler for chaining.
func (h *DeployHandler) WithSettings(s store.SettingsStore) *DeployHandler {
	h.settings = s
	return h
}

// registerRun stores the cancel func for an in-flight deployment.
func (h *DeployHandler) registerRun(id string, cancel context.CancelFunc) {
	h.runsMu.Lock()
	defer h.runsMu.Unlock()
	h.runs[id] = cancel
}

// deregisterRun removes and returns the cancel func for a deployment.
func (h *DeployHandler) deregisterRun(id string) {
	h.runsMu.Lock()
	defer h.runsMu.Unlock()
	delete(h.runs, id)
}

// servedISOHold bounds how long a completed deployment keeps its served ISO
// while waiting to be finalized. It outlasts the auto-eject fallback window so a
// phone-home inside that window still finds the media served.
const servedISOHold = ejectFallbackWindow + 15*time.Minute

// holdServedISO keeps a deployment's serve token live past the end of the deploy
// run, so the BMC can keep reading the installer ISO until the deployment is
// finalized. If finalize never comes, the token is released after servedISOHold.
// It reports false when the token could not be extended.
func (h *DeployHandler) holdServedISO(id, token string) bool {
	if !h.isoServe.Extend(token, servedISOHold) {
		return false
	}
	h.runsMu.Lock()
	h.servedISOs[id] = token
	h.runsMu.Unlock()
	time.AfterFunc(servedISOHold, func() { h.releaseServedISO(id) })
	return true
}

// releaseServedISO revokes the serve token held for a deployment, if any.
func (h *DeployHandler) releaseServedISO(id string) {
	h.runsMu.Lock()
	token, ok := h.servedISOs[id]
	delete(h.servedISOs, id)
	h.runsMu.Unlock()
	if ok && h.isoServe != nil {
		h.isoServe.Revoke(token)
	}
}

// CancelRun aborts an in-flight deployment by id. It reports whether a matching
// run was found.
func (h *DeployHandler) CancelRun(id string) bool {
	h.runsMu.Lock()
	cancel, ok := h.runs[id]
	h.runsMu.Unlock()
	if ok {
		cancel()
	}
	return ok
}

// ReconcileOrphanedDeployments fails every deployment still marked Active. A
// process restart orphans the goroutine driving an Active deployment, so on
// startup those rows can never reach a terminal state on their own; flip them to
// Failed with an explanatory message. Safe to call once during bootstrap.
func ReconcileOrphanedDeployments(ctx context.Context, deployments store.DeploymentStore) error {
	deps, err := deployments.List(ctx)
	if err != nil {
		return fmt.Errorf("listing deployments: %w", err)
	}
	for _, dep := range deps {
		// A finalize goroutine that died mid-eject leaves EjectState=="ejecting"
		// with no routine to advance it. Reset it to pending so the next phone-home
		// or a manual finalize retries. NEVER auto-eject at startup, and never touch
		// a terminal ejected/eject-failed row.
		if dep.EjectState == store.EjectStateEjecting {
			dep.EjectState = store.EjectStatePending
			if err := deployments.Update(ctx, dep); err != nil {
				return fmt.Errorf("resetting orphaned eject %s: %w", dep.ID, err)
			}
		}

		if dep.Status != store.DeployActive {
			continue
		}
		now := time.Now()
		dep.Status = store.DeployFailed
		dep.Message = "interrupted by server restart"
		dep.CompletedAt = &now
		if err := deployments.Update(ctx, dep); err != nil {
			return fmt.Errorf("failing orphaned deployment %s: %w", dep.ID, err)
		}
	}
	return nil
}

// --- Netboot endpoints ---

type startNetbootRequest struct {
	ArtifactID string `json:"artifactId"`
}

// StartNetboot handles POST /api/v1/netboot/start.
func (h *DeployHandler) StartNetboot(c echo.Context) error {
	var req startNetbootRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	if req.ArtifactID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "artifactId is required"})
	}
	// The ID becomes a directory under the artifacts dir, so it must be a
	// single path segment naming a known artifact.
	if err := safePathSegment(req.ArtifactID); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid artifactId"})
	}
	if h.artifacts != nil {
		if _, err := h.artifacts.GetByID(c.Request().Context(), req.ArtifactID); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "artifact not found"})
		}
	}

	if err := h.netboot.Start(h.artifactsDir, req.ArtifactID); err != nil {
		// The detail can name files on this server, so it goes to the log
		// only and the response carries a fixed message.
		log.Printf("netboot start for artifact %q failed: %v", req.ArtifactID, err)
		switch {
		case errors.Is(err, netbootmgr.ErrAlreadyRunning):
			return c.JSON(http.StatusConflict, map[string]string{"error": "netboot server is already running"})
		case errors.Is(err, netbootmgr.ErrNoNetbootFiles):
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "artifact has no netboot files"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to start netboot server; see the server log"})
	}

	return c.JSON(http.StatusOK, h.netboot.GetStatus())
}

// StopNetboot handles POST /api/v1/netboot/stop.
func (h *DeployHandler) StopNetboot(c echo.Context) error {
	if err := h.netboot.Stop(); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, h.netboot.GetStatus())
}

// NetbootStatus handles GET /api/v1/netboot/status.
func (h *DeployHandler) NetbootStatus(c echo.Context) error {
	return c.JSON(http.StatusOK, h.netboot.GetStatus())
}

// NetbootLogs handles GET /api/v1/netboot/logs.
//
//	@Summary		Get netboot server log snapshot
//	@Description	Returns the current (or most recent) PXE/netboot session's captured output as text/plain. For real-time updates subscribe to the UI WebSocket and filter {type:"netboot-log"} envelopes.
//	@Tags			Netboot
//	@Produce		plain
//	@Security		AdminBearer
//	@Success		200	{string}	string
//	@Router			/api/v1/netboot/logs [get]
func (h *DeployHandler) NetbootLogs(c echo.Context) error {
	return c.String(http.StatusOK, h.netboot.GetLogs())
}

// --- RedFish deploy ---

type deployRedfishRequest struct {
	BMCTargetID string `json:"bmcTargetId"`
	// ImageURL is the HTTP(S) URL the BMC pulls the ISO from (VirtualMedia
	// InsertMedia is URL-pull). Optional: when empty, AuroraBoot serves the
	// artifact's on-disk ISO over an ephemeral tokenized URL (requires the server
	// to have a configured ISO-serve).
	ImageURL string `json:"imageUrl"`
	// Inline BMC credentials (used when bmcTargetId is empty).
	Endpoint  string `json:"endpoint"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	Vendor    string `json:"vendor"`
	VerifySSL *bool  `json:"verifySSL"`
	// SystemID optionally pins the target ComputerSystem by its Redfish Id. When
	// set it takes precedence over a saved target's SystemID; required when the BMC
	// exposes more than one system. Mirrors the CLI's --system-id.
	SystemID string `json:"systemId"`
	// EjectAfterInstall overrides the target's durable eject policy for this one
	// deploy. nil leaves the target's BMCTarget.EjectAfterInstall in effect; a
	// non-nil value forces it on/off. When effectively on, the deployment is marked
	// pending-eject at completion and auto-ejected on the node's phone-home.
	EjectAfterInstall *bool `json:"ejectAfterInstall"`
}

// resolveOperatorImageURL applies the operator-supplied image-URL precedence
// (per-deploy > per-BMC > global default), all model (a). It returns "" when no
// tier supplies a URL, signalling the caller to fall back to local serving
// (model b). It does not validate; the caller SSRF-validates the chosen URL.
func resolveOperatorImageURL(reqURL, bmcURL, defaultURL string) string {
	switch {
	case reqURL != "":
		return reqURL
	case bmcURL != "":
		return bmcURL
	default:
		return defaultURL
	}
}

// defaultImageURL returns the global default image URL setting, or "" when no
// settings store is wired or the key is unset. A read error is surfaced so the
// caller can fail closed rather than silently treating it as unset.
func (h *DeployHandler) defaultImageURL(ctx context.Context) (string, error) {
	if h.settings == nil {
		return "", nil
	}
	v, _, err := h.settings.Get(ctx, SettingDefaultImageURL)
	return v, err
}

// localServeAdvertisedURL returns the runtime advertised base URL for local
// serving, or "" when no settings store is wired or the key is unset, in which
// case the listener's launch BaseURL is used.
func (h *DeployHandler) localServeAdvertisedURL(ctx context.Context) (string, error) {
	if h.settings == nil {
		return "", nil
	}
	v, _, err := h.settings.Get(ctx, SettingLocalServeAdvertisedURL)
	return strings.TrimSpace(v), err
}

// localServeEnabled reports whether the runtime local-serve enable flag is set.
// It is the gate (alongside a launch-configured listener) for serving the
// artifact ISO locally. Defaults to false: missing setting, no settings store, or
// a read error all yield false so deploys never silently serve without an
// explicit opt-in.
func (h *DeployHandler) localServeEnabled(ctx context.Context) bool {
	if h.settings == nil {
		return false
	}
	v, _, err := h.settings.Get(ctx, SettingLocalServeEnabled)
	if err != nil {
		return false
	}
	return v == "true"
}

// DeployRedfish handles POST /api/v1/artifacts/:id/deploy/redfish.
func (h *DeployHandler) DeployRedfish(c echo.Context) error {
	artifactID := c.Param("id")
	ctx := c.Request().Context()

	artifact, err := h.artifacts.GetByID(ctx, artifactID)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "artifact not found"})
	}

	var req deployRedfishRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	var (
		endpoint        string
		username        string
		password        string
		vendor          string
		verifySSL       bool
		systemID        string
		bmcImageURL     string
		bmcID           string
		bmcEjectDefault bool
	)

	if req.BMCTargetID != "" {
		target, err := h.bmcTargets.GetByID(ctx, req.BMCTargetID)
		if err != nil {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "BMC target not found"})
		}
		endpoint = target.Endpoint
		username = target.Username
		password = target.Password
		vendor = target.Vendor
		verifySSL = target.VerifySSL
		systemID = target.SystemID
		bmcImageURL = target.ImageURL
		bmcID = target.ID
		bmcEjectDefault = target.EjectAfterInstall
	} else {
		if req.Endpoint == "" || req.Username == "" || req.Password == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "endpoint, username, and password are required when bmcTargetId is not provided"})
		}
		endpoint = req.Endpoint
		username = req.Username
		password = req.Password
		vendor = req.Vendor
		if vendor == "" {
			vendor = "generic"
		}
		if req.VerifySSL != nil {
			verifySSL = *req.VerifySSL
		} else {
			verifySSL = true
		}
	}

	// An explicit request SystemID overrides the saved target's (and is the only
	// way to select a system for an inline-credentials deploy). When empty, fall
	// back to whatever the saved target carries.
	if req.SystemID != "" {
		systemID = req.SystemID
	}

	// Resolve the effective eject policy: a per-deploy override wins, else the
	// target's durable default, else off. Eject only makes sense for a saved BMC
	// target (the auto path correlates node -> BMC -> deployment); an inline-creds
	// deploy has no durable BMC to link, so it can only opt in per-deploy.
	ejectAfter := bmcEjectDefault
	if req.EjectAfterInstall != nil {
		ejectAfter = *req.EjectAfterInstall
	}

	// Locate the artifact's on-disk ISO. InsertMedia is URL-pull (no byte
	// upload), so the BMC fetches the image from a URL.
	isoFile := ""
	for _, f := range artifact.ArtifactFiles {
		if filepath.Ext(f) == ".iso" {
			isoFile = f
			break
		}
	}
	if isoFile == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "no ISO file found for this artifact"})
	}

	// Resolve the image URL the BMC will fetch, and whether the BMC must fetch it
	// over HTTPS. InsertMedia advertises a TransferProtocolType that must match the
	// served URL's scheme, so derive useHTTPS alongside the URL. Image-source
	// precedence (highest to lowest), each operator-supplied URL SSRF-validated as
	// defense in depth even when validated on write:
	//   1. req.ImageURL  — per-deploy override (model a).
	//   2. BMC ImageURL  — per-BMC override (model a).
	//   3. global defaultImageURL setting (model a).
	//   4. local serve: AuroraBoot serves the artifact's on-disk ISO over a
	//      tokenized, BMC-reachable URL (model b), only when a listener was
	//      configured at launch AND the runtime enable flag is set.
	//   5. otherwise 503.
	defaultURL, err := h.defaultImageURL(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read image-source settings"})
	}
	imageURL := resolveOperatorImageURL(req.ImageURL, bmcImageURL, defaultURL)

	serveToken := ""
	useHTTPS := false
	if imageURL != "" {
		if err := isoserve.ValidateMediaURL(imageURL); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid imageUrl: %v", err)})
		}
		useHTTPS, err = isoserve.URLUsesHTTPS(imageURL)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid imageUrl: %v", err)})
		}
	} else {
		// No operator-supplied URL at any tier: fall back to local serving, which
		// requires both a launch-configured listener and the runtime enable flag.
		if h.isoServe == nil || !h.localServeEnabled(ctx) {
			return c.JSON(http.StatusServiceUnavailable, map[string]string{
				"error": "local ISO serving is not enabled on this server; provide imageUrl with a URL the BMC can reach, set a default image URL, or enable AuroraBoot-served artifacts",
			})
		}
		// ArtifactFiles holds either a bare file name (uploaded files, stored on
		// disk under artifactsDir/<id>/) or a path that already carries its
		// directory (local builds write "data/artifacts/<id>/foo.iso" relative to
		// the server's working directory, or a full path). Only a bare name is
		// joined with artifactsDir/artifactID; joining a path that already has
		// the prefix would double it. isoserve.Register needs an absolute path.
		isoPath := isoFile
		if filepath.Base(isoFile) == isoFile {
			isoPath = filepath.Join(h.artifactsDir, artifactID, isoFile)
		}
		absISO, err := filepath.Abs(isoPath)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("resolving artifact ISO path: %v", err)})
		}
		advertisedURL, err := h.localServeAdvertisedURL(ctx)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read image-source settings"})
		}
		useHTTPS = h.isoServe.UsesTLS()
		if advertisedURL != "" {
			if err := isoserve.ValidateMediaURL(advertisedURL); err != nil {
				return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid localServeAdvertisedURL: %v", err)})
			}
			// The advertised URL may be a TLS-terminating proxy in front of a
			// plain-HTTP listener, so the BMC protocol follows its scheme.
			useHTTPS, err = isoserve.URLUsesHTTPS(advertisedURL)
			if err != nil {
				return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid localServeAdvertisedURL: %v", err)})
			}
		}
		servedURL, token, err := h.isoServe.RegisterAt(advertisedURL, absISO, redfishDeployTimeout+5*time.Minute)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to serve artifact ISO: %v", err)})
		}
		imageURL = servedURL
		serveToken = token
	}

	dep := &store.Deployment{
		ID:          uuid.New().String(),
		ArtifactID:  artifactID,
		Method:      "redfish",
		Status:      store.DeployActive,
		Message:     "Deployment initiated",
		BMCTargetID: bmcID,
		Progress:    0,
		StartedAt:   time.Now(),
	}

	if err := h.deployments.Create(ctx, dep); err != nil {
		// Don't leak a live serve token for a deployment that never started.
		if serveToken != "" && h.isoServe != nil {
			h.isoServe.Revoke(serveToken)
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create deployment record"})
	}

	// Start async deployment goroutine. The HTTP request context is request-scoped
	// and would be cancelled on return, so the goroutine derives its own context
	// from the server-lifecycle base context (cancelled on shutdown) plus the
	// deploy timeout. The cancel func is registered so the run can be aborted.
	runCtx, cancel := context.WithTimeout(h.baseCtx, redfishDeployTimeout)
	h.registerRun(dep.ID, cancel)

	go h.runRedfishDeploy(runCtx, cancel, dep.ID, imageURL, serveToken, endpoint, username, password, vendor, systemID, verifySSL, useHTTPS, ejectAfter)

	return c.JSON(http.StatusAccepted, dep)
}

// runRedfishDeploy drives the gofish-backed virtual-media deployment in the
// background and records the outcome on the store.Deployment row. It never logs
// credentials. ctx is derived from the server-lifecycle base context plus the
// deploy timeout; cancel is its cancel func, deregistered and called on return so
// the run-registry never retains a finished deploy. useHTTPS sets the InsertMedia
// TransferProtocolType so it matches the scheme the BMC actually fetches over.
func (h *DeployHandler) runRedfishDeploy(ctx context.Context, cancel context.CancelFunc, deploymentID, imageURL, serveToken, endpoint, username, password, vendor, systemID string, verifySSL, useHTTPS, ejectAfter bool) {
	logPrefix := fmt.Sprintf("deployment %s", deploymentID)

	defer cancel()
	defer h.deregisterRun(deploymentID)

	// Revoke the serve token when the deploy fails or is cancelled, or when no
	// eject is armed. A successful deploy that arms the eject hands the token to
	// holdServedISO instead: the Reset only starts the installer boot, and the
	// BMC keeps reading the ISO until the media is ejected.
	keepServed := false
	if serveToken != "" && h.isoServe != nil {
		defer func() {
			if !keepServed {
				h.isoServe.Revoke(serveToken)
			}
		}()
	}

	deployer := redfish.NewDeployer(redfish.Config{
		Endpoint:  endpoint,
		Username:  username,
		Password:  password,
		Vendor:    redfish.VendorType(vendor),
		VerifySSL: verifySSL,
		SystemID:  systemID,
		Timeout:   redfishDeployTimeout,
	})

	if err := deployer.Connect(ctx); err != nil {
		log.Printf("[%s] connecting to redfish endpoint failed: %v", logPrefix, err)
		h.failDeployment(deploymentID, fmt.Sprintf("connecting to redfish endpoint: %v", err))
		return
	}
	// Always tear the session down (DELETE) on both success and error.
	defer func() { _ = deployer.Close() }()

	result, err := deployer.Deploy(ctx, redfish.DeployRequest{
		ImageURL:              imageURL,
		BootTarget:            redfish.BootTargetCd,
		BootMode:              redfish.BootModeUEFI,
		TransferProtocolHTTPS: useHTTPS,
		Progress:              h.progressUpdater(deploymentID),
	})
	if err != nil {
		log.Printf("[%s] redfish deploy failed: %v", logPrefix, err)
		h.failDeployment(deploymentID, fmt.Sprintf("redfish deploy failed: %v", err))
		return
	}

	if ejectAfter && serveToken != "" && h.isoServe != nil {
		keepServed = h.holdServedISO(deploymentID, serveToken)
	}

	msg := "Deployment completed successfully"
	if result.TaskState != "" {
		msg = fmt.Sprintf("Deployment completed (task state: %s)", result.TaskState)
	}
	h.completeDeployment(deploymentID, msg)

	// Arm the eject lifecycle when the policy is on: mark the deployment
	// pending-eject so the freshly-installed node's phone-home (or a manual
	// finalize) ejects the media. We do NOT eject inline — that would be
	// mid-reboot, the wrong moment (the install loop this whole mechanism fixes).
	if ejectAfter {
		h.markEjectPending(deploymentID)
	}
}

// markEjectPending arms the eject lifecycle on a completed deployment: it sets the
// policy to on-phone-home and EjectState to pending so the auto path (node
// phone-home) or a manual finalize picks it up. Best-effort: a store failure leaves
// the deployment un-armed (no eject), which is safe — the operator can finalize
// manually.
func (h *DeployHandler) markEjectPending(id string) {
	ctx := context.Background()
	dep, err := h.deployments.GetByID(ctx, id)
	if err != nil {
		return
	}
	dep.EjectPolicy = store.EjectPolicyOnPhoneHome
	dep.EjectState = store.EjectStatePending
	_ = h.deployments.Update(ctx, dep)
}

// broadcastProgress fans a live deploy-progress event to all connected UI
// clients over the existing UI WebSocket. It is best-effort and nil-safe: when
// no hub is wired the event is simply dropped, and clients fall back to polling
// the deployment store. Broadcast does its own marshalling and write fan-out, so
// this never blocks the deploy on a slow client. The payload is nested under
// "data" to match the existing UI message envelope ({"type":…,"data":…}).
func (h *DeployHandler) broadcastProgress(id, status, step, message string, progress int) {
	if h.hub == nil || h.hub.UI == nil {
		return
	}
	h.hub.UI.Broadcast(map[string]any{
		"type": "deploy-progress",
		"data": map[string]any{
			"deploymentId": id,
			"status":       status,
			"progress":     progress,
			"step":         step,
			"message":      message,
		},
	})
}

// progressUpdater returns a redfish progress callback that writes live step and
// percentage onto the Deployment row. It only advances Progress (never regresses)
// and leaves the terminal Completed/Failed write to the caller. A read/update
// failure is best-effort and silently ignored — progress is non-authoritative.
// Alongside the store write it broadcasts the step to UI clients so the
// Deployments page reflects progress in real time rather than only on poll.
func (h *DeployHandler) progressUpdater(id string) func(step string, percent int) {
	return func(step string, percent int) {
		dep, err := h.deployments.GetByID(context.Background(), id)
		if err != nil {
			return
		}
		if percent <= dep.Progress && dep.Message == step {
			return
		}
		if percent > dep.Progress {
			dep.Progress = percent
		}
		dep.Message = step
		_ = h.deployments.Update(context.Background(), dep)
		h.broadcastProgress(id, dep.Status, step, dep.Message, dep.Progress)
	}
}

func (h *DeployHandler) completeDeployment(id, message string) {
	dep, err := h.deployments.GetByID(context.Background(), id)
	if err != nil {
		return
	}
	now := time.Now()
	dep.Status = store.DeployCompleted
	dep.Message = message
	dep.Progress = 100
	dep.CompletedAt = &now
	_ = h.deployments.Update(context.Background(), dep)
	h.broadcastProgress(id, dep.Status, "completed", message, dep.Progress)
}

func (h *DeployHandler) failDeployment(id, message string) {
	dep, err := h.deployments.GetByID(context.Background(), id)
	if err != nil {
		return
	}
	now := time.Now()
	dep.Status = store.DeployFailed
	dep.Message = message
	dep.CompletedAt = &now
	_ = h.deployments.Update(context.Background(), dep)
	h.broadcastProgress(id, dep.Status, "failed", message, dep.Progress)
}

// --- Hardware inspection ---

type inspectResponse struct {
	MemoryGiB      int    `json:"memoryGiB"`
	ProcessorCount int    `json:"processorCount"`
	Model          string `json:"model"`
	Manufacturer   string `json:"manufacturer"`
	SerialNumber   string `json:"serialNumber"`
	// SupportedFeatures lists the capabilities AuroraBoot positively detected for
	// this system (e.g. "UEFI", "SecureBoot"). It is informational: the API does
	// not gate on required features (the CLI does). A capability AuroraBoot could
	// not determine is simply absent from this list.
	SupportedFeatures []string `json:"supportedFeatures"`
}

// InspectHardware handles POST /api/v1/bmc-targets/:id/inspect.
func (h *DeployHandler) InspectHardware(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()

	target, err := h.bmcTargets.GetByID(ctx, id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "BMC target not found"})
	}

	deployer := redfish.NewDeployer(redfish.Config{
		Endpoint:  target.Endpoint,
		Username:  target.Username,
		Password:  target.Password,
		Vendor:    redfish.VendorType(target.Vendor),
		VerifySSL: target.VerifySSL,
		SystemID:  target.SystemID,
		Timeout:   30 * time.Second,
	})
	if err := deployer.Connect(ctx); err != nil {
		h.persistInspectFailure(ctx, target, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to connect to redfish endpoint: %v", err)})
	}
	// Always tear the session down (DELETE) on both success and error.
	defer func() { _ = deployer.Close() }()

	inspector := hardware.NewInspector(deployer)
	info, err := inspector.InspectSystem(ctx)
	if err != nil {
		h.persistInspectFailure(ctx, target, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to inspect hardware: %v", err)})
	}

	features := sortedFeatures(info.Features)
	h.persistInspectSuccess(ctx, target, info, features)

	return c.JSON(http.StatusOK, inspectResponse{
		MemoryGiB:         info.MemoryGiB,
		ProcessorCount:    info.ProcessorCount,
		Model:             info.Model,
		Manufacturer:      info.Manufacturer,
		SerialNumber:      info.SerialNumber,
		SupportedFeatures: features,
	})
}

// persistInspectSuccess writes a successful inspect result into the BMC target's
// server-owned status cache so the BMC page can render details without contacting
// the BMC again. It sets LastStatus="reachable", the hardware facts, LastInspectAt,
// and clears LastError. A store failure is best-effort and swallowed: the inspect
// response itself is authoritative and must not fail because the cache write did.
func (h *DeployHandler) persistInspectSuccess(ctx context.Context, target *store.BMCTarget, info *hardware.SystemInfo, features []string) {
	now := time.Now()
	target.LastStatus = bmcStatusReachable
	target.LastError = ""
	target.LastInspectAt = &now
	target.LastModel = info.Model
	target.LastManufacturer = info.Manufacturer
	target.LastSerial = info.SerialNumber
	target.LastMemoryGiB = info.MemoryGiB
	target.LastCPUCount = info.ProcessorCount
	target.LastFeatures = features
	_ = h.bmcTargets.Update(ctx, target)
}

// persistInspectFailure records an unreachable/inspect-error outcome into the
// status cache: LastStatus="unreachable", a scrubbed LastError, LastInspectAt=now.
// The error text is scrubbed of credentials at its source (the redfish package),
// but is re-scrubbed defensively here in case a future error path is not. A store
// failure is best-effort and swallowed.
func (h *DeployHandler) persistInspectFailure(ctx context.Context, target *store.BMCTarget, cause error) {
	now := time.Now()
	target.LastStatus = bmcStatusUnreachable
	target.LastError = scrubBMCError(cause)
	target.LastInspectAt = &now
	_ = h.bmcTargets.Update(ctx, target)
}

// sortedFeatures returns the detected feature names as a stable, sorted slice for
// a deterministic JSON response.
func sortedFeatures(features map[string]bool) []string {
	names := make([]string, 0, len(features))
	for name, present := range features {
		if present {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// --- BMC Target CRUD ---

// bmcTargetRequest is the JSON body of POST and PUT /api/v1/bmc-targets.
// store.BMCTarget keeps Password out of JSON so responses never carry it; the
// request shape is the one place a client can send it.
type bmcTargetRequest struct {
	store.BMCTarget
	Password string `json:"password"`
}

// CreateBMCTarget handles POST /api/v1/bmc-targets.
func (h *DeployHandler) CreateBMCTarget(c echo.Context) error {
	var req bmcTargetRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	target := req.BMCTarget
	target.Password = req.Password
	if target.Endpoint == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "endpoint is required"})
	}
	// SSRF-validate the BMC endpoint on write. The session-free reachability ping
	// and refresh-all actively GET this endpoint, so an unvalidated value is a
	// confused-deputy/internal-fetch primitive. Mirror the CLI, which guards the
	// endpoint with the same validator (internal/cmd/redfish.go).
	if err := isoserve.ValidateMediaURL(target.Endpoint); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid endpoint: %v", err)})
	}
	// SSRF-validate a per-BMC image-URL override on write (and again at deploy).
	if target.ImageURL != "" {
		if err := isoserve.ValidateMediaURL(target.ImageURL); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid imageUrl: %v", err)})
		}
	}
	// Status-cache fields are server-owned (populated only by inspect/ping/refresh).
	// Drop anything a client may have smuggled in via the bound body so it cannot
	// fabricate a "reachable" status without an actual check.
	clearBMCStatusCache(&target)

	if err := h.bmcTargets.Create(c.Request().Context(), &target); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create BMC target"})
	}
	return c.JSON(http.StatusCreated, target)
}

// ListBMCTargets handles GET /api/v1/bmc-targets.
func (h *DeployHandler) ListBMCTargets(c echo.Context) error {
	targets, err := h.bmcTargets.List(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to list BMC targets"})
	}
	return c.JSON(http.StatusOK, targets)
}

// UpdateBMCTarget handles PUT /api/v1/bmc-targets/:id.
func (h *DeployHandler) UpdateBMCTarget(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()

	existing, err := h.bmcTargets.GetByID(ctx, id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "BMC target not found"})
	}

	var req bmcTargetRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	updated := req.BMCTarget
	updated.Password = req.Password
	if updated.Endpoint == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "endpoint is required"})
	}
	// SSRF-validate the BMC endpoint on write (same posture as Create and the CLI).
	// The ping/refresh-all paths actively fetch this endpoint, so it must be guarded.
	if err := isoserve.ValidateMediaURL(updated.Endpoint); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid endpoint: %v", err)})
	}
	// SSRF-validate a per-BMC image-URL override on write (and again at deploy).
	if updated.ImageURL != "" {
		if err := isoserve.ValidateMediaURL(updated.ImageURL); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid imageUrl: %v", err)})
		}
	}

	existing.Name = updated.Name
	existing.Endpoint = updated.Endpoint
	existing.Vendor = updated.Vendor
	existing.Username = updated.Username
	if updated.Password != "" {
		existing.Password = updated.Password
	}
	existing.VerifySSL = updated.VerifySSL
	existing.SystemID = updated.SystemID
	existing.ImageURL = updated.ImageURL
	existing.NodeID = updated.NodeID
	existing.EjectAfterInstall = updated.EjectAfterInstall
	existing.EjectPowerCycle = updated.EjectPowerCycle

	if err := h.bmcTargets.Update(ctx, existing); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to update BMC target"})
	}
	return c.JSON(http.StatusOK, existing)
}

// DeleteBMCTarget handles DELETE /api/v1/bmc-targets/:id.
func (h *DeployHandler) DeleteBMCTarget(c echo.Context) error {
	id := c.Param("id")
	if err := h.bmcTargets.Delete(c.Request().Context(), id); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to delete BMC target"})
	}
	return c.NoContent(http.StatusNoContent)
}

// --- Deployment history ---

// ListDeployments handles GET /api/v1/deployments.
func (h *DeployHandler) ListDeployments(c echo.Context) error {
	deps, err := h.deployments.List(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to list deployments"})
	}
	return c.JSON(http.StatusOK, deps)
}

// GetDeployment handles GET /api/v1/deployments/:id.
func (h *DeployHandler) GetDeployment(c echo.Context) error {
	id := c.Param("id")
	dep, err := h.deployments.GetByID(c.Request().Context(), id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "deployment not found"})
	}
	return c.JSON(http.StatusOK, dep)
}
