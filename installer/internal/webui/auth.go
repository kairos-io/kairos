package webui

import (
	"net/http"
	"strings"

	"github.com/kairos-io/kairos/v4/sdk/branding"
	"github.com/labstack/echo/v5"
)

// tokenCookie is where a browser keeps the token after it arrived once as a
// query parameter. The install form posts and the progress websocket both
// happen from pages the user navigated to, and neither carries the parameter
// the QR code put on the first URL, so without the cookie the second request
// of the session would be refused.
const tokenCookie = "kairos-installer-token"

// requireToken refuses every request that does not present the token the image
// set. It returns nil when no token was set, because an unbranded live ISO
// answers to the person standing in front of it and has no secret to check.
//
// A request may present the token three ways:
//
//   - "Authorization: Bearer <token>", which is what an agent harness or a
//     curl in an unattended install sends,
//   - "?token=<token>", which is what the URL the installer prints and encodes
//     as a QR code carries, so opening it from a phone is enough,
//   - the cookie a query parameter leaves behind, so the pages that URL leads
//     to keep working.
func requireToken(w branding.WebUI) echo.MiddlewareFunc {
	if !w.HasToken() {
		return nil
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if w.TokenMatches(bearerToken(c.Request())) {
				return next(c)
			}

			if q := c.QueryParam(branding.TokenParam); w.TokenMatches(q) {
				// Hand the browser a cookie, so the form it is about to
				// submit and the websocket the progress page opens do not
				// have to carry the parameter themselves.
				c.SetCookie(&http.Cookie{
					Name:     tokenCookie,
					Value:    q,
					Path:     "/",
					HttpOnly: true,
					SameSite: http.SameSiteStrictMode,
				})
				return next(c)
			}

			if ck, err := c.Cookie(tokenCookie); err == nil && w.TokenMatches(ck.Value) {
				return next(c)
			}

			// No body: what is behind this is which disks the machine has and
			// whether an install is running, and a caller that did not
			// authenticate is told none of it.
			return c.NoContent(http.StatusUnauthorized)
		}
	}
}

// bearerToken returns the token in an Authorization header, or "" when the
// header is absent or is not a bearer one.
func bearerToken(r *http.Request) string {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if len(h) < len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(h[len(prefix):])
}
