package handlers

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/kairos-io/AuroraBoot/pkg/auth"
	"github.com/labstack/echo/v4"
)

// maxLoginBodyBytes caps the login body, which is read before the caller is
// authenticated: it carries one password.
const maxLoginBodyBytes = 4 * 1024

// SessionHandler signs the UI in and out with the admin password.
type SessionHandler struct {
	auth *auth.AdminAuth
}

// NewSessionHandler returns a SessionHandler that checks the password and
// keeps sessions through the given AdminAuth.
func NewSessionHandler(a *auth.AdminAuth) *SessionHandler {
	return &SessionHandler{auth: a}
}

// LoginRequest is the body of POST /api/v1/session.
type LoginRequest struct {
	Password string `json:"password"`
}

// Login handles POST /api/v1/session.
//
//	@Summary		Sign in
//	@Description	Exchanges the admin password for an HttpOnly session cookie that lasts 12 hours. A request carrying an Origin header must come from this server's origin.
//	@Tags			session
//	@Accept			json
//	@Param			body	body	LoginRequest	true	"Admin password"
//	@Success		204
//	@Failure		400	{object}	APIError
//	@Failure		401	{object}	APIError
//	@Failure		403	{object}	APIError
//	@Failure		413	{object}	APIError
//	@Router			/api/v1/session [post]
func (h *SessionHandler) Login(c echo.Context) error {
	if !h.auth.OriginAllowed(c.Request()) {
		return c.JSON(http.StatusForbidden, APIError{Error: "forbidden"})
	}
	req := c.Request()
	if req.ContentLength > maxLoginBodyBytes {
		return bodyTooLarge(c)
	}
	raw, err := io.ReadAll(io.LimitReader(req.Body, maxLoginBodyBytes+1))
	if err != nil {
		return c.JSON(http.StatusBadRequest, APIError{Error: "cannot read request body"})
	}
	if len(raw) > maxLoginBodyBytes {
		return bodyTooLarge(c)
	}
	var body LoginRequest
	if err := json.Unmarshal(raw, &body); err != nil {
		return c.JSON(http.StatusBadRequest, APIError{Error: "invalid JSON body"})
	}
	if !h.auth.PasswordMatches(body.Password) {
		return c.JSON(http.StatusUnauthorized, APIError{Error: "unauthorized"})
	}
	id, expires := h.auth.Sessions().Create()
	auth.SetSessionCookie(c, id, expires)
	return c.NoContent(http.StatusNoContent)
}

// Check handles GET /api/v1/session. The admin middleware has already
// authenticated the request, so reaching it means the caller is signed in.
//
//	@Summary		Check the session
//	@Tags			session
//	@Success		204
//	@Failure		401	{object}	APIError
//	@Router			/api/v1/session [get]
func (h *SessionHandler) Check(c echo.Context) error {
	return c.NoContent(http.StatusNoContent)
}

// Logout handles DELETE /api/v1/session. It ends the session named by the
// cookie, if any, and clears the cookie.
//
//	@Summary		Sign out
//	@Tags			session
//	@Success		204
//	@Failure		403	{object}	APIError
//	@Router			/api/v1/session [delete]
func (h *SessionHandler) Logout(c echo.Context) error {
	if !h.auth.OriginAllowed(c.Request()) {
		return c.JSON(http.StatusForbidden, APIError{Error: "forbidden"})
	}
	if ck, err := c.Cookie(auth.SessionCookieName); err == nil {
		h.auth.Sessions().Delete(ck.Value)
	}
	auth.ClearSessionCookie(c)
	return c.NoContent(http.StatusNoContent)
}
