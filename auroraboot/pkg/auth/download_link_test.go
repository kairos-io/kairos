package auth_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/auth"
	"github.com/labstack/echo/v4"
)

var _ = Describe("Signed download links", func() {
	var (
		now    time.Time
		signer *auth.LinkSigner
	)

	BeforeEach(func() {
		now = time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
		signer = auth.NewLinkSigner().WithClock(func() time.Time { return now })
	})

	It("signs a link that works for one hour, for that file only", func() {
		q, expires := signer.Sign("art-1", "kairos.iso")
		Expect(expires).To(Equal(now.Add(time.Hour)))
		Expect(q.Get("exp")).To(Equal(strconv.FormatInt(expires.Unix(), 10)))
		Expect(q.Get("sig")).NotTo(BeEmpty())

		Expect(signer.Verify("art-1", "kairos.iso", q)).To(BeTrue())
		Expect(signer.Verify("art-1", "kairos.sha256", q)).To(BeFalse())
		Expect(signer.Verify("art-2", "kairos.iso", q)).To(BeFalse())

		now = now.Add(time.Hour)
		Expect(signer.Verify("art-1", "kairos.iso", q)).To(BeFalse())
	})

	It("refuses an edited expiry, a tampered signature and missing fields", func() {
		q, _ := signer.Sign("art-1", "kairos.iso")

		later := url.Values{"exp": {strconv.FormatInt(now.Add(48*time.Hour).Unix(), 10)}, "sig": {q.Get("sig")}}
		Expect(signer.Verify("art-1", "kairos.iso", later)).To(BeFalse())

		tampered := url.Values{"exp": {q.Get("exp")}, "sig": {q.Get("sig") + "x"}}
		Expect(signer.Verify("art-1", "kairos.iso", tampered)).To(BeFalse())

		Expect(signer.Verify("art-1", "kairos.iso", url.Values{"sig": {q.Get("sig")}})).To(BeFalse())
		Expect(signer.Verify("art-1", "kairos.iso", url.Values{"exp": {q.Get("exp")}})).To(BeFalse())
		Expect(signer.Verify("art-1", "kairos.iso", url.Values{"exp": {"soon"}, "sig": {q.Get("sig")}})).To(BeFalse())
	})

	It("is not valid under another server's key", func() {
		q, _ := signer.Sign("art-1", "kairos.iso")
		Expect(auth.NewLinkSigner().Verify("art-1", "kairos.iso", q)).To(BeFalse())
	})

	Describe("DownloadMiddleware", func() {
		serve := func(target string, mw echo.MiddlewareFunc) int {
			e := echo.New()
			e.GET("/api/v1/artifacts/:id/download/*", func(c echo.Context) error { return c.NoContent(http.StatusOK) }, mw)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
			return rec.Code
		}

		It("lets a signed link through and nothing else without credentials", func() {
			admin := auth.NewAdminAuth("admin-pass", auth.NewSessionStore(), "")
			mw := auth.DownloadMiddleware(admin, signer)
			q, _ := signer.Sign("art-1", "kairos.iso")

			Expect(serve("/api/v1/artifacts/art-1/download/kairos.iso?"+q.Encode(), mw)).To(Equal(http.StatusOK))
			Expect(serve("/api/v1/artifacts/art-1/download/kairos.sha256?"+q.Encode(), mw)).To(Equal(http.StatusUnauthorized))
			Expect(serve("/api/v1/artifacts/art-1/download/kairos.iso", mw)).To(Equal(http.StatusUnauthorized))
			Expect(serve("/api/v1/artifacts/art-1/download/kairos.iso?token=admin-pass", mw)).To(Equal(http.StatusUnauthorized))
		})
	})
})
