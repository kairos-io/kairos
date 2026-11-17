package server

import (
	"bytes"
	"io"
	"net/http"

	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/labstack/echo/v4"
)

// maxRequestBodyBytes caps the body of every authenticated route without a
// limit of its own: the JSON API takes a few KiB per request, so 4 MiB is
// generous headroom.
const maxRequestBodyBytes = 4 * 1024 * 1024

// ownBodyLimitRoutes are the admin routes, as "METHOD path-template", that
// carry file uploads and enforce a larger limit of their own.
var ownBodyLimitRoutes = map[string]bool{
	http.MethodPost + " /api/v1/artifacts/upload-overlay": true,
	http.MethodPost + " /api/v1/secureboot-keys/import":   true,
}

// noSkip applies the limit to every route of a group.
func noSkip(echo.Context) bool { return false }

// skipOwnBodyLimit reports whether the matched route enforces its own limit.
func skipOwnBodyLimit(c echo.Context) bool {
	return ownBodyLimitRoutes[c.Request().Method+" "+c.Path()]
}

// bodyLimit refuses with 413 a request whose body exceeds limit. It reads the
// body, so it is mounted on each route group after that group's
// authentication: a client without credentials must not make the server read
// or hold anything. A declared
// Content-Length over the limit is refused without reading anything. A body of
// unknown length is read up to one byte past the limit and refused if it gets
// there; otherwise the handler gets the bytes read. Reading it ahead is what
// lets the refusal be a 413 rather than whatever a handler makes of a failed
// read, and it never holds more than limit bytes.
func bodyLimit(limit int64, skip func(echo.Context) bool) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if skip(c) {
				return next(c)
			}
			req := c.Request()
			if req.ContentLength > limit {
				return bodyTooLarge(c)
			}
			if req.ContentLength < 0 && req.Body != nil && req.Body != http.NoBody {
				buf, err := io.ReadAll(io.LimitReader(req.Body, limit+1))
				if err != nil {
					return c.JSON(http.StatusBadRequest, handlers.APIError{Error: "cannot read request body"})
				}
				if int64(len(buf)) > limit {
					return bodyTooLarge(c)
				}
				req.Body = readCloser{Reader: bytes.NewReader(buf), Closer: req.Body}
			}
			return next(c)
		}
	}
}

func bodyTooLarge(c echo.Context) error {
	return c.JSON(http.StatusRequestEntityTooLarge, handlers.APIError{Error: "request body too large"})
}

// readCloser serves buffered bytes while closing the original body.
type readCloser struct {
	io.Reader
	io.Closer
}
