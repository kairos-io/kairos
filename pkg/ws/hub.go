package ws

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
)

// Hub tracks active WebSocket connections per node.
//
// mu guards the connections map only. Each stored *wsConn carries its own write
// mutex, so concurrent SendCommand calls to the same node — and any other writer
// to that connection — are serialized at the connection level, as gorilla
// requires, without holding the map lock across a network write.
type Hub struct {
	connections map[string]*wsConn
	mu          sync.RWMutex
	UI          *UIHub
}

// NewHub creates a new Hub with an embedded UIHub.
func NewHub() *Hub {
	return &Hub{
		connections: make(map[string]*wsConn),
		UI:          NewUIHub(),
	}
}

// Register stores the WebSocket connection for the given node ID and returns the
// wrapped connection whose writeMessage/writeControl methods serialize writes.
// Callers (e.g. the agent read-loop) must route all of their own writes through
// the returned wrapper so they don't race the hub's writes on the same conn.
func (h *Hub) Register(nodeID string, conn *websocket.Conn) *wsConn {
	wc := newConn(conn)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.connections[nodeID] = wc
	return wc
}

// Unregister removes the WebSocket connection for the given node ID, but only
// while wc is still the one registered for it. An agent that reconnects before
// its old socket times out registers a new connection over the old one; the old
// socket's teardown must then leave the live connection in place. It reports
// whether wc was removed, which is false once a newer connection replaced it.
func (h *Hub) Unregister(nodeID string, wc *wsConn) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if cur, ok := h.connections[nodeID]; !ok || cur != wc {
		return false
	}
	delete(h.connections, nodeID)
	return true
}

// SendCommand sends a command message to the specified node via WebSocket.
// Returns an error if the node is not connected.
func (h *Hub) SendCommand(nodeID string, cmd any) error {
	// Hold the map lock only long enough to look up the connection; the actual
	// network write happens under the per-connection write lock so concurrent
	// SendCommand calls to the same node are serialized without blocking the map.
	h.mu.RLock()
	conn, ok := h.connections[nodeID]
	h.mu.RUnlock()

	if !ok || conn == nil {
		return fmt.Errorf("node %s is not connected", nodeID)
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal command data: %w", err)
	}

	msg := wsMessage{
		Type: "command",
		Data: data,
	}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal ws message: %w", err)
	}

	if err := conn.writeMessage(websocket.TextMessage, msgBytes); err != nil {
		return fmt.Errorf("write message: %w", err)
	}

	return nil
}

// UIHub tracks active WebSocket connections from UI clients.
//
// As with Hub, mu guards the connections map only; each stored *wsConn carries
// its own write mutex so concurrent Broadcasts (e.g. a build-log chunk and a
// deploy-progress update) never race on the same UI connection.
type UIHub struct {
	connections map[string]*wsConn
	mu          sync.RWMutex
	counter     atomic.Int64
}

// NewUIHub creates a new UIHub.
func NewUIHub() *UIHub {
	return &UIHub{connections: make(map[string]*wsConn)}
}

// Register stores the WebSocket connection and returns its unique ID and the
// wrapped connection. The UI handler must route its own writes (the ping ticker)
// through the returned wrapper so they don't race the hub's Broadcasts.
func (h *UIHub) Register(conn *websocket.Conn) (string, *wsConn) {
	id := fmt.Sprintf("ui-%d", h.counter.Add(1))
	wc := newConn(conn)
	h.mu.Lock()
	h.connections[id] = wc
	h.mu.Unlock()
	return id, wc
}

// Unregister removes the WebSocket connection with the given ID.
func (h *UIHub) Unregister(id string) {
	h.mu.Lock()
	delete(h.connections, id)
	h.mu.Unlock()
}

// Count returns the number of currently connected UI clients.
func (h *UIHub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.connections)
}

// Broadcast sends a message to all connected UI clients.
func (h *UIHub) Broadcast(msg any) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	// Snapshot the connection set under the map lock, then write outside it so a
	// slow client can't block other Broadcasts. Each write takes the per-conn
	// write lock, serializing concurrent Broadcasts on the same connection.
	h.mu.RLock()
	conns := make([]*wsConn, 0, len(h.connections))
	for _, conn := range h.connections {
		conns = append(conns, conn)
	}
	h.mu.RUnlock()
	// Write to every client at once, so a client that stopped reading costs one
	// writeWait instead of delaying the others behind it. Waiting for all of
	// them keeps per-client message order for callers that broadcast in a row.
	var wg sync.WaitGroup
	for _, conn := range conns {
		wg.Add(1)
		go func(c *wsConn) {
			defer wg.Done()
			_ = c.writeMessage(websocket.TextMessage, data)
		}(conn)
	}
	wg.Wait()
}

// BroadcastLogChunk fans out a chunk of build log to every connected UI
// client as a {"type":"build-log","data":{"id":…,"chunk":…}} envelope.
// Clients filter by build ID on the receive side; broadcasting to all is
// cheap and matches the single-tenant deployment model of auroraboot.
func (h *UIHub) BroadcastLogChunk(buildID string, chunk string) {
	h.Broadcast(map[string]any{
		"type": "build-log",
		"data": map[string]any{
			"id":    buildID,
			"chunk": chunk,
		},
	})
}

// BroadcastNetbootLogChunk fans out a chunk of the PXE/netboot server's live
// output to every connected UI client as a
// {"type":"netboot-log","data":{"chunk":…}} envelope, so a client watching a
// PXE boot in progress can see why it stalls or fails (kairos-io/kairos#4596).
// There is at most one netboot session at a time (internal/netbootmgr.Manager
// is a singleton), so unlike build-log there is no id to filter on.
func (h *UIHub) BroadcastNetbootLogChunk(chunk string) {
	h.Broadcast(map[string]any{
		"type": "netboot-log",
		"data": map[string]any{
			"chunk": chunk,
		},
	})
}

// IsOnline returns true if the node has an active WebSocket connection.
func (h *Hub) IsOnline(nodeID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.connections[nodeID]
	return ok
}

// OnlineCount returns the number of currently connected nodes.
func (h *Hub) OnlineCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.connections)
}
