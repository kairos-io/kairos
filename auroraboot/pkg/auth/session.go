package auth

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

// SessionCookieName is the cookie that carries an admin session ID.
const SessionCookieName = "auroraboot_session"

// SessionLifetime is how long an admin session lasts after login. It is
// absolute: using the session does not extend it.
const SessionLifetime = 12 * time.Hour

// SessionStore holds admin sessions in memory. A restart signs everyone out;
// sessions shared across replicas need a database-backed store.
type SessionStore struct {
	mu       sync.Mutex
	sessions map[string]time.Time
	now      func() time.Time
}

// NewSessionStore returns an empty store using the wall clock.
func NewSessionStore() *SessionStore {
	return &SessionStore{sessions: map[string]time.Time{}, now: time.Now}
}

// WithClock replaces the store's clock, for tests.
func (s *SessionStore) WithClock(now func() time.Time) *SessionStore {
	s.now = now
	return s
}

// Create starts a session and returns its ID and expiry. Expired sessions are
// dropped first, so abandoned ones cannot pile up.
func (s *SessionStore) Create() (string, time.Time) {
	buf := make([]byte, 32)
	_, _ = rand.Read(buf) // crypto/rand.Read never returns an error since Go 1.24
	id := base64.RawURLEncoding.EncodeToString(buf)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for k, exp := range s.sessions {
		if !now.Before(exp) {
			delete(s.sessions, k)
		}
	}
	expires := now.Add(SessionLifetime)
	s.sessions[id] = expires
	return id, expires
}

// Valid reports whether id names a live session.
func (s *SessionStore) Valid(id string) bool {
	if id == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.sessions[id]
	if !ok {
		return false
	}
	if !s.now().Before(exp) {
		delete(s.sessions, id)
		return false
	}
	return true
}

// Delete ends a session.
func (s *SessionStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

// requestIsHTTPS reports whether the request arrived over HTTPS. Echo's
// Scheme() method only trusts X-Forwarded-Proto from private peers; we check
// it directly because a spoofed header only causes the spoofer's own cookie
// to be marked Secure, which a browser will simply refuse to send back over
// plain HTTP, making the spoof harmless.
func requestIsHTTPS(c echo.Context) bool {
	if c.Scheme() == "https" {
		return true
	}
	return c.Request().Header.Get("X-Forwarded-Proto") == "https"
}

// SetSessionCookie writes the session cookie. It is Secure when the request
// came over HTTPS, directly or through a proxy that sets X-Forwarded-Proto.
func SetSessionCookie(c echo.Context, id string, expires time.Time) {
	c.SetCookie(&http.Cookie{
		Name:     SessionCookieName,
		Value:    id,
		Path:     "/",
		MaxAge:   int(time.Until(expires).Seconds()),
		HttpOnly: true,
		Secure:   requestIsHTTPS(c),
		SameSite: http.SameSiteStrictMode,
	})
}

// ClearSessionCookie tells the browser to drop the session cookie.
func ClearSessionCookie(c echo.Context) {
	c.SetCookie(&http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   requestIsHTTPS(c),
		SameSite: http.SameSiteStrictMode,
	})
}
