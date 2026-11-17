// AuroraBoot fleet management server. This file is also the entry point
// for swag-generated OpenAPI documentation.
//
//	@title			AuroraBoot API
//	@version		0.1.0
//	@description	HTTP API for the AuroraBoot Kairos node manager.
//	@description
//	@description	Three security schemes are used across the API:
//	@description	  * AdminBearer: Authorization: Bearer <admin-password>. Scripts and automation callers. The web UI signs in through POST /api/v1/session and uses a session cookie instead.
//	@description	  * NodeAPIKey: Authorization: Bearer <api-key>. Registered Kairos nodes calling their own endpoints. The api-key is returned from POST /api/v1/nodes/register.
//	@description	  * RegistrationToken: shared secret carried inside the body of POST /api/v1/nodes/register.
//	@description
//	@description	Two WebSocket endpoints also exist but are not described in this spec:
//	@description	  * GET /api/v1/ws: agent command channel (auth via Authorization: Bearer <api-key>, or ?token=<api-key> for agents that cannot set headers).
//	@description	  * GET /api/v1/ws/ui: UI live update channel (auth via the session cookie, or Authorization: Bearer <admin-password>; same-origin only).
//
//	@contact.name	Kairos authors
//	@contact.url	https://github.com/kairos-io/AuroraBoot
//	@license.name	Apache-2.0
//	@license.url	https://www.apache.org/licenses/LICENSE-2.0
//
//	@BasePath		/
//
//	@securityDefinitions.apikey	AdminBearer
//	@in							header
//	@name						Authorization
//	@description				Supply as "Bearer <admin-password>".
//
//	@securityDefinitions.apikey	NodeAPIKey
//	@in							header
//	@name						Authorization
//	@description				Supply as "Bearer <api-key>" returned from POST /api/v1/nodes/register.
//
//	@tag.name		Health
//	@tag.description	Liveness and readiness probes.
//	@tag.name		Agent bootstrap
//	@tag.description	Public endpoints a brand-new Kairos node uses to join the fleet.
//	@tag.name		Agent
//	@tag.description	Endpoints a registered node uses with its api-key.
//	@tag.name		Nodes
//	@tag.description	Fleet read and admin operations.
//	@tag.name		Groups
//	@tag.description	Logical groups for nodes.
//	@tag.name		Commands
//	@tag.description	Queued remote operations (upgrade, reset, exec...).
//	@tag.name		Artifacts
//	@tag.description	Image builds produced by the embedded AuroraBoot.
//	@tag.name		SecureBoot
//	@tag.description	UKI signing key sets.
//	@tag.name		Settings
//	@tag.description	Instance-level settings (registration token, ...).
package cmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/kairos-io/AuroraBoot/docs" // swag-generated; populates docs.SwaggerInfo
	"github.com/kairos-io/AuroraBoot/internal/builder/auroraboot"
	"github.com/kairos-io/AuroraBoot/internal/builder/operator"
	netbootmgr "github.com/kairos-io/AuroraBoot/internal/netbootmgr"
	"github.com/kairos-io/AuroraBoot/internal/secrets"
	gormstore "github.com/kairos-io/AuroraBoot/internal/store/gorm"
	"github.com/kairos-io/AuroraBoot/pkg/batches"
	"github.com/kairos-io/AuroraBoot/pkg/builder"
	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/kairos-io/AuroraBoot/pkg/isoserve"
	"github.com/kairos-io/AuroraBoot/pkg/server"
	"github.com/kairos-io/AuroraBoot/pkg/ws"

	"github.com/labstack/echo/v4"
	"github.com/urfave/cli/v2"
)

// WebCMD launches the fleet management server: REST API, React UI, agent
// and UI WebSockets, GORM-backed store, build runner, install-agent
// bootstrap script. This replaces the historical Alpine.js + jobstorage
// `web` subcommand wholesale.
var WebCMD = cli.Command{
	Name:    "web",
	Aliases: []string{"w"},
	Usage:   "Run the fleet management web UI and REST API",
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "listen", Value: ":8080", Usage: "HTTP listen address"},
		&cli.StringFlag{Name: "data-dir", Value: "./data", Usage: "Base directory for all AuroraBoot data (db, artifacts, keys)"},
		&cli.StringFlag{Name: "db", Usage: "Database DSN (default: <data-dir>/auroraboot.db)"},
		&cli.StringFlag{Name: "artifacts-dir", Usage: "Directory for built artifacts (default: <data-dir>/artifacts)"},
		&cli.StringFlag{Name: "admin-password", Usage: "Admin password for UI access", EnvVars: []string{"AURORABOOT_ADMIN_PASSWORD"}},
		&cli.StringFlag{Name: "registration-token", Usage: "Registration token for node enrollment", EnvVars: []string{"AURORABOOT_REG_TOKEN"}},
		&cli.StringFlag{Name: "url", Usage: "External URL of this AuroraBoot instance (for cloud-config injection)", EnvVars: []string{"AURORABOOT_URL"}},
		&cli.StringFlag{Name: "tls-cert", Usage: "Path to the TLS certificate for the API server. Both --tls-cert and --tls-key must be set to enable HTTPS", EnvVars: []string{"AURORABOOT_TLS_CERT"}},
		&cli.StringFlag{Name: "tls-key", Usage: "Path to the TLS private key for the API server. Both --tls-cert and --tls-key must be set to enable HTTPS", EnvVars: []string{"AURORABOOT_TLS_KEY"}},
		&cli.StringFlag{Name: "redfish-serve-url", Usage: "Advertised base URL a BMC fetches an artifact ISO from for Redfish deploys (default: --url). The BMC management network may differ from the UI network", EnvVars: []string{"AURORABOOT_REDFISH_SERVE_URL"}},
		&cli.StringFlag{Name: "redfish-serve-addr", Usage: "Bind address for the Redfish ISO-serve (e.g. 10.0.0.5:8090). Required to enable serving local artifact ISOs to a BMC", EnvVars: []string{"AURORABOOT_REDFISH_SERVE_ADDR"}},
		&cli.StringFlag{Name: "redfish-serve-tls-cert", Usage: "TLS certificate for the Redfish ISO-serve (opt-in HTTPS; requires a BMC-trusted cert)"},
		&cli.StringFlag{Name: "redfish-serve-tls-key", Usage: "TLS key for the Redfish ISO-serve"},
		&cli.StringFlag{Name: "redfish-quirks-dir", Usage: "Directory of operator-supplied *.yaml/*.yml Redfish quirk profiles, loaded once at server start (not hot-reloaded). A BMCTarget's vendor resolves to a profile by name; an operator profile named the same as a built-in overrides it (logged). A malformed profile is skipped, not fatal", EnvVars: []string{redfishQuirksDirEnv}},
		&cli.StringSliceFlag{Name: "extensions-catalog", Usage: "System extension catalog URL the UI offers when building an artifact, repeatable. The Hadron flavor always has the hadron-layers catalog; other flavors only get the catalogs given here or saved in Settings", EnvVars: []string{"AURORABOOT_EXTENSIONS_CATALOG"}},
		&cli.StringFlag{Name: "builder", Value: "local", Usage: "Which builder backend to use: 'local' or 'operator'"},
		&cli.StringFlag{Name: "kubeconfig", Usage: "Path to a kubeconfig file for the operator builder (single file). Empty means try in-cluster config first, then the default client-go loading rules (which honour a multi-file KUBECONFIG env)"},
		&cli.StringFlag{Name: "builder-namespace", Value: "default", Usage: "Namespace in which OSArtifact CRs are created. Used only when --builder=operator"},
		&cli.StringFlag{Name: "builder-upload-url", Usage: "URL exporter pods PUT built artifacts to. Only needs cluster-internal reachability, so a Service DNS name is appropriate. Defaults to --url. Used only when --builder=operator", EnvVars: []string{"AURORABOOT_BUILDER_UPLOAD_URL"}},
		&cli.BoolFlag{Name: "disable-rate-limit", Usage: "Disable per-identity rate limiting of the node-driven endpoints (registration, heartbeat, command polling). Admin/UI/API traffic is never rate-limited regardless. Consider this for a large fleet behind a shared NAT egress IP", EnvVars: []string{"AURORABOOT_DISABLE_RATE_LIMIT"}},
		&cli.Float64Flag{Name: "node-rate-limit", Usage: "Per-node requests/sec for heartbeat and command polling (0 = generous default). Admin traffic is exempt", EnvVars: []string{"AURORABOOT_NODE_RATE_LIMIT"}},
		&cli.Float64Flag{Name: "register-rate-limit", Usage: "Per-client-IP registration requests/sec (0 = generous default)", EnvVars: []string{"AURORABOOT_REGISTER_RATE_LIMIT"}},
		&cli.IntFlag{Name: "node-rate-limit-burst", Usage: "Per-node instantaneous allowance for heartbeat and command polling (0 = the larger of 20 and one second of --node-rate-limit)", EnvVars: []string{"AURORABOOT_NODE_RATE_LIMIT_BURST"}},
		&cli.IntFlag{Name: "register-rate-limit-burst", Usage: "Per-client-IP instantaneous registration allowance (0 = the larger of 20 and one second of --register-rate-limit). Lower it to tighten a token brute-force at a low sustained rate", EnvVars: []string{"AURORABOOT_REGISTER_RATE_LIMIT_BURST"}},
		&cli.StringSliceFlag{Name: "trusted-proxy", Usage: "Reverse proxy address or CIDR whose X-Forwarded-For header is trusted for the client IP, repeatable. Unset means the TCP peer address is always the client IP", EnvVars: []string{"AURORABOOT_TRUSTED_PROXY"}},
		&cli.DurationFlag{Name: "reset-timeout", Value: handlers.DefaultResetTimeout, Usage: "Fail pending or in-progress resets that have not returned within this duration. The clock restarts when a node re-registers mid-reset, so a long wipe is not reported failed; set a negative duration to disable", EnvVars: []string{"AURORABOOT_RESET_TIMEOUT"}},
	},
	Action: runWeb,
}

func runWeb(c *cli.Context) error {
	builderKind := c.String("builder")
	switch builderKind {
	case "local", "operator":
	default:
		return fmt.Errorf("--builder must be 'local' or 'operator', got %q", builderKind)
	}

	listenAddr := c.String("listen")
	dataDir := c.String("data-dir")
	dbDSN := c.String("db")
	artifactsDir := c.String("artifacts-dir")
	adminPassword := c.String("admin-password")
	regToken := c.String("registration-token")
	externalURL := c.String("url")
	tlsCert := c.String("tls-cert")
	tlsKey := c.String("tls-key")
	redfishServeURL := c.String("redfish-serve-url")
	redfishServeAddr := c.String("redfish-serve-addr")
	redfishServeTLSCert := c.String("redfish-serve-tls-cert")
	redfishServeTLSKey := c.String("redfish-serve-tls-key")
	redfishQuirksDir := c.String("redfish-quirks-dir")
	resetTimeout := c.Duration("reset-timeout")
	trustedProxies, err := server.ParseTrustedProxies(c.StringSlice("trusted-proxy"))
	if err != nil {
		return fmt.Errorf("--trusted-proxy: %w", err)
	}

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	if dbDSN == "" {
		dbDSN = filepath.Join(dataDir, "auroraboot.db")
	}
	if artifactsDir == "" {
		artifactsDir = filepath.Join(dataDir, "artifacts")
	}
	keysDir := filepath.Join(dataDir, "keys")

	// Persisted secrets live under <data-dir>/secrets/ so they survive restarts.
	// Admin password resolves as flag/env > file > generate. The registration
	// token treats the launch-time value (from --registration-token or its
	// AURORABOOT_REG_TOKEN env var) as a seed rather than a hard override,
	// so a runtime rotation via the settings API survives a pod restart;
	// see ResolveRegistrationToken.
	secretsDir := filepath.Join(dataDir, "secrets")
	if err := os.MkdirAll(secretsDir, 0700); err != nil {
		return fmt.Errorf("create secrets directory: %w", err)
	}
	adminPasswordFile := filepath.Join(secretsDir, "admin-password")
	regTokenFile := filepath.Join(secretsDir, "registration-token")
	regTokenSeedFile := filepath.Join(secretsDir, "registration-token.seed")

	if adminPassword == "" {
		adminPassword = loadOrGenerateSecret(adminPasswordFile, "admin password")
	}
	resolvedRegToken, err := ResolveRegistrationToken(regToken, regTokenFile, regTokenSeedFile)
	if err != nil {
		return fmt.Errorf("resolve registration token: %w", err)
	}
	regToken = resolvedRegToken
	if externalURL == "" {
		hostname, _ := os.Hostname()
		if hostname == "" {
			hostname = "localhost"
		}
		externalURL = fmt.Sprintf("http://%s%s", hostname, listenAddr)
		fmt.Fprintf(os.Stderr, "Warning: --url not set, using %s\n", externalURL)
		fmt.Fprintf(os.Stderr, "  Set --url or AURORABOOT_URL for the externally reachable URL\n")
	}

	if err := os.MkdirAll(artifactsDir, 0755); err != nil {
		return fmt.Errorf("create artifacts directory: %w", err)
	}

	// Data encryption key for BMC credentials at rest (AES-256-GCM). Lives
	// alongside the other secrets as a 0600 file; generated on first run.
	bmcKeyFile := filepath.Join(secretsDir, "bmc-key")
	bmcCipher, err := secrets.LoadOrGenerateCipher(bmcKeyFile)
	if err != nil {
		return fmt.Errorf("load BMC encryption key: %w", err)
	}

	store, err := gormstore.New(dbDSN)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	store.WithCipher(bmcCipher)
	// The handlers read a zero --reset-timeout as the default, so the batch
	// reset commands get the same window they give a single-node reset.
	batchResetTimeout := resetTimeout
	if batchResetTimeout == 0 {
		batchResetTimeout = handlers.DefaultResetTimeout
	}
	store.WithResetTimeout(batchResetTimeout)

	// Reconcile deployments left Active by a previous process: a restart orphans
	// their in-flight goroutine, so they can never complete. Mark them Failed.
	if err := handlers.ReconcileOrphanedDeployments(context.Background(), &gormstore.DeploymentStoreAdapter{S: store}); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: reconciling orphaned deployments: %v\n", err)
	}

	artifactStore := &gormstore.ArtifactStoreAdapter{S: store}
	extensionStore := &gormstore.ExtensionStoreAdapter{S: store}

	// Reconcile artifacts left Pending or Building by a previous process: a
	// restart orphans their build goroutine, so they can never reach Ready on
	// their own. Mark them Error so the UI reflects a terminal state.
	if err := handlers.ReconcileOrphanedArtifacts(context.Background(), artifactStore); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: reconciling orphaned artifacts: %v\n", err)
	}

	// Create the WebSocket hub up front so the builder can broadcast log
	// chunks to subscribed UI clients as they arrive. The same hub is
	// handed to server.New below so the HTTP routes share it.
	wsHub := ws.NewHub()

	// Read --url again rather than passing externalURL: the fallback above
	// rewrites externalURL to this container's own hostname, which is exactly
	// the value nodes cannot resolve. With no --url the manager is better off
	// finding a local interface address itself.
	netbootManager := netbootmgr.NewManager(c.String("url"), wsHub.UI)

	var artifactBuilder builder.ArtifactBuilder
	var systemInfo handlers.APISystemBuilder
	switch builderKind {
	case "local":
		local, err := auroraboot.New(artifactsDir, nil, artifactStore).
			WithKairosInitImage(os.Getenv("KAIROS_INIT_IMAGE"))
		if err != nil {
			return fmt.Errorf("KAIROS_INIT_IMAGE: %w", err)
		}
		artifactBuilder = local.
			WithLogBroadcaster(wsHub.UI).
			WithNetbootManager(netbootManager)
		systemInfo = handlers.APISystemBuilder{
			Backend:           "local",
			DownloadSupported: true,
		}
	case "operator":
		cfg, err := loadKubeConfig(c.String("kubeconfig"))
		if err != nil {
			return err
		}
		// Operator builds ship the per-build upload token plus the finished
		// artifact bytes back to AuroraBoot over --builder-upload-url. The
		// exporter pods run inside the cluster, so a Service DNS name works;
		// only cluster-internal reachability is required. When --builder-upload-url
		// is unset we fall back to --url for back-compat, which forces the upload
		// to traverse the public path. Over plaintext http the per-build token
		// and artifact bytes travel in the clear, so anyone with network access
		// on the traversed path can capture the token and overwrite artifacts
		// while the build is Building (watchCRPhase zeroes the token on terminal
		// transition, closing the window at Ready/Error). Left unforced so dev
		// environments can iterate without cert setup; a production deployment
		// should either terminate the upload path via https or scope the URL to
		// cluster-internal Service DNS.
		uploadURL := ResolveBuilderUploadURL(c.String("builder-upload-url"), externalURL)
		if strings.HasPrefix(strings.ToLower(uploadURL), "http://") {
			fmt.Fprintf(os.Stderr, "Warning: --builder=operator with plaintext upload URL (%s): exporter Pods will PUT the per-build upload token and artifact bytes over http. Fine on cluster-internal Service DNS, risky over public paths.\n", uploadURL)
		}
		b, err := operator.New(operator.Config{
			RESTConfig:    cfg,
			Namespace:     c.String("builder-namespace"),
			Store:         artifactStore,
			AuroraBootURL: uploadURL,
		})
		if err != nil {
			return err
		}
		artifactBuilder = b.WithLogBroadcaster(wsHub.UI)
		systemInfo = handlers.APISystemBuilder{
			Backend:   "operator",
			Cluster:   sanitizeClusterURL(cfg.Host),
			Namespace: c.String("builder-namespace"),
			// Operator builds ship their finished artifacts back to
			// AuroraBoot's on-disk store via the exporter Job we inject in
			// internal/builder/operator, so Download serves them the same
			// way as local builds. The UI no longer needs to gate its
			// download link by backend.
			DownloadSupported: true,
		}
	default:
		return fmt.Errorf("unreachable: --builder=%q survived validation", builderKind)
	}

	// Extension builds drop their .raw outputs under artifactsDir/extensions/<id>/.
	extensionsDir := filepath.Join(artifactsDir, "extensions")
	if err := os.MkdirAll(extensionsDir, 0755); err != nil {
		return fmt.Errorf("create extensions directory: %w", err)
	}
	extensionBuilder := auroraboot.NewExtensionBuilder(extensionsDir, extensionStore).
		WithArtifactStore(artifactStore).
		WithLogBroadcaster(wsHub.UI)

	nodeStore := &gormstore.NodeStoreAdapter{S: store}
	commandStore := &gormstore.CommandStoreAdapter{S: store}
	groupStore := &gormstore.GroupStoreAdapter{S: store}
	secureBootKeySetStore := &gormstore.SecureBootKeySetStoreAdapter{S: store}
	deploymentStore := &gormstore.DeploymentStoreAdapter{S: store}
	bmcTargetStore := &gormstore.BMCTargetStoreAdapter{S: store}
	settingsStore := &gormstore.SettingsStoreAdapter{S: store}

	// The bundle and node-extension stores are method sets on *Store; we wrap
	// them as adapters that satisfy the store.ArtifactExtensionBundleStore and
	// store.NodeExtensionStore interfaces.
	bundleStore := &gormstore.ArtifactExtensionBundleStoreAdapter{S: store}
	nodeExtensionStore := &gormstore.NodeExtensionStoreAdapter{S: store}

	// Optional Redfish ISO-serve: serves a local artifact ISO over a tokenized,
	// BMC-reachable URL so virtual-media (URL-pull) deploys work without an
	// operator-supplied imageUrl. It only starts when a bind address is given; the
	// advertised base defaults to --url but can differ (the BMC management network
	// is often not the UI network).
	var isoServe *isoserve.Server
	// serveURL is the advertised base URL a BMC fetches the served ISO from. It is
	// hoisted out of the start block so it can seed the runtime image-source
	// settings' advertised URL even before an operator overrides it.
	serveURL := redfishServeURL
	if serveURL == "" {
		serveURL = externalURL
	}
	if redfishServeAddr != "" {
		// The scheme of every URL the BMC is handed comes from BaseURL, while the
		// transport comes from the cert/key pair. Nothing downstream reconciles the
		// two: the deploy handler takes the URL from one and the InsertMedia
		// TransferProtocolType from the other, so a disagreement here is only
		// discovered by the BMC, which fails the fetch without naming a flag.
		serveURLSource := "--redfish-serve-url"
		if redfishServeURL == "" {
			serveURLSource = "--url (--redfish-serve-url is not set)"
		}
		if err := checkServeURLScheme(serveURL, serveURLSource, redfishServeTLSCert != "" && redfishServeTLSKey != ""); err != nil {
			return err
		}
		isoServe = isoserve.New(isoserve.Config{
			BaseURL:  serveURL,
			BindAddr: redfishServeAddr,
			CertFile: redfishServeTLSCert,
			KeyFile:  redfishServeTLSKey,
		})
		if err := isoServe.Start(context.Background()); err != nil {
			return fmt.Errorf("start redfish iso-serve: %w", err)
		}
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = isoServe.Shutdown(shutdownCtx)
		}()
		fmt.Fprintf(os.Stderr, "  ISO-serve: %s (bind %s)\n", serveURL, redfishServeAddr)
	}

	// Load operator-supplied Redfish quirk profiles once at start (not
	// hot-reloaded: an in-flight deploy must never have its profile swapped). The
	// loaded registry becomes the process-wide default, so the Redfish deploy path
	// resolves a BMCTarget's vendor to a profile by name — operator profiles
	// included — inside redfish.NewDeployer. Each profile and each skipped file is
	// logged by the loader.
	if err := loadRedfishQuirksDir(redfishQuirksDir); err != nil {
		return err
	}

	// Root context for background deploy goroutines, cancelled when runWeb
	// returns so in-flight Redfish deploys are aborted on shutdown.
	baseCtx, baseCancel := context.WithCancel(context.Background())
	defer baseCancel()

	// The scheduler pushes the commands a batch creates the way a single-node
	// command is pushed, which needs only the command store and the hub, so it
	// gets a command handler of its own rather than the one server.New builds.
	batchStore := &gormstore.BatchStoreAdapter{S: store}
	pusher := handlers.NewCommandHandler(commandStore, nodeStore, wsHub, nil, nil)
	sched := batches.New(batchStore, pusher.PushCommand)
	go sched.Run(baseCtx, 30*time.Second)

	e := server.New(server.Config{
		BaseContext:                  baseCtx,
		NodeStore:                    nodeStore,
		CommandStore:                 commandStore,
		BatchStore:                   batchStore,
		Batches:                      sched,
		GroupStore:                   groupStore,
		ArtifactStore:                artifactStore,
		SecureBootKeySetStore:        secureBootKeySetStore,
		ExtensionStore:               extensionStore,
		ArtifactExtensionBundleStore: bundleStore,
		NodeExtensionStore:           nodeExtensionStore,
		ExtensionBuilder:             extensionBuilder,

		NetbootManager:  netbootManager,
		DeploymentStore: deploymentStore,
		BMCTargetStore:  bmcTargetStore,
		SettingsStore:   settingsStore,
		Builder:         artifactBuilder,
		SystemInfo:      systemInfo,
		AdminPassword:   adminPassword,
		RegToken:        regToken,
		RegTokenFile:    regTokenFile,
		AuroraBootURL:   externalURL,
		ArtifactsDir:    artifactsDir,
		KeysDir:         keysDir,
		Hub:             wsHub,
		ISOServe:        isoServe,
		RedfishServeURL: redfishServeURLSeed(isoServe, serveURL),

		ExtensionCatalogs: c.StringSlice("extensions-catalog"),

		DisableRateLimit:       c.Bool("disable-rate-limit"),
		NodeRateLimitRPS:       c.Float64("node-rate-limit"),
		NodeRateLimitBurst:     c.Int("node-rate-limit-burst"),
		RegisterRateLimitRPS:   c.Float64("register-rate-limit"),
		RegisterRateLimitBurst: c.Int("register-rate-limit-burst"),
		TrustedProxies:         trustedProxies,
		ResetTimeout:           resetTimeout,
	})

	fmt.Fprintf(os.Stderr, "AuroraBoot fleet server starting on %s\n", listenAddr)
	fmt.Fprintf(os.Stderr, "  UI:        %s\n", externalURL)
	fmt.Fprintf(os.Stderr, "  DB:        %s\n", dbDSN)
	fmt.Fprintf(os.Stderr, "  Artifacts: %s\n", artifactsDir)

	return serve(e, listenAddr, tlsCert, tlsKey)
}

// serve starts the Echo server, choosing HTTPS when both a TLS certificate and
// key are provided and falling back to plaintext HTTP otherwise. When serving
// plaintext it logs a prominent warning, because all three bearer credentials
// (admin password, node API keys, registration token) and cloud-configs cross
// the wire in clear; a TLS cert/key or a TLS-terminating reverse proxy is
// strongly recommended for production. Plaintext is intentionally not a hard
// failure so dev and proxied deployments keep working.
//
// Both code paths use Echo's Start/StartTLS, which install the same signal
// handling and graceful-shutdown behaviour.
func serve(e *echo.Echo, listenAddr, tlsCert, tlsKey string) error {
	return serveWith(os.Stderr, listenAddr, tlsCert, tlsKey,
		func() error { return e.StartTLS(listenAddr, tlsCert, tlsKey) },
		func() error { return e.Start(listenAddr) })
}

// serveWith holds the TLS-vs-plaintext decision and the plaintext warning,
// independent of the concrete Echo server, so the decision can be unit-tested.
// It calls startTLS when both a cert and key are provided, otherwise it emits
// the plaintext warning to w and calls startPlain. The chosen starter's error
// is returned unchanged.
func serveWith(w io.Writer, listenAddr, tlsCert, tlsKey string, startTLS, startPlain func() error) error {
	if tlsCert != "" && tlsKey != "" {
		_, _ = fmt.Fprintf(w, "TLS enabled: serving HTTPS on %s using cert %s\n", listenAddr, tlsCert)
		return startTLS()
	}

	const warning = "\n" +
		"*****************************************************************************\n" +
		"WARNING: serving plaintext HTTP — TLS is NOT enabled.\n" +
		"  Admin password, node API keys, the registration token, and cloud-configs\n" +
		"  all traverse the network UNENCRYPTED. For production, set --tls-cert and\n" +
		"  --tls-key, or place AuroraBoot behind a TLS-terminating reverse proxy.\n" +
		"*****************************************************************************\n\n"
	_, _ = io.WriteString(w, warning)
	return startPlain()
}

// ResolveBuilderUploadURL returns the URL exporter pods should PUT built
// artifacts to. An explicit --builder-upload-url wins; otherwise it falls
// back to --url so single-URL deployments keep working unchanged.
func ResolveBuilderUploadURL(builderUploadURL, externalURL string) string {
	if builderUploadURL != "" {
		return builderUploadURL
	}
	return externalURL
}

// loadOrGenerateSecret reads the secret from path if it exists, otherwise
// generates a new one and persists it. The label is used in log messages.
func loadOrGenerateSecret(path, label string) string {
	if data, err := os.ReadFile(path); err == nil {
		secret := string(data)
		for len(secret) > 0 && (secret[len(secret)-1] == '\n' || secret[len(secret)-1] == '\r') {
			secret = secret[:len(secret)-1]
		}
		if secret != "" {
			return secret
		}
	}
	secret := generateToken(16)
	if err := os.WriteFile(path, []byte(secret), 0600); err != nil {
		log.Fatalf("Failed to persist %s: %v", label, err)
	}
	fmt.Fprintf(os.Stderr, "Generated %s: %s (saved to %s)\n", label, secret, path)
	return secret
}

// checkServeURLScheme reports whether the base URL advertised to a BMC agrees
// with how the local ISO server actually serves it. isoserve.Server.Register
// mints every served URL from that base, so the base carries the scheme the BMC
// reads, while usesTLS is the transport the listener will speak. An InsertMedia
// request built from both is self-contradictory whenever they disagree, so this
// refuses the launch instead of deferring the failure to the BMC. source names
// the flag serveURL came from, since it falls back to --url.
func checkServeURLScheme(serveURL, source string, usesTLS bool) error {
	parsed, err := url.Parse(serveURL)
	if err != nil {
		return fmt.Errorf("parsing %s %q: %w", source, serveURL, err)
	}
	switch parsed.Scheme {
	case "https":
		if !usesTLS {
			return fmt.Errorf("%s is %q, but the Redfish ISO server has no TLS material, so it would serve cleartext under an https:// URL: pass --redfish-serve-tls-cert and --redfish-serve-tls-key, or advertise an http:// URL", source, serveURL)
		}
	case "http":
		if usesTLS {
			return fmt.Errorf("%s is %q, but --redfish-serve-tls-cert and --redfish-serve-tls-key make the Redfish ISO server serve HTTPS: advertise an https:// URL, or drop the TLS flags", source, serveURL)
		}
	default:
		return fmt.Errorf("%s is %q, which a BMC cannot fetch: the advertised base URL must use http or https", source, serveURL)
	}
	return nil
}

// redfishServeURLSeed returns the advertised URL to seed the image-source
// settings with, but only when a local ISO-serve listener was actually
// configured. With no listener the advertised URL is irrelevant (local serving
// cannot be enabled), so seeding it would be misleading.
func redfishServeURLSeed(isoServe *isoserve.Server, serveURL string) string {
	if isoServe == nil {
		return ""
	}
	return serveURL
}

func generateToken(bytes int) string {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		log.Fatalf("Failed to generate token: %v", err)
	}
	return hex.EncodeToString(b)
}
