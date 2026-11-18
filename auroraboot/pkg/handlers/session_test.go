package handlers_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/auth"
	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/labstack/echo/v4"
)

// countingBody yields size bytes of "a" and records how many were read.
type countingBody struct{ size, read int64 }

func (b *countingBody) Read(p []byte) (int, error) {
	if b.read >= b.size {
		return 0, io.EOF
	}
	n := int64(len(p))
	if left := b.size - b.read; n > left {
		n = left
	}
	for i := range p[:n] {
		p[i] = 'a'
	}
	b.read += n
	return int(n), nil
}

var _ = Describe("SessionHandler", func() {
	var (
		e        *echo.Echo
		sessions *auth.SessionStore
	)

	BeforeEach(func() {
		sessions = auth.NewSessionStore()
		admin := auth.NewAdminAuth("admin-pass", sessions, "https://aurora.example.com")
		h := handlers.NewSessionHandler(admin)
		e = echo.New()
		e.POST("/api/v1/session", h.Login)
		e.DELETE("/api/v1/session", h.Logout)
		g := e.Group("/api/v1", auth.AdminMiddleware(admin))
		g.GET("/session", h.Check)
	})

	do := func(req *http.Request) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}
	login := func(body string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/session", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		return req
	}
	sessionCookie := func(rec *httptest.ResponseRecorder) *http.Cookie {
		for _, c := range (&http.Response{Header: rec.Header()}).Cookies() {
			if c.Name == auth.SessionCookieName {
				return c
			}
		}
		return nil
	}

	It("logs in with the admin password and sets a session cookie", func() {
		rec := do(login(`{"password":"admin-pass"}`))
		Expect(rec.Code).To(Equal(http.StatusNoContent))
		ck := sessionCookie(rec)
		Expect(ck).NotTo(BeNil())
		Expect(ck.HttpOnly).To(BeTrue())
		Expect(sessions.Valid(ck.Value)).To(BeTrue())
	})

	It("refuses a wrong password without a cookie", func() {
		rec := do(login(`{"password":"nope"}`))
		Expect(rec.Code).To(Equal(http.StatusUnauthorized))
		Expect(sessionCookie(rec)).To(BeNil())
	})

	It("refuses a body that is not JSON", func() {
		Expect(do(login(`password=admin-pass`)).Code).To(Equal(http.StatusBadRequest))
	})

	It("refuses a login from a foreign origin", func() {
		req := login(`{"password":"admin-pass"}`)
		req.Header.Set("Origin", "https://evil.example")
		rec := do(req)
		Expect(rec.Code).To(Equal(http.StatusForbidden))
		Expect(sessionCookie(rec)).To(BeNil())
	})

	It("refuses a declared body over 4 KiB without reading it", func() {
		body := &countingBody{size: 4097}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/session", body)
		req.ContentLength = body.size
		Expect(do(req).Code).To(Equal(http.StatusRequestEntityTooLarge))
		Expect(body.read).To(BeZero())
	})

	It("reads at most 4 KiB and one byte of a body of unknown length", func() {
		body := &countingBody{size: 1 << 20}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/session", body)
		req.ContentLength = -1
		Expect(do(req).Code).To(Equal(http.StatusRequestEntityTooLarge))
		Expect(body.read).To(BeNumerically("<=", 4097))
	})

	It("reports a live session as signed in and a missing one as not", func() {
		ck := sessionCookie(do(login(`{"password":"admin-pass"}`)))
		req := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
		req.AddCookie(ck)
		Expect(do(req).Code).To(Equal(http.StatusNoContent))
		Expect(do(httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)).Code).To(Equal(http.StatusUnauthorized))
	})

	It("logs out: the session stops working and the cookie is cleared", func() {
		ck := sessionCookie(do(login(`{"password":"admin-pass"}`)))
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/session", nil)
		req.AddCookie(ck)
		rec := do(req)
		Expect(rec.Code).To(Equal(http.StatusNoContent))
		Expect(sessionCookie(rec).MaxAge).To(Equal(-1))
		Expect(sessions.Valid(ck.Value)).To(BeFalse())
	})

	It("answers a logout without a session with 204", func() {
		Expect(do(httptest.NewRequest(http.MethodDelete, "/api/v1/session", nil)).Code).To(Equal(http.StatusNoContent))
	})

	It("refuses a logout from a foreign origin", func() {
		ck := sessionCookie(do(login(`{"password":"admin-pass"}`)))
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/session", nil)
		req.AddCookie(ck)
		req.Header.Set("Origin", "https://evil.example")
		Expect(do(req).Code).To(Equal(http.StatusForbidden))
		Expect(sessions.Valid(ck.Value)).To(BeTrue())
	})
})
