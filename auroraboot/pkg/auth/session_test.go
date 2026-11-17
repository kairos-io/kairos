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

var _ = Describe("SessionStore", func() {
	var (
		now      time.Time
		sessions *auth.SessionStore
	)

	BeforeEach(func() {
		now = time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
		sessions = auth.NewSessionStore().WithClock(func() time.Time { return now })
	})

	It("creates distinct random IDs that are valid until 12 hours after creation", func() {
		id, expires := sessions.Create()
		other, _ := sessions.Create()
		Expect(id).To(HaveLen(43)) // 32 bytes, unpadded base64url
		Expect(other).NotTo(Equal(id))
		Expect(expires).To(Equal(now.Add(12 * time.Hour)))

		now = now.Add(12*time.Hour - time.Second)
		Expect(sessions.Valid(id)).To(BeTrue())
		now = now.Add(time.Second)
		Expect(sessions.Valid(id)).To(BeFalse())
	})

	It("refuses unknown, empty and deleted IDs", func() {
		id, _ := sessions.Create()
		Expect(sessions.Valid("unknown")).To(BeFalse())
		Expect(sessions.Valid("")).To(BeFalse())
		sessions.Delete(id)
		Expect(sessions.Valid(id)).To(BeFalse())
	})

	It("drops expired sessions when a new one is created", func() {
		sessions.Create()
		sessions.Create()
		now = now.Add(13 * time.Hour)
		sessions.Create()
		Expect(auth.SessionCount(sessions)).To(Equal(1))
	})
})

var _ = Describe("Session cookie", func() {
	cookieFrom := func(rec *httptest.ResponseRecorder) *http.Cookie {
		GinkgoHelper()
		cookies := (&http.Response{Header: rec.Header()}).Cookies()
		Expect(cookies).To(HaveLen(1))
		return cookies[0]
	}

	It("is HttpOnly, SameSite=Strict, scoped to / and expires with the session", func() {
		e := echo.New()
		rec := httptest.NewRecorder()
		c := e.NewContext(httptest.NewRequest(http.MethodPost, "/api/v1/session", nil), rec)
		expires := time.Now().Add(auth.SessionLifetime).Truncate(time.Second)

		auth.SetSessionCookie(c, "abc", expires)

		ck := cookieFrom(rec)
		Expect(ck.Name).To(Equal("auroraboot_session"))
		Expect(ck.Value).To(Equal("abc"))
		Expect(ck.Path).To(Equal("/"))
		Expect(ck.HttpOnly).To(BeTrue())
		Expect(ck.SameSite).To(Equal(http.SameSiteStrictMode))
		Expect(ck.Secure).To(BeFalse())
		Expect(ck.MaxAge).To(BeNumerically("~", int(auth.SessionLifetime.Seconds()), 2))
	})

	It("is Secure when the request arrived over HTTPS through a proxy", func() {
		e := echo.New()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/session", nil)
		req.Header.Set("X-Forwarded-Proto", "https")
		rec := httptest.NewRecorder()
		auth.SetSessionCookie(e.NewContext(req, rec), "abc", time.Now().Add(time.Hour))
		Expect(cookieFrom(rec).Secure).To(BeTrue())
	})

	It("is cleared with an expired, empty cookie", func() {
		e := echo.New()
		rec := httptest.NewRecorder()
		auth.ClearSessionCookie(e.NewContext(httptest.NewRequest(http.MethodDelete, "/api/v1/session", nil), rec))
		ck := cookieFrom(rec)
		Expect(ck.Name).To(Equal("auroraboot_session"))
		Expect(ck.Value).To(BeEmpty())
		Expect(ck.MaxAge).To(Equal(-1)) // net/http parses Max-Age=0 as -1
	})

	It("clears with Secure, HttpOnly and SameSite=Strict when X-Forwarded-Proto indicates HTTPS", func() {
		e := echo.New()
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/session", nil)
		req.Header.Set("X-Forwarded-Proto", "https")
		rec := httptest.NewRecorder()
		auth.ClearSessionCookie(e.NewContext(req, rec))
		ck := cookieFrom(rec)
		Expect(ck.Name).To(Equal("auroraboot_session"))
		Expect(ck.Value).To(BeEmpty())
		Expect(ck.MaxAge).To(Equal(-1))
		Expect(ck.HttpOnly).To(BeTrue())
		Expect(ck.SameSite).To(Equal(http.SameSiteStrictMode))
		Expect(ck.Secure).To(BeTrue())
	})
})
