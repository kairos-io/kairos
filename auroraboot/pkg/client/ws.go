package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
)

// WSMessage is the envelope used by both the agent (/api/v1/ws) and
// UI (/api/v1/ws/ui) WebSocket channels. `Data` is delivered as raw
// bytes so callers can unmarshal into a type specific to each Type.
type WSMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// DialAgentWS opens the agent command WebSocket using the client's
// node API key, sent in the Authorization header. Returns a live *websocket.Conn the caller owns.
//
// The agent receives {type:"command", data:{...}} envelopes and
// replies with {type:"status", data:{...}}.
func (c *Client) DialAgentWS(ctx context.Context) (*websocket.Conn, error) {
	if c.nodeAPIKey == "" {
		return nil, fmt.Errorf("client: DialAgentWS requires a node API key (use WithNodeAPIKey)")
	}
	wsURL, err := toWSURL(c.baseURL, "/api/v1/ws")
	if err != nil {
		return nil, err
	}

	header := http.Header{"Authorization": {"Bearer " + c.nodeAPIKey}}
	conn, resp, err := websocket.DefaultDialer.DialContext(ctx, wsURL, header)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("dial agent ws: %w", err)
	}
	return conn, nil
}

// DialUIWS opens the UI live-update WebSocket using the client's
// admin password, sent in the Authorization header. Returns a live *websocket.Conn the caller owns.
//
// The UI channel broadcasts heterogeneous envelopes such as
// {type:"build-log", data:{id,chunk}} and
// {type:"artifact-update", data:{id, phase}}.
func (c *Client) DialUIWS(ctx context.Context) (*websocket.Conn, error) {
	if c.adminPassword == "" {
		return nil, fmt.Errorf("client: DialUIWS requires an admin password (use WithAdminPassword)")
	}
	wsURL, err := toWSURL(c.baseURL, "/api/v1/ws/ui")
	if err != nil {
		return nil, err
	}

	header := http.Header{"Authorization": {"Bearer " + c.adminPassword}}
	conn, resp, err := websocket.DefaultDialer.DialContext(ctx, wsURL, header)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("dial ui ws: %w", err)
	}
	return conn, nil
}

// toWSURL converts an http:// or https:// base URL into the matching
// ws:// or wss:// form and appends the given path.
func toWSURL(base, path string) (string, error) {
	switch {
	case strings.HasPrefix(base, "http://"):
		return "ws" + strings.TrimPrefix(base, "http") + path, nil
	case strings.HasPrefix(base, "https://"):
		return "wss" + strings.TrimPrefix(base, "https") + path, nil
	default:
		return "", fmt.Errorf("client: base URL %q must be http:// or https://", base)
	}
}
