package redfish_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/redfish"
)

var _ = Describe("redfish.Reachable", func() {
	It("returns nil for a 2xx ServiceRoot it can parse", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.URL.Path).To(Equal("/redfish/v1/"))
			Expect(r.Method).To(Equal(http.MethodGet))
			// No credentials: a session-free reachability probe never authenticates.
			Expect(r.Header.Get("Authorization")).To(BeEmpty())
			Expect(r.Header.Get("X-Auth-Token")).To(BeEmpty())
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"@odata.id":"/redfish/v1/","SessionService":{"@odata.id":"/redfish/v1/SessionService"}}`))
		}))
		defer srv.Close()

		Expect(redfish.Reachable(context.Background(), srv.URL, false)).To(Succeed())
	})

	It("does not create a Redfish session (single GET, no POST)", func() {
		var posts int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				posts++
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"@odata.id":"/redfish/v1/"}`))
		}))
		defer srv.Close()

		Expect(redfish.Reachable(context.Background(), srv.URL, false)).To(Succeed())
		Expect(posts).To(Equal(0), "Reachable must not POST a session-create")
	})

	It("errors on a non-2xx ServiceRoot (e.g. 401 on a hardened BMC)", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer srv.Close()

		err := redfish.Reachable(context.Background(), srv.URL, false)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("status 401"))
	})

	It("errors on a refused/closed endpoint", func() {
		// Stand a server up then immediately close it so the address refuses.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		url := srv.URL
		srv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		Expect(redfish.Reachable(ctx, url, false)).To(HaveOccurred())
	})

	It("errors on an unparseable body", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("not json"))
		}))
		defer srv.Close()

		Expect(redfish.Reachable(context.Background(), srv.URL, false)).To(HaveOccurred())
	})

	It("rejects an empty endpoint", func() {
		Expect(redfish.Reachable(context.Background(), "", false)).To(HaveOccurred())
	})
})
