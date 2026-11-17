package handlers

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
)

// maxOverlayRequestBytes caps an overlay upload request: it matches the
// extraction cap, since a larger upload could not be stored anyway.
const maxOverlayRequestBytes = maxOverlaySize

// maxImportRequestBytes caps a key set import request: it matches the
// extraction cap, since a real key set is a few tens of KiB.
const maxImportRequestBytes = maxImportSize

// limitBody caps the request body at limit. A declared Content-Length over
// the limit is refused with 413 before anything is read, and limitBody
// reports false after writing that response. Otherwise the body is wrapped so
// a read past the limit fails with an error isBodyTooLarge recognises.
func limitBody(c echo.Context, limit int64) (bool, error) {
	req := c.Request()
	if req.ContentLength > limit {
		return false, bodyTooLarge(c)
	}
	req.Body = http.MaxBytesReader(c.Response().Writer, req.Body, limit)
	return true, nil
}

// isBodyTooLarge reports whether err comes from reading past a limitBody cap.
func isBodyTooLarge(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}

func bodyTooLarge(c echo.Context) error {
	return c.JSON(http.StatusRequestEntityTooLarge, APIError{Error: "request body too large"})
}
