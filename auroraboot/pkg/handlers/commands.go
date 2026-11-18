package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kairos-io/AuroraBoot/pkg/auth"
	"github.com/kairos-io/AuroraBoot/pkg/batches"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/kairos-io/AuroraBoot/pkg/ws"
	"github.com/labstack/echo/v4"
)

// CommandHandler handles command-related REST endpoints.
type CommandHandler struct {
	commands       store.CommandStore
	nodes          store.NodeStore
	hub            *ws.Hub
	nodeExtensions store.NodeExtensionStore
	extensions     store.ExtensionStore
	// resetTimeout also bounds delivery: an offline node must not execute a
	// reset after its node-level lifecycle has already timed out.
	resetTimeout time.Duration
	// batches starts and advances rolling batches. nil answers the batch
	// endpoints with 503.
	batches *batches.Scheduler
	// groups resolves the group a group command names. nil skips the check.
	groups store.GroupStore
}

// NewCommandHandler creates a new CommandHandler.
func NewCommandHandler(commands store.CommandStore, nodes store.NodeStore, hub *ws.Hub, nodeExtensions store.NodeExtensionStore, extensions store.ExtensionStore) *CommandHandler {
	return &CommandHandler{
		commands:       commands,
		nodes:          nodes,
		hub:            hub,
		nodeExtensions: nodeExtensions,
		extensions:     extensions,
		resetTimeout:   DefaultResetTimeout,
	}
}

// WithResetTimeout configures reset command delivery expiry. Zero keeps the
// default; a negative duration disables expiry.
func (h *CommandHandler) WithResetTimeout(timeout time.Duration) *CommandHandler {
	if timeout == 0 {
		timeout = DefaultResetTimeout
	}
	h.resetTimeout = timeout
	return h
}

func (h *CommandHandler) setResetExpiry(cmd *store.NodeCommand) {
	if cmd.Command != store.CmdReset || h.resetTimeout < 0 {
		return
	}
	expires := time.Now().Add(h.resetTimeout)
	cmd.ExpiresAt = &expires
}

// WithBatches wires the scheduler the group and bulk endpoints start batches
// with and that command status reports advance. Returns the handler for chaining.
func (h *CommandHandler) WithBatches(s *batches.Scheduler) *CommandHandler {
	h.batches = s
	return h
}

// createCommandRequest is the expected body for creating a command.
type createCommandRequest struct {
	Command string            `json:"command"`
	Args    map[string]string `json:"args"`
}

// rolloutOptions are the batch settings the group and bulk endpoints accept.
type rolloutOptions struct {
	// Concurrency caps how many nodes run at once. 0 runs every node at once.
	Concurrency int `json:"concurrency,omitempty"`
	// StopOnFailure stops the batch when a node fails. nil means true.
	StopOnFailure *bool `json:"stopOnFailure,omitempty"`
	// RebootTimeoutSeconds is how long a node that reboots may take to come
	// back. 0 means store.DefaultRebootTimeoutSeconds; at most
	// store.MaxRebootTimeoutSeconds.
	RebootTimeoutSeconds int `json:"rebootTimeoutSeconds,omitempty"`
}

// WithGroups wires the group store CreateForGroup checks the group against.
// Returns the handler for chaining.
func (h *CommandHandler) WithGroups(groups store.GroupStore) *CommandHandler {
	h.groups = groups
	return h
}

// groupCommandRequest is the expected body for sending a command to a group.
// It is deliberately not bulkCommandRequest: the group is named in the path,
// so a selector in the body would be silently ignored.
type groupCommandRequest struct {
	Command string            `json:"command"`
	Args    map[string]string `json:"args"`
	rolloutOptions
}

// Create handles POST /api/v1/nodes/:nodeID/commands.
//
//	@Summary	Queue a command for a single node
//	@Tags		Commands
//	@Accept		json
//	@Produce	json
//	@Security	AdminBearer
//	@Param		nodeID	path		string					true	"Node ID"
//	@Param		body	body		APICreateCommandRequest	true	"Command payload"
//	@Success	201		{object}	store.NodeCommand
//	@Failure	404		{object}	APIError
//	@Failure	409		{object}	APIBusyNodesError	"The node is in a running batch"
//	@Failure	413		{object}	APIError
//	@Router		/api/v1/nodes/{nodeID}/commands [post]
func (h *CommandHandler) Create(c echo.Context) error {
	nodeID := c.Param("nodeID")
	var req createCommandRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if req.Command == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "command is required"})
	}

	ctx := c.Request().Context()
	if _, err := h.nodes.GetByID(ctx, nodeID); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "node not found"})
	}

	cmd := &store.NodeCommand{
		ID:            uuid.New().String(),
		ManagedNodeID: nodeID,
		Command:       req.Command,
		Args:          req.Args,
		Phase:         store.CommandPending,
	}
	h.setResetExpiry(cmd)

	// CreateBatch also marks a reset or upgrade target pending, in the same
	// write, without taking over a reset or upgrade that is already pending.
	// Both reboot the node, so the agent's ack only means the reboot is
	// scheduled, and the re-register resolver reports the real outcome
	// (kairos-io/kairos#4255).
	create := func() error { return h.commands.CreateBatch(ctx, []*store.NodeCommand{cmd}) }

	// A batch settles a node that reboots from the node's lifecycle state,
	// which a single-node upgrade, reset or reboot shares. A node whose batch
	// row is not final, in a stopped batch too, is still settled by it.
	// WhenIdle checks and creates with no batch start in between.
	var err error
	if store.RebootsNode(req.Command) && h.batches != nil {
		err = h.batches.WhenIdle(ctx, []string{nodeID}, create)
	} else {
		err = create()
	}
	var busy *batches.ErrNodesBusy
	if errors.As(err, &busy) {
		return c.JSON(http.StatusConflict, map[string]any{"error": "node is in a running batch", "nodeIDs": busy.NodeIDs})
	}
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create command"})
	}

	// Push command via WebSocket if node is online.
	h.PushCommand(c.Request().Context(), cmd)

	return c.JSON(http.StatusCreated, cmd)
}

// bulkCommandRequest is the expected body for creating commands in bulk.
type bulkCommandRequest struct {
	Selector store.CommandSelector `json:"selector"`
	Command  string                `json:"command"`
	Args     map[string]string     `json:"args"`
	rolloutOptions
}

// CreateBulk handles POST /api/v1/nodes/commands.
//
//	@Summary	Run a command on the nodes matching a selector
//	@Description	Starts a rolling batch and returns its record. Nodes that are already in a running batch are rejected with 409 and listed in nodeIDs.
//	@Tags		Commands
//	@Accept		json
//	@Produce	json
//	@Security	AdminBearer
//	@Param		body	body		APIBulkCommandRequest	true	"Selector, command and rollout options"
//	@Success	201		{object}	store.CommandBatch
//	@Failure	400		{object}	APIError
//	@Failure	409		{object}	APIBusyNodesError
//	@Failure	413		{object}	APIError
//	@Failure	503		{object}	APIError
//	@Router		/api/v1/nodes/commands [post]
func (h *CommandHandler) CreateBulk(c echo.Context) error {
	var req bulkCommandRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if req.Command == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "command is required"})
	}

	if req.Selector.GroupID == "" && len(req.Selector.NodeIDs) == 0 && len(req.Selector.Labels) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "selector must specify at least one of: groupID, nodeIDs, or labels"})
	}

	ctx := c.Request().Context()
	nodes, err := h.nodes.ListBySelector(ctx, req.Selector)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to find nodes"})
	}

	// A selector that names a group records it even when other fields narrow
	// the selection, so the group's page lists the batch.
	var groupID *string
	if req.Selector.GroupID != "" {
		groupID = &req.Selector.GroupID
	}
	return h.startBatch(c, req.Command, req.Args, req.rolloutOptions, nodes, selectorText(req.Selector, nodes), groupID)
}

// CreateForGroup handles POST /api/v1/groups/:id/commands.
//
//	@Summary	Run a command on every node of a group
//	@Description	Starts a rolling batch and returns its record. Nodes that are already in a running batch are rejected with 409 and listed in nodeIDs.
//	@Tags		Commands
//	@Accept		json
//	@Produce	json
//	@Security	AdminBearer
//	@Param		id		path		string					true	"Group ID"
//	@Param		body	body		APIGroupCommandRequest	true	"Command and rollout options"
//	@Success	201		{object}	store.CommandBatch
//	@Failure	400		{object}	APIError
//	@Failure	404		{object}	APIError
//	@Failure	409		{object}	APIBusyNodesError
//	@Failure	413		{object}	APIError
//	@Failure	503		{object}	APIError
//	@Router		/api/v1/groups/{id}/commands [post]
func (h *CommandHandler) CreateForGroup(c echo.Context) error {
	groupID := c.Param("id")
	var req groupCommandRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if req.Command == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "command is required"})
	}

	ctx := c.Request().Context()
	if h.groups != nil {
		if _, err := h.groups.GetByID(ctx, groupID); err != nil {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "group not found"})
		}
	}
	nodes, err := h.nodes.ListByGroup(ctx, groupID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to find nodes in group"})
	}

	return h.startBatch(c, req.Command, req.Args, req.rolloutOptions, nodes, "group "+groupName(groupID, nodes), &groupID)
}

// startBatch validates the rollout options, starts a batch over nodes and
// answers with the batch record.
func (h *CommandHandler) startBatch(c echo.Context, command string, args map[string]string, opts rolloutOptions, nodes []*store.ManagedNode, selection string, groupID *string) error {
	if h.batches == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "batches are not available"})
	}
	if opts.Concurrency < 0 || opts.RebootTimeoutSeconds < 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "concurrency and rebootTimeoutSeconds must not be negative"})
	}
	if opts.RebootTimeoutSeconds > store.MaxRebootTimeoutSeconds {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("rebootTimeoutSeconds must not exceed %d (7 days)", store.MaxRebootTimeoutSeconds)})
	}
	stopOnFailure := true
	if opts.StopOnFailure != nil {
		stopOnFailure = *opts.StopOnFailure
	}
	timeout := opts.RebootTimeoutSeconds
	if timeout == 0 {
		timeout = store.DefaultRebootTimeoutSeconds
	}

	b, err := h.batches.Start(c.Request().Context(), batches.StartRequest{
		Command:              command,
		Args:                 args,
		Nodes:                nodes,
		Selection:            selection,
		GroupID:              groupID,
		Concurrency:          opts.Concurrency,
		StopOnFailure:        stopOnFailure,
		RebootTimeoutSeconds: timeout,
	})
	var busy *batches.ErrNodesBusy
	switch {
	case errors.Is(err, batches.ErrNoNodes):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "no nodes match the selection"})
	case errors.As(err, &busy):
		return c.JSON(http.StatusConflict, map[string]any{"error": "nodes are already in a running batch", "nodeIDs": busy.NodeIDs})
	case err != nil:
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to start batch"})
	}
	return c.JSON(http.StatusCreated, b)
}

// selectorText describes a bulk selector for the batch's Selection, one part
// per selector field, joined with ", ".
func selectorText(sel store.CommandSelector, nodes []*store.ManagedNode) string {
	var parts []string
	if sel.GroupID != "" {
		parts = append(parts, "group "+groupName(sel.GroupID, nodes))
	}
	if len(sel.Labels) > 0 {
		pairs := make([]string, 0, len(sel.Labels))
		for _, k := range slices.Sorted(maps.Keys(sel.Labels)) {
			pairs = append(pairs, k+"="+sel.Labels[k])
		}
		parts = append(parts, "label "+strings.Join(pairs, ", "))
	}
	if n := len(sel.NodeIDs); n == 1 {
		parts = append(parts, "1 selected node")
	} else if n > 1 {
		parts = append(parts, fmt.Sprintf("%d selected nodes", n))
	}
	return strings.Join(parts, ", ")
}

// groupName returns the name of group groupID from the group the node store
// preloads on each node, or the ID when no node carries it.
func groupName(groupID string, nodes []*store.ManagedNode) string {
	for _, n := range nodes {
		if n.Group != nil && n.Group.ID == groupID && n.Group.Name != "" {
			return n.Group.Name
		}
	}
	return groupID
}

// PushCommand attempts to deliver a command via WebSocket if the hub is available
// and the target node is online. It is also the scheduler's Deliver.
//
// To avoid double-delivery (push over WS, then the agent re-polls the same
// still-Pending command via GetCommands), the command is atomically claimed
// Pending→Delivered first and only pushed if this call won the claim. When the
// node is offline we do NOT claim, leaving the command Pending for the next poll
// or reconnect. Errors are best-effort: the command is persisted either way.
func (h *CommandHandler) PushCommand(ctx context.Context, cmd *store.NodeCommand) {
	if h.hub == nil || !h.hub.IsOnline(cmd.ManagedNodeID) {
		return
	}
	claimed, err := h.commands.ClaimForDelivery(ctx, cmd.ID)
	if err != nil || !claimed {
		// Either the claim failed or another path (a concurrent poll) already
		// claimed it and will deliver it; don't push a duplicate.
		return
	}
	store.MarkResetDelivered(ctx, h.nodes, cmd)
	payload := struct {
		ID      string            `json:"id"`
		Command string            `json:"command"`
		Args    map[string]string `json:"args,omitempty"`
	}{
		ID:      cmd.ID,
		Command: cmd.Command,
		Args:    cmd.Args,
	}
	_ = h.hub.SendCommand(cmd.ManagedNodeID, payload)
}

// Delete handles DELETE /api/v1/nodes/:nodeID/commands/:commandID.
func (h *CommandHandler) Delete(c echo.Context) error {
	commandID := c.Param("commandID")
	ctx := c.Request().Context()
	if err := h.commands.Delete(ctx, commandID); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "command not found"})
	}
	// A running batch row whose command is gone settles as canceled and frees
	// its slot.
	if h.batches != nil {
		h.batches.Tick(ctx)
	}
	return c.NoContent(http.StatusNoContent)
}

// ClearHistory handles DELETE /api/v1/nodes/:nodeID/commands.
func (h *CommandHandler) ClearHistory(c echo.Context) error {
	nodeID := c.Param("nodeID")
	ctx := c.Request().Context()
	if err := h.commands.DeleteTerminal(ctx, nodeID); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to clear history"})
	}
	return c.NoContent(http.StatusNoContent)
}

// updateStatusRequest is the expected body for updating command status.
type updateStatusRequest struct {
	Phase  string `json:"phase"`
	Result string `json:"result"`
}

// UpdateStatus handles PUT /api/v1/nodes/:nodeID/commands/:commandID/status.
//
// This handler is mounted on both the agent group (node API key) and the admin
// group (admin bearer). For agent callers we scope the update to the
// authenticated node: a node may only update the status of commands addressed
// to it, never another node's (BOLA). Admins may update any node's command,
// under the path of the node that owns it: a command that does not exist or
// belongs to another node answers 404.
func (h *CommandHandler) UpdateStatus(c echo.Context) error {
	commandID := c.Param("commandID")
	nodeID := c.Param("nodeID")
	var req updateStatusRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if req.Phase == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "phase is required"})
	}
	if !store.IsValidCommandPhase(req.Phase) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "unknown phase"})
	}

	ctx := c.Request().Context()

	// Agent request: node API key auth set the node identity. Scope the update
	// to that node so a foreign or non-existent command surfaces as 403 rather
	// than a silent success (gorm Updates returns nil on zero matched rows).
	if authNodeID := auth.AuthNodeID(c); authNodeID != "" {
		err := h.commands.UpdateStatusForNode(ctx, commandID, authNodeID, req.Phase, req.Result)
		if errors.Is(err, store.ErrCommandNotFound) {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "forbidden"})
		}
		if errors.Is(err, store.ErrCommandTerminal) {
			return c.JSON(http.StatusConflict, map[string]string{"error": "command already finished"})
		}
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to update command status"})
		}
		// node_extensions tracking on the agent path: same hook the admin path
		// (below) and the WS command-status branch invoke when the phase is
		// terminal.
		if req.Phase == store.CommandCompleted && h.nodeExtensions != nil {
			if cmd, err := h.commands.GetByID(ctx, commandID); err == nil && cmd != nil {
				h.applyExtensionTracking(ctx, authNodeID, cmd)
			}
		}
		h.AdvanceBatchOf(ctx, commandID)
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	err := h.commands.UpdateStatusForNode(ctx, commandID, nodeID, req.Phase, req.Result)
	if errors.Is(err, store.ErrCommandNotFound) {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "command not found"})
	}
	if errors.Is(err, store.ErrCommandTerminal) {
		return c.JSON(http.StatusConflict, map[string]string{"error": "command already finished"})
	}
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to update command status"})
	}

	// Track node_extensions only on successful completion.
	if req.Phase == store.CommandCompleted && h.nodeExtensions != nil {
		if cmd, err := h.commands.GetByID(ctx, commandID); err == nil {
			h.applyExtensionTracking(ctx, nodeID, cmd)
		}
	}

	h.AdvanceBatchOf(ctx, commandID)

	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// AdvanceBatchOf advances the batch of the command whose status was just
// written, so the batch settles the node and fills its slot. The batch comes
// from the stored command, never from the reporting node's request. The
// WebSocket status path reaches it through ws.AgentHandler.OnCommandPhase.
func (h *CommandHandler) AdvanceBatchOf(ctx context.Context, commandID string) {
	if h.batches == nil {
		return
	}
	cmd, err := h.commands.GetByID(ctx, commandID)
	if err != nil || cmd == nil || cmd.BatchID == "" {
		return
	}
	h.batches.Advance(ctx, cmd.BatchID)
}

// bundledExtension matches the wire shape AuroraBoot emits in
// commands.Args["extensions"]. The optional Version field is server-side
// only — the agent ignores unknown JSON fields.
type bundledExtension struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Source  string `json:"source"`
	Version string `json:"version,omitempty"`
}

// ApplyExtensionTracking is the public entry point so callers outside this
// package (notably the WebSocket command-status handler at
// pkg/ws/handler.go) can drive node_extensions writes on success. Mirrors
// the body of applyExtensionTracking.
func (h *CommandHandler) ApplyExtensionTracking(ctx context.Context, nodeID string, cmd *store.NodeCommand) {
	h.applyExtensionTracking(ctx, nodeID, cmd)
}

func (h *CommandHandler) applyExtensionTracking(ctx context.Context, nodeID string, cmd *store.NodeCommand) {
	switch cmd.Command {
	case store.CmdExtension:
		h.applyManualExtension(ctx, nodeID, cmd)
	case store.CmdUpgrade, store.CmdUpgradeRecovery:
		h.applyBundledExtensions(ctx, nodeID, cmd)
	}
}

func (h *CommandHandler) applyManualExtension(ctx context.Context, nodeID string, cmd *store.NodeCommand) {
	args := cmd.Args
	action, extType, name, bootState := args["action"], args["type"], args["name"], args["bootState"]
	if name == "" || extType == "" {
		return
	}
	switch action {
	case "install", "enable":
		version := ""
		extensionID := ""
		if h.extensions != nil {
			if ext, err := h.extensions.FindLatestReadyByName(ctx, extType, name); err == nil && ext != nil {
				version = ext.Version
				extensionID = ext.ID
			}
		}
		_ = h.nodeExtensions.Upsert(ctx, &store.NodeExtensionRow{
			NodeID: nodeID, Name: name, Type: extType, BootState: bootState,
			Version: version, ExtensionID: extensionID,
		})
	case "disable":
		_ = h.nodeExtensions.DeleteByScope(ctx, nodeID, extType, name, bootState)
	case "remove":
		_ = h.nodeExtensions.DeleteByName(ctx, nodeID, extType, name)
	}
}

func (h *CommandHandler) applyBundledExtensions(ctx context.Context, nodeID string, cmd *store.NodeCommand) {
	raw := cmd.Args["extensions"]
	if raw == "" {
		return
	}
	var list []bundledExtension
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return
	}
	scope := "active"
	if cmd.Command == store.CmdUpgradeRecovery {
		scope = "recovery"
	}
	for _, e := range list {
		_ = h.nodeExtensions.Upsert(ctx, &store.NodeExtensionRow{
			NodeID: nodeID, Name: e.Name, Type: e.Type, BootState: scope,
			Version: e.Version,
		})
	}
}
