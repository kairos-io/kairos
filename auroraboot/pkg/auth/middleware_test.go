package auth_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/auth"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
)

// fakeNodeStore implements store.NodeStore for testing.
type fakeNodeStore struct {
	nodes []*store.ManagedNode
}

func (f *fakeNodeStore) Register(_ context.Context, n *store.ManagedNode) error {
	f.nodes = append(f.nodes, n)
	return nil
}
func (f *fakeNodeStore) GetByID(_ context.Context, id string) (*store.ManagedNode, error) {
	for _, n := range f.nodes {
		if n.ID == id {
			return n, nil
		}
	}
	return nil, fmt.Errorf("not found")
}
func (f *fakeNodeStore) GetByMachineID(_ context.Context, mid string) (*store.ManagedNode, error) {
	for _, n := range f.nodes {
		if n.MachineID == mid {
			return n, nil
		}
	}
	return nil, fmt.Errorf("not found")
}
func (f *fakeNodeStore) GetByAPIKey(_ context.Context, key string) (*store.ManagedNode, error) {
	for _, n := range f.nodes {
		if n.APIKey == key {
			return n, nil
		}
	}
	return nil, fmt.Errorf("not found")
}
func (f *fakeNodeStore) List(_ context.Context) ([]*store.ManagedNode, error) {
	return f.nodes, nil
}
func (f *fakeNodeStore) ListByGroup(_ context.Context, _ string) ([]*store.ManagedNode, error) {
	return nil, nil
}
func (f *fakeNodeStore) ListByLabels(_ context.Context, _ map[string]string) ([]*store.ManagedNode, error) {
	return nil, nil
}
func (f *fakeNodeStore) ListBySelector(_ context.Context, _ store.CommandSelector) ([]*store.ManagedNode, error) {
	return nil, nil
}
func (f *fakeNodeStore) UpdateHeartbeat(_ context.Context, _ string, _ string, _ map[string]string, _ []store.NodeAddress, _ string, _ string, _ string) error {
	return nil
}
func (f *fakeNodeStore) UpdatePhase(_ context.Context, _ string, _ string) error { return nil }
func (f *fakeNodeStore) SetGroup(_ context.Context, _ string, _ *string) error   { return nil }
func (f *fakeNodeStore) SetLabels(_ context.Context, _ string, _ map[string]string) error {
	return nil
}
func (f *fakeNodeStore) ClaimNode(_ context.Context, _, _ string) (*store.ManagedNode, error) {
	return nil, store.ErrNoClaimCapacity
}
func (f *fakeNodeStore) ReleaseNode(_ context.Context, _, _ string) (bool, error) { return false, nil }
func (f *fakeNodeStore) AdvanceReset(_ context.Context, _ string, _ []string, _ string, _ bool) (bool, error) {
	return false, nil
}
func (f *fakeNodeStore) StampResetProgress(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (f *fakeNodeStore) FailResetBefore(_ context.Context, _ string, _ time.Time) (bool, error) {
	return false, nil
}
func (f *fakeNodeStore) SettleAfterReboot(_ context.Context, _, _ string, _ time.Time) error {
	return nil
}

func (f *fakeNodeStore) AdvanceUpgrade(_ context.Context, _ string, _ []string, _ string, _ bool) (bool, error) {
	return false, nil
}
func (f *fakeNodeStore) Delete(_ context.Context, _ string) error { return nil }

var _ = Describe("AdminMiddleware", func() {
	var (
		e          *echo.Echo
		middleware echo.MiddlewareFunc
	)

	BeforeEach(func() {
		e = echo.New()
		middleware = auth.AdminMiddleware(auth.NewAdminAuth("secret-password", auth.NewSessionStore(), ""))
	})

	It("should allow requests with valid Bearer token", func() {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer secret-password")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		handler := middleware(func(c echo.Context) error {
			return c.String(http.StatusOK, "ok")
		})
		err := handler(c)
		Expect(err).NotTo(HaveOccurred())
		Expect(rec.Code).To(Equal(http.StatusOK))
	})

	It("should reject requests with invalid Bearer token", func() {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer wrong-password")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		handler := middleware(func(c echo.Context) error {
			return c.String(http.StatusOK, "ok")
		})
		err := handler(c)
		Expect(err).NotTo(HaveOccurred())
		Expect(rec.Code).To(Equal(http.StatusUnauthorized))
	})

	It("should reject requests with no Authorization header", func() {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		handler := middleware(func(c echo.Context) error {
			return c.String(http.StatusOK, "ok")
		})
		err := handler(c)
		Expect(err).NotTo(HaveOccurred())
		Expect(rec.Code).To(Equal(http.StatusUnauthorized))
	})
})

var _ = Describe("NodeAPIKeyMiddleware", func() {
	var (
		e         *echo.Echo
		nodeStore *fakeNodeStore
	)

	BeforeEach(func() {
		e = echo.New()
		nodeStore = &fakeNodeStore{
			nodes: []*store.ManagedNode{
				{ID: "node-1", APIKey: "valid-api-key"},
			},
		}
	})

	It("should allow requests with a valid API key and set nodeID", func() {
		middleware := auth.NodeAPIKeyMiddleware(nodeStore)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer valid-api-key")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		var capturedNodeID string
		handler := middleware(func(c echo.Context) error {
			capturedNodeID = c.Get(auth.ContextKeyNodeID).(string)
			return c.String(http.StatusOK, "ok")
		})
		err := handler(c)
		Expect(err).NotTo(HaveOccurred())
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(capturedNodeID).To(Equal("node-1"))
	})

	It("should reject requests with an invalid API key", func() {
		middleware := auth.NodeAPIKeyMiddleware(nodeStore)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer invalid-key")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		handler := middleware(func(c echo.Context) error {
			return c.String(http.StatusOK, "ok")
		})
		err := handler(c)
		Expect(err).NotTo(HaveOccurred())
		Expect(rec.Code).To(Equal(http.StatusUnauthorized))
	})

	It("should reject requests with no Authorization header", func() {
		middleware := auth.NodeAPIKeyMiddleware(nodeStore)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		handler := middleware(func(c echo.Context) error {
			return c.String(http.StatusOK, "ok")
		})
		err := handler(c)
		Expect(err).NotTo(HaveOccurred())
		Expect(rec.Code).To(Equal(http.StatusUnauthorized))
	})
})

var _ = Describe("RequireNodeMatch", func() {
	var e *echo.Echo

	BeforeEach(func() {
		e = echo.New()
	})

	// newCtx builds a context with the given path :nodeID and (optionally) an
	// authenticated node identity, mirroring what NodeAPIKeyMiddleware sets.
	newCtx := func(pathNodeID, authNodeID string) (echo.Context, *httptest.ResponseRecorder) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("nodeID")
		c.SetParamValues(pathNodeID)
		if authNodeID != "" {
			c.Set(auth.ContextKeyNodeID, authNodeID)
		}
		return c, rec
	}

	ok := func(c echo.Context) error { return c.String(http.StatusOK, "ok") }

	It("allows when the path nodeID matches the authenticated node", func() {
		c, rec := newCtx("node-1", "node-1")
		Expect(auth.RequireNodeMatch(ok)(c)).To(Succeed())
		Expect(rec.Code).To(Equal(http.StatusOK))
	})

	It("forbids when the path nodeID differs from the authenticated node", func() {
		c, rec := newCtx("node-2", "node-1")
		Expect(auth.RequireNodeMatch(ok)(c)).To(Succeed())
		Expect(rec.Code).To(Equal(http.StatusForbidden))
	})

	It("rejects with 401 when no node identity is present", func() {
		c, rec := newCtx("node-1", "")
		Expect(auth.RequireNodeMatch(ok)(c)).To(Succeed())
		Expect(rec.Code).To(Equal(http.StatusUnauthorized))
	})
})

var _ = Describe("RegistrationTokenAuth", func() {
	var (
		e          *echo.Echo
		tok        *auth.RegistrationToken
		middleware echo.MiddlewareFunc
	)

	BeforeEach(func() {
		e = echo.New()
		// Shared holder so the rotation specs below can change what the
		// middleware sees on subsequent requests.
		tok = auth.NewRegistrationToken("reg-token-123")
		middleware = auth.RegistrationTokenAuth(tok)
	})

	register := func(token string) int {
		body := `{"registrationToken":"` + token + `","hostname":"test-node"}`
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler := middleware(func(c echo.Context) error {
			return c.String(http.StatusOK, "ok")
		})
		Expect(handler(e.NewContext(req, rec))).To(Succeed())
		return rec.Code
	}

	DescribeTable("rejects a wrong token",
		func(wrong string) {
			Expect(register(wrong)).To(Equal(http.StatusUnauthorized))
		},
		Entry("of the same length", "reg-token-124"),
		Entry("that is a prefix of the token", "reg-token-12"),
		Entry("that extends the token", "reg-token-1234"),
		Entry("that is empty", ""),
	)

	It("accepts only the current token after a rotation", func() {
		tok.Set("rotated-token")
		Expect(register("reg-token-123")).To(Equal(http.StatusUnauthorized))
		Expect(register("rotated-tokem")).To(Equal(http.StatusUnauthorized))
		Expect(register("rotated-token")).To(Equal(http.StatusOK))
	})

	It("should allow requests with a valid registration token", func() {
		body := `{"registrationToken":"reg-token-123","hostname":"test-node"}`
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		handler := middleware(func(c echo.Context) error {
			return c.String(http.StatusOK, "ok")
		})
		err := handler(c)
		Expect(err).NotTo(HaveOccurred())
		Expect(rec.Code).To(Equal(http.StatusOK))
	})

	It("should reject requests with an invalid registration token", func() {
		body := `{"registrationToken":"wrong-token","hostname":"test-node"}`
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		handler := middleware(func(c echo.Context) error {
			return c.String(http.StatusOK, "ok")
		})
		err := handler(c)
		Expect(err).NotTo(HaveOccurred())
		Expect(rec.Code).To(Equal(http.StatusUnauthorized))
	})

	It("should reject requests with no registration token", func() {
		body := `{"hostname":"test-node"}`
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		handler := middleware(func(c echo.Context) error {
			return c.String(http.StatusOK, "ok")
		})
		err := handler(c)
		Expect(err).NotTo(HaveOccurred())
		Expect(rec.Code).To(Equal(http.StatusUnauthorized))
	})

	It("should reject requests with invalid JSON", func() {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("not json"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		handler := middleware(func(c echo.Context) error {
			return c.String(http.StatusOK, "ok")
		})
		err := handler(c)
		Expect(err).NotTo(HaveOccurred())
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
	})

	// paddedBody is a registration body carrying the valid token, padded to
	// size bytes so only its size can make it fail.
	paddedBody := func(size int) string {
		prefix := `{"registrationToken":"reg-token-123","hostname":"`
		suffix := `"}`
		return prefix + strings.Repeat("h", size-len(prefix)-len(suffix)) + suffix
	}

	DescribeTable("refuses a body over 64 KiB with 413 without calling the next handler",
		func(contentLength func(body string) int64) {
			body := paddedBody(64*1024 + 1)
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
			req.ContentLength = contentLength(body)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			called := false
			handler := middleware(func(c echo.Context) error {
				called = true
				return c.String(http.StatusOK, "ok")
			})
			Expect(handler(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusRequestEntityTooLarge))
			Expect(rec.Body.String()).To(ContainSubstring(`"error"`))
			Expect(called).To(BeFalse())
		},
		Entry("with a declared Content-Length", func(body string) int64 { return int64(len(body)) }),
		Entry("with a body of unknown length", func(string) int64 { return -1 }),
	)

	It("authenticates a body of exactly 64 KiB and hands it on intact", func() {
		body := paddedBody(64 * 1024)
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		var seen []byte
		handler := middleware(func(c echo.Context) error {
			var err error
			seen, err = io.ReadAll(c.Request().Body)
			Expect(err).NotTo(HaveOccurred())
			return c.String(http.StatusOK, "ok")
		})
		Expect(handler(c)).To(Succeed())
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(string(seen)).To(Equal(body))
	})
})
