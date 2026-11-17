package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// BatchesService groups the rolling-batch endpoints.
type BatchesService struct{ c *Client }

// Get fetches a batch with its per-node rows.
func (s *BatchesService) Get(ctx context.Context, id string) (*Batch, error) {
	var out Batch
	if err := s.c.do(ctx, http.MethodGet, "/api/v1/batches/"+url.PathEscape(id), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// List returns a page of batches, newest first, filtered by opts.
func (s *BatchesService) List(ctx context.Context, opts BatchListOptions) (*BatchList, error) {
	q := url.Values{}
	if opts.GroupID != "" {
		q.Set("groupID", opts.GroupID)
	}
	if opts.NodeID != "" {
		q.Set("nodeID", opts.NodeID)
	}
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Offset > 0 {
		q.Set("offset", strconv.Itoa(opts.Offset))
	}
	var out BatchList
	if err := s.c.do(ctx, http.MethodGet, "/api/v1/batches", q, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Stop stops a running batch so it starts no more nodes. The returned error
// satisfies IsConflict when the batch is not running.
func (s *BatchesService) Stop(ctx context.Context, id string) (*Batch, error) {
	var out Batch
	if err := s.c.do(ctx, http.MethodPost, "/api/v1/batches/"+url.PathEscape(id)+"/stop", nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
