package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kairos-io/AuroraBoot/pkg/auth"
	"github.com/kairos-io/AuroraBoot/pkg/batches"
	"github.com/kairos-io/AuroraBoot/pkg/metrics"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/kairos-io/AuroraBoot/pkg/ws"
	"github.com/labstack/echo/v4"
)

// decommissionTimeout bounds how long we're willing to let a remote teardown
// run before the UI offers "force delete anyway". The value is also baked
// into the command's ExpiresAt so the DB reflects the same deadline — a node
// that comes online after the window is closed will not run the teardown.
const decommissionTimeout = 30 * time.Second

// DefaultResetTimeout is how long AuroraBoot waits for a node to return after
// an automatic reset before reporting the lifecycle as failed.
const DefaultResetTimeout = 30 * time.Minute

// nodeFinalizer is the auto eject-on-phone-home hook: given a node id it ejects the
// virtual media of that node's pending-eject Redfish deployment (when one can be
// unambiguously correlated). It is satisfied by DeployHandler.maybeFinalizeForNode.
// It is always invoked off the request goroutine so it adds no registration latency.
type nodeFinalizer func(ctx context.Context, nodeID string)

// NodeHandler handles node-related REST endpoints.
type NodeHandler struct {
	nodes         store.NodeStore
	commands      store.CommandStore
	groups        store.GroupStore
	hub           *ws.Hub // optional; when nil, Decommission reports the node as offline
	regToken      string
	aurorabootURL string

	// finalize, when non-nil, is the auto eject-on-phone-home hook invoked from
	// Register and Heartbeat on a background goroutine bound to baseCtx. nil
	// disables auto-eject (e.g. when no deploy/BMC stores are wired).
	finalize nodeFinalizer
	// baseCtx is the parent context for the background finalize goroutine, tied to
	// the server lifecycle so a shutdown cancels an in-flight eject. Defaults to
	// context.Background().
	baseCtx context.Context

	// metrics, when non-nil, receives the resource sample a heartbeat carries
	// and drops a node's samples when the node is deleted. nil ignores metrics.
	metrics *metrics.Buffer

	// resetTimeout bounds the pending/in-progress reset lifecycle. Expiration is
	// evaluated lazily when nodes are read, which matches the UI and controller
	// polling paths without adding a process-wide background worker.
	resetTimeout time.Duration
	// batches settles batch rows of nodes that come back from a reboot and
	// advances batches after a node is deleted. nil skips both.
	batches *batches.Scheduler
}

// NewNodeHandler creates a new NodeHandler.
func NewNodeHandler(nodes store.NodeStore, commands store.CommandStore, groups store.GroupStore, hub *ws.Hub, regToken string, aurorabootURL string) *NodeHandler {
	return &NodeHandler{
		nodes:         nodes,
		commands:      commands,
		groups:        groups,
		hub:           hub,
		regToken:      regToken,
		aurorabootURL: aurorabootURL,
		baseCtx:       context.Background(),
		resetTimeout:  DefaultResetTimeout,
	}
}

// SetMetrics wires the in-memory metrics buffer. A nil buffer disables metrics
// recording: heartbeats that carry metrics are accepted and the metrics ignored.
func (h *NodeHandler) SetMetrics(b *metrics.Buffer) {
	h.metrics = b
}

// WithResetTimeout configures how long an automatic reset may remain pending or
// in-progress. Zero keeps the default; a negative duration disables expiration.
func (h *NodeHandler) WithResetTimeout(timeout time.Duration) *NodeHandler {
	if timeout == 0 {
		timeout = DefaultResetTimeout
	}
	h.resetTimeout = timeout
	return h
}

// WithBatches wires the scheduler that registrations, heartbeats and node
// deletions feed. Returns the handler for chaining.
func (h *NodeHandler) WithBatches(s *batches.Scheduler) *NodeHandler {
	h.batches = s
	return h
}

// WithFinalizer wires the auto eject-on-phone-home hook and the server base context
// for its background goroutine. Passing a nil finalizer leaves auto-eject disabled.
// Returns the handler for chaining.
func (h *NodeHandler) WithFinalizer(f nodeFinalizer, baseCtx context.Context) *NodeHandler {
	h.finalize = f
	if baseCtx != nil {
		h.baseCtx = baseCtx
	}
	return h
}

// triggerFinalize fires the auto eject-on-phone-home hook off the request goroutine
// so it never adds latency to register/heartbeat. The background context is derived
// from the server base context (cancelled on shutdown) plus a short timeout; the
// finalize hook owns its own per-deployment CAS so a duplicate phone-home is a
// harmless no-op. It is nil-safe.
func (h *NodeHandler) triggerFinalize(nodeID string) {
	if h.finalize == nil || nodeID == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(h.baseCtx, finalizeTimeout)
		defer cancel()
		h.finalize(ctx, nodeID)
	}()
}

// expireReset marks an overdue reset failed. FailResetBefore provides the atomic
// compare-and-set, so concurrent UI/controller reads can safely race. Updating
// the supplied snapshot after a successful transition ensures this response
// immediately contains the terminal state.
//
// Best-effort, like store.ResolveReset: a store error is logged and discarded rather
// than failing the read. These endpoints are the UI's node page and the CAPI
// provider's read path, so one rejected bookkeeping write must not hide the
// whole fleet — and a lost CAS already returns the stale pending state, so
// there would be no consistent behaviour to fail for anyway. The next read
// retries the transition.
func (h *NodeHandler) expireReset(ctx context.Context, node *store.ManagedNode, now time.Time) {
	if !resetExpired(node, h.resetTimeout, now) {
		return
	}

	advanced, err := h.nodes.FailResetBefore(ctx, node.ID, now.Add(-h.resetTimeout))
	if err != nil {
		log.Printf("reset expiry: node %s: %v", node.ID, err)
		return
	}
	if advanced {
		node.ResetState = store.ResetStateFailed
	}
}

func (h *NodeHandler) expireResets(ctx context.Context, nodes []*store.ManagedNode) {
	now := time.Now()
	for _, node := range nodes {
		h.expireReset(ctx, node, now)
	}
}

// registerRequest is the expected body for node registration.
type registerRequest struct {
	RegistrationToken string              `json:"registrationToken"`
	MachineID         string              `json:"machineID"`
	Hostname          string              `json:"hostname"`
	AgentVersion      string              `json:"agentVersion"`
	Addresses         []store.NodeAddress `json:"addresses,omitempty"`
	BootState         string              `json:"bootState,omitempty"`
	// Group is the ID or name of the group a new node joins, as written into
	// the phonehome config. Artifacts write the group ID; older ones wrote
	// the name. An unknown group leaves the node ungrouped.
	Group string `json:"group,omitempty"`
	// Labels are a new node's initial labels.
	Labels map[string]string `json:"labels,omitempty"`
}

// Register handles POST /api/v1/nodes/register.
//
//	@Summary		Register a node
//	@Description	Idempotent by machineID: if a node with the same machineID already exists, the existing record is returned so the agent can resume with its persisted API key. Authenticated by the registrationToken inside the request body.
//	@Tags			Agent bootstrap
//	@Accept			json
//	@Produce		json
//	@Param			body	body		APIRegisterRequest	true	"Registration payload"
//	@Success		200		{object}	APIRegisterResponse
//	@Success		201		{object}	APIRegisterResponse
//	@Failure		400		{object}	APIError
//	@Failure		401		{object}	APIError
//	@Failure		413		{object}	APIError
//	@Router			/api/v1/nodes/register [post]
func (h *NodeHandler) Register(c echo.Context) error {
	var req registerRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if req.MachineID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "machineID is required"})
	}

	// Check if node already exists by machineID
	existing, _ := h.nodes.GetByMachineID(c.Request().Context(), req.MachineID)
	if existing != nil {
		// A re-register is a strong "OS is up" signal too (a freshly-installed node
		// phones home on first boot): attempt the auto eject-on-phone-home.
		h.triggerFinalize(existing.ID)
		// A node that registers again has rebooted: one that lost its
		// credentials in a reset comes back through here. Resolve a pending
		// reset or upgrade from the boot state it reports (kairos-io/kairos#4255).
		store.ResolveReset(c.Request().Context(), h.nodes, existing, req.BootState)
		store.ResolveUpgrade(c.Request().Context(), h.nodes, existing, req.BootState)
		// A batch timeout fails the upgrade or reset with its reboot still
		// recorded, and the resolvers above leave a failed state alone. A
		// registration always follows a boot, so its time stands in for the
		// boot time an agent without uptime in its heartbeats never reports.
		if err := h.nodes.SettleAfterReboot(c.Request().Context(), existing.ID, req.BootState, time.Now()); err != nil {
			c.Logger().Warnf("settling node %s after its reboot: %v", existing.ID, err)
		}
		// The same boot settles a batch row waiting on the node's reboot.
		if h.batches != nil {
			h.batches.NodeBooted(c.Request().Context(), existing.ID, req.BootState, nil)
		}
		// Return existing node info
		return c.JSON(http.StatusOK, map[string]any{
			"id":     existing.ID,
			"apiKey": existing.APIKey,
		})
	}

	node := &store.ManagedNode{
		ID:           uuid.New().String(),
		MachineID:    req.MachineID,
		Hostname:     req.Hostname,
		AgentVersion: req.AgentVersion,
		Phase:        store.PhaseRegistered,
		APIKey:       uuid.New().String(),
		Labels:       make(map[string]string),
		GroupID:      h.resolveGroupID(c.Request().Context(), req.Group),
		// Optional; empty when an older agent does not report them. Stored exactly
		// as received — interface filtering is the agent's responsibility.
		Addresses: req.Addresses,
		BootState: req.BootState,
		// RemoteIP is server-observed, not agent-reported: the request the agent
		// just made to register is itself the observation.
		RemoteIP: c.RealIP(),
	}

	for k, v := range req.Labels {
		node.Labels[k] = v
	}

	if err := h.nodes.Register(c.Request().Context(), node); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to register node"})
	}

	// The node just phoned home for the first time — the OS is up. Attempt the auto
	// eject-on-phone-home for its pending-eject Redfish deployment, off-request.
	h.triggerFinalize(node.ID)

	return c.JSON(http.StatusCreated, map[string]any{
		"id":     node.ID,
		"apiKey": node.APIKey,
	})
}

// resolveGroupID returns the ID of the group whose ID or name is ref, or nil
// when ref is empty or matches no group. The ID wins so a renamed group still
// claims the nodes its artifacts register. Registration goes ahead either
// way: the group only places the node, and an admin can place it later.
func (h *NodeHandler) resolveGroupID(ctx context.Context, ref string) *string {
	if ref == "" || h.groups == nil {
		return nil
	}
	if g, err := h.groups.GetByID(ctx, ref); err == nil && g != nil {
		return &g.ID
	}
	g, err := h.groups.GetByName(ctx, ref)
	if err != nil || g == nil {
		return nil
	}
	return &g.ID
}

// List handles GET /api/v1/nodes.
//
//	@Summary		List nodes
//	@Description	Returns every registered node. Optional group/label filters.
//	@Tags			Nodes
//	@Produce		json
//	@Security		AdminBearer
//	@Param			group	query		string					false	"Filter by group ID"
//	@Param			label	query		string					false	"Filter by a single key:value label pair"
//	@Success		200		{array}		store.ManagedNode
//	@Failure		401		{object}	APIError
//	@Router			/api/v1/nodes [get]
func (h *NodeHandler) List(c echo.Context) error {
	ctx := c.Request().Context()

	groupParam := c.QueryParam("group")
	labelParam := c.QueryParam("label")

	if groupParam != "" {
		nodes, err := h.nodes.ListByGroup(ctx, groupParam)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to list nodes"})
		}
		h.expireResets(ctx, nodes)
		return c.JSON(http.StatusOK, nodes)
	}

	if labelParam != "" {
		key, value, ok := strings.Cut(labelParam, ":")
		if !ok {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "label must be in key:value format"})
		}
		labels := map[string]string{key: value}
		nodes, err := h.nodes.ListByLabels(ctx, labels)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to list nodes"})
		}
		h.expireResets(ctx, nodes)
		return c.JSON(http.StatusOK, nodes)
	}

	nodes, err := h.nodes.List(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to list nodes"})
	}
	h.expireResets(ctx, nodes)
	return c.JSON(http.StatusOK, nodes)
}

// Get handles GET /api/v1/nodes/:nodeID.
//
//	@Summary		Get a node
//	@Tags			Nodes
//	@Produce		json
//	@Security		AdminBearer
//	@Param			nodeID	path		string	true	"Node ID"
//	@Success		200		{object}	store.ManagedNode
//	@Failure		404		{object}	APIError
//	@Router			/api/v1/nodes/{nodeID} [get]
func (h *NodeHandler) Get(c echo.Context) error {
	nodeID := c.Param("nodeID")
	ctx := c.Request().Context()
	node, err := h.nodes.GetByID(ctx, nodeID)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "node not found"})
	}
	h.expireReset(ctx, node, time.Now())
	return c.JSON(http.StatusOK, node)
}

// Delete handles DELETE /api/v1/nodes/:nodeID.
//
//	@Summary		Delete a node
//	@Tags			Nodes
//	@Security		AdminBearer
//	@Param			nodeID	path	string	true	"Node ID"
//	@Success		204
//	@Failure		404	{object}	APIError
//	@Router			/api/v1/nodes/{nodeID} [delete]
func (h *NodeHandler) Delete(c echo.Context) error {
	nodeID := c.Param("nodeID")
	if err := h.nodes.Delete(c.Request().Context(), nodeID); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to delete node"})
	}
	if h.metrics != nil {
		h.metrics.Forget(nodeID)
	}
	// Deleting a node also revokes its API key, so close any socket it already
	// has open instead of letting it keep receiving commands (kairos-io/kairos#5785).
	if h.hub != nil {
		h.hub.Disconnect(nodeID)
	}
	// The deleted node's batch rows settle as canceled and free their slots.
	if h.batches != nil {
		h.batches.Tick(c.Request().Context())
	}
	return c.NoContent(http.StatusNoContent)
}

// decommissionResponse is returned by POST /api/v1/nodes/:nodeID/decommission.
// When NodeOnline is true, CommandID identifies the teardown command the UI
// should subscribe to via the UI WebSocket; when false, no command was issued
// and the UI should fall back to warning the operator to run
// `kairos-agent phone-home uninstall` manually on the box.
type decommissionResponse struct {
	CommandID  string `json:"commandID"`
	NodeOnline bool   `json:"nodeOnline"`
}

// Decommission handles POST /api/v1/nodes/:nodeID/decommission.
//
// Unlike DELETE, this endpoint does NOT remove the node from the database.
// It only dispatches a remote `unregister` command so the agent can stop the
// phone-home service and drop its local state; the UI then calls DELETE as a
// second step once the command reaches a terminal phase (or the operator
// decides to force-delete). Splitting the two lets the UI show real teardown
// progress to the operator instead of a blind one-shot delete.
//
//	@Summary		Dispatch remote teardown before deleting a node
//	@Description	Sends an `unregister` command to the node if it is currently online. The UI subscribes to the returned commandID via the UI WebSocket for live progress, then issues DELETE /api/v1/nodes/{nodeID} when the command reaches Completed. When the node is offline, this returns nodeOnline=false with an empty commandID — the operator should then force-delete and run `kairos-agent phone-home uninstall` on the box manually.
//	@Tags			Nodes
//	@Security		AdminBearer
//	@Param			nodeID	path		string	true	"Node ID"
//	@Success		200		{object}	decommissionResponse
//	@Failure		404		{object}	APIError
//	@Failure		413		{object}	APIError
//	@Router			/api/v1/nodes/{nodeID}/decommission [post]
func (h *NodeHandler) Decommission(c echo.Context) error {
	nodeID := c.Param("nodeID")
	ctx := c.Request().Context()

	// Verify the node exists; otherwise return 404 so the UI doesn't proceed
	// to issue a DELETE against a non-existent record.
	if _, err := h.nodes.GetByID(ctx, nodeID); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "node not found"})
	}

	online := h.hub != nil && h.hub.IsOnline(nodeID)
	if !online {
		// Offline nodes can't run the teardown remotely. The UI surfaces the
		// CLI fallback; we still return 200 with nodeOnline=false so the
		// caller can switch into the "force delete + instructions" branch
		// without having to treat this as an error.
		return c.JSON(http.StatusOK, decommissionResponse{CommandID: "", NodeOnline: false})
	}

	expires := time.Now().Add(decommissionTimeout)
	cmd := &store.NodeCommand{
		ID:            uuid.New().String(),
		ManagedNodeID: nodeID,
		Command:       "unregister",
		Args:          nil,
		Phase:         store.CommandPending,
		ExpiresAt:     &expires,
	}
	if err := h.commands.Create(ctx, cmd); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to queue unregister command"})
	}

	// Claim the command Pending→Delivered before pushing so the agent's next
	// REST poll won't re-deliver this same unregister command. If another path
	// already claimed it (a poll racing this push) we skip the push: it will be
	// delivered exactly once by whichever path won.
	if claimed, err := h.commands.ClaimForDelivery(ctx, cmd.ID); err == nil && claimed {
		payload := struct {
			ID      string            `json:"id"`
			Command string            `json:"command"`
			Args    map[string]string `json:"args,omitempty"`
		}{
			ID:      cmd.ID,
			Command: cmd.Command,
		}
		// Errors from SendCommand are best-effort — the command is already
		// persisted (and now Delivered), and the WS layer will surface it on
		// the next reconnect if the agent happens to drop between IsOnline and
		// the write. The UI has the ExpiresAt to decide when to give up.
		_ = h.hub.SendCommand(nodeID, payload)
	}

	return c.JSON(http.StatusOK, decommissionResponse{CommandID: cmd.ID, NodeOnline: true})
}

// setLabelsRequest is the expected body for setting labels.
type setLabelsRequest struct {
	Labels map[string]string `json:"labels"`
}

// SetLabels handles PUT /api/v1/nodes/:nodeID/labels.
//
//	@Summary		Replace a node's labels
//	@Tags			Nodes
//	@Accept			json
//	@Produce		json
//	@Security		AdminBearer
//	@Param			nodeID	path		string				true	"Node ID"
//	@Param			body	body		APISetLabelsRequest	true	"Labels payload"
//	@Success		200
//	@Failure		404		{object}	APIError
//	@Failure		413		{object}	APIError
//	@Router			/api/v1/nodes/{nodeID}/labels [put]
func (h *NodeHandler) SetLabels(c echo.Context) error {
	nodeID := c.Param("nodeID")
	var req setLabelsRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	ctx := c.Request().Context()
	if _, err := h.nodes.GetByID(ctx, nodeID); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "node not found"})
	}
	if err := h.nodes.SetLabels(ctx, nodeID, req.Labels); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to set labels"})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// setGroupRequest is the expected body for setting a group.
type setGroupRequest struct {
	// GroupID names the target group; null moves the node out of any group.
	GroupID *string `json:"groupID"`
}

// SetGroup handles PUT /api/v1/nodes/:nodeID/group.
//
//	@Summary		Assign a node to a group
//	@Tags			Nodes
//	@Accept			json
//	@Security		AdminBearer
//	@Param			nodeID	path		string				true	"Node ID"
//	@Param			body	body		APISetGroupRequest	true	"Target group"
//	@Success		200
//	@Failure		404		{object}	APIError
//	@Failure		413		{object}	APIError
//	@Router			/api/v1/nodes/{nodeID}/group [put]
func (h *NodeHandler) SetGroup(c echo.Context) error {
	nodeID := c.Param("nodeID")
	var req setGroupRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	if req.GroupID != nil && *req.GroupID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "groupID must name a group, or be null for no group"})
	}
	ctx := c.Request().Context()
	if _, err := h.nodes.GetByID(ctx, nodeID); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "node not found"})
	}
	if req.GroupID != nil {
		if _, err := h.groups.GetByID(ctx, *req.GroupID); err != nil {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "group not found"})
		}
	}
	if err := h.nodes.SetGroup(ctx, nodeID, req.GroupID); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to set group"})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// Claim handles POST /api/v1/groups/:id/claim.
//
// It atomically assigns one unclaimed node in the group to the caller-supplied
// claimKey and returns it. The operation is idempotent: replaying with the same
// claimKey returns the same node, so a controller that crashed mid-provision and
// reconciles again re-finds its own node instead of grabbing a second. When the
// group has no unclaimed node, it returns 409 with code "NoCapacity" so the
// caller can surface "waiting for capacity" and requeue rather than treat it as
// a hard error.
//
//	@Summary		Claim a node from a group
//	@Tags			Groups
//	@Accept			json
//	@Produce		json
//	@Security		AdminBearer
//	@Param			id		path		string				true	"Group ID"
//	@Param			body	body		APIClaimRequest		true	"Claim payload"
//	@Success		200		{object}	store.ManagedNode
//	@Failure		400		{object}	APIError
//	@Failure		404		{object}	APIError
//	@Failure		409		{object}	APIError	"No unclaimed node available (code=NoCapacity)"
//	@Failure		413		{object}	APIError
//	@Router			/api/v1/groups/{id}/claim [post]
func (h *NodeHandler) Claim(c echo.Context) error {
	groupID := c.Param("id")
	ctx := c.Request().Context()

	var req APIClaimRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, APIError{Error: "invalid request body"})
	}
	if req.ClaimKey == "" {
		return c.JSON(http.StatusBadRequest, APIError{Error: "claimKey is required"})
	}

	// Reject an unknown group up front so the caller gets 404 rather than an
	// indistinguishable "no capacity" for a group that does not exist.
	if _, err := h.groups.GetByID(ctx, groupID); err != nil {
		return c.JSON(http.StatusNotFound, APIError{Error: "group not found"})
	}

	node, err := h.nodes.ClaimNode(ctx, groupID, req.ClaimKey)
	if err != nil {
		if errors.Is(err, store.ErrNoClaimCapacity) {
			return c.JSON(http.StatusConflict, APIError{
				Error: "no unclaimed node available in group",
				Code:  ClaimErrorCodeNoCapacity,
			})
		}
		return c.JSON(http.StatusInternalServerError, APIError{Error: "failed to claim node"})
	}
	return c.JSON(http.StatusOK, node)
}

// Release handles POST /api/v1/nodes/:nodeID/release.
//
// It clears the claim on the node and returns it to the group's pool, but only
// if the claim is currently held by the supplied claimKey. Releasing an already
// unclaimed node is a no-op that still returns 200 with released=false
// (idempotent). A node claimed by a DIFFERENT key returns 409 with code
// "ClaimMismatch" so a caller cannot release someone else's claim by accident.
//
//	@Summary		Release a node's claim
//	@Tags			Nodes
//	@Accept			json
//	@Produce		json
//	@Security		AdminBearer
//	@Param			nodeID	path		string				true	"Node ID"
//	@Param			body	body		APIReleaseRequest	true	"Release payload"
//	@Success		200		{object}	APIReleaseResponse
//	@Failure		400		{object}	APIError
//	@Failure		404		{object}	APIError
//	@Failure		409		{object}	APIError	"Claimed by a different key (code=ClaimMismatch)"
//	@Failure		413		{object}	APIError
//	@Router			/api/v1/nodes/{nodeID}/release [post]
func (h *NodeHandler) Release(c echo.Context) error {
	nodeID := c.Param("nodeID")
	ctx := c.Request().Context()

	var req APIReleaseRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, APIError{Error: "invalid request body"})
	}
	if req.ClaimKey == "" {
		return c.JSON(http.StatusBadRequest, APIError{Error: "claimKey is required"})
	}

	node, err := h.nodes.GetByID(ctx, nodeID)
	if err != nil {
		return c.JSON(http.StatusNotFound, APIError{Error: "node not found"})
	}

	// Already unclaimed: nothing to do, report it plainly (idempotent).
	if node.ClaimKey == nil {
		return c.JSON(http.StatusOK, APIReleaseResponse{Released: false})
	}
	// Claimed by someone else: refuse rather than silently no-op, so the caller
	// learns it does not own this node.
	if *node.ClaimKey != req.ClaimKey {
		return c.JSON(http.StatusConflict, APIError{
			Error: "node is claimed by a different key",
			Code:  ClaimErrorCodeClaimMismatch,
		})
	}

	released, err := h.nodes.ReleaseNode(ctx, nodeID, req.ClaimKey)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, APIError{Error: "failed to release node"})
	}
	return c.JSON(http.StatusOK, APIReleaseResponse{Released: released})
}

// heartbeatRequest is the expected body for a heartbeat.
type heartbeatRequest struct {
	AgentVersion string              `json:"agentVersion"`
	OSRelease    map[string]string   `json:"osRelease"`
	Addresses    []store.NodeAddress `json:"addresses,omitempty"`
	BootState    string              `json:"bootState,omitempty"`
	// Hostname is the node's current hostname (optional). It rides the heartbeat
	// rather than only the registration payload because a node registers before
	// cloud-init has applied a templated hostname, and because a node may be
	// renamed later in its life (kairos-io/kairos#4196). Omitted by older agents,
	// in which case the stored value is preserved.
	Hostname string `json:"hostname,omitempty"`
	// Metrics is an optional resource sample (store.NodeMetrics). It is kept raw
	// and decoded separately so a malformed sample never fails the heartbeat.
	Metrics json.RawMessage `json:"metrics,omitempty"`
}

// Heartbeat handles POST /api/v1/nodes/:nodeID/heartbeat.
//
//	@Summary		Agent heartbeat
//	@Description	Transitions the node to Online and records the latest agent version, OS info and hostname.
//	@Tags			Agent
//	@Accept			json
//	@Security		NodeAPIKey
//	@Param			nodeID	path		string					true	"Node ID"
//	@Param			body	body		APIHeartbeatRequest	true	"Heartbeat payload"
//	@Success		200
//	@Failure		413		{object}	APIError
//	@Router			/api/v1/nodes/{nodeID}/heartbeat [post]
func (h *NodeHandler) Heartbeat(c echo.Context) error {
	nodeID := c.Param("nodeID")
	var req heartbeatRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	if err := h.nodes.UpdateHeartbeat(c.Request().Context(), nodeID, req.AgentVersion, req.OSRelease, req.Addresses, req.BootState, req.Hostname, c.RealIP()); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to update heartbeat"})
	}
	if err := h.nodes.UpdatePhase(c.Request().Context(), nodeID, store.PhaseOnline); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to update phase"})
	}
	h.recordMetrics(c, nodeID, req.Metrics)
	if bootedAt, ok := store.BootedAt(time.Now(), req.Metrics); ok {
		if err := h.nodes.SettleAfterReboot(c.Request().Context(), nodeID, req.BootState, bootedAt); err != nil {
			c.Logger().Warnf("settling node %s after its reboot: %v", nodeID, err)
		}
		if h.batches != nil {
			h.batches.NodeBooted(c.Request().Context(), nodeID, req.BootState, &bootedAt)
		}
	}
	// Heartbeat is the universal "OS is up" signal (and the fallback when a node
	// never re-registers): attempt the auto eject-on-phone-home off-request. The
	// per-deployment CAS makes a repeated heartbeat a harmless no-op once ejected.
	h.triggerFinalize(nodeID)
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// recordMetrics stores the heartbeat's metrics sample, if any. The route is
// bound to the authenticated node (RequireNodeMatch), so nodeID is the node's
// own identity. A sample that does not decode is logged and dropped.
func (h *NodeHandler) recordMetrics(c echo.Context, nodeID string, raw json.RawMessage) {
	if h.metrics == nil || len(raw) == 0 || string(raw) == "null" {
		return
	}
	var m store.NodeMetrics
	if err := json.Unmarshal(raw, &m); err != nil {
		c.Logger().Warnf("ignoring invalid heartbeat metrics from node %s: %v", nodeID, err)
		return
	}
	h.metrics.Record(nodeID, m)
}

// GetCommands handles GET /api/v1/nodes/:nodeID/commands.
//
// Served under AgentOrAdminMiddleware, so it handles two callers:
//   - agent (node API key): returns only pending commands and marks them
//     delivered, scoped to the authenticated node. A node may only read its
//     own queue — requesting another node's :nodeID is rejected with 403
//     (node-impersonation / BOLA).
//   - admin (bearer): returns all commands for the requested node.
func (h *NodeHandler) GetCommands(c echo.Context) error {
	nodeID := c.Param("nodeID")

	// Check if this is an agent request (node API key auth sets nodeID in context)
	ctxNodeID := auth.AuthNodeID(c)
	isAgent := ctxNodeID != ""

	// Settle overdue commands before anyone reads the queue. This is the same
	// lazy-expiry-on-read shape the reset lifecycle uses instead of a
	// process-wide sweeper: every consumer of the Expired phase (the dashboard,
	// and a CAPI provider polling a command it queued) goes through this
	// endpoint, so the terminal state lands before it is observed. A failure
	// here is not fatal to the read: the caller still gets the queue, just with
	// the stale phases it would have seen before.
	if err := h.commands.ExpireBefore(c.Request().Context(), nodeID, time.Now()); err != nil {
		c.Logger().Warnf("expiring overdue commands for node %s: %v", nodeID, err)
	}

	if isAgent {
		// Bind the agent to its own identity: the path :nodeID must be the
		// node the API key belongs to.
		if ctxNodeID != nodeID {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "forbidden"})
		}

		ctx := c.Request().Context()
		cmds, err := h.commands.GetPending(ctx, nodeID)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to get commands"})
		}

		// Atomically claim each pending command Pending→Delivered and return only
		// the ones THIS poll actually claimed. Claiming per command (instead of a
		// bulk MarkDelivered) makes delivery exactly-once: if a concurrent poll or
		// a WS push already claimed a command, our claim returns false and we skip
		// it, so the same command is never handed to two deliveries.
		now := time.Now()
		claimed := make([]*store.NodeCommand, 0, len(cmds))
		for _, cmd := range cmds {
			ok, err := h.commands.ClaimForDelivery(ctx, cmd.ID)
			if err != nil {
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to get commands"})
			}
			if !ok {
				continue
			}
			store.MarkResetDelivered(ctx, h.nodes, cmd)
			cmd.Phase = store.CommandDelivered
			cmd.DeliveredAt = &now
			claimed = append(claimed, cmd)
		}

		return c.JSON(http.StatusOK, claimed)
	}

	// Admin request: return all commands for the node
	cmds, err := h.commands.ListByNode(c.Request().Context(), nodeID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to get commands"})
	}
	if cmds == nil {
		cmds = []*store.NodeCommand{}
	}
	return c.JSON(http.StatusOK, cmds)
}

// InstallScript handles GET /api/v1/install-agent.
func (h *NodeHandler) InstallScript(c echo.Context) error {
	script := fmt.Sprintf(`#!/bin/sh
# AuroraBoot agent install script
# Usage: curl -sSL %s/api/v1/install-agent | AURORABOOT_GROUP=mygroup sh

set -e

AURORABOOT_URL="${AURORABOOT_URL:-%s}"
REG_TOKEN="${REGISTRATION_TOKEN:-}"

if [ -z "$AURORABOOT_URL" ]; then
    echo "Error: AURORABOOT_URL is required"
    exit 1
fi
if [ -z "$REG_TOKEN" ]; then
    echo "Error: REGISTRATION_TOKEN is required"
    exit 1
fi
GROUP="${AURORABOOT_GROUP:-}"

# Build the phonehome.allowed_commands list. The env var is comma-separated;
# when unset, fall back to AuroraBoot's safe-default set so the emitted YAML
# always carries the key (never rely on implicit agent-side defaults).
# Set AURORABOOT_ALLOWED_COMMANDS="" explicitly and you will get the safe
# defaults too — deny-all must be configured from the AuroraBoot UI instead.
ALLOWED_RAW="${AURORABOOT_ALLOWED_COMMANDS:-upgrade,upgrade-recovery,reboot,unregister}"
ALLOWED_YAML=""
# _NL is a literal newline, used to put each list entry on its own line.
# Do NOT build the list with $(printf '\n'): command substitution strips
# trailing newlines, so the separator would expand to nothing and every entry
# would end up concatenated on a single line ("    - a    - b"), which does not
# parse as a YAML sequence.
_NL='
'
_old_ifs="$IFS"
IFS=','
for c in $ALLOWED_RAW; do
    c=$(echo "$c" | xargs)  # trim whitespace
    if [ -n "$c" ]; then
        if [ -n "$ALLOWED_YAML" ]; then
            ALLOWED_YAML="${ALLOWED_YAML}${_NL}"
        fi
        ALLOWED_YAML="${ALLOWED_YAML}    - ${c}"
    fi
done
IFS="$_old_ifs"

echo "Installing AuroraBoot agent..."
echo "Server: ${AURORABOOT_URL}"

# Write phonehome config to /oem/ (Kairos standard config location)
mkdir -p /oem
cat > /oem/phonehome.yaml << EOF
#cloud-config
phonehome:
  url: "${AURORABOOT_URL}"
  registration_token: "${REG_TOKEN}"
  group: "${GROUP}"
  allowed_commands:
${ALLOWED_YAML}
EOF

echo "Config written to /oem/phonehome.yaml"

# Start kairos-agent which auto-detects the auroraboot config in /oem and
# installs + starts the kairos-agent-phonehome systemd service.
if ! command -v kairos-agent >/dev/null 2>&1; then
  echo "Error: kairos-agent not found. Install it first — phonehome requires kairos-agent to run." >&2
  exit 1
fi
echo "Starting kairos-agent..."
kairos-agent start

# Agent < v2.30.1 may write ExecStart=/usr/sbin/kairos-agent while kairos-init
# installs to /usr/bin (kairos-io/kairos#4189). Patch only when needed.
agent_version_lt() {
    ver="$1"
    min="$2"
    _old_ifs="$IFS"
    IFS=.
    set -- $ver
    v_major=${1:-0}; v_minor=${2:-0}; v_patch=${3:-0}
    set -- $min
    m_major=${1:-0}; m_minor=${2:-0}; m_patch=${3:-0}
    IFS="$_old_ifs"
    if [ "$v_major" -lt "$m_major" ]; then return 0; fi
    if [ "$v_major" -gt "$m_major" ]; then return 1; fi
    if [ "$v_minor" -lt "$m_minor" ]; then return 0; fi
    if [ "$v_minor" -gt "$m_minor" ]; then return 1; fi
    if [ "$v_patch" -lt "$m_patch" ]; then return 0; fi
    return 1
}
agent_needs_phonehome_execstart_fix() {
    ver="$(kairos-agent version 2>/dev/null | sed 's/^v//' | cut -d- -f1)"
    if [ -z "$ver" ]; then
        return 0
    fi
    case "$ver" in
        *[!0-9.]*|.*|*.|*..*) return 0 ;;
    esac
    if agent_version_lt "$ver" "2.30.1"; then
        return 0
    fi
    return 1
}
if agent_needs_phonehome_execstart_fix; then
    AGENT_BIN="$(command -v kairos-agent)"
    UNIT=/etc/systemd/system/kairos-agent-phonehome.service
    if [ -n "$AGENT_BIN" ] && [ -f "$UNIT" ]; then
        sed -i "s|^ExecStart=.*|ExecStart=${AGENT_BIN} phone-home|" "$UNIT"
        systemctl daemon-reload
        systemctl restart kairos-agent-phonehome
    fi
fi

if systemctl is-active --quiet kairos-agent-phonehome; then
    echo "kairos-agent-phonehome service is active."
else
    echo "Warning: kairos-agent-phonehome is not active — check: systemctl status kairos-agent-phonehome" >&2
fi

echo "AuroraBoot agent installation complete."
`, h.aurorabootURL, h.aurorabootURL)

	return c.String(http.StatusOK, script)
}
