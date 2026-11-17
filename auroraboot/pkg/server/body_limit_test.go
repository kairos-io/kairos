package server_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/builder"
	"github.com/kairos-io/AuroraBoot/pkg/server"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
)

const mib = 1024 * 1024

// countingReader yields up to size zero bytes and records how many the server
// pulled, so a spec can tell whether a request was refused before its body was
// read.
type countingReader struct {
	size int64
	read int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	if r.read >= r.size {
		return 0, io.EOF
	}
	n := int64(len(p))
	if left := r.size - r.read; n > left {
		n = left
	}
	clear(p[:n])
	r.read += n
	return int(n), nil
}

var _ = Describe("Request body limits", func() {
	var (
		app          http.Handler
		artifactsDir string
	)

	BeforeEach(func() {
		artifactsDir = GinkgoT().TempDir()
		app = server.New(server.Config{
			NodeStore:     &fakeNodeStore{},
			CommandStore:  &fakeCommandStore{},
			GroupStore:    &fakeGroupStore{},
			ArtifactStore: &fakeArtifactStore{},
			Builder:       &fakeBuilder{},
			AdminPassword: "admin-pass",
			RegToken:      "reg-token",
			AuroraBootURL: "http://localhost:8080",
			ArtifactsDir:  artifactsDir,
			KeysDir:       GinkgoT().TempDir(),
		})
	})

	// serve sends a request straight to the handler. contentLength is what the
	// request declares; -1 makes it a body of unknown length, as a chunked
	// upload is.
	serve := func(method, path string, body io.Reader, contentLength int64, contentType string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, body)
		req.ContentLength = contentLength
		req.Header.Set("Authorization", "Bearer admin-pass")
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		return rec
	}

	expectTooLarge := func(rec *httptest.ResponseRecorder) {
		GinkgoHelper()
		Expect(rec.Code).To(Equal(http.StatusRequestEntityTooLarge))
		var body map[string]any
		Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed(), rec.Body.String())
		Expect(body).To(HaveKeyWithValue("error", Not(BeEmpty())))
	}

	multipartBody := func(field, filename string, size int) (*bytes.Buffer, string) {
		GinkgoHelper()
		buf := &bytes.Buffer{}
		w := multipart.NewWriter(buf)
		part, err := w.CreateFormFile(field, filename)
		Expect(err).NotTo(HaveOccurred())
		_, err = part.Write(bytes.Repeat([]byte("a"), size))
		Expect(err).NotTo(HaveOccurred())
		Expect(w.Close()).To(Succeed())
		return buf, w.FormDataContentType()
	}

	Describe("the global limit", func() {
		It("refuses a declared Content-Length over 4 MiB without reading the body", func() {
			body := &countingReader{size: 4*mib + 1}
			rec := serve(http.MethodPost, "/api/v1/groups", body, body.size, "application/json")
			expectTooLarge(rec)
			Expect(body.read).To(BeZero())
		})

		It("refuses a body of unknown length once it passes 4 MiB", func() {
			body := &countingReader{size: 5 * mib}
			rec := serve(http.MethodPost, "/api/v1/groups", body, -1, "application/json")
			expectTooLarge(rec)
			Expect(body.read).To(BeNumerically("<=", 4*mib+64*1024))
		})

		It("accepts a JSON body under 4 MiB", func() {
			payload := `{"name":"big","description":"` + strings.Repeat("d", 3*mib) + `"}`
			rec := serve(http.MethodPost, "/api/v1/groups", strings.NewReader(payload), int64(len(payload)), "application/json")
			Expect(rec.Code).To(Equal(http.StatusCreated), rec.Body.String())
		})

		It("accepts a body of unknown length under 4 MiB", func() {
			payload := `{"name":"chunked","description":"` + strings.Repeat("d", 3*mib) + `"}`
			rec := serve(http.MethodPost, "/api/v1/groups", strings.NewReader(payload), -1, "application/json")
			Expect(rec.Code).To(Equal(http.StatusCreated), rec.Body.String())
		})
	})

	Describe("the node registration", func() {
		It("reads no more than 64 KiB of a body of unknown length", func() {
			body := &countingReader{size: 1 * mib}
			rec := serve(http.MethodPost, "/api/v1/nodes/register", body, -1, "application/json")
			expectTooLarge(rec)
			Expect(body.read).To(BeNumerically("<=", 64*1024+1))
		})
	})

	Describe("the overlay upload", func() {
		It("accepts more than 4 MiB", func() {
			body, ct := multipartBody("files", "big.bin", 5*mib)
			rec := serve(http.MethodPost, "/api/v1/artifacts/upload-overlay", body, int64(body.Len()), ct)
			Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		})

		It("refuses a declared Content-Length over 512 MiB without reading the body", func() {
			body := &countingReader{size: 512*mib + 1}
			rec := serve(http.MethodPost, "/api/v1/artifacts/upload-overlay", body, body.size, "multipart/form-data; boundary=x")
			expectTooLarge(rec)
			Expect(body.read).To(BeZero())
		})
	})

	Describe("the SecureBoot key import", func() {
		It("accepts more than 4 MiB", func() {
			body, ct := multipartBody("file", "keys.tar.gz", 5*mib)
			rec := serve(http.MethodPost, "/api/v1/secureboot-keys/import", body, int64(body.Len()), ct)
			Expect(rec.Code).NotTo(Equal(http.StatusRequestEntityTooLarge), rec.Body.String())
			Expect(rec.Code).To(Equal(http.StatusBadRequest), rec.Body.String())
		})

		It("refuses a declared Content-Length over 16 MiB without reading the body", func() {
			body := &countingReader{size: 16*mib + 1}
			rec := serve(http.MethodPost, "/api/v1/secureboot-keys/import", body, body.size, "multipart/form-data; boundary=x")
			expectTooLarge(rec)
			Expect(body.read).To(BeZero())
		})

		It("refuses a body of unknown length once it passes 16 MiB", func() {
			body, ct := multipartBody("file", "keys.tar.gz", 17*mib)
			rec := serve(http.MethodPost, "/api/v1/secureboot-keys/import", body, -1, ct)
			expectTooLarge(rec)
		})
	})

	Describe("the exporter upload", func() {
		It("refuses a declared Content-Length over 20 GiB before writing anything", func() {
			body := &countingReader{size: 20*1024*mib + 1}
			rec := serve(http.MethodPut, "/api/v1/artifacts/art-1/upload/kairos.iso", body, body.size, "application/octet-stream")
			expectTooLarge(rec)
			Expect(body.read).To(BeZero())
			entries, err := os.ReadDir(artifactsDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).To(BeEmpty())
		})
	})
})

// unusedExtensionBuilder and unusedDeploymentStore only make server.New mount
// the extension and deploy routes. The specs that use them never get past
// authentication or the body limit, so their methods are never called.
type unusedExtensionBuilder struct{ builder.ExtensionBuilder }

type unusedDeploymentStore struct{ store.DeploymentStore }

// Every route the server mounts, walked from the router itself, so a route
// added later is covered without touching these specs.
var _ = Describe("Request body limits on every route", func() {
	const (
		nodeID  = "node-1"
		nodeKey = "node-key"
	)

	// Routes with a limit of their own, covered by their own specs above.
	ownLimit := map[string]bool{
		http.MethodPost + " /api/v1/nodes/register":           true,
		http.MethodPost + " /api/v1/session":                  true,
		http.MethodPost + " /api/v1/artifacts/upload-overlay": true,
		http.MethodPost + " /api/v1/secureboot-keys/import":   true,
		http.MethodPut + " /api/v1/artifacts/:id/upload/*":    true,
	}

	var e *echo.Echo

	BeforeEach(func() {
		e = server.New(server.Config{
			NodeStore:        &fakeNodeStore{nodes: []*store.ManagedNode{{ID: nodeID, APIKey: nodeKey}}},
			CommandStore:     &fakeCommandStore{},
			GroupStore:       &fakeGroupStore{},
			ArtifactStore:    &fakeArtifactStore{},
			Builder:          &fakeBuilder{},
			ExtensionBuilder: unusedExtensionBuilder{},
			DeploymentStore:  unusedDeploymentStore{},
			AdminPassword:    "admin-pass",
			RegToken:         "reg-token",
			AuroraBootURL:    "http://localhost:8080",
			ArtifactsDir:     GinkgoT().TempDir(),
			KeysDir:          GinkgoT().TempDir(),
		})
	})

	// bodyRoutes lists the mounted routes that take a request body.
	bodyRoutes := func() []*echo.Route {
		var routes []*echo.Route
		for _, r := range e.Routes() {
			switch r.Method {
			case http.MethodPost, http.MethodPut, http.MethodPatch:
				routes = append(routes, r)
			}
		}
		Expect(routes).NotTo(BeEmpty())
		return routes
	}

	// concretePath fills a route template's parameters with values that pass
	// the node identity check.
	concretePath := func(template string) string {
		parts := strings.Split(template, "/")
		for i, p := range parts {
			switch {
			case p == ":nodeID":
				parts[i] = nodeID
			case strings.HasPrefix(p, ":"):
				parts[i] = "x"
			case p == "*":
				parts[i] = "file"
			}
		}
		return strings.Join(parts, "/")
	}

	// send posts 5 MiB of unknown length to the route with the given bearer
	// token, and returns the response and how much of the body was read.
	send := func(r *echo.Route, bearer string) (*httptest.ResponseRecorder, int64) {
		body := &countingReader{size: 5 * mib}
		req := httptest.NewRequest(r.Method, concretePath(r.Path), body)
		req.ContentLength = -1
		req.Header.Set("Content-Type", "application/json")
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec, body.read
	}

	It("reads no body before authenticating, apart from the capped registration and login reads", func() {
		for _, r := range bodyRoutes() {
			route := r.Method + " " + r.Path
			rec, read := send(r, "")
			Expect(rec.Code).To(BeNumerically(">=", 400), route)
			switch route {
			case http.MethodPost + " /api/v1/nodes/register":
				Expect(read).To(BeNumerically("<=", 64*1024+1), route)
				continue
			case http.MethodPost + " /api/v1/session":
				Expect(read).To(BeNumerically("<=", 4*1024+1), route)
				continue
			}
			Expect(read).To(BeZero(), route)
		}
	})

	It("reads no body of a request to a path with no route", func() {
		body := &countingReader{size: 5 * mib}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/no-such-route", body)
		req.ContentLength = -1
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		Expect(body.read).To(BeZero())
	})

	It("refuses an oversized body of unknown length with 413 once authenticated", func() {
		for _, r := range bodyRoutes() {
			route := r.Method + " " + r.Path
			if ownLimit[route] {
				continue
			}
			// Admin routes take the admin password and agent routes the node
			// key; a route that refuses one is retried with the other.
			rec, _ := send(r, "admin-pass")
			if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
				rec, _ = send(r, nodeKey)
			}
			Expect(rec.Code).To(Equal(http.StatusRequestEntityTooLarge), route+": "+rec.Body.String())
		}
	})
})
