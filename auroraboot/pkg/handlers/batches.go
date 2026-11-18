package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/kairos-io/AuroraBoot/pkg/batches"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
)

// maxBatchListLimit caps the page size of GET /api/v1/batches.
const maxBatchListLimit = 200

// BatchHandler handles the batch inspection and stop endpoints.
type BatchHandler struct {
	store     store.BatchStore
	scheduler *batches.Scheduler
}

// NewBatchHandler creates a BatchHandler. A nil store or scheduler answers
// every endpoint with 503.
func NewBatchHandler(bs store.BatchStore, scheduler *batches.Scheduler) *BatchHandler {
	return &BatchHandler{store: bs, scheduler: scheduler}
}

func (h *BatchHandler) unavailable(c echo.Context) bool {
	if h.store != nil && h.scheduler != nil {
		return false
	}
	_ = c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "batches are not available"})
	return true
}

// batchList is the body of GET /api/v1/batches.
type batchList struct {
	Batches []*store.CommandBatch `json:"batches"`
	Total   int                   `json:"total"`
}

// List handles GET /api/v1/batches.
//
//	@Summary	List batches, newest first
//	@Tags		Batches
//	@Produce	json
//	@Security	AdminBearer
//	@Param		groupID	query		string	false	"Only batches started for this group"
//	@Param		nodeID	query		string	false	"Only batches that include this node"
//	@Param		limit	query		int		false	"Page size (default 50, at most 200)"
//	@Param		offset	query		int		false	"Number of batches to skip"
//	@Success	200		{object}	batchList
//	@Failure	400		{object}	APIError
//	@Failure	503		{object}	APIError
//	@Router		/api/v1/batches [get]
func (h *BatchHandler) List(c echo.Context) error {
	if h.unavailable(c) {
		return nil
	}
	f := store.BatchFilter{GroupID: c.QueryParam("groupID"), NodeID: c.QueryParam("nodeID")}
	var err error
	if f.Limit, err = queryInt(c, "limit"); err != nil || f.Limit > maxBatchListLimit {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "limit must be between 0 and 200"})
	}
	if f.Offset, err = queryInt(c, "offset"); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "offset must not be negative"})
	}
	list, total, err := h.store.List(c.Request().Context(), f)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to list batches"})
	}
	if list == nil {
		list = []*store.CommandBatch{}
	}
	return c.JSON(http.StatusOK, batchList{Batches: list, Total: total})
}

// queryInt reads a non-negative integer query parameter; absent means 0.
func queryInt(c echo.Context, name string) (int, error) {
	raw := c.QueryParam(name)
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, errors.New(name + " is negative")
	}
	return n, nil
}

// Get handles GET /api/v1/batches/:id.
//
//	@Summary	Get a batch with its per-node state
//	@Tags		Batches
//	@Produce	json
//	@Security	AdminBearer
//	@Param		id	path		string	true	"Batch ID"
//	@Success	200	{object}	store.CommandBatch
//	@Failure	404	{object}	APIError
//	@Failure	503	{object}	APIError
//	@Router		/api/v1/batches/{id} [get]
func (h *BatchHandler) Get(c echo.Context) error {
	if h.unavailable(c) {
		return nil
	}
	b, err := h.store.Get(c.Request().Context(), c.Param("id"))
	if errors.Is(err, store.ErrBatchNotFound) {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "batch not found"})
	}
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to get batch"})
	}
	return c.JSON(http.StatusOK, b)
}

// Stop handles POST /api/v1/batches/:id/stop.
//
//	@Summary	Stop a running batch
//	@Tags		Batches
//	@Produce	json
//	@Security	AdminBearer
//	@Param		id	path		string	true	"Batch ID"
//	@Success	200	{object}	store.CommandBatch
//	@Failure	404	{object}	APIError
//	@Failure	409	{object}	APIError
//	@Failure	503	{object}	APIError
//	@Router		/api/v1/batches/{id}/stop [post]
func (h *BatchHandler) Stop(c echo.Context) error {
	if h.unavailable(c) {
		return nil
	}
	ctx := c.Request().Context()
	id := c.Param("id")
	switch err := h.scheduler.Stop(ctx, id); {
	case errors.Is(err, store.ErrBatchNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "batch not found"})
	case errors.Is(err, store.ErrBatchNotRunning):
		return c.JSON(http.StatusConflict, map[string]string{"error": "batch is not running"})
	case err != nil:
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to stop batch"})
	}
	b, err := h.store.Get(ctx, id)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to get batch"})
	}
	return c.JSON(http.StatusOK, b)
}
