package ws

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/websocket"
	"github.com/kairos-io/AuroraBoot/pkg/auth"
	"github.com/kairos-io/AuroraBoot/pkg/metrics"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
)

// wsMessage is the envelope for all WebSocket messages.
type wsMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// heartbeatData is the heartbeat payload the agent sends. handleHeartbeat
// applies every field except Labels, which is decoded but not applied.
type heartbeatData struct {
	AgentVersion string            `json:"agentVersion"`
	OSRelease    map[string]string `json:"osRelease,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
	// Addresses and BootState are optional. The agent heartbeats over this
	// socket and stops sending registration payloads once its credentials are
	// on disk, so this is the path its current addresses and the boot entry it
	// runs from arrive on after a reboot.
	Addresses []store.NodeAddress `json:"addresses,omitempty"`
	BootState string              `json:"bootState,omitempty"`
	// Hostname is the node's current hostname (optional), read here for the
	// same reason (kairos-io/kairos#4196).
	Hostname string `json:"hostname,omitempty"`
	// Metrics is an optional resource sample (store.NodeMetrics). It is kept raw
	// and decoded separately so a malformed sample never drops the heartbeat.
	Metrics json.RawMessage `json:"metrics,omitempty"`
}

// commandData is sent to the agent.
type commandData struct {
	ID      string            `json:"id"`
	Command string            `json:"command"`
	Args    map[string]string `json:"args,omitempty"`
}

// commandStatusData is sent by the agent to report command status.
type commandStatusData struct {
	ID     string `json:"id"`
	Phase  string `json:"phase"`
	Result string `json:"result,omitempty"`
}

// newUpgrader returns an upgrader that refuses a browser upgrade from another
// origin. Without it a page on any site could open the UI socket with the
// admin's session cookie and read every event.
func newUpgrader(publicURL string) *websocket.Upgrader {
	return &websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return auth.OriginAllowed(r, publicURL) },
	}
}

// maxMessageBytes caps one incoming WebSocket message on the agent and UI
// connections: agent messages are small JSON envelopes and the UI sends next
// to nothing, so 4 MiB is generous. A larger message makes gorilla close the
// connection with 1009 (message too big) and fail the pending read.
const maxMessageBytes = 4 * 1024 * 1024

// finalizeTimeout bounds one auto eject-on-phone-home attempt fired from a WS
// heartbeat, mirroring the REST handlers' budget (pkg/handlers/finalize.go).
const finalizeTimeout = 2 * time.Minute

// Keepalive timings for the agent connection.
//
// A node can disappear without its socket ever being closed: power cut, network
// partition, or a NAT / load balancer silently dropping an idle flow. TCP does
// not notice, so a server that only ever blocks in ReadMessage waits forever.
// That matters here beyond the leaked goroutine, because HandleAgentWS's
// deferred Unregister + UpdatePhase is the ONLY place in AuroraBoot that writes
// PhaseOffline: there is no heartbeat-age sweeper. A read that never returns
// therefore leaves the node reported Online indefinitely, and leaves it
// registered in the hub, where SendCommand writes into a dead socket and
// succeeds (the bytes only reach the kernel buffer).
//
// Ping every agentPingInterval and require any frame within agentReadTimeout.
// The timeout is deliberately three times both the ping interval and the
// agent's own default heartbeat interval (30s, see the phonehome client), so
// two consecutive misses are tolerated before a live node is dropped.
const (
	agentPingInterval = 30 * time.Second
	agentReadTimeout  = 90 * time.Second
	// pingWriteWait bounds one ping write so a full socket buffer on a wedged
	// peer cannot block the ping goroutine forever.
	pingWriteWait = 10 * time.Second
)

// AgentHandler handles WebSocket connections from agents.
type AgentHandler struct {
	Hub      *Hub
	Nodes    store.NodeStore
	Commands store.CommandStore

	// PublicURL is the server's public URL (--url); an upgrade whose Origin
	// names it is accepted even when a proxy rewrote Host.
	PublicURL string

	// Finalize, when set, is the auto eject-on-phone-home hook (the same
	// MaybeFinalizeForNode wired into the REST Register/Heartbeat handlers). A WS
	// heartbeat is just as much an "OS is up" signal as a REST one, so it must
	// trigger the pending-eject finalize too — otherwise a node that reports
	// liveness only over the agent channel never gets its media ejected.
	Finalize func(ctx context.Context, nodeID string)
	// BaseCtx is the server lifecycle context the finalize goroutine derives from
	// (cancelled on shutdown). Nil means context.Background().
	BaseCtx context.Context

	// Metrics, when set, receives the resource sample a heartbeat carries. nil
	// ignores metrics.
	Metrics metrics.Recorder

	// OnCommandStatus is invoked after the agent's command-status report has
	// been persisted. The server uses this hook to update node_extensions
	// tracking for the new `extension` command and for compound `upgrade`s
	// that carry an extensions[] payload. nil-safe -- if unset, only the
	// command-status row is updated. Wired in pkg/server/server.go.
	OnCommandStatus func(ctx context.Context, nodeID string, cmd *store.NodeCommand)
	// OnCommandPhase is invoked with the command's ID after every status
	// report this handler applied, whatever the phase, so the batch the
	// command belongs to can settle the node and fill its slot. pkg/ws cannot
	// import the scheduler, so the server wires it. nil-safe.
	OnCommandPhase func(ctx context.Context, commandID string)
	// OnNodeBooted is invoked for a heartbeat that carries an uptime, with the
	// boot time computed from it, so a batch waiting on the node's reboot can
	// settle it. Wired in pkg/server/server.go. nil-safe.
	OnNodeBooted func(ctx context.Context, nodeID, bootState string, bootedAt *time.Time)
	// PingInterval and ReadTimeout override agentPingInterval and
	// agentReadTimeout. Zero or negative means the default. Only tests set
	// these; production wiring leaves them unset.
	PingInterval time.Duration
	ReadTimeout  time.Duration
}

// triggerFinalize fires the auto eject-on-phone-home hook off the WS read loop so
// it never blocks message handling. Nil-safe; the hook's per-deployment CAS makes
// repeated heartbeats harmless no-ops.
func (h *AgentHandler) triggerFinalize(nodeID string) {
	if h.Finalize == nil || nodeID == "" {
		return
	}
	base := h.BaseCtx
	if base == nil {
		base = context.Background()
	}
	go func() {
		ctx, cancel := context.WithTimeout(base, finalizeTimeout)
		defer cancel()
		h.Finalize(ctx, nodeID)
	}()
}

// HandleAgentWS handles GET /api/v1/ws. The node key is read from the
// Authorization header, or from ?token= for agents that send it there.
func (h *AgentHandler) HandleAgentWS(c echo.Context) error {
	token := auth.BearerToken(c)
	if token == "" {
		token = c.QueryParam("token")
	}
	if token == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "missing token"})
	}

	ctx := c.Request().Context()
	node, err := h.Nodes.GetByAPIKey(ctx, token)
	if err != nil || node == nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid token"})
	}

	conn, err := newUpgrader(h.PublicURL).Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetReadLimit(maxMessageBytes)

	// Captured once: the connection's remote address does not change over its
	// life, and the read loop below no longer has c in scope by the time a
	// heartbeat frame arrives.
	remoteIP := c.RealIP()

	// Register in hub and mark node online. wc wraps conn with a per-connection
	// write lock; all writes to this connection (here and from the hub) must go
	// through it so they don't race.
	wc := h.Hub.Register(node.ID, conn)
	if err := h.Nodes.UpdatePhase(ctx, node.ID, store.PhaseOnline); err != nil {
		log.Printf("ws: failed to update node phase: %v", err)
	}

	defer func() {
		// A newer connection from the same node replaced this one: the node is
		// still online on it, so neither drop its hub entry nor mark it offline.
		if !h.Hub.Unregister(node.ID, wc) {
			return
		}
		// context.Background() is correct here: the HTTP request context is
		// already done when the WS connection tears down, but we still need
		// to persist the offline phase change.
		if err := h.Nodes.UpdatePhase(context.Background(), node.ID, store.PhaseOffline); err != nil {
			log.Printf("ws: failed to update node phase: %v", err)
		}
	}()

	// Keepalive. Without this the read below blocks forever on a peer that
	// stopped talking without closing, and the deferred offline write above
	// never runs. The UI handler has had this since it was written; the agent
	// connection is the one that actually carries fleet liveness.
	readTimeout := h.ReadTimeout
	if readTimeout <= 0 {
		readTimeout = agentReadTimeout
	}
	pingInterval := h.PingInterval
	if pingInterval <= 0 {
		pingInterval = agentPingInterval
	}
	_ = conn.SetReadDeadline(time.Now().Add(readTimeout))
	// gorilla answers our ping from its default ping handler, which both the
	// agent's phonehome client and pkg/client leave in place, so a healthy
	// agent needs no change to keep this connection alive.
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(readTimeout))
	})

	// Ping off a separate goroutine: the read loop below is blocking, so it
	// cannot drive a ticker itself. Writes go through wc, not conn, because the
	// hub writes to this same connection concurrently.
	pingDone := make(chan struct{})
	defer close(pingDone)
	go func() {
		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-pingDone:
				return
			case <-ticker.C:
				if err := wc.writeControl(websocket.PingMessage, nil, time.Now().Add(pingWriteWait)); err != nil {
					// The read loop sees the same failure and tears the
					// connection down; nothing to do here but stop pinging.
					return
				}
			}
		}
	}()

	// Send pending commands on connect.
	h.sendPendingCommands(node.ID, wc)

	// Read loop.
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			switch {
			case websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure):
				log.Printf("ws read error for node %s: %v", node.ID, err)
			case errors.Is(err, websocket.ErrReadLimit):
				log.Printf("ws: node %s sent a message over %d bytes, dropping connection", node.ID, maxMessageBytes)
			case errors.Is(err, os.ErrDeadlineExceeded):
				// Worth its own line: this is the silent-peer case, and it is
				// not an abnormal close, so IsUnexpectedCloseError says nothing
				// about it. Logging it is how an operator learns that a node
				// reported Offline because it went quiet rather than because it
				// shut down cleanly.
				log.Printf("ws: node %s missed keepalive for %s, dropping connection", node.ID, readTimeout)
			}
			return nil
		}
		// Any frame proves the peer is alive, not just a pong.
		_ = conn.SetReadDeadline(time.Now().Add(readTimeout))

		var msg wsMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			log.Printf("ws invalid message from node %s: %v", node.ID, err)
			continue
		}

		switch msg.Type {
		case "heartbeat":
			h.handleHeartbeat(node.ID, msg.Data, remoteIP)
		case "command_status":
			h.handleCommandStatus(node.ID, msg.Data)
		default:
			log.Printf("ws unknown message type from node %s: %s", node.ID, msg.Type)
		}
	}
}

func (h *AgentHandler) handleHeartbeat(nodeID string, data json.RawMessage, remoteIP string) {
	var hb heartbeatData
	if err := json.Unmarshal(data, &hb); err != nil {
		log.Printf("ws invalid heartbeat from node %s: %v", nodeID, err)
		return
	}

	// context.Background() is correct here: handleHeartbeat is called from the
	// WS read loop which outlives the original HTTP request context.
	ctx := context.Background()
	// An agent that omits addresses, boot state or hostname sends nil or "",
	// which leaves the stored value alone. remoteIP is always known here: it is
	// the live connection's address, not something the agent reports.
	heartbeatErr := h.Nodes.UpdateHeartbeat(ctx, nodeID, hb.AgentVersion, hb.OSRelease, hb.Addresses, hb.BootState, hb.Hostname, remoteIP)
	if heartbeatErr != nil {
		log.Printf("ws: failed to update heartbeat for node %s: %v", nodeID, heartbeatErr)
	}
	if err := h.Nodes.UpdatePhase(ctx, nodeID, store.PhaseOnline); err != nil {
		log.Printf("ws: failed to update node phase: %v", err)
	}
	// Record metrics only for a heartbeat the store accepted: a node deleted
	// while its socket is still open must not get samples back after Forget.
	if heartbeatErr == nil {
		h.recordMetrics(nodeID, hb.Metrics)
		if bootedAt, ok := store.BootedAt(time.Now(), hb.Metrics); ok {
			if err := h.Nodes.SettleAfterReboot(ctx, nodeID, hb.BootState, bootedAt); err != nil {
				log.Printf("ws: failed to settle node %s after its reboot: %v", nodeID, err)
			}
			if h.OnNodeBooted != nil {
				h.OnNodeBooted(ctx, nodeID, hb.BootState, &bootedAt)
			}
		}
	}
	// A WS heartbeat is an "OS is up" signal exactly like the REST heartbeat:
	// attempt the auto eject-on-phone-home (nil-safe, off this goroutine).
	h.triggerFinalize(nodeID)
}

// recordMetrics stores the heartbeat's metrics sample for nodeID (the node this
// connection authenticated as), if any. A sample that does not decode is logged
// and dropped.
func (h *AgentHandler) recordMetrics(nodeID string, raw json.RawMessage) {
	if h.Metrics == nil || len(raw) == 0 || string(raw) == "null" {
		return
	}
	var m store.NodeMetrics
	if err := json.Unmarshal(raw, &m); err != nil {
		log.Printf("ws: ignoring invalid heartbeat metrics from node %s: %v", nodeID, err)
		return
	}
	h.Metrics.Record(nodeID, m)
}

// handleCommandStatus applies a command_status report from the agent. The
// update is scoped to nodeID — the node this WS connection authenticated as —
// so a node cannot move another node's command by sending its id. A miss
// (foreign or unknown command), an unknown phase, or a report on a command
// that already finished is rejected without broadcasting.
func (h *AgentHandler) handleCommandStatus(nodeID string, data json.RawMessage) {
	var status commandStatusData
	if err := json.Unmarshal(data, &status); err != nil {
		log.Printf("ws invalid command_status: %v", err)
		return
	}
	if !store.IsValidCommandPhase(status.Phase) {
		log.Printf("ws: node %s reported unknown phase %q for command %s; ignored", nodeID, status.Phase, status.ID)
		return
	}

	// context.Background() is correct here: handleCommandStatus is called from
	// the WS read loop which outlives the original HTTP request context.
	ctx := context.Background()
	if err := h.Commands.UpdateStatusForNode(ctx, status.ID, nodeID, status.Phase, status.Result); err != nil {
		if errors.Is(err, store.ErrCommandNotFound) {
			log.Printf("ws: node %s attempted to update command %s it does not own; rejected", nodeID, status.ID)
		} else if errors.Is(err, store.ErrCommandTerminal) {
			log.Printf("ws: node %s reported %s for command %s that already finished; ignored", nodeID, status.Phase, status.ID)
		} else {
			log.Printf("ws: failed to update command status for %s: %v", status.ID, err)
		}
		return
	}
	if status.Phase == store.CommandCompleted && h.OnCommandStatus != nil {
		if cmd, err := h.Commands.GetByID(ctx, status.ID); err == nil && cmd != nil {
			h.OnCommandStatus(ctx, cmd.ManagedNodeID, cmd)
		}
	}

	if h.OnCommandPhase != nil {
		h.OnCommandPhase(ctx, status.ID)
	}

	if h.Hub != nil && h.Hub.UI != nil {
		h.Hub.UI.Broadcast(wsMessage{
			Type: "command_update",
			Data: data,
		})
	}
}

func (h *AgentHandler) sendPendingCommands(nodeID string, conn *wsConn) {
	ctx := context.Background()
	cmds, err := h.Commands.GetPending(ctx, nodeID)
	if err != nil || len(cmds) == 0 {
		return
	}

	for _, cmd := range cmds {
		// Atomically claim Pending→Delivered before sending so a concurrent REST
		// poll or WS push can't also deliver the same command. If another path
		// already claimed it, our claim returns false and we skip it — exactly-once
		// delivery, consistent with the poll path in NodeHandler.GetCommands.
		claimed, err := h.Commands.ClaimForDelivery(ctx, cmd.ID)
		if err != nil {
			log.Printf("ws: failed to claim pending command %s for node %s: %v", cmd.ID, nodeID, err)
			continue
		}
		if !claimed {
			continue
		}
		store.MarkResetDelivered(ctx, h.Nodes, cmd)

		cmdMsg := commandData{
			ID:      cmd.ID,
			Command: cmd.Command,
			Args:    cmd.Args,
		}
		data, err := json.Marshal(cmdMsg)
		if err != nil {
			log.Printf("ws: failed to marshal command %s: %v", cmd.ID, err)
			continue
		}
		msg := wsMessage{Type: "command", Data: data}
		msgBytes, err := json.Marshal(msg)
		if err != nil {
			log.Printf("ws: failed to marshal ws message for command %s: %v", cmd.ID, err)
			continue
		}

		if err := conn.writeMessage(websocket.TextMessage, msgBytes); err != nil {
			log.Printf("ws: failed to send pending command %s to node %s: %v", cmd.ID, nodeID, err)
			break
		}
	}
}

// UIHandler handles WebSocket connections from the UI.
type UIHandler struct {
	Hub *Hub

	// PublicURL is the server's public URL (--url); an upgrade whose Origin
	// names it is accepted even when a proxy rewrote Host.
	PublicURL string
}

// HandleUIWS handles GET /api/v1/ws/ui.
func (h *UIHandler) HandleUIWS(c echo.Context) error {
	conn, err := newUpgrader(h.PublicURL).Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetReadLimit(maxMessageBytes)

	// Register with UIHub for broadcast support. wc wraps conn with a
	// per-connection write lock; the ping ticker below writes through it so it
	// never races a concurrent Broadcast on the same conn.
	var wc *wsConn
	if h.Hub != nil && h.Hub.UI != nil {
		var uiID string
		uiID, wc = h.Hub.UI.Register(conn)
		defer h.Hub.UI.Unregister(uiID)
	} else {
		wc = newConn(conn)
	}

	// Keep connection alive with ping/pong.
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-done:
			return nil
		case <-ticker.C:
			if err := wc.writeControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)); err != nil {
				return nil
			}
		}
	}
}
