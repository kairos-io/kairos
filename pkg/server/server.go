package server

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kairos-io/AuroraBoot/docs"
	netbootpkg "github.com/kairos-io/AuroraBoot/internal/netbootmgr"
	"github.com/kairos-io/AuroraBoot/internal/ui"
	"github.com/kairos-io/AuroraBoot/pkg/auth"
	"github.com/kairos-io/AuroraBoot/pkg/batches"
	"github.com/kairos-io/AuroraBoot/pkg/builder"
	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/kairos-io/AuroraBoot/pkg/isoserve"
	"github.com/kairos-io/AuroraBoot/pkg/metrics"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/kairos-io/AuroraBoot/pkg/ws"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// Config holds all dependencies needed by the server.
type Config struct {
	NodeStore             store.NodeStore
	CommandStore          store.CommandStore
	GroupStore            store.GroupStore
	ArtifactStore         store.ArtifactStore
	SecureBootKeySetStore store.SecureBootKeySetStore

	// BatchStore persists rolling batches.
	BatchStore store.BatchStore
	// Batches starts and advances rolling batches. nil answers the group and
	// bulk command endpoints with 503.
	Batches *batches.Scheduler

	ExtensionStore               store.ExtensionStore
	ArtifactExtensionBundleStore store.ArtifactExtensionBundleStore
	NodeExtensionStore           store.NodeExtensionStore
	ExtensionBuilder             builder.ExtensionBuilder

	NetbootManager  *netbootpkg.Manager
	DeploymentStore store.DeploymentStore
	BMCTargetStore  store.BMCTargetStore
	SettingsStore   store.SettingsStore
	Builder         builder.ArtifactBuilder
	// SystemInfo describes the active builder backend for the
	// /api/v1/system/builder introspection endpoint. Populated at wire time in
	// runWeb from the flags plus the resolved kube REST config.
	SystemInfo    handlers.APISystemBuilder
	AdminPassword string
	RegToken      string
	RegTokenFile  string // path where reg token is persisted (for rotation)
	AuroraBootURL string
	ArtifactsDir  string
	KeysDir       string  // base directory for SecureBoot key sets
	Hub           *ws.Hub // optional, created if nil
	// Metrics holds the in-memory node metrics that heartbeats carry. Optional,
	// created if nil.
	Metrics *metrics.Buffer
	// ISOServe serves a local artifact ISO over a tokenized, BMC-reachable URL
	// for Redfish virtual-media deployments. Optional; when nil the Redfish
	// deploy path requires an explicit imageUrl.
	ISOServe *isoserve.Server
	// RedfishServeURL is the advertised base URL the BMC fetches the served ISO
	// from, as set by the launch --redfish-serve-url flag. It seeds the
	// image-source settings' advertised URL until an operator overrides it at
	// runtime.
	RedfishServeURL string
	// ExtensionCatalogs are the extension catalogs given at launch with
	// --extensions-catalog. The UI offers them for every flavor.
	ExtensionCatalogs []string
	// BaseContext, when non-nil, is the parent context for background deploy
	// goroutines so a server shutdown cancels in-flight Redfish deploys. Defaults
	// to context.Background().
	BaseContext context.Context

	// Rate limiting of the node-driven endpoints (registration, heartbeat, command
	// polling) — fleet-server hardening, kairos-io/kairos#4117. It is on by
	// default: a zero RPS falls back to the auth package default, and a zero Burst
	// to the larger of that package's burst floor and one second of the RPS in
	// effect. Admin-authenticated requests (the UI and the CAPI infra provider)
	// are never limited. DisableRateLimit turns the limiters off entirely.
	DisableRateLimit       bool
	NodeRateLimitRPS       float64 // per-node requests/sec for heartbeat + command polling
	NodeRateLimitBurst     int     // per-node instantaneous allowance
	RegisterRateLimitRPS   float64 // per-IP requests/sec for registration
	RegisterRateLimitBurst int     // per-IP instantaneous allowance
	// TrustedProxies are the reverse proxies whose X-Forwarded-For is believed
	// when working out a request's client IP (registration rate limit key, node
	// remote IP). Empty means the TCP peer address is always the client IP.
	TrustedProxies []*net.IPNet
	// ResetTimeout bounds the pending/in-progress automatic-reset lifecycle.
	// Zero uses handlers.DefaultResetTimeout; a negative value disables expiration.
	ResetTimeout time.Duration
}

// firstPositive returns v if it is positive, otherwise fallback. It lets a zero
// (unset) config value fall back to a default while still allowing an explicit
// override.
func firstPositive(v, fallback float64) float64 {
	if v > 0 {
		return v
	}
	return fallback
}

// redactedParams are query parameters that carry credentials: a node key, a
// per-extension download token, or a download link signature.
var redactedParams = []string{"token", "sig"}

// redactToken returns requestURI with the value of every credential query
// parameter (token, sig) replaced by "REDACTED", so node keys, extension
// download tokens and link signatures never reach the access log. Other query
// parameters and the path are preserved to keep the log useful. The query is
// always parsed, because a percent-encoded name such as %74oken is decoded to
// token by the handlers too. If it cannot be parsed, the whole query is
// redacted to err on the side of never emitting the secret.
func redactToken(requestURI string) string {
	i := strings.IndexByte(requestURI, '?')
	if i < 0 {
		return requestURI
	}
	path, rawQuery := requestURI[:i], requestURI[i+1:]

	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		// Could not parse the query string; refuse to risk leaking a credential.
		return path + "?REDACTED"
	}
	changed := false
	for _, p := range redactedParams {
		if values.Has(p) {
			values.Set(p, "REDACTED")
			changed = true
		}
	}
	if !changed {
		return requestURI
	}
	return path + "?" + values.Encode()
}

// New creates and configures an Echo server with all routes and middleware.
func New(cfg Config) *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.IPExtractor = clientIPExtractor(cfg.TrustedProxies)

	// Use a request logger whose URI is run through redactToken before it is
	// written. The agent WebSocket takes the node API key as a ?token= query
	// param, the extension download route takes the per-extension download
	// token there, and signed artifact download links carry a ?sig= signature,
	// so the raw request URI would otherwise leak those credentials into the
	// access log (and any downstream proxy logs). RequestLoggerWithConfig (the
	// non-deprecated logger) lets us emit a redacted URI explicitly instead of
	// the verbatim ${uri} the default middleware.Logger() prints.
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogRemoteIP: true,
		LogMethod:   true,
		LogURI:      true,
		LogStatus:   true,
		LogLatency:  true,
		LogValuesFunc: func(_ echo.Context, v middleware.RequestLoggerValues) error {
			log.Printf("%s %s %s %d %s",
				v.RemoteIP, v.Method, redactToken(v.URI), v.Status, v.Latency)
			return nil
		},
	}))
	e.Use(middleware.Recover())

	regToken := cfg.RegToken
	sharedRegToken := auth.NewRegistrationToken(regToken)
	sessions := auth.NewSessionStore()
	adminAuth := auth.NewAdminAuth(cfg.AdminPassword, sessions, cfg.AuroraBootURL)
	linkSigner := auth.NewLinkSigner()

	// Create WebSocket hub.
	hub := cfg.Hub
	if hub == nil {
		hub = ws.NewHub()
	}

	// Health endpoints
	e.GET("/healthz", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})
	e.GET("/readyz", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	// OpenAPI spec + Swagger UI. The spec is generated from the swag
	// annotations on the handlers via `make openapi`; the generated
	// docs package embeds the swagger.json and swagger.yaml strings.
	// Both endpoints are public — they're just the schema, no secrets.
	e.GET("/api/v1/openapi.json", func(c echo.Context) error {
		return c.Blob(http.StatusOK, "application/json", []byte(docs.SwaggerInfo.ReadDoc()))
	})
	e.GET("/api/v1/openapi.yaml", func(c echo.Context) error {
		return c.Blob(http.StatusOK, "application/yaml", swaggerYAMLBytes)
	})
	e.GET("/api/docs", func(c echo.Context) error {
		return c.HTML(http.StatusOK, swaggerUIPage)
	})

	// Build the deploy handler first (when a deployment store is wired) so its auto
	// eject-on-phone-home hook can be handed to the node handler below. The same
	// instance is reused for the deploy routes in the cfg.DeploymentStore block.
	var deployHandler *handlers.DeployHandler
	if cfg.DeploymentStore != nil {
		deployHandler = handlers.NewDeployHandler(cfg.ArtifactStore, cfg.DeploymentStore, cfg.BMCTargetStore, cfg.NetbootManager, cfg.ArtifactsDir, cfg.ISOServe, hub).
			WithBaseContext(cfg.BaseContext).
			WithSettings(cfg.SettingsStore)
	}

	// Create handlers
	nodeHandler := handlers.NewNodeHandler(cfg.NodeStore, cfg.CommandStore, cfg.GroupStore, hub, regToken, cfg.AuroraBootURL).
		WithResetTimeout(cfg.ResetTimeout)
	// Wire the auto eject-on-phone-home hook so a freshly-installed node's
	// Register/Heartbeat ejects its pending-eject Redfish deployment's media. The
	// hook lives on the deploy handler (it holds the deployment + BMC stores and the
	// Deployer factory); the node handler only invokes it off-request.
	if deployHandler != nil {
		nodeHandler.WithFinalizer(deployHandler.MaybeFinalizeForNode, cfg.BaseContext)
	}
	metricsBuf := cfg.Metrics
	if metricsBuf == nil {
		metricsBuf = metrics.NewBuffer(metrics.DefaultCapacity)
	}
	nodeHandler.SetMetrics(metricsBuf)
	cmdHandler := handlers.NewCommandHandler(cfg.CommandStore, cfg.NodeStore, hub, cfg.NodeExtensionStore, cfg.ExtensionStore).
		WithResetTimeout(cfg.ResetTimeout).
		WithBatches(cfg.Batches).
		WithGroups(cfg.GroupStore)
	nodeHandler.WithBatches(cfg.Batches)
	artifactHandler := handlers.NewArtifactHandler(cfg.Builder, cfg.ArtifactStore, cfg.GroupStore, cfg.SecureBootKeySetStore, cfg.ExtensionStore, cfg.ArtifactExtensionBundleStore, cfg.ArtifactsDir, regToken, cfg.AuroraBootURL).WithLinkSigner(linkSigner).
		WithRegistrationToken(sharedRegToken)
	var extensionHandler *handlers.ExtensionHandler
	if cfg.ExtensionBuilder != nil {
		extensionHandler = handlers.NewExtensionHandler(
			cfg.ExtensionBuilder, cfg.ExtensionStore, cfg.ArtifactExtensionBundleStore,
			cfg.SecureBootKeySetStore, cfg.NodeExtensionStore, cfg.ArtifactsDir,
		)
	}
	groupHandler := handlers.NewGroupHandler(cfg.GroupStore)
	settingsHandler := handlers.NewSettingsHandler(sharedRegToken, cfg.RegTokenFile).
		WithImageSource(cfg.SettingsStore, cfg.ISOServe, cfg.RedfishServeURL).
		WithExtensionCatalogs(cfg.ExtensionCatalogs)

	// WebSocket handlers
	agentWSHandler := &ws.AgentHandler{
		Hub:       hub,
		Nodes:     cfg.NodeStore,
		Commands:  cfg.CommandStore,
		PublicURL: cfg.AuroraBootURL,
		// Agents report command results over the WS, so the node_extensions
		// tracking write must hook in here too (not just on the REST
		// PUT /commands/:id/status path that CommandHandler.UpdateStatus owns).
		OnCommandStatus: cmdHandler.ApplyExtensionTracking,
		Metrics:         metricsBuf,
	}
	// pkg/ws cannot import the scheduler, so the agent socket reaches it
	// through these hooks: a status report advances the command's batch and a
	// heartbeat from a new boot settles the node's rebooting rows.
	if cfg.Batches != nil {
		agentWSHandler.OnCommandPhase = cmdHandler.AdvanceBatchOf
		agentWSHandler.OnNodeBooted = cfg.Batches.NodeBooted
	}
	// A WS heartbeat is an "OS is up" signal like the REST one, so it triggers the
	// same auto eject-on-phone-home hook — a node that reports liveness only over
	// the agent channel must still get its pending-eject media ejected.
	if deployHandler != nil {
		agentWSHandler.Finalize = deployHandler.MaybeFinalizeForNode
		agentWSHandler.BaseCtx = cfg.BaseContext
	}
	uiWSHandler := &ws.UIHandler{Hub: hub, PublicURL: cfg.AuroraBootURL}

	// Public endpoints
	e.GET("/api/v1/install-agent", nodeHandler.InstallScript)

	// WebSocket endpoints (the node key comes from the Authorization header or ?token=)
	e.GET("/api/v1/ws", agentWSHandler.HandleAgentWS)

	// Rate limits for the node-driven endpoints (fleet-server hardening,
	// kairos-io/kairos#4117). On by default with generous limits; a zero value
	// falls back to the auth package default, and DisableRateLimit forces the
	// limiters off (passing 0 makes them no-ops). Admin-authenticated traffic —
	// the UI and the CAPI infra provider — is never limited (see NodeRateLimiter).
	nodeRateRPS := firstPositive(cfg.NodeRateLimitRPS, auth.DefaultNodeRateLimitRPS)
	registerRateRPS := firstPositive(cfg.RegisterRateLimitRPS, auth.DefaultRegisterRateLimitRPS)
	if cfg.DisableRateLimit {
		nodeRateRPS, registerRateRPS = 0, 0
	}
	// Build the node limiter ONCE and share it across the heartbeat and shared-
	// command groups, so a node draws heartbeats and command polls from a single
	// bucket keyed on its node ID. A separate instance per group would give each
	// node two independent buckets and an effective ~2x the configured rate.
	nodeLimiter := auth.NodeRateLimiter(nodeRateRPS, cfg.NodeRateLimitBurst)

	// Agent registration (registration token auth). The per-IP rate limiter runs
	// BEFORE the token check so it also throttles invalid-token (brute-force)
	// attempts.
	regGroup := e.Group("/api/v1/nodes")
	regGroup.Use(auth.RegistrationRateLimiter(registerRateRPS, cfg.RegisterRateLimitBurst))
	regGroup.Use(auth.RegistrationTokenAuth(sharedRegToken))
	regGroup.POST("/register", nodeHandler.Register)

	// Agent-only node endpoints (node API key auth). RequireNodeMatch binds
	// every route to the authenticated node's identity: the :nodeID in the path
	// must equal the node that the API key belongs to, so a registered node can
	// only act on its own resources (prevents node-impersonation / BOLA).
	agentGroup := e.Group("/api/v1/nodes/:nodeID")
	agentGroup.Use(auth.NodeAPIKeyMiddleware(cfg.NodeStore))
	agentGroup.Use(auth.RequireNodeMatch)
	// After node auth, so the limiter keys on the authenticated node ID.
	agentGroup.Use(nodeLimiter)
	agentGroup.Use(bodyLimit(maxRequestBodyBytes, noSkip))
	agentGroup.POST("/heartbeat", nodeHandler.Heartbeat)

	// Shared command routes used by BOTH the agent (poll/report) and the
	// admin/UI (inspect/override). Registered ONCE under AgentOrAdminMiddleware:
	// registering them on two separate auth groups makes Echo silently shadow
	// the first, which would route every agent poll into the admin-only handler
	// (401). The handlers branch on the authenticated identity
	// (auth.AuthNodeID): agents are node-scoped (own commands only), admins act
	// across nodes.
	sharedCmd := e.Group("/api/v1/nodes/:nodeID")
	sharedCmd.Use(auth.AgentOrAdminMiddleware(adminAuth, cfg.NodeStore))
	// After auth, so it limits node polls but skips admin (which sets no node ID)
	// — keeping the CAPI infra provider's command polling exempt. Same instance as
	// the heartbeat group, so both share one bucket per node.
	sharedCmd.Use(nodeLimiter)
	sharedCmd.Use(bodyLimit(maxRequestBodyBytes, noSkip))
	sharedCmd.GET("/commands", nodeHandler.GetCommands)
	sharedCmd.PUT("/commands/:commandID/status", cmdHandler.UpdateStatus)

	// Admin/UI endpoints (admin password auth)
	adminGroup := e.Group("/api/v1")
	adminGroup.Use(auth.AdminMiddleware(adminAuth))
	adminGroup.Use(bodyLimit(maxRequestBodyBytes, skipOwnBodyLimit))

	// Session: login reads a capped body before authentication; logout clears
	// whatever cookie it is given; the check sits behind the admin middleware.
	sessionHandler := handlers.NewSessionHandler(adminAuth)
	e.POST("/api/v1/session", sessionHandler.Login)
	e.DELETE("/api/v1/session", sessionHandler.Logout)
	adminGroup.GET("/session", sessionHandler.Check)

	// Node management
	adminGroup.GET("/nodes", nodeHandler.List)
	adminGroup.GET("/nodes/:nodeID", nodeHandler.Get)
	adminGroup.DELETE("/nodes/:nodeID", nodeHandler.Delete)
	adminGroup.POST("/nodes/:nodeID/decommission", nodeHandler.Decommission)
	adminGroup.PUT("/nodes/:nodeID/labels", nodeHandler.SetLabels)
	adminGroup.PUT("/nodes/:nodeID/group", nodeHandler.SetGroup)
	adminGroup.POST("/nodes/:nodeID/release", nodeHandler.Release)
	metricsHandler := handlers.NewMetricsHandler(metricsBuf)
	adminGroup.GET("/nodes/:nodeID/metrics", metricsHandler.GetNode)
	adminGroup.GET("/metrics/latest", metricsHandler.GetLatest)
	// GET /nodes/:nodeID/commands and PUT .../commands/:commandID/status are
	// served by the shared agent-or-admin group above (single registration to
	// avoid Echo route shadowing); they branch on the caller's identity.
	adminGroup.POST("/nodes/:nodeID/commands", cmdHandler.Create)
	adminGroup.DELETE("/nodes/:nodeID/commands/:commandID", cmdHandler.Delete)
	adminGroup.DELETE("/nodes/:nodeID/commands", cmdHandler.ClearHistory)
	adminGroup.POST("/nodes/commands", cmdHandler.CreateBulk)

	// Batches
	batchHandler := handlers.NewBatchHandler(cfg.BatchStore, cfg.Batches)
	adminGroup.GET("/batches", batchHandler.List)
	adminGroup.GET("/batches/:id", batchHandler.Get)
	adminGroup.POST("/batches/:id/stop", batchHandler.Stop)

	// Group management
	adminGroup.POST("/groups", groupHandler.Create)
	adminGroup.GET("/groups", groupHandler.List)
	adminGroup.GET("/groups/:id", groupHandler.Get)
	adminGroup.PUT("/groups/:id", groupHandler.Update)
	adminGroup.DELETE("/groups/:id", groupHandler.Delete)
	adminGroup.POST("/groups/:id/claim", nodeHandler.Claim)
	adminGroup.POST("/groups/:id/commands", cmdHandler.CreateForGroup)

	// Artifact management
	adminGroup.POST("/artifacts/upload-overlay", artifactHandler.UploadOverlay)
	adminGroup.POST("/artifacts", artifactHandler.Create)
	adminGroup.GET("/artifacts", artifactHandler.List)
	adminGroup.DELETE("/artifacts/failed", artifactHandler.ClearFailed)
	adminGroup.GET("/artifacts/:id", artifactHandler.Get)
	adminGroup.GET("/artifacts/:id/logs", artifactHandler.GetLogs)
	adminGroup.POST("/artifacts/:id/cancel", artifactHandler.Cancel)
	adminGroup.POST("/artifacts/:id/download-link", artifactHandler.CreateDownloadLink)
	adminGroup.PATCH("/artifacts/:id", artifactHandler.Update)
	adminGroup.DELETE("/artifacts/:id", artifactHandler.Delete)
	adminGroup.GET("/artifacts/:id/bundle-extensions", artifactHandler.ListBundleExtensions)
	adminGroup.PUT("/artifacts/:id/bundle-extensions", artifactHandler.SetBundleExtensions)
	adminGroup.POST("/artifacts/:id/bundle-resolve", artifactHandler.ResolveBundle)

	// Extension routes are registered only when the extension builder is wired
	// (the in-process builder is constructed in internal/cmd/web.go).
	if extensionHandler != nil {
		adminGroup.POST("/extensions", extensionHandler.Create)
		adminGroup.GET("/extensions", extensionHandler.List)
		adminGroup.GET("/extensions/:id", extensionHandler.Get)
		adminGroup.PATCH("/extensions/:id", extensionHandler.Update)
		adminGroup.DELETE("/extensions/:id", extensionHandler.Delete)
		adminGroup.GET("/extensions/:id/logs", extensionHandler.GetLogs)
		adminGroup.POST("/extensions/:id/cancel", extensionHandler.Cancel)
		adminGroup.GET("/extensions/:id/nodes", extensionHandler.ListNodesForExtension)
		adminGroup.GET("/nodes/:nodeID/extensions", extensionHandler.ListNodeExtensions)
	}

	// Artifact downloads (fleet-server hardening, kairos-io/kairos#4117). Scoped so
	// a node key can't pull arbitrary build artifacts. Registered before the admin
	// group catches them, using inline middleware.
	//
	// Raw build files (ISO/UKI/raw disk/netboot) are admin-only: no node flow
	// consumes them over HTTP (netboot/Redfish read them from disk server-side), so
	// a node key must not reach them. They are served to an admin (session cookie
	// or bearer) or to a signed link from POST /artifacts/:id/download-link that
	// names exactly this file and has not expired.
	e.GET("/api/v1/artifacts/:id/download/*", artifactHandler.Download, auth.DownloadMiddleware(adminAuth, linkSigner))
	// The container image is the one artifact a node downloads (to satisfy an
	// "upgrade from built artifact" command), so it is admin OR a node that such a
	// command actually assigned this artifact to — never an arbitrary artifact.
	e.GET("/api/v1/artifacts/:id/image", artifactHandler.ExportImage,
		auth.ArtifactImageMiddleware(adminAuth, cfg.NodeStore, cfg.CommandStore))
	// Extension downloads: admin, the extension's own download token, OR any
	// authenticated node. Nodes need to fetch extensions bundled into an
	// assigned upgrade command; per-command scoping analogous to
	// ArtifactImageMiddleware is a follow-up. The per-extension token in ?token= is
	// what an install command's source URL carries, so that URL never holds the
	// admin password. An admin uses the session cookie or a Bearer header, and a
	// node key stays header-only.
	if extensionHandler != nil {
		e.GET("/api/v1/extensions/:id/download/:filename", extensionHandler.Download,
			auth.ExtensionDownloadMiddleware(adminAuth, cfg.NodeStore, cfg.ExtensionStore))
	}

	// Artifact upload — per-build UploadToken bearer (minted at Create time,
	// stored on the ArtifactRecord). Used by the operator backend's exporter
	// Job; the admin bearer does not grant access here. Registered outside
	// adminGroup so the admin middleware does not intercept the request.
	e.PUT("/api/v1/artifacts/:id/upload/*", artifactHandler.Upload)

	// UI WebSocket (admin auth)
	adminGroup.GET("/ws/ui", uiWSHandler.HandleUIWS)

	// System introspection
	systemHandler := handlers.NewSystemHandler(cfg.SystemInfo)
	adminGroup.GET("/system/builder", systemHandler.GetBuilder)

	// Settings
	adminGroup.GET("/settings/registration-token", settingsHandler.GetRegistrationToken)
	adminGroup.POST("/settings/registration-token/rotate", settingsHandler.RotateRegistrationToken)
	adminGroup.GET("/settings/image-source", settingsHandler.GetImageSource)
	adminGroup.PUT("/settings/image-source", settingsHandler.UpdateImageSource)
	adminGroup.GET("/settings/extension-catalogs", settingsHandler.GetExtensionCatalogs)
	adminGroup.PUT("/settings/extension-catalogs", settingsHandler.UpdateExtensionCatalogs)

	// SecureBoot key management
	sbHandler := handlers.NewSecureBootHandler(cfg.SecureBootKeySetStore, cfg.KeysDir)
	adminGroup.POST("/secureboot-keys/generate", sbHandler.GenerateKeys)
	adminGroup.GET("/secureboot-keys", sbHandler.ListKeys)
	adminGroup.GET("/secureboot-keys/:id/export", sbHandler.ExportKeys)
	adminGroup.POST("/secureboot-keys/import", sbHandler.ImportKeys)
	adminGroup.DELETE("/secureboot-keys/:id", sbHandler.DeleteKeys)

	// Deploy hub. deployHandler was constructed above so its eject hook could be
	// wired into the node handler; here we register its routes.
	if deployHandler != nil {
		adminGroup.POST("/netboot/start", deployHandler.StartNetboot)
		adminGroup.POST("/netboot/stop", deployHandler.StopNetboot)
		adminGroup.GET("/netboot/status", deployHandler.NetbootStatus)
		adminGroup.GET("/netboot/logs", deployHandler.NetbootLogs)
		adminGroup.POST("/artifacts/:id/deploy/redfish", deployHandler.DeployRedfish)
		adminGroup.GET("/redfish/quirk-profiles", deployHandler.ListQuirkProfiles)
		adminGroup.POST("/bmc-targets", deployHandler.CreateBMCTarget)
		adminGroup.GET("/bmc-targets", deployHandler.ListBMCTargets)
		adminGroup.PUT("/bmc-targets/:id", deployHandler.UpdateBMCTarget)
		adminGroup.DELETE("/bmc-targets/:id", deployHandler.DeleteBMCTarget)
		adminGroup.POST("/bmc-targets/:id/inspect", deployHandler.InspectHardware)
		adminGroup.GET("/bmc-targets/:id/status", deployHandler.PingBMCTarget)
		adminGroup.POST("/bmc-targets/refresh-all", deployHandler.RefreshAllBMCTargets)
		adminGroup.POST("/bmc-targets/:id/eject", deployHandler.EjectBMCTarget)
		adminGroup.GET("/deployments", deployHandler.ListDeployments)
		adminGroup.GET("/deployments/:id", deployHandler.GetDeployment)
		adminGroup.POST("/deployments/:id/finalize", deployHandler.FinalizeDeployment)
	}

	// SPA static files - serve from embedded UI assets
	setupSPA(e, ui.Assets)

	return e
}

// noUIMessage answers browser requests from a binary built without the web
// UI, such as one from `go install`, instead of a bare 404. It points at the
// builds that carry the UI rather than at a source checkout the user may
// not have.
const noUIMessage = "This auroraboot binary was built without the web UI. The release binaries and the quay.io/kairos/auroraboot container image include it.\n"

// setupSPA configures the Echo server to serve the SPA frontend from the
// dist/ directory of assets. It serves static files under /assets/ and, for
// any other path no route matches, a file from dist/ or, for requests that
// accept text/html, index.html (SPA client-side routing). Registering it as
// the not-found route keeps it away from every route the server defines,
// such as /healthz, whatever the request accepts.
func setupSPA(e *echo.Echo, assets fs.FS) {
	distFS, err := fs.Sub(assets, "dist")
	if err != nil {
		return
	}
	httpFS := http.FS(distFS)

	e.GET("/assets/*", echo.WrapHandler(http.FileServer(httpFS)))

	e.RouteNotFound("/*", func(c echo.Context) error {
		path := c.Request().URL.Path
		if strings.HasPrefix(path, "/api/") {
			return echo.ErrNotFound
		}
		if path == "/" {
			path = "/index.html"
		}

		if f, err := distFS.Open(strings.TrimPrefix(path, "/")); err == nil {
			f.Close()
			http.FileServer(httpFS).ServeHTTP(c.Response(), c.Request())
			return nil
		}

		if !strings.Contains(c.Request().Header.Get("Accept"), "text/html") {
			return echo.ErrNotFound
		}
		indexFile, err := distFS.Open("index.html")
		if errors.Is(err, fs.ErrNotExist) {
			return c.String(http.StatusNotFound, noUIMessage)
		}
		if err != nil {
			return echo.ErrNotFound
		}
		defer indexFile.Close()

		stat, err := indexFile.Stat()
		if err != nil {
			return echo.ErrNotFound
		}
		rs, ok := indexFile.(io.ReadSeeker)
		if !ok {
			return echo.ErrNotFound
		}
		http.ServeContent(c.Response(), c.Request(), "index.html", stat.ModTime(), rs)
		return nil
	})
}
