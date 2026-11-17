package handlers

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kairos-io/AuroraBoot/pkg/redfish"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
)

// BMC status-cache values. These mirror store.BMCTarget.LastStatus; "" (unknown)
// is the third, implicit state for a target that has never been pinged/inspected.
const (
	bmcStatusReachable   = "reachable"
	bmcStatusUnreachable = "unreachable"
)

// refreshAllInterTargetDelay paces a sequential refresh-all so a single BMC (or a
// shared sushy service fronting many systems) is not hit back-to-back.
const refreshAllInterTargetDelay = 250 * time.Millisecond

// refreshAllPerTargetTimeout bounds each individual reachability ping during a
// refresh-all so one unresponsive BMC cannot stall the whole sweep.
const refreshAllPerTargetTimeout = 5 * time.Second

// bmcStatusResponse is the per-target status payload returned by the ping and
// refresh-all endpoints.
type bmcStatusResponse struct {
	ID         string     `json:"id,omitempty"`
	Status     string     `json:"status"`
	LastPingAt *time.Time `json:"lastPingAt,omitempty"`
	Error      string     `json:"error,omitempty"`
}

// clearBMCStatusCache zeroes the server-owned status-cache fields on a target.
// CreateBMCTarget calls it after binding the request body so a client cannot
// fabricate a cached status: these fields are populated only by the inspect, ping,
// and refresh-all handlers.
func clearBMCStatusCache(t *store.BMCTarget) {
	t.LastStatus = ""
	t.LastError = ""
	t.LastInspectAt = nil
	t.LastPingAt = nil
	t.LastModel = ""
	t.LastManufacturer = ""
	t.LastSerial = ""
	t.LastMemoryGiB = 0
	t.LastCPUCount = 0
	t.LastFeatures = nil
}

// scrubBMCError renders an error for storage/return in the status cache. The
// redfish package already scrubs credentials at the source; this is a thin,
// nil-safe accessor kept as a single choke point so any future defensive
// redaction lives in one place.
func scrubBMCError(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// pingTarget performs the session-free reachability ping for one target and
// persists the outcome (LastStatus / LastError / LastPingAt) into its status
// cache. It never creates a Redfish session, so it adds nothing to the BMC's
// capped concurrent-session count. It returns the status payload (mutated target's
// LastPingAt is reused) so callers can include it in a response without re-reading
// the store. A store-write failure is best-effort and swallowed.
func (h *DeployHandler) pingTarget(ctx context.Context, target *store.BMCTarget) bmcStatusResponse {
	now := time.Now()
	err := redfish.Reachable(ctx, target.Endpoint, !target.VerifySSL)

	target.LastPingAt = &now
	if err != nil {
		target.LastStatus = bmcStatusUnreachable
		target.LastError = pingErrorMessage(err)
	} else {
		target.LastStatus = bmcStatusReachable
		target.LastError = ""
	}
	_ = h.bmcTargets.Update(ctx, target)

	return bmcStatusResponse{
		ID:         target.ID,
		Status:     target.LastStatus,
		LastPingAt: target.LastPingAt,
		Error:      target.LastError,
	}
}

// pingErrorMessage maps a session-free ping failure to an operator-facing message.
// A ServiceRoot gated behind authentication (401/403) is a common posture on
// hardened BMCs; the ping cannot authenticate by design, so we steer the operator
// to an authenticated Inspect rather than reporting a misleading hard failure.
func pingErrorMessage(err error) string {
	msg := scrubBMCError(err)
	if strings.Contains(msg, "status 401") || strings.Contains(msg, "status 403") {
		return "ServiceRoot requires authentication; use Inspect for an authenticated check"
	}
	return msg
}

// PingBMCTarget handles GET /api/v1/bmc-targets/:id/status. It runs a session-free
// reachability ping against the target's ServiceRoot, persists the outcome into
// the status cache, and returns it. No Redfish session is created, so this is safe
// to call without burning a BMC session.
func (h *DeployHandler) PingBMCTarget(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()

	target, err := h.bmcTargets.GetByID(ctx, id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "BMC target not found"})
	}

	resp := h.pingTarget(ctx, target)
	// The per-target id is implicit in the URL for this endpoint; omit it from the
	// single-target body to keep the shape minimal.
	resp.ID = ""
	return c.JSON(http.StatusOK, resp)
}

// RefreshAllBMCTargets handles POST /api/v1/bmc-targets/refresh-all. It pings every
// saved BMC sequentially (concurrency = 1) with a small inter-target delay and a
// per-target timeout, persisting each outcome into the status cache, and returns
// the per-target results.
//
// Only one refresh may run at a time: a concurrent call returns 409. Sequential,
// session-free pings avoid a thundering herd against a shared sushy service
// fronting many systems and never consume BMC sessions.
func (h *DeployHandler) RefreshAllBMCTargets(c echo.Context) error {
	ctx := c.Request().Context()

	if !h.refreshAll.tryAcquire() {
		return c.JSON(http.StatusConflict, map[string]string{"error": "a refresh is already in progress"})
	}
	defer h.refreshAll.release()

	targets, err := h.bmcTargets.List(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to list BMC targets"})
	}

	results := make([]bmcStatusResponse, 0, len(targets))
	for i, target := range targets {
		if err := ctx.Err(); err != nil {
			// The client went away (or the request was cancelled). Stop early rather
			// than continue hammering BMCs for a response nobody will read.
			break
		}
		if i > 0 {
			select {
			case <-ctx.Done():
				return c.JSON(http.StatusOK, results)
			case <-time.After(refreshAllInterTargetDelay):
			}
		}

		pingCtx, cancel := context.WithTimeout(ctx, refreshAllPerTargetTimeout)
		results = append(results, h.pingTarget(pingCtx, target))
		cancel()
	}

	return c.JSON(http.StatusOK, results)
}

// refreshGuard enforces a single in-flight refresh-all. tryAcquire reports whether
// the caller won the slot; a loser must NOT release. The winner releases on return.
type refreshGuard struct {
	mu       sync.Mutex
	inflight bool
}

func (g *refreshGuard) tryAcquire() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.inflight {
		return false
	}
	g.inflight = true
	return true
}

func (g *refreshGuard) release() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.inflight = false
}
