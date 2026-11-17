package client

import (
	"context"
	"net/http"
)

// CommandsService groups the command-queue endpoints.
type CommandsService struct{ c *Client }

// Create queues a command for a single node.
func (s *CommandsService) Create(ctx context.Context, nodeID string, req CreateCommandRequest) (*NodeCommand, error) {
	var out NodeCommand
	if err := s.c.do(ctx, http.MethodPost, "/api/v1/nodes/"+nodeID+"/commands", nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateBulk starts a batch that runs a command on every node matching a
// selector. Either groupID, labels, or explicit nodeIDs can be used.
func (s *CommandsService) CreateBulk(ctx context.Context, req BulkCommandRequest) (*Batch, error) {
	var out Batch
	if err := s.c.do(ctx, http.MethodPost, "/api/v1/nodes/commands", nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateStatus updates a command's phase and/or result. Used by
// agents to report progress after executing a command received on
// the WebSocket; admins can also use it to force a status transition.
func (s *CommandsService) UpdateStatus(ctx context.Context, nodeID, commandID string, req UpdateCommandStatusRequest) error {
	return s.c.do(ctx, http.MethodPut, "/api/v1/nodes/"+nodeID+"/commands/"+commandID+"/status", nil, req, nil)
}

// Delete removes a single command.
func (s *CommandsService) Delete(ctx context.Context, nodeID, commandID string) error {
	return s.c.do(ctx, http.MethodDelete, "/api/v1/nodes/"+nodeID+"/commands/"+commandID, nil, nil, nil)
}

// ClearHistory removes all terminal (Completed/Failed/Expired)
// commands for a node.
func (s *CommandsService) ClearHistory(ctx context.Context, nodeID string) error {
	return s.c.do(ctx, http.MethodDelete, "/api/v1/nodes/"+nodeID+"/commands", nil, nil, nil)
}
