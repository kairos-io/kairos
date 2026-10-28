package handlers

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// SystemHandler serves the /api/v1/system/* introspection endpoints. Its info
// value is populated at wire time in runWeb from the flags plus the resolved
// kube REST config, and is returned verbatim to callers.
type SystemHandler struct {
	info APISystemBuilder
}

// NewSystemHandler creates a SystemHandler that reports the given wire-time info.
func NewSystemHandler(info APISystemBuilder) *SystemHandler {
	return &SystemHandler{info: info}
}

// GetBuilder handles GET /api/v1/system/builder.
//
//	@Summary	Report the active builder backend
//	@Tags		System
//	@Produce	json
//	@Security	AdminBearer
//	@Success	200	{object}	APISystemBuilder
//	@Router		/api/v1/system/builder [get]
func (h *SystemHandler) GetBuilder(c echo.Context) error {
	return c.JSON(http.StatusOK, h.info)
}
