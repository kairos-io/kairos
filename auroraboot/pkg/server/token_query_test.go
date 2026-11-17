package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/server"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
)

// The admin password in a query string is no credential on any route, walked
// from the router so a route added later is covered too. Each request carries
// no other credential, so every guarded route must answer 401 before its
// handler runs: a handler's own 400 or 404 would hide a query password that
// authenticated.
var _ = Describe("Admin password in the query string", func() {
	It("is refused on every route", func() {
		e := server.New(server.Config{
			NodeStore:        &fakeNodeStore{nodes: []*store.ManagedNode{{ID: "node-1", APIKey: "node-key"}}},
			CommandStore:     &fakeCommandStore{},
			GroupStore:       &fakeGroupStore{},
			ArtifactStore:    &anyArtifactStore{},
			Builder:          &fakeBuilder{},
			ExtensionBuilder: unusedExtensionBuilder{},
			DeploymentStore:  unusedDeploymentStore{},
			AdminPassword:    "admin-pass",
			RegToken:         "reg-token",
			AuroraBootURL:    "http://localhost:8080",
			ArtifactsDir:     GinkgoT().TempDir(),
			KeysDir:          GinkgoT().TempDir(),
		})

		public := map[string]bool{
			"GET /healthz": true, "GET /readyz": true,
			"GET /api/v1/openapi.json": true, "GET /api/v1/openapi.yaml": true,
			"GET /api/docs": true, "GET /api/v1/install-agent": true,
			// The public credential exchange: login takes the password in its body, logout clears any cookie.
			"POST /api/v1/session": true, "DELETE /api/v1/session": true,
			// The static UI and node registration, which takes the registration token in its body.
			"GET /assets/*": true, "POST /api/v1/nodes/register": true,
		}
		for _, r := range e.Routes() {
			route := r.Method + " " + r.Path
			if public[route] || r.Method == echo.RouteNotFound || r.Path == "/*" {
				continue
			}
			req := httptest.NewRequest(r.Method, concreteRoutePath(r.Path)+"?token=admin-pass", nil)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			Expect(rec.Code).To(Equal(http.StatusUnauthorized), route)
		}
	})
})

// anyArtifactStore finds an artifact for every ID, so the upload route, which
// checks its per-build token after looking the artifact up, reaches that check.
type anyArtifactStore struct{ fakeArtifactStore }

func (*anyArtifactStore) GetByID(_ context.Context, id string) (*store.ArtifactRecord, error) {
	return &store.ArtifactRecord{ID: id, UploadToken: "digest-of-another-token"}, nil
}

// concreteRoutePath fills a route template's parameters.
func concreteRoutePath(template string) string {
	out := []byte{}
	for i := 0; i < len(template); i++ {
		switch template[i] {
		case ':':
			for i < len(template) && template[i] != '/' {
				i++
			}
			out = append(out, 'x')
			if i < len(template) {
				out = append(out, '/')
			}
		case '*':
			out = append(out, "file"...)
		default:
			out = append(out, template[i])
		}
	}
	return string(out)
}
