package auth_test

import (
	"net/http"
	"net/http/httptest"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/auth"
	"github.com/labstack/echo/v4"
)

var _ = Describe("Admin authentication", func() {
	const password = "secret-password"

	var (
		now      time.Time
		sessions *auth.SessionStore
		admin    *auth.AdminAuth
	)

	BeforeEach(func() {
		now = time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
		sessions = auth.NewSessionStore().WithClock(func() time.Time { return now })
		admin = auth.NewAdminAuth(password, sessions, "https://aurora.example.com")
	})

	middlewares := map[string]func() echo.MiddlewareFunc{
		"AdminMiddleware": func() echo.MiddlewareFunc { return auth.AdminMiddleware(admin) },
		"AgentOrAdminMiddleware": func() echo.MiddlewareFunc {
			return auth.AgentOrAdminMiddleware(admin, &fakeNodeStore{})
		},
		"ArtifactImageMiddleware": func() echo.MiddlewareFunc {
			return auth.ArtifactImageMiddleware(admin, &fakeNodeStore{}, &fakeCommandStore{})
		},
		"ExtensionDownloadMiddleware": func() echo.MiddlewareFunc {
			return auth.ExtensionDownloadMiddleware(admin, &fakeNodeStore{}, nil)
		},
	}

	serve := func(mw echo.MiddlewareFunc, req *http.Request) int {
		e := echo.New()
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues("art-1")
		Expect(mw(func(c echo.Context) error { return c.NoContent(http.StatusOK) })(c)).To(Succeed())
		return rec.Code
	}

	withCookie := func(req *http.Request, id string) *http.Request {
		req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: id})
		return req
	}

	for name, build := range middlewares {
		Context(name, func() {
			It("accepts the password as a Bearer header", func() {
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				req.Header.Set("Authorization", "Bearer "+password)
				Expect(serve(build(), req)).To(Equal(http.StatusOK))
			})

			It("refuses the password in ?token=", func() {
				req := httptest.NewRequest(http.MethodGet, "/?token="+password, nil)
				Expect(serve(build(), req)).To(Equal(http.StatusUnauthorized))
			})

			It("accepts a live session cookie on a GET", func() {
				id, _ := sessions.Create()
				Expect(serve(build(), withCookie(httptest.NewRequest(http.MethodGet, "/", nil), id))).To(Equal(http.StatusOK))
			})

			It("refuses an expired session cookie", func() {
				id, _ := sessions.Create()
				now = now.Add(auth.SessionLifetime)
				Expect(serve(build(), withCookie(httptest.NewRequest(http.MethodGet, "/", nil), id))).To(Equal(http.StatusUnauthorized))
			})

			It("refuses an unknown session cookie", func() {
				Expect(serve(build(), withCookie(httptest.NewRequest(http.MethodGet, "/", nil), "forged"))).To(Equal(http.StatusUnauthorized))
			})

			It("refuses a cookie mutation from a foreign origin with 403", func() {
				id, _ := sessions.Create()
				req := withCookie(httptest.NewRequest(http.MethodPost, "/", nil), id)
				req.Header.Set("Origin", "https://evil.example")
				Expect(serve(build(), req)).To(Equal(http.StatusForbidden))
			})

			It("accepts a cookie mutation from the public URL's origin", func() {
				id, _ := sessions.Create()
				req := withCookie(httptest.NewRequest(http.MethodPost, "/", nil), id)
				req.Header.Set("Origin", "https://aurora.example.com")
				Expect(serve(build(), req)).To(Equal(http.StatusOK))
			})

			It("does not check the origin of a Bearer mutation", func() {
				req := httptest.NewRequest(http.MethodPost, "/", nil)
				req.Header.Set("Authorization", "Bearer "+password)
				req.Header.Set("Origin", "https://evil.example")
				Expect(serve(build(), req)).To(Equal(http.StatusOK))
			})
		})
	}

	It("matches the password in constant time and refuses an empty one", func() {
		Expect(admin.PasswordMatches(password)).To(BeTrue())
		Expect(admin.PasswordMatches("wrong")).To(BeFalse())
		Expect(admin.PasswordMatches("")).To(BeFalse())
	})
})
