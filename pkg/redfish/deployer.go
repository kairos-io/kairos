// Package redfish provides a gofish-backed Deployer that performs spec-compliant
// Redfish virtual-media deployments: it discovers Systems/Managers (no hardcoded
// IDs), inserts media by URL (VirtualMedia.InsertMedia), sets a one-time boot
// override, resets the system with an explicit ResetType, and polls the returned
// Task to a terminal state. The gofish *APIClient is kept private; callers only
// ever see our own types.
package redfish

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/stmcginnis/gofish"
	"github.com/stmcginnis/gofish/schemas"
)

// taskPollInterval is the delay between Task polls. Kept modest so a cancelled
// context aborts promptly.
const taskPollInterval = 3 * time.Second

// powerOffTimeout bounds how long Finalize's power-cycle path waits for the system
// to reach the Off PowerState after the shutdown request. It is deliberately short:
// a graceful shutdown that has not landed within this window is escalated to a
// ForceOff, and if even the off-poll never reports Off we proceed with the eject
// anyway (the eject is what breaks the install loop). Honour ctx alongside it.
const powerOffTimeout = 60 * time.Second

// powerPollInterval is the delay between PowerState polls during the power-cycle
// finalize. Kept modest so a cancelled context aborts promptly.
const powerPollInterval = 3 * time.Second

// Deployer drives a Redfish virtual-media deployment against one BMC. Create it
// with NewDeployer, call Connect, defer Close (which logs the session out), then
// Inspect and/or Deploy. It is not safe for concurrent use.
type Deployer struct {
	endpoint  string
	username  string
	password  string
	vendor    VendorType
	verifySSL bool
	timeout   time.Duration

	// systemID, when non-empty, pins the ComputerSystem the Deployer targets by
	// its Redfish Id (the last path segment of the member, e.g. the libvirt
	// domain UUID under sushy-tools). It is the fail-safe selector: with more than
	// one system exposed, selection is required rather than silently picking [0].
	systemID string

	// quirks is the per-vendor profile selected from vendor. For VendorGeneric it
	// is the zero-value (all-nil-hook) profile, so the default path is unchanged.
	quirks quirks

	// authMode selects the authentication scheme (auto/session/basic). Empty is
	// normalised to AuthModeAuto in NewDeployer.
	authMode AuthMode

	// usedBasicAuth records the scheme actually used by the last successful
	// Connect: true when the connection authenticates with HTTP Basic per request
	// (no session), false when a deletable Redfish session was established. Close
	// reads it to decide whether a session DELETE (Logout) is required.
	usedBasicAuth bool

	// client is the live gofish connection. It is private by design: gofish types
	// must not leak into pkg/hardware or the CLI (decision D1 guardrail).
	client *gofish.APIClient

	// deadline is when the timeout, counted from Connect, runs out. The gofish
	// client is bound to a context with this deadline, and Deploy stops polling at
	// it. cancel releases that context; Close calls it.
	deadline time.Time
	cancel   context.CancelFunc
}

// Config holds the connection parameters for a Deployer.
type Config struct {
	Endpoint  string
	Username  string
	Password  string
	Vendor    VendorType
	VerifySSL bool
	Timeout   time.Duration
	// SystemID optionally pins the target ComputerSystem by its Redfish Id (the
	// last path segment of the member). Leave empty when the BMC exposes exactly
	// one system. It is REQUIRED when the BMC exposes more than one: selectSystem
	// refuses to guess and lists the available Ids instead.
	SystemID string
	// AuthMode selects the authentication scheme: AuthModeAuto (default/empty),
	// AuthModeSession, or AuthModeBasic. Auto pre-checks the ServiceRoot and uses
	// session auth when a SessionService is advertised, falling back to Basic auth
	// otherwise. An unknown value is rejected at Connect.
	AuthMode AuthMode
}

// NewDeployer builds a Deployer from a Config. It does not open a connection;
// call Connect for that. VerifySSL defaults to enforcing TLS verification: the
// only way to disable it is an explicit VerifySSL:false, so there is no silent
// downgrade.
func NewDeployer(cfg Config) *Deployer {
	vendor := cfg.Vendor
	if vendor == "" {
		vendor = VendorGeneric
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Minute
	}
	authMode := cfg.AuthMode
	if authMode == "" {
		authMode = AuthModeAuto
	}
	return &Deployer{
		endpoint:  cfg.Endpoint,
		username:  cfg.Username,
		password:  cfg.Password,
		vendor:    vendor,
		verifySSL: cfg.VerifySSL,
		timeout:   timeout,
		systemID:  cfg.SystemID,
		authMode:  authMode,
		// Resolve through the process-wide registry: name-first (operator/built-in),
		// then the VendorType mapping, then generic. This honours operator-supplied
		// profiles installed via SetDefaultRegistry while preserving the invariant
		// that an unknown vendor/name resolves to the spec-default generic profile.
		quirks: resolveQuirks(string(vendor)),
	}
}

// Connect opens an authenticated Redfish connection. The scheme is chosen by the
// Deployer's AuthMode:
//
//   - AuthModeSession: a deletable Redfish session is created with a JSON
//     credential body and an X-Auth-Token is obtained; Close tears it down.
//   - AuthModeBasic: HTTP Basic auth is used, sending credentials on every
//     request; no session is created, so Close is a no-op.
//   - AuthModeAuto (default): the ServiceRoot is pre-checked; session auth is used
//     when a SessionService is advertised, otherwise it falls back to Basic auth.
//
// The supplied context bounds the connection setup. Config.Timeout starts
// counting here: every Redfish request on the connection, and Deploy's task poll,
// fail once it has run out. An unknown AuthMode is rejected here.
func (d *Deployer) Connect(ctx context.Context) error {
	if d.client != nil {
		return errors.New("already connected")
	}

	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	deadline, _ := ctx.Deadline()

	basicAuth, err := d.resolveBasicAuth(ctx)
	if err != nil {
		cancel()
		return err
	}

	cfg := gofish.ClientConfig{
		Endpoint: d.endpoint,
		Username: d.username,
		Password: d.password,
		// Insecure is the inverse of VerifySSL; verification stays on unless the
		// caller explicitly opted out.
		Insecure: !d.verifySSL,
		// BasicAuth false means token/session auth (a deletable session); true means
		// HTTP Basic per request (no session). Chosen by resolveBasicAuth above.
		BasicAuth: basicAuth,
		// Inject a defensive HTTP client that rejects cross-origin redirects and
		// caps response-body size, so a compromised BMC cannot bounce our
		// credentialed client to another host or OOM us. We set NoModifyTransport
		// because our wrapping RoundTripper is not a bare *http.Transport, so gofish
		// must not try to mutate it — TLS verification is configured inside the
		// defensive client (mirroring Insecure) instead.
		HTTPClient:        newDefensiveHTTPClient(!d.verifySSL),
		NoModifyTransport: true,
	}

	client, err := gofish.ConnectContext(ctx, cfg)
	if err != nil {
		cancel()
		// gofish error strings can echo the endpoint but never the credentials;
		// scrub defensively in case a future version changes that.
		return fmt.Errorf("connecting to Redfish service: %w", d.scrub(err))
	}
	d.client = client
	d.deadline = deadline
	d.cancel = cancel
	d.usedBasicAuth = basicAuth
	return nil
}

// resolveBasicAuth maps the Deployer's AuthMode to the gofish BasicAuth flag:
// false selects session auth, true selects HTTP Basic auth. For AuthModeAuto it
// pre-checks the ServiceRoot and falls back to Basic auth when the endpoint
// exposes no SessionService. An unknown AuthMode is rejected.
func (d *Deployer) resolveBasicAuth(ctx context.Context) (bool, error) {
	switch d.authMode {
	case AuthModeSession:
		return false, nil
	case AuthModeBasic:
		return true, nil
	case AuthModeAuto:
		hasSession, err := d.serviceRootHasSessionService(ctx)
		if err != nil {
			return false, fmt.Errorf("auto-detecting Redfish auth mode: %w", d.scrub(err))
		}
		if hasSession {
			return false, nil
		}
		// No SessionService: fall back to Basic auth. Credentials are sent on every
		// request and there is no session to tear down. The note is credential-free.
		log.Printf("redfish: endpoint %s advertises no SessionService; falling back to HTTP Basic auth (credentials are sent on every request, no session to tear down)", d.endpoint)
		return true, nil
	default:
		return false, fmt.Errorf("unknown Redfish auth mode %q (valid: auto, session, basic)", d.authMode)
	}
}

// serviceRootHasSessionService performs a lightweight unauthenticated GET of the
// Redfish ServiceRoot and reports whether it advertises a SessionService — either
// a top-level SessionService.@odata.id or a Links.Sessions.@odata.id. It reuses
// the defensive HTTP client (cross-origin redirect reject + body cap + TLS-verify
// honouring Insecure). The endpoint URL it fetches carries no credentials.
func (d *Deployer) serviceRootHasSessionService(ctx context.Context) (bool, error) {
	root, err := fetchServiceRoot(ctx, d.endpoint, !d.verifySSL)
	if err != nil {
		return false, err
	}
	return root.SessionService.ODataID != "" || root.Links.Sessions.ODataID != "", nil
}

// serviceRoot is the minimal subset of the Redfish ServiceRoot we parse for the
// session-free reachability checks (auth-mode detection and Ping).
type serviceRoot struct {
	SessionService struct {
		ODataID string `json:"@odata.id"`
	} `json:"SessionService"`
	Links struct {
		Sessions struct {
			ODataID string `json:"@odata.id"`
		} `json:"Sessions"`
	} `json:"Links"`
}

// fetchServiceRoot performs an unauthenticated GET of {endpoint}/redfish/v1/ using
// the defensive HTTP client (cross-origin redirect reject + body cap + TLS-verify
// honouring insecure) and parses the minimal ServiceRoot shape. It sends NO
// credentials and creates NO session, so it never adds to a BMC's session count.
// A non-2xx status (including 401 on hardened BMCs) or an unparseable body is an
// error. The endpoint URL it fetches carries no credentials.
func fetchServiceRoot(ctx context.Context, endpoint string, insecure bool) (*serviceRoot, error) {
	rootURL := strings.TrimRight(endpoint, "/") + "/redfish/v1/"

	client := newDefensiveHTTPClient(insecure)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rootURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building ServiceRoot request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching ServiceRoot: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetching ServiceRoot: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading ServiceRoot: %w", err)
	}

	var root serviceRoot
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("parsing ServiceRoot: %w", err)
	}
	return &root, nil
}

// Close drops the connection. For session auth it logs the Redfish session out
// (DELETE on the session resource); for Basic auth there is no session, so Close
// is a safe no-op beyond clearing the client. It is safe to call on a
// never-connected or already-closed Deployer, and callers MUST defer it so a
// session is not leaked on either the success or the error path.
func (d *Deployer) Close() error {
	if d.client == nil {
		return nil
	}
	// Only tear down a session when one was actually established. Under Basic auth
	// gofish created no session, so Logout would have nothing to DELETE.
	if !d.usedBasicAuth {
		// Logout swaps in a fresh context when the bound one has expired, so a
		// timed-out deploy still deletes its session.
		d.client.Logout()
	}
	d.cancel()
	d.client = nil
	d.cancel = nil
	d.deadline = time.Time{}
	d.usedBasicAuth = false
	return nil
}

// Inspect selects the target ComputerSystem (see selectSystem) and returns its
// typed hardware summary. Memory and CPU come from the nested
// MemorySummary/ProcessorSummary (fixing the historical 0/0 bug).
func (d *Deployer) Inspect(ctx context.Context) (*SystemInfo, error) {
	if d.client == nil {
		return nil, errors.New("not connected: call Connect first")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	system, err := d.selectSystem()
	if err != nil {
		return nil, err
	}

	info := &SystemInfo{
		ID:           system.ID,
		Name:         system.Name,
		Model:        system.Model,
		Manufacturer: system.Manufacturer,
		SerialNumber: system.SerialNumber,
		PowerState:   string(system.PowerState),
		// Capabilities we could positively determine from the ComputerSystem (e.g.
		// UEFI boot support). Anything not detected is omitted, and the hardware
		// gate treats a required-but-undetected feature as unsupported.
		Features: detectFeatures(system),
	}
	if v := system.MemorySummary.TotalSystemMemoryGiB; v != nil {
		info.MemoryGiB = int(*v)
	}
	if v := system.ProcessorSummary.Count; v != nil {
		info.ProcessorCount = int(*v)
	}
	return info, nil
}

// Deploy performs the full virtual-media deployment flow against the discovered
// system: InsertMedia (URL-pull) -> one-time boot override -> Reset (with an
// explicit ResetType) -> Task poll to terminal. It honours context cancellation
// between and during steps, and gives up once Config.Timeout, counted from
// Connect, has run out.
func (d *Deployer) Deploy(ctx context.Context, req DeployRequest) (*DeployResult, error) {
	if d.client == nil {
		return nil, errors.New("not connected: call Connect first")
	}
	ctx, cancel := context.WithDeadline(ctx, d.deadline)
	defer cancel()
	if strings.TrimSpace(req.ImageURL) == "" {
		return nil, errors.New("DeployRequest.ImageURL is required (InsertMedia is URL-pull)")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	report := newProgressReporter(req.Progress)

	result := &DeployResult{StartedAt: time.Now()}

	report("discovering", 10)
	system, err := d.selectSystem()
	if err != nil {
		return nil, err
	}
	result.SystemID = system.ID

	media, mediaView, err := d.findCDMedia(system)
	if err != nil {
		return nil, err
	}
	result.MediaID = media.ID

	// 1. Insert media (URL-pull). The BMC fetches the ISO from ImageURL.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	report("inserting media", 30)
	insertTask, err := d.insertMedia(media, mediaView, req)
	if err != nil {
		return nil, fmt.Errorf("inserting virtual media: %w", d.scrub(enrichRedfishError(err)))
	}

	// 2. One-time boot override (Cd/UEFI by default).
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	report("setting boot", 50)
	if err := d.setOneTimeBoot(system, req); err != nil {
		return nil, fmt.Errorf("setting one-time boot override: %w", d.scrub(enrichRedfishError(err)))
	}

	// 3. Reset with an explicit ResetType chosen from the current power state and
	//    the system's allowable values.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	report("resetting", 70)
	resetTask, err := d.reset(system, req)
	if err != nil {
		return nil, fmt.Errorf("resetting system: %w", d.scrub(enrichRedfishError(err)))
	}

	// 4. Poll any returned Task to a terminal state. Prefer the reset task (the
	//    operation that actually drives the install boot); fall back to the
	//    InsertMedia task if that is the only async one.
	taskURI := pickTaskURI(resetTask, insertTask)
	if taskURI != "" {
		report("polling task", 80)
		state, msgs, err := d.pollTask(ctx, taskURI, report)
		if err != nil && time.Now().After(d.deadline) {
			return nil, fmt.Errorf("deployment task did not finish within the %s timeout: %w", d.timeout, d.scrub(err))
		}
		if err != nil {
			return nil, fmt.Errorf("polling deployment task: %w", d.scrub(err))
		}
		result.TaskCompleted = state == schemas.CompletedTaskState
		result.TaskState = string(state)
		result.Messages = msgs
		if state == schemas.ExceptionTaskState || state == schemas.KilledTaskState || state == schemas.CancelledTaskState {
			return result, fmt.Errorf("deployment task ended in state %q: %s", state, strings.Join(msgs, "; "))
		}
	}

	// NOTE: media is intentionally NOT ejected here. Ejecting at deploy-Task
	// completion is mid-install — the BMC is still booting the installer ISO. On
	// BMCs that honour the one-time boot override the media can stay mounted across
	// the install reboot harmlessly; on BMCs that ignore it, ejecting now would not
	// help and ejecting at the wrong moment can disrupt the running install. Eject
	// is decoupled into Finalize, driven by an "OS is up" signal (phone-home or a
	// manual operator action). req.EjectAfter is deprecated and ignored.

	report("completed", 100)
	result.FinishedAt = time.Now()
	return result, nil
}

// Finalize ejects the virtual media and best-effort steers the next boot to disk.
// It is the signal-driven counterpart to Deploy: call it once the freshly-installed
// OS is up (phone-home) or on an explicit operator action, NOT at deploy-Task
// completion. The eject is load-bearing — it is what breaks the post-install
// install loop on BMCs that ignore the one-time boot override, because an empty CD
// falls through to disk. The boot-to-disk PATCH is opportunistic: some BMCs/emulators
// (sushy-tools) error on a PATCH that clears the boot source, and the lab confirmed
// eject alone is sufficient, so a boot-PATCH error is logged and swallowed rather
// than failing the finalize.
//
// Flow (default, in-place): selectSystem (honours SystemID) -> findCDMedia ->
// EjectMedia -> opportunistic boot-to-disk. The caller owns Connect/Close (session
// teardown), mirroring Deploy.
//
// Flow (req.PowerCycle): selectSystem -> findCDMedia -> power OFF (GracefulShutdown,
// escalating to ForceOff, polled to the Off PowerState within powerOffTimeout) ->
// EjectMedia (now applied while the machine is off) -> opportunistic boot-to-disk ->
// power ON. This is the robust mode for BMCs/emulators that do not apply a live eject.
// The power-off poll degrades gracefully: a poll timeout is logged and the eject is
// attempted anyway rather than aborting the whole finalize.
func (d *Deployer) Finalize(ctx context.Context, req FinalizeRequest) error {
	if d.client == nil {
		return errors.New("not connected: call Connect first")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	report := newProgressReporter(req.Progress)

	report("discovering", 20)
	system, err := d.selectSystem()
	if err != nil {
		return err
	}

	media, _, err := d.findCDMedia(system)
	if err != nil {
		return err
	}

	if req.PowerCycle {
		return d.finalizePowerCycle(ctx, system, media, report)
	}

	// Eject is load-bearing: this is the step that breaks the install loop.
	if err := ctx.Err(); err != nil {
		return err
	}
	report("ejecting media", 60)
	if _, err := media.EjectMedia(); err != nil {
		return fmt.Errorf("ejecting virtual media: %w", d.scrub(enrichRedfishError(err)))
	}

	// Best-effort boot-to-disk. Never fail Finalize on a boot-PATCH error: eject
	// alone already breaks the loop, and some BMCs reject clearing the boot source.
	report("boot to disk", 80)
	d.bootToDisk(system)

	report("completed", 100)
	return nil
}

// finalizePowerCycle runs the opt-in power-cycle finalize: power off -> eject ->
// boot-to-disk -> power on. It is the robust path for BMCs/emulators that report a
// live eject but keep the running machine booting the ISO; performing the eject while
// the system is powered off forces the BMC to apply it.
//
// The power-off is graceful-first (GracefulShutdown, falling back to ForceOff if the
// system has not reached Off within powerOffTimeout) and the PowerState is polled to
// Off. A power-off poll timeout is NON-fatal: it is logged and the eject proceeds, so
// a stubborn power-state report never blocks the load-bearing eject. Context
// cancellation is honoured throughout. The final power-on is a best-effort On Reset.
func (d *Deployer) finalizePowerCycle(ctx context.Context, system *schemas.ComputerSystem, media *schemas.VirtualMedia, report progressFunc) error {
	// 1. Power off (graceful first, escalate to ForceOff, poll to Off). A timeout
	//    here is logged and swallowed: we still eject below.
	if err := ctx.Err(); err != nil {
		return err
	}
	report("powering off", 40)
	if err := d.powerOff(ctx, system); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		// Degrade gracefully: a power-off poll timeout (or a transient reset error)
		// must not abort the finalize — proceed to the load-bearing eject regardless.
		log.Printf("redfish: power-off before eject did not confirm Off (proceeding to eject anyway): %v", d.scrub(err))
	}

	// 2. Eject is load-bearing and now applied while the system is off.
	if err := ctx.Err(); err != nil {
		return err
	}
	report("ejecting media", 60)
	if _, err := media.EjectMedia(); err != nil {
		return fmt.Errorf("ejecting virtual media: %w", d.scrub(enrichRedfishError(err)))
	}

	// 3. Best-effort boot-to-disk, same as the in-place path.
	report("boot to disk", 80)
	d.bootToDisk(system)

	// 4. Power back on. Best-effort: the eject already broke the loop; a power-on
	//    failure is logged, not fatal, so operators can still power the node on.
	if err := ctx.Err(); err != nil {
		return err
	}
	report("powering on", 90)
	if _, err := system.Reset(schemas.OnResetType); err != nil {
		log.Printf("redfish: powering system back on after eject failed (media is ejected; power it on manually): %v", d.scrub(enrichRedfishError(err)))
	}

	report("completed", 100)
	return nil
}

// powerOff drives the system to the Off PowerState. It issues a GracefulShutdown
// (preferred, validated against the system's allowable values) and polls the
// PowerState until Off or powerOffTimeout. If the graceful shutdown has not landed
// within the window it escalates to a ForceOff and polls once more. A system already
// Off returns immediately. The supplied ctx bounds the whole operation and is honoured
// during polling; a poll that exhausts powerOffTimeout (but not ctx) returns a
// non-cancellation error the caller may log-and-proceed on.
func (d *Deployer) powerOff(ctx context.Context, system *schemas.ComputerSystem) error {
	if system.PowerState == schemas.OffPowerState {
		return nil
	}

	graceful := firstSupportedPowerOff(system, schemas.GracefulShutdownResetType, schemas.ForceOffResetType)
	if _, err := system.Reset(graceful); err != nil {
		return fmt.Errorf("requesting power off (%s): %w", graceful, enrichRedfishError(err))
	}

	if err := d.pollPowerOff(ctx, system); err == nil {
		return nil
	} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	// Graceful shutdown did not reach Off in time: escalate to a hard ForceOff and
	// poll once more. If ForceOff is not advertised we still try it (the BMC will
	// reject it with a useful error, surfaced by the poll's final state).
	if force := firstSupportedPowerOff(system, schemas.ForceOffResetType); force != graceful {
		if _, err := system.Reset(force); err != nil {
			return fmt.Errorf("requesting forced power off (%s): %w", force, enrichRedfishError(err))
		}
	}
	return d.pollPowerOff(ctx, system)
}

// pollPowerOff polls the system's PowerState until it reports Off, ctx is cancelled,
// or powerOffTimeout elapses. It re-fetches the ComputerSystem each tick (the gofish
// struct is a snapshot; PowerState does not update in place). A timeout returns a
// non-cancellation error so the caller can degrade gracefully and still eject.
func (d *Deployer) pollPowerOff(ctx context.Context, system *schemas.ComputerSystem) error {
	deadline := time.Now().Add(powerOffTimeout)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		fresh, err := schemas.GetComputerSystem(d.client, system.ODataID)
		if err == nil && fresh.PowerState == schemas.OffPowerState {
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("system did not reach Off PowerState within %s", powerOffTimeout)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(powerPollInterval):
		}
	}
}

// firstSupportedPowerOff returns the first preference advertised in the system's
// allowable ResetTypes; when the system advertises nothing it returns the first
// preference unchanged (the BMC then rejects an unsupported type with a useful error).
func firstSupportedPowerOff(system *schemas.ComputerSystem, preferences ...schemas.ResetType) schemas.ResetType {
	allowed, _ := system.GetSupportedResetTypes()
	return firstSupported(allowed, preferences...)
}

// bootToDisk best-effort steers the next boot to disk. It first tries to clear the
// one-time boot override (BootSourceOverrideEnabled: Disabled), which on a
// spec-compliant BMC drops back to the persistent BIOS/UEFI boot order (now the
// freshly-installed disk). If that PATCH errors (sushy-tools has been observed to
// reject clearing the source), it falls back to an explicit one-time Hdd override.
// Both errors are logged credential-free and swallowed — see Finalize.
func (d *Deployer) bootToDisk(system *schemas.ComputerSystem) {
	disabled := &schemas.Boot{
		BootSourceOverrideEnabled: schemas.DisabledBootSourceOverrideEnabled,
	}
	if err := system.SetBoot(disabled); err == nil {
		return
	} else {
		log.Printf("redfish: clearing boot override failed (eject already broke the loop); trying one-time Hdd: %v", d.scrub(enrichRedfishError(err)))
	}

	hdd := &schemas.Boot{
		BootSourceOverrideEnabled: schemas.OnceBootSourceOverrideEnabled,
		BootSourceOverrideTarget:  schemas.HddBootSource,
	}
	if err := system.SetBoot(hdd); err != nil {
		log.Printf("redfish: one-time Hdd boot override also failed (eject already broke the loop, proceeding): %v", d.scrub(enrichRedfishError(err)))
	}
}

// selectSystem discovers the Systems collection and resolves the single
// ComputerSystem to operate on. It is fail-safe: it never silently picks the
// first member when more than one exists.
//
//   - With d.systemID set, it returns the member whose Redfish Id matches
//     exactly (the gofish ComputerSystem.ID, i.e. the last path segment of the
//     member @odata.id — sushy-tools uses the libvirt domain UUID here). If
//     nothing matches it errors and lists the available Ids so the operator can
//     pick.
//   - With d.systemID empty it uses the sole member when there is exactly one;
//     errors when there are none; and, crucially, errors when there is more than
//     one — listing the available Ids and requiring an explicit selection rather
//     than resetting an arbitrary machine.
//
// No IDs are hardcoded; matching is exact on the Redfish Id.
func (d *Deployer) selectSystem() (*schemas.ComputerSystem, error) {
	systems, err := d.client.GetService().Systems()
	if err != nil {
		return nil, fmt.Errorf("discovering systems: %w", d.scrub(err))
	}
	if len(systems) == 0 {
		return nil, errors.New("no ComputerSystem members found on the Redfish service")
	}

	if d.systemID != "" {
		for _, sys := range systems {
			if sys.ID == d.systemID {
				return sys, nil
			}
		}
		return nil, fmt.Errorf("no ComputerSystem with Id %q found on the Redfish service; available system Ids: %s",
			d.systemID, strings.Join(systemIDs(systems), ", "))
	}

	if len(systems) > 1 {
		return nil, fmt.Errorf("the Redfish service exposes %d ComputerSystems; a system selection is required (set SystemID): available system Ids: %s",
			len(systems), strings.Join(systemIDs(systems), ", "))
	}

	return systems[0], nil
}

// systemIDs returns the Redfish Ids of the given systems, preserving the
// collection order, for inclusion in selection-error messages.
func systemIDs(systems []*schemas.ComputerSystem) []string {
	ids := make([]string, 0, len(systems))
	for _, sys := range systems {
		ids = append(ids, sys.ID)
	}
	return ids
}

// findCDMedia locates a CD/DVD-capable VirtualMedia resource. Per the spec it may
// hang off the ComputerSystem or the Manager. The spec-default search order is
// System first, then each Manager; a vendor quirk profile (e.g. HPE iLO) may
// reorder this. A member with no advertised MediaTypes is accepted as a last
// resort (some BMCs omit the field).
//
// The mediaSearch quirk hook works over a read-only []MediaView (the §2 boundary):
// the core flattens the spec-default collections into the candidate list, lets the
// hook return an ordering of indexes, then maps those indexes back to its own
// *VirtualMedia slice (out-of-range/duplicate indexes dropped). The hook never
// sees a gofish object or the *Deployer.
func (d *Deployer) findCDMedia(system *schemas.ComputerSystem) (*schemas.VirtualMedia, MediaView, error) {
	candidates, views := d.mediaCandidates(system)

	order := defaultMediaOrder(len(candidates))
	if d.quirks.mediaSearch != nil {
		if reordered := sanitizeMediaOrder(d.quirks.mediaSearch(views), len(candidates)); len(reordered) > 0 {
			order = reordered
		}
	}

	fallback := -1
	for _, i := range order {
		if mediaSupportsCD(candidates[i]) {
			return candidates[i], views[i], nil
		}
		if fallback < 0 && len(candidates[i].MediaTypes) == 0 {
			fallback = i
		}
	}
	if fallback >= 0 {
		return candidates[fallback], views[fallback], nil
	}
	return nil, MediaView{}, errors.New("no CD/DVD-capable VirtualMedia resource found on the system or its managers")
}

// mediaCandidates flattens the spec-default VirtualMedia collections (System-hosted
// first, then each Manager's) into a parallel pair: the gofish members the core
// will act on, and their read-only MediaView projections handed to a quirk hook.
// The two slices share indexes, which is how a hook's returned ordering maps back
// to the real members. Manager-hosted members are labelled "manager:<id>" and
// System-hosted ones "system" so a profile can select by location.
func (d *Deployer) mediaCandidates(system *schemas.ComputerSystem) ([]*schemas.VirtualMedia, []MediaView) {
	var (
		members []*schemas.VirtualMedia
		views   []MediaView
	)
	add := func(vms []*schemas.VirtualMedia, location string) {
		for _, vm := range vms {
			views = append(views, MediaView{
				Index:        len(members),
				ID:           vm.ID,
				Location:     location,
				MediaTypes:   mediaTypeStrings(vm.MediaTypes),
				ConnectedVia: string(vm.ConnectedVia),
				Inserted:     vm.Inserted != nil && *vm.Inserted,
			})
			members = append(members, vm)
		}
	}

	for _, c := range d.systemMediaCollections(system) {
		add(c, "system")
	}
	if managers, err := d.client.GetService().Managers(); err == nil {
		for _, m := range managers {
			if mgrMedia, err := m.VirtualMedia(); err == nil {
				add(mgrMedia, "manager:"+m.ID)
			}
		}
	}
	return members, views
}

// defaultMediaOrder returns the identity ordering 0..n-1, the spec-default search
// order used when no quirk hook reorders the candidates.
func defaultMediaOrder(n int) []int {
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	return order
}

// sanitizeMediaOrder constrains a quirk hook's returned ordering to valid,
// de-duplicated indexes into a candidate list of length n. Out-of-range and
// duplicate indexes are dropped — a profile can reorder/filter but never conjure a
// member that does not exist. An empty result tells the caller to fall back to the
// default order.
func sanitizeMediaOrder(order []int, n int) []int {
	if len(order) == 0 {
		return nil
	}
	seen := make(map[int]bool, len(order))
	out := make([]int, 0, len(order))
	for _, i := range order {
		if i < 0 || i >= n || seen[i] {
			continue
		}
		seen[i] = true
		out = append(out, i)
	}
	return out
}

// mediaTypeStrings projects gofish VirtualMediaType values to plain strings for a
// MediaView (the boundary carries no gofish types).
func mediaTypeStrings(types []schemas.VirtualMediaType) []string {
	if len(types) == 0 {
		return nil
	}
	out := make([]string, 0, len(types))
	for _, t := range types {
		out = append(out, string(t))
	}
	return out
}

// systemMediaCollections returns the VirtualMedia hosted on the ComputerSystem
// (zero or one collection). Errors are swallowed: a missing System.VirtualMedia
// link simply means there is nothing to search there. mediaCandidates uses it for
// the System-hosted half of the flat candidate list.
func (d *Deployer) systemMediaCollections(system *schemas.ComputerSystem) mediaCollections {
	if sysMedia, err := system.VirtualMedia(); err == nil {
		return mediaCollections{sysMedia}
	}
	return nil
}

// insertMedia performs the media insertion. By default this is the spec
// VirtualMedia.InsertMedia URL-pull action; a vendor profile may take over via the
// pushMedia hook (a stub seam for a future multipart push — no profile uses it
// today). The InsertMedia parameters and MediaType flow through the quirk profile
// so a vendor can tweak them without re-implementing the flow.
func (d *Deployer) insertMedia(media *schemas.VirtualMedia, view MediaView, req DeployRequest) (*schemas.TaskMonitorInfo, error) {
	// Future multipart-push extension point. No profile implements it yet; the
	// default (nil hook) always falls through to the URL-pull InsertMedia below.
	if d.quirks.pushMedia != nil {
		if handled, info, err := d.quirks.pushMedia(d, media, req); handled {
			return info, err
		}
	}

	inserted := true
	writeProtected := true

	mediaType := schemas.CDVirtualMediaType
	if d.quirks.mediaType != nil {
		mediaType = d.quirks.mediaType(view)
	}

	params := &schemas.VirtualMediaInsertMediaParameters{
		Image:          req.ImageURL,
		Inserted:       &inserted,
		WriteProtected: &writeProtected,
		MediaType:      mediaType,
	}
	protocol := schemas.HTTPTransferProtocolType
	if req.TransferProtocolHTTPS {
		protocol = schemas.HTTPSTransferProtocolType
	}
	params.TransferProtocolType = &protocol

	// tuneInsertParams works over a read-only InsertParamsView (which carries NO
	// image URL) and returns a sparse patch the core applies to the allowlisted
	// fields. The image URL is core-owned and SSRF-validated, so it is structurally
	// out of a profile's reach.
	if d.quirks.tuneInsertParams != nil {
		applyInsertPatch(params, d.quirks.tuneInsertParams(insertParamsView(params)))
	}

	return media.InsertMedia(params)
}

// insertParamsView projects the spec-default InsertMedia parameters into the
// read-only InsertParamsView handed to the tuneInsertParams hook. It deliberately
// drops the image URL, exposing only HasImage.
func insertParamsView(p *schemas.VirtualMediaInsertMediaParameters) InsertParamsView {
	v := InsertParamsView{
		MediaType: string(p.MediaType),
		HasImage:  p.Image != "",
	}
	if p.TransferProtocolType != nil {
		v.TransferProtocolType = string(*p.TransferProtocolType)
	}
	if p.Inserted != nil {
		v.Inserted, v.InsertedSet = *p.Inserted, true
	}
	if p.WriteProtected != nil {
		v.WriteProtected, v.WriteProtectedSet = *p.WriteProtected, true
	}
	return v
}

// applyInsertPatch applies a tuneInsertParams hook's sparse patch to the
// InsertMedia parameters. Only the allowlisted fields are reachable; the image URL
// is never touched. Clear takes precedence over Set for TransferProtocolType so a
// profile can unconditionally drop it.
func applyInsertPatch(p *schemas.VirtualMediaInsertMediaParameters, patch InsertParamsPatch) {
	if patch.SetMediaType != "" {
		p.MediaType = schemas.VirtualMediaType(patch.SetMediaType)
	}
	switch {
	case patch.ClearTransferProtocolType:
		p.TransferProtocolType = nil
	case patch.SetTransferProtocolType != "":
		t := schemas.TransferProtocolType(patch.SetTransferProtocolType)
		p.TransferProtocolType = &t
	}
	if patch.SetInsertedSet {
		v := patch.SetInserted
		p.Inserted = &v
	}
	if patch.SetWriteProtectedSet {
		v := patch.SetWriteProtected
		p.WriteProtected = &v
	}
}

// setOneTimeBoot patches the system to boot once from the requested target. The
// firmware boot mode is only set when the operator explicitly asks for one
// (req.BootMode != ""): forcing BootSourceOverrideMode makes some BMCs/emulators
// (e.g. sushy-tools on libvirt) reconfigure the domain firmware, which can fail
// (an OVMF enrolled-keys/secure-boot conflict). With BootMode empty,
// BootSourceOverrideMode is left unset so gofish omits it from the PATCH (the
// field is `json:",omitempty"`), leaving the system in its current firmware mode.
func (d *Deployer) setOneTimeBoot(system *schemas.ComputerSystem, req DeployRequest) error {
	boot := &schemas.Boot{
		BootSourceOverrideEnabled: schemas.OnceBootSourceOverrideEnabled,
		BootSourceOverrideTarget:  bootSource(req.BootTarget),
	}
	if req.BootMode != "" {
		boot.BootSourceOverrideMode = bootMode(req.BootMode)
	}
	return system.SetBoot(boot)
}

// reset powers/restarts the system using an explicit ResetType. The caller can
// force a ResetType; otherwise one is chosen from the current power state and
// validated against the system's allowable values.
//
// A vendor profile may prefer a different ResetType via the resetType hook (over a
// read-only ResetView). It only runs when the caller did not force one, so an
// explicit DeployRequest.ResetType always wins. The hook's preference is still
// re-validated against the system's advertised allowable values (firstSupported):
// a profile can prefer but never force an unsupported type.
func (d *Deployer) reset(system *schemas.ComputerSystem, req DeployRequest) (*schemas.TaskMonitorInfo, error) {
	rt := schemas.ResetType(req.ResetType)
	if rt == "" {
		rt = chooseResetType(system)
		if d.quirks.resetType != nil {
			allowed, _ := system.GetSupportedResetTypes()
			preferred := d.quirks.resetType(ResetView{
				PowerState:          string(system.PowerState),
				AllowableResetTypes: resetTypeStrings(allowed),
				Default:             string(rt),
			})
			// Re-validate the hook's preference: if the BMC advertises allowable
			// values and the preference is not among them, fall back to the core
			// default rather than sending an unsupported type.
			rt = firstSupported(allowed, preferred, rt)
		}
	}
	return system.Reset(rt)
}

// resetTypeStrings projects gofish ResetType values to plain strings for a
// ResetView (the boundary carries no gofish types).
func resetTypeStrings(types []schemas.ResetType) []string {
	if len(types) == 0 {
		return nil
	}
	out := make([]string, 0, len(types))
	for _, t := range types {
		out = append(out, string(t))
	}
	return out
}

// pollTask GETs the Task at uri repeatedly until it reaches a terminal state or
// the context is cancelled. While polling it reports progress between
// taskPollFloor and taskPollCeil, scaling the Task's PercentComplete when present
// so callers see live movement during a long install boot.
func (d *Deployer) pollTask(ctx context.Context, uri string, report progressFunc) (schemas.TaskState, []string, error) {
	for {
		if err := ctx.Err(); err != nil {
			return "", nil, err
		}

		task, err := schemas.GetTask(d.client, uri)
		if err != nil {
			return "", nil, fmt.Errorf("getting task %s: %w", uri, d.scrub(enrichRedfishError(err)))
		}

		// Reject an unknown/garbage TaskState rather than looping forever on it. A
		// state outside the Redfish enum means the BMC returned something we cannot
		// reason about; the context still bounds the loop, but a clear error is far
		// more useful than silently spinning until the deadline.
		if !isKnownTaskState(task.TaskState) {
			return "", nil, fmt.Errorf("BMC returned an unknown Redfish TaskState %q for task %s", task.TaskState, uri)
		}

		report("polling task", taskPollPercent(task))

		if isTerminalTaskState(task.TaskState) {
			return task.TaskState, taskMessages(task), nil
		}

		select {
		case <-ctx.Done():
			return "", nil, ctx.Err()
		case <-time.After(taskPollInterval):
		}
	}
}

// scrub removes credentials from an error before it crosses the package boundary.
// gofish does not place credentials in error strings, but a creds-bearing URL
// could theoretically appear; redact basic-auth userinfo defensively.
func (d *Deployer) scrub(err error) error {
	return scrubError(err, d.password)
}
