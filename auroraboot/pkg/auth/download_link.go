package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
)

// DownloadLinkLifetime is how long a signed download link works.
const DownloadLinkLifetime = time.Hour

// LinkSigner signs download links for one artifact file. The key is random
// per process, so a restart invalidates every link; links are short-lived, so
// that is acceptable, and it means no long-lived secret exists to leak.
type LinkSigner struct {
	key []byte
	now func() time.Time
}

// NewLinkSigner returns a signer with a fresh random key.
func NewLinkSigner() *LinkSigner {
	key := make([]byte, 32)
	_, _ = rand.Read(key) // crypto/rand.Read never returns an error since Go 1.24
	return &LinkSigner{key: key, now: time.Now}
}

// WithClock replaces the signer's clock, for tests.
func (s *LinkSigner) WithClock(now func() time.Time) *LinkSigner {
	s.now = now
	return s
}

// Sign returns the exp and sig query values for a link to filename of
// artifact id, and when the link expires.
func (s *LinkSigner) Sign(id, filename string) (url.Values, time.Time) {
	expires := s.now().Add(DownloadLinkLifetime).Truncate(time.Second)
	exp := strconv.FormatInt(expires.Unix(), 10)
	q := url.Values{}
	q.Set("exp", exp)
	q.Set("sig", base64.RawURLEncoding.EncodeToString(s.mac(id, filename, exp)))
	return q, expires
}

// Verify reports whether q carries a signature for filename of artifact id
// that has not expired.
func (s *LinkSigner) Verify(id, filename string, q url.Values) bool {
	exp, sig := q.Get("exp"), q.Get("sig")
	if exp == "" || sig == "" {
		return false
	}
	unix, err := strconv.ParseInt(exp, 10, 64)
	if err != nil || !s.now().Before(time.Unix(unix, 0)) {
		return false
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return false
	}
	return hmac.Equal(got, s.mac(id, filename, exp))
}

func (s *LinkSigner) mac(id, filename, exp string) []byte {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte("download\n" + id + "\n" + filename + "\n" + exp))
	return m.Sum(nil)
}

// DownloadMiddleware authorizes an artifact file download for an admin
// (AdminAuth) or for a signed link to exactly that file of that artifact.
func DownloadMiddleware(a *AdminAuth, s *LinkSigner) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			switch a.check(c) {
			case isAdmin:
				return next(c)
			case foreignOrigin:
				return forbidden(c)
			}
			if s != nil && s.Verify(c.Param("id"), c.Param("*"), c.QueryParams()) {
				return next(c)
			}
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		}
	}
}
