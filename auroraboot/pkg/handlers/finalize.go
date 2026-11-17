package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/kairos-io/AuroraBoot/pkg/redfish"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
)

// finalizeTimeout bounds a single eject/finalize session (Connect -> discover ->
// EjectMedia -> best-effort boot-to-disk -> Close). It is far shorter than a
// deploy: there is no media fetch or install boot to wait on.
const finalizeTimeout = 2 * time.Minute

// ejectFallbackWindow time-boxes the unlinked single-pending auto-eject fallback.
// When there is no node<->BMC link, we only treat the sole pending deployment as
// "this node's" if it completed recently — a phone-home long after the install is
// unlikely to be the freshly provisioned node and must not auto-eject on a guess.
// The window is generous (slow installs, first-boot config) but bounded; older
// pending deployments stay pending for the explicit manual Finalize endpoint.
const ejectFallbackWindow = 60 * time.Minute

// redfishFinalizer is the slice of the Redfish Deployer the finalize path needs.
// Keeping it an interface (mirroring hardware.systemInspector) lets tests inject a
// fake BMC client without standing up a real gofish connection.
type redfishFinalizer interface {
	Connect(ctx context.Context) error
	Finalize(ctx context.Context, req redfish.FinalizeRequest) error
	Close() error
}

// finalizerFactory builds a redfishFinalizer from connection params. The default
// (defaultFinalizerFactory) returns a real *redfish.Deployer; tests override it.
type finalizerFactory func(cfg redfish.Config) redfishFinalizer

// defaultFinalizerFactory builds a real gofish-backed Deployer.
func defaultFinalizerFactory(cfg redfish.Config) redfishFinalizer {
	return redfish.NewDeployer(cfg)
}

// finalizer returns the configured factory, defaulting to the real Deployer when
// none was injected.
func (h *DeployHandler) finalizer() finalizerFactory {
	if h.finalizerFn != nil {
		return h.finalizerFn
	}
	return defaultFinalizerFactory
}

// WithFinalizerFactory overrides the Deployer factory used by the finalize path.
// Intended for tests; production wiring leaves it nil so the real Deployer is used.
func (h *DeployHandler) WithFinalizerFactory(f finalizerFactory) *DeployHandler {
	h.finalizerFn = f
	return h
}

// runFinalize connects to the deployment's BMC, ejects the media (and best-effort
// boots to disk), and tears the session down. It does NOT touch EjectState — the
// caller owns the CAS lifecycle (so the same routine drives both the auto and the
// manual path). The BMC credentials are decrypted by the store on read and used
// only for this session; they are never logged.
func (h *DeployHandler) runFinalize(ctx context.Context, target *store.BMCTarget) error {
	deployer := h.finalizer()(redfish.Config{
		Endpoint:  target.Endpoint,
		Username:  target.Username,
		Password:  target.Password,
		Vendor:    redfish.VendorType(target.Vendor),
		VerifySSL: target.VerifySSL,
		SystemID:  target.SystemID,
		Timeout:   finalizeTimeout,
	})
	if err := deployer.Connect(ctx); err != nil {
		return fmt.Errorf("connecting to redfish endpoint: %w", err)
	}
	defer func() { _ = deployer.Close() }()

	if err := deployer.Finalize(ctx, redfish.FinalizeRequest{
		// Opt-in power-cycle: when the BMC target requests it, power the machine off
		// before ejecting and back on after, for BMCs/emulators that do not apply a
		// live eject. Default (false) preserves the in-place eject. This single
		// chokepoint covers every Finalize call site (manual finalize, eject, and the
		// auto eject-on-phone-home).
		PowerCycle: target.EjectPowerCycle,
	}); err != nil {
		return fmt.Errorf("finalizing (eject): %w", err)
	}
	return nil
}

// finalizeDeployment runs the full guarded finalize for one deployment: resolve its
// BMC, run the eject session, and CAS EjectState to a terminal value. It assumes the
// caller already won the CAS into store.EjectStateEjecting (so exactly one routine
// reaches here for a given deployment). On success it CASes ejecting->ejected and
// stamps EjectedAt; on failure ejecting->eject-failed with a scrubbed EjectError.
//
// A nil bmcTargetID is a programming error (the deploy path only sets pending when a
// BMC is attached); it is surfaced as eject-failed rather than panicking.
func (h *DeployHandler) finalizeDeployment(ctx context.Context, dep *store.Deployment) error {
	if dep.BMCTargetID == "" {
		h.markEjectFailed(ctx, dep.ID, "deployment has no BMC target to eject")
		return fmt.Errorf("deployment %s has no BMC target", dep.ID)
	}

	target, err := h.bmcTargets.GetByID(ctx, dep.BMCTargetID)
	if err != nil {
		h.markEjectFailed(ctx, dep.ID, "BMC target not found")
		return fmt.Errorf("resolving BMC target: %w", err)
	}

	if err := h.runFinalize(ctx, target); err != nil {
		h.markEjectFailed(ctx, dep.ID, scrubBMCError(err))
		return err
	}

	h.releaseServedISO(dep.ID)
	h.markEjected(ctx, dep.ID)
	return nil
}

// markEjected CASes ejecting->ejected and stamps EjectedAt. Best-effort: a store
// failure leaves the row in ejecting for the restart reconciler to reset.
func (h *DeployHandler) markEjected(ctx context.Context, id string) {
	ok, err := h.deployments.CASEjectState(ctx, id, store.EjectStateEjecting, store.EjectStateEjected)
	if err != nil || !ok {
		return
	}
	dep, err := h.deployments.GetByID(ctx, id)
	if err != nil {
		return
	}
	now := time.Now()
	dep.EjectedAt = &now
	dep.EjectError = ""
	_ = h.deployments.Update(ctx, dep)
}

// markEjectFailed CASes ejecting->eject-failed and records a scrubbed reason.
func (h *DeployHandler) markEjectFailed(ctx context.Context, id, reason string) {
	ok, err := h.deployments.CASEjectState(ctx, id, store.EjectStateEjecting, store.EjectStateFailed)
	if err != nil || !ok {
		return
	}
	dep, err := h.deployments.GetByID(ctx, id)
	if err != nil {
		return
	}
	dep.EjectError = reason
	_ = h.deployments.Update(ctx, dep)
}

// MaybeFinalizeForNode is the auto eject-on-phone-home entry point. It is called
// off the request goroutine from Register/Heartbeat (the "OS is up" signal). It
// finds a Redfish deployment pending eject that legitimately belongs to this node
// and finalizes it. Correlation is deliberately conservative (mirrors the BOLA
// hardening): it acts only on a deployment whose BMC is linked to this node, or —
// when no such link exists — on the SOLE unambiguous pending-eject deployment.
// When the correlation is ambiguous (zero, or more than one candidate) it does
// nothing and leaves the operator the manual Finalize action.
//
// It must add no latency to registration: callers spawn it on a background
// goroutine with the server base context.
func (h *DeployHandler) MaybeFinalizeForNode(ctx context.Context, nodeID string) {
	deps, err := h.deployments.List(ctx)
	if err != nil {
		return
	}

	var pending []*store.Deployment
	for _, dep := range deps {
		if dep.Method == "redfish" && dep.EjectState == store.EjectStatePending {
			pending = append(pending, dep)
		}
	}
	if len(pending) == 0 {
		return
	}

	chosen, viaFallback := h.correlateDeployment(ctx, nodeID, pending)
	if chosen == nil {
		// Ambiguous or unlinked: refuse to auto-eject. The operator can finalize
		// manually. This is the security-critical refusal — never finalize a
		// deployment a node is not legitimately correlated with.
		return
	}

	if viaFallback {
		// Loudly distinguish the heuristic fallback from a real node<->BMC link:
		// this path ejected a deployment a node is only *circumstantially*
		// correlated with (sole recent pending), and is worth auditing.
		log.Printf("finalize: node %s triggered eject of deployment %s via the unlinked single-pending fallback (no node<->BMC link)", nodeID, chosen.ID)
	} else {
		log.Printf("finalize: node %s triggered eject of deployment %s via node<->BMC link", nodeID, chosen.ID)
	}

	// CAS pending->ejecting: only one winner across concurrent phone-homes / a
	// racing manual finalize. The loser returns without touching the BMC.
	ok, err := h.deployments.CASEjectState(ctx, chosen.ID, store.EjectStatePending, store.EjectStateEjecting)
	if err != nil || !ok {
		return
	}

	// Record the node linkage now that we own the deployment.
	chosen.NodeID = nodeID
	chosen.EjectState = store.EjectStateEjecting
	_ = h.deployments.Update(ctx, chosen)

	_ = h.finalizeDeployment(ctx, chosen)
}

// correlateDeployment picks the one pending-eject deployment this node may finalize,
// or nil when the correlation is ambiguous. The second return reports whether the
// pick came from the unlinked single-pending fallback (true) rather than a real
// node<->BMC link (false), so the caller can audit-log the heuristic path. A nil
// deployment always pairs with false. Preference order:
//  1. deployments whose linked BMCTarget.NodeID == nodeID (or Deployment.NodeID ==
//     nodeID) — if exactly one, that's it (multiple linked is ambiguous -> nil).
//     The linked path is always honoured and is NOT time-boxed.
//  2. otherwise, when there is exactly ONE pending-eject deployment in total AND it
//     completed within ejectFallbackWindow, use it (the time-boxed fallback).
//  3. otherwise nil.
func (h *DeployHandler) correlateDeployment(ctx context.Context, nodeID string, pending []*store.Deployment) (*store.Deployment, bool) {
	var linked []*store.Deployment
	for _, dep := range pending {
		// A deployment already linked to this node (e.g. a manual finalize set it).
		if dep.NodeID == nodeID {
			linked = append(linked, dep)
			continue
		}
		if dep.BMCTargetID == "" {
			continue
		}
		target, err := h.bmcTargets.GetByID(ctx, dep.BMCTargetID)
		if err != nil {
			continue
		}
		if target.NodeID == nodeID {
			linked = append(linked, dep)
		}
	}
	if len(linked) == 1 {
		// A real link is always honoured regardless of age.
		return linked[0], false
	}
	if len(linked) > 1 {
		// Multiple deployments correlate to this node: ambiguous, refuse.
		return nil, false
	}
	// No explicit link. Fall back to the sole pending deployment ONLY when it
	// completed recently: a phone-home long after install is unlikely to be the
	// node we just provisioned, and auto-ejecting on that guess is unsafe. An
	// out-of-window deployment stays pending for the manual Finalize endpoint.
	if len(pending) == 1 && withinEjectFallbackWindow(pending[0]) {
		return pending[0], true
	}
	return nil, false
}

// withinEjectFallbackWindow reports whether dep completed recently enough for the
// unlinked single-pending fallback to act on it. A deployment with no CompletedAt
// stamp is treated as out of window (we cannot prove it is recent).
func withinEjectFallbackWindow(dep *store.Deployment) bool {
	if dep.CompletedAt == nil {
		return false
	}
	return time.Since(*dep.CompletedAt) <= ejectFallbackWindow
}

// --- Manual finalize / eject endpoints (admin-only) ---

// FinalizeDeployment handles POST /api/v1/deployments/:id/finalize. It is the
// universal, network-independent operator override: eject the media and boot to
// disk for one deployment, regardless of its eject policy. It is CAS-guarded so it
// cannot race the auto path: it claims pending|eject-failed|ejected -> ejecting and
// returns 409 when another finalize already holds the slot (state == ejecting).
func (h *DeployHandler) FinalizeDeployment(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()

	dep, err := h.deployments.GetByID(ctx, id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "deployment not found"})
	}
	if dep.BMCTargetID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "deployment has no BMC target to eject"})
	}

	// Claim the slot from any non-in-flight state. A manual finalize is allowed to
	// retry an eject-failed row and to re-eject an already-ejected one (idempotent
	// operator action), but never to double-run a finalize that is in flight.
	if !h.claimManualFinalize(ctx, dep) {
		return c.JSON(http.StatusConflict, map[string]string{"error": "a finalize is already in progress for this deployment"})
	}

	if err := h.finalizeDeployment(ctx, dep); err != nil {
		// finalizeDeployment already CAS'd the row to eject-failed and recorded the
		// scrubbed reason; surface it.
		updated, gerr := h.deployments.GetByID(ctx, id)
		if gerr == nil {
			return c.JSON(http.StatusInternalServerError, updated)
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": scrubBMCError(err)})
	}

	updated, err := h.deployments.GetByID(ctx, id)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "finalize succeeded but failed to read back deployment"})
	}
	return c.JSON(http.StatusOK, updated)
}

// claimManualFinalize CASes the deployment into ejecting from any of the allowed
// resting states (pending, eject-failed, ejected, or unset/off). It returns true on
// the winning transition. An unset EjectState ("") is allowed so an operator can
// finalize a completed deployment whose policy was off. It returns false only when
// the row is already ejecting (an in-flight finalize).
func (h *DeployHandler) claimManualFinalize(ctx context.Context, dep *store.Deployment) bool {
	for _, from := range []string{
		store.EjectStatePending,
		store.EjectStateFailed,
		store.EjectStateEjected,
		"", // policy was off: allow an explicit operator override.
	} {
		ok, err := h.deployments.CASEjectState(ctx, dep.ID, from, store.EjectStateEjecting)
		if err != nil {
			return false
		}
		if ok {
			dep.EjectState = store.EjectStateEjecting
			return true
		}
	}
	return false
}

// EjectBMCTarget handles POST /api/v1/bmc-targets/:id/eject. It is the pure
// operator escape hatch: eject the virtual media (and best-effort boot to disk) for
// a BMC directly, with no deployment context or eject-state bookkeeping. Admin-only.
func (h *DeployHandler) EjectBMCTarget(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()

	target, err := h.bmcTargets.GetByID(ctx, id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "BMC target not found"})
	}

	finalizeCtx, cancel := context.WithTimeout(ctx, finalizeTimeout)
	defer cancel()
	if err := h.runFinalize(finalizeCtx, target); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": scrubBMCError(err)})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ejected"})
}
