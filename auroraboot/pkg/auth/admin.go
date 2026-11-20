package auth

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// AdminAuth is the admin check shared by every middleware that lets an admin
// through: the password as a Bearer header (the SDK, the CLI, the CAPI
// provider) or a session cookie (the UI). A cookie is sent by the browser on
// its own, so a request it authenticates that changes state must also come
// from this server's origin.
type AdminAuth struct {
	password  string
	sessions  *SessionStore
	publicURL string
}

// NewAdminAuth returns the admin check for the given password, session store
// and public URL (the --url the server is reached at).
func NewAdminAuth(password string, sessions *SessionStore, publicURL string) *AdminAuth {
	return &AdminAuth{password: password, sessions: sessions, publicURL: publicURL}
}

// PasswordMatches reports whether p is the admin password.
func (a *AdminAuth) PasswordMatches(p string) bool {
	return p != "" && a.password != "" && secureCompare(p, a.password)
}

// Sessions returns the session store.
func (a *AdminAuth) Sessions() *SessionStore { return a.sessions }

// OriginAllowed applies the Origin rule with this server's public URL.
func (a *AdminAuth) OriginAllowed(r *http.Request) bool {
	return OriginAllowed(r, a.publicURL)
}

type adminResult int

const (
	notAdmin adminResult = iota
	isAdmin
	foreignOrigin
)

func (a *AdminAuth) check(c echo.Context) adminResult {
	if a.PasswordMatches(BearerToken(c)) {
		return isAdmin
	}
	if a.sessions == nil {
		return notAdmin
	}
	ck, err := c.Cookie(SessionCookieName)
	if err != nil || !a.sessions.Valid(ck.Value) {
		return notAdmin
	}
	if !safeMethod(c.Request().Method) && !a.OriginAllowed(c.Request()) {
		return foreignOrigin
	}
	return isAdmin
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// BearerToken returns the token of an "Authorization: Bearer" header, or "".
func BearerToken(c echo.Context) string {
	return extractBearer(c.Request().Header.Get("Authorization"))
}

func forbidden(c echo.Context) error {
	return c.JSON(http.StatusForbidden, map[string]string{"error": "forbidden"})
}
