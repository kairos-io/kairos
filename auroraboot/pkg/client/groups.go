package client

import (
	"context"
	"net/http"
)

// GroupsService groups the node-group endpoints.
type GroupsService struct{ c *Client }

// Create creates a new group.
func (s *GroupsService) Create(ctx context.Context, name, description string) (*Group, error) {
	body := map[string]string{"name": name, "description": description}
	var out Group
	if err := s.c.do(ctx, http.MethodPost, "/api/v1/groups", nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// List returns every group.
func (s *GroupsService) List(ctx context.Context) ([]Group, error) {
	var out []Group
	if err := s.c.do(ctx, http.MethodGet, "/api/v1/groups", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Get fetches a single group by ID.
func (s *GroupsService) Get(ctx context.Context, groupID string) (*Group, error) {
	var out Group
	if err := s.c.do(ctx, http.MethodGet, "/api/v1/groups/"+groupID, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Update patches a group's name and/or description. Empty fields are ignored server-side.
func (s *GroupsService) Update(ctx context.Context, groupID, name, description string) (*Group, error) {
	body := map[string]string{"name": name, "description": description}
	var out Group
	if err := s.c.do(ctx, http.MethodPut, "/api/v1/groups/"+groupID, nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Delete removes a group. Member nodes are detached (their groupID is
// cleared) in the same transaction; nodes themselves are never deleted.
func (s *GroupsService) Delete(ctx context.Context, groupID string) error {
	return s.c.do(ctx, http.MethodDelete, "/api/v1/groups/"+groupID, nil, nil, nil)
}

// SendCommand starts a batch that runs a command on every member of a group.
func (s *GroupsService) SendCommand(ctx context.Context, groupID string, req GroupCommandRequest) (*Batch, error) {
	var out Batch
	if err := s.c.do(ctx, http.MethodPost, "/api/v1/groups/"+groupID+"/commands", nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Claim atomically assigns one unclaimed node in the group to claimKey and
// returns it. It is idempotent: calling it again with the same claimKey returns
// the same node, so a retried or restarted reconcile re-finds its own node. When
// the group has no unclaimed node the returned error satisfies IsNoCapacity, so
// a caller can wait for capacity instead of failing.
func (s *GroupsService) Claim(ctx context.Context, groupID, claimKey string) (*Node, error) {
	var out Node
	body := map[string]string{"claimKey": claimKey}
	if err := s.c.do(ctx, http.MethodPost, "/api/v1/groups/"+groupID+"/claim", nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
