package handlers

import (
	"net/http"

	"github.com/kairos-io/AuroraBoot/pkg/metrics"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
)

// MetricsHandler serves the in-memory node metrics that heartbeats carry.
type MetricsHandler struct {
	buf *metrics.Buffer
}

// NewMetricsHandler returns a MetricsHandler that reads from b.
func NewMetricsHandler(b *metrics.Buffer) *MetricsHandler {
	return &MetricsHandler{buf: b}
}

// GetNode handles GET /api/v1/nodes/:nodeID/metrics.
//
//	@Summary		Get node metrics
//	@Description	Returns the latest resource sample of a node and its recent samples, oldest first. Samples are kept in memory only. A node without samples returns a null latest and an empty samples list.
//	@Tags			Nodes
//	@Produce		json
//	@Security		AdminBearer
//	@Param			nodeID	path		string	true	"Node ID"
//	@Success		200		{object}	APINodeMetricsResponse
//	@Failure		401		{object}	APIError
//	@Router			/api/v1/nodes/{nodeID}/metrics [get]
func (h *MetricsHandler) GetNode(c echo.Context) error {
	nodeID := c.Param("nodeID")
	resp := APINodeMetricsResponse{Samples: []store.NodeMetrics{}}
	if latest, ok := h.buf.Latest(nodeID); ok {
		resp.Latest = &latest
	}
	if s := h.buf.Samples(nodeID); len(s) > 0 {
		resp.Samples = s
	}
	return c.JSON(http.StatusOK, resp)
}

// GetLatest handles GET /api/v1/metrics/latest.
//
//	@Summary		Get the latest metrics of every node
//	@Description	Returns the latest resource sample of every node that has one, keyed by node ID. Samples are kept in memory only.
//	@Tags			Nodes
//	@Produce		json
//	@Security		AdminBearer
//	@Success		200	{object}	map[string]store.NodeMetrics
//	@Failure		401	{object}	APIError
//	@Router			/api/v1/metrics/latest [get]
func (h *MetricsHandler) GetLatest(c echo.Context) error {
	return c.JSON(http.StatusOK, h.buf.AllLatest())
}
