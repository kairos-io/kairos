package server

import (
	"net/http"
	"net/url"

	"github.com/labstack/echo/v4"
)

// unescapePathParams decodes the route's path parameters before the auth
// middleware and the handler read them.
//
// Echo v4 matches routes against the raw path when the request has one, so
// c.Param returns the value as the client escaped it. Go keeps a raw path only
// when the client escaped the path differently from Go's default encoding. The web UI builds file
// download URLs with encodeURIComponent, which turns the "+" of a name such as
// kairos-...-k3sv1.37.2+k3s1.iso into "%2B". Without this, the handler looks
// for a file literally named "...%2Bk3s1.iso" and the signed link check sees a
// different name than the one that was signed, so the download is a 404.
func unescapePathParams(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		// Echo routes on RawPath only when Go kept one; otherwise the params are
		// already decoded and decoding again would corrupt a literal "%".
		if c.Request().URL.RawPath == "" {
			return next(c)
		}
		values := c.ParamValues()
		decoded := make([]string, len(values))
		for i, v := range values {
			d, err := url.PathUnescape(v)
			if err != nil {
				return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid path escape"})
			}
			decoded[i] = d
		}
		c.SetParamValues(decoded...)
		return next(c)
	}
}
