package handlers

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
)

// GroupHandler handles group-related REST endpoints.
type GroupHandler struct {
	groups store.GroupStore
}

// NewGroupHandler creates a new GroupHandler.
func NewGroupHandler(groups store.GroupStore) *GroupHandler {
	return &GroupHandler{groups: groups}
}

// createGroupRequest is the expected body for creating a group.
type createGroupRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Create handles POST /api/v1/groups.
//
//	@Summary		Create a group
//	@Tags			Groups
//	@Accept			json
//	@Produce		json
//	@Security		AdminBearer
//	@Param			body	body		APICreateGroupRequest	true	"Group payload"
//	@Success		201		{object}	store.NodeGroup
//	@Failure		400		{object}	APIError
//	@Failure		409		{object}	APIError
//	@Failure		413		{object}	APIError
//	@Router			/api/v1/groups [post]
func (h *GroupHandler) Create(c echo.Context) error {
	var req createGroupRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if req.Name == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "name is required"})
	}

	group := &store.NodeGroup{
		ID:          uuid.New().String(),
		Name:        req.Name,
		Description: req.Description,
	}

	if err := h.groups.Create(c.Request().Context(), group); err != nil {
		if errors.Is(err, store.ErrGroupNameTaken) {
			return nameTaken(c, req.Name)
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create group"})
	}

	return c.JSON(http.StatusCreated, group)
}

// List handles GET /api/v1/groups.
//
//	@Summary	List groups
//	@Tags		Groups
//	@Produce	json
//	@Security	AdminBearer
//	@Success	200	{array}	store.NodeGroup
//	@Router		/api/v1/groups [get]
func (h *GroupHandler) List(c echo.Context) error {
	ctx := c.Request().Context()
	groups, err := h.groups.List(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to list groups"})
	}
	counts, err := h.groups.NodeCounts(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to count group nodes"})
	}
	if groups == nil {
		groups = []*store.NodeGroup{}
	}
	for _, g := range groups {
		setNodeCount(g, counts)
	}
	return c.JSON(http.StatusOK, groups)
}

// Get handles GET /api/v1/groups/:id.
//
//	@Summary	Get a group
//	@Tags		Groups
//	@Produce	json
//	@Security	AdminBearer
//	@Param		id	path		string	true	"Group ID"
//	@Success	200	{object}	store.NodeGroup
//	@Failure	404	{object}	APIError
//	@Router		/api/v1/groups/{id} [get]
func (h *GroupHandler) Get(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()
	group, err := h.groups.GetByID(ctx, id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "group not found"})
	}
	counts, err := h.groups.NodeCounts(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to count group nodes"})
	}
	setNodeCount(group, counts)
	return c.JSON(http.StatusOK, group)
}

// setNodeCount sets the group's NodeCount from counts; a group with no key
// gets 0.
func setNodeCount(g *store.NodeGroup, counts map[string]int) {
	n := counts[g.ID]
	g.NodeCount = &n
}

// updateGroupRequest is the expected body for updating a group.
// An omitted description leaves the stored one unchanged; an empty string
// clears it.
type updateGroupRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
}

// Update handles PUT /api/v1/groups/:id.
//
//	@Summary	Update a group
//	@Tags		Groups
//	@Accept		json
//	@Produce	json
//	@Security	AdminBearer
//	@Param		id		path		string					true	"Group ID"
//	@Param		body	body		APIUpdateGroupRequest	true	"Update payload"
//	@Success	200		{object}	store.NodeGroup
//	@Failure	404		{object}	APIError
//	@Failure	409		{object}	APIError
//	@Failure	413		{object}	APIError
//	@Router		/api/v1/groups/{id} [put]
func (h *GroupHandler) Update(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()

	group, err := h.groups.GetByID(ctx, id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "group not found"})
	}

	var req updateGroupRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if req.Name != "" {
		group.Name = req.Name
	}
	if req.Description != nil {
		group.Description = *req.Description
	}

	if err := h.groups.Update(ctx, group); err != nil {
		if errors.Is(err, store.ErrGroupNameTaken) {
			return nameTaken(c, group.Name)
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to update group"})
	}

	return c.JSON(http.StatusOK, group)
}

func nameTaken(c echo.Context, name string) error {
	return c.JSON(http.StatusConflict, map[string]string{"error": fmt.Sprintf("a group named %q already exists", name)})
}

// Delete handles DELETE /api/v1/groups/:id.
//
//	@Summary		Delete a group
//	@Description	Member nodes are detached in the same transaction; nodes are never deleted.
//	@Tags			Groups
//	@Security		AdminBearer
//	@Param			id	path	string	true	"Group ID"
//	@Success		204
//	@Router			/api/v1/groups/{id} [delete]
func (h *GroupHandler) Delete(c echo.Context) error {
	id := c.Param("id")
	if err := h.groups.Delete(c.Request().Context(), id); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to delete group"})
	}
	return c.NoContent(http.StatusNoContent)
}
