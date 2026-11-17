package auth_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/kairos-io/AuroraBoot/pkg/auth"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakeCommandStore is a minimal store.CommandStore for the authorization tests;
// only ListByNode is functional.
type fakeCommandStore struct {
	byNode map[string][]*store.NodeCommand
}

func (f *fakeCommandStore) ListByNode(_ context.Context, nodeID string) ([]*store.NodeCommand, error) {
	return f.byNode[nodeID], nil
}
func (f *fakeCommandStore) Create(context.Context, *store.NodeCommand) error { return nil }
func (f *fakeCommandStore) CreateBatch(context.Context, []*store.NodeCommand) error {
	return nil
}
func (f *fakeCommandStore) GetByID(context.Context, string) (*store.NodeCommand, error) {
	return nil, nil
}
func (f *fakeCommandStore) GetPending(context.Context, string) ([]*store.NodeCommand, error) {
	return nil, nil
}
func (f *fakeCommandStore) MarkDelivered(context.Context, []string) error          { return nil }
func (f *fakeCommandStore) ClaimForDelivery(context.Context, string) (bool, error) { return false, nil }
func (f *fakeCommandStore) UpdateStatus(context.Context, string, string, string) error {
	return nil
}
func (f *fakeCommandStore) UpdateStatusForNode(context.Context, string, string, string, string) error {
	return nil
}
func (f *fakeCommandStore) ExpireBefore(context.Context, string, time.Time) error { return nil }
func (f *fakeCommandStore) Delete(context.Context, string) error                  { return nil }
func (f *fakeCommandStore) DeleteTerminal(context.Context, string) error          { return nil }

// fakeExtensionStore is a minimal store.ExtensionStore; only GetByID is
// functional, which is all ExtensionDownloadMiddleware reads.
type fakeExtensionStore struct {
	byID map[string]*store.ExtensionRecord
	err  error
}

func (f *fakeExtensionStore) GetByID(_ context.Context, id string) (*store.ExtensionRecord, error) {
	if f.err != nil {
		return nil, f.err
	}
	rec, ok := f.byID[id]
	if !ok {
		return nil, nil
	}
	return rec, nil
}
func (f *fakeExtensionStore) Create(context.Context, *store.ExtensionRecord) error { return nil }
func (f *fakeExtensionStore) List(context.Context) ([]store.ExtensionRecord, error) {
	return nil, nil
}
func (f *fakeExtensionStore) Delete(context.Context, string) error { return nil }
func (f *fakeExtensionStore) FindLatestReadyByName(context.Context, string, string) (*store.ExtensionRecord, error) {
	return nil, nil
}
func (f *fakeExtensionStore) FindByNameAndVersion(context.Context, string, string, string) (*store.ExtensionRecord, error) {
	return nil, nil
}
func (f *fakeExtensionStore) AppendLog(context.Context, string, string) error { return nil }

var _ = Describe("ArtifactImageMiddleware", func() {
	const (
		adminPass = "admin-pass"
		artID     = "art-1"
	)

	var (
		e  *echo.Echo
		ns *fakeNodeStore
		cs *fakeCommandStore
		mw echo.MiddlewareFunc
	)

	BeforeEach(func() {
		e = echo.New()
		ns = &fakeNodeStore{nodes: []*store.ManagedNode{{ID: "node-1", APIKey: "node-1-key"}}}
		cs = &fakeCommandStore{byNode: map[string][]*store.NodeCommand{}}
		mw = auth.ArtifactImageMiddleware(auth.NewAdminAuth(adminPass, auth.NewSessionStore(), ""), ns, cs)
	})

	// do runs GET /api/v1/artifacts/:id/image through the middleware and returns
	// the status code. header sets a Bearer token; query sets ?token=.
	do := func(header, query string) int {
		target := "/api/v1/artifacts/" + artID + "/image"
		if query != "" {
			target += "?token=" + query
		}
		req := httptest.NewRequest(http.MethodGet, target, nil)
		if header != "" {
			req.Header.Set("Authorization", "Bearer "+header)
		}
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues(artID)
		handler := mw(func(c echo.Context) error { return c.String(http.StatusOK, "image") })
		_ = handler(c)
		return rec.Code
	}

	assign := func(cmd string, source string) {
		cs.byNode["node-1"] = []*store.NodeCommand{
			{ManagedNodeID: "node-1", Command: cmd, Args: map[string]string{"source": source}},
		}
	}

	It("allows the admin via the Authorization header", func() {
		Expect(do(adminPass, "")).To(Equal(http.StatusOK))
	})

	It("refuses the admin password in ?token=", func() {
		Expect(do("", adminPass)).To(Equal(http.StatusUnauthorized))
	})

	It("401s with no credentials", func() {
		Expect(do("", "")).To(Equal(http.StatusUnauthorized))
	})

	It("401s an unknown token (neither admin nor a node key)", func() {
		Expect(do("bogus", "")).To(Equal(http.StatusUnauthorized))
	})

	It("allows a node assigned this artifact by an upgrade command", func() {
		assign(store.CmdUpgrade, "artifact:"+artID)
		Expect(do("node-1-key", "")).To(Equal(http.StatusOK))
	})

	It("allows a node assigned this artifact by an upgrade-recovery command", func() {
		assign(store.CmdUpgradeRecovery, "artifact:"+artID)
		Expect(do("node-1-key", "")).To(Equal(http.StatusOK))
	})

	It("rejects a node API key supplied via ?token= (node keys must use the Authorization header)", func() {
		// Even when the node IS assigned the artifact, a node key in the URL is not
		// accepted — it would leak through logs/proxies/history. Header only.
		assign(store.CmdUpgrade, "artifact:"+artID)
		Expect(do("", "node-1-key")).To(Equal(http.StatusUnauthorized))
	})

	It("403s a node with no command for this artifact", func() {
		Expect(do("node-1-key", "")).To(Equal(http.StatusForbidden))
	})

	It("403s a node assigned a DIFFERENT artifact", func() {
		assign(store.CmdUpgrade, "artifact:other")
		Expect(do("node-1-key", "")).To(Equal(http.StatusForbidden))
	})

	It("403s a node whose only command naming this artifact is not an upgrade", func() {
		assign("exec", "artifact:"+artID)
		Expect(do("node-1-key", "")).To(Equal(http.StatusForbidden))
	})
})

var _ = Describe("ExtensionDownloadMiddleware", func() {
	const (
		adminPass = "admin-pass"
		extID     = "ext-1"
		extToken  = "ext-1-download-token"
	)

	var (
		e  *echo.Echo
		ns *fakeNodeStore
		es *fakeExtensionStore
		mw echo.MiddlewareFunc
	)

	BeforeEach(func() {
		e = echo.New()
		ns = &fakeNodeStore{nodes: []*store.ManagedNode{{ID: "node-1", APIKey: "node-1-key"}}}
		es = &fakeExtensionStore{byID: map[string]*store.ExtensionRecord{
			extID:   {ID: extID, DownloadToken: extToken},
			"ext-2": {ID: "ext-2", DownloadToken: "ext-2-download-token"},
		}}
		mw = auth.ExtensionDownloadMiddleware(auth.NewAdminAuth(adminPass, auth.NewSessionStore(), ""), ns, es)
	})

	// do runs GET /api/v1/extensions/:id/download/:filename through the
	// middleware and returns the status code. header sets a Bearer token;
	// query sets ?token=.
	// doRaw is do with the query string spelled out, so a spec can send a bare
	// "?token=" — present but empty — which do cannot express.
	doRaw := func(header, rawQuery string) int {
		target := "/api/v1/extensions/" + extID + "/download/x.sysext.raw" + rawQuery
		req := httptest.NewRequest(http.MethodGet, target, nil)
		if header != "" {
			req.Header.Set("Authorization", "Bearer "+header)
		}
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id", "filename")
		c.SetParamValues(extID, "x.sysext.raw")
		handler := mw(func(c echo.Context) error { return c.String(http.StatusOK, "raw") })
		_ = handler(c)
		return rec.Code
	}

	do := func(header, query string) int {
		target := "/api/v1/extensions/" + extID + "/download/x.sysext.raw"
		if query != "" {
			target += "?token=" + query
		}
		req := httptest.NewRequest(http.MethodGet, target, nil)
		if header != "" {
			req.Header.Set("Authorization", "Bearer "+header)
		}
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id", "filename")
		c.SetParamValues(extID, "x.sysext.raw")
		handler := mw(func(c echo.Context) error { return c.String(http.StatusOK, "raw") })
		_ = handler(c)
		return rec.Code
	}

	It("allows the admin via the Authorization header", func() {
		Expect(do(adminPass, "")).To(Equal(http.StatusOK))
	})

	It("refuses the admin password in ?token=", func() {
		Expect(do("", adminPass)).To(Equal(http.StatusUnauthorized))
	})

	It("allows a registered node via the Authorization header", func() {
		Expect(do("node-1-key", "")).To(Equal(http.StatusOK))
	})

	It("rejects a node API key supplied via ?token=", func() {
		Expect(do("", "node-1-key")).To(Equal(http.StatusUnauthorized))
	})

	It("401s with no credentials", func() {
		Expect(do("", "")).To(Equal(http.StatusUnauthorized))
	})

	It("401s an unknown token", func() {
		Expect(do("bogus", "")).To(Equal(http.StatusUnauthorized))
		Expect(do("", "bogus")).To(Equal(http.StatusUnauthorized))
	})

	// The per-extension token is what an install command's source URL carries,
	// so that URL never has to hold the admin password.
	It("allows this extension's own download token via ?token=", func() {
		Expect(do("", extToken)).To(Equal(http.StatusOK))
	})

	It("rejects another extension's download token", func() {
		Expect(do("", "ext-2-download-token")).To(Equal(http.StatusUnauthorized))
	})

	It("rejects the download token from the Authorization header", func() {
		// It is a URL-borne credential by design; accepting it as a bearer
		// would widen it for no caller that needs it.
		Expect(do(extToken, "")).To(Equal(http.StatusUnauthorized))
	})

	// A record written before the column existed has an empty DownloadToken,
	// and secureCompare is a constant-time byte compare, so "" equals "". With
	// nothing but a bare "?token=" the middleware's both-credentials-empty
	// return already answers 401, but ANY Authorization header gets the request
	// past that and down to the token comparison, where an unguarded ""-vs-""
	// would authorize. Hence the emptiness guards in the lookup.
	It("never authorizes an extension whose stored token is empty", func() {
		es.byID[extID] = &store.ExtensionRecord{ID: extID}
		Expect(doRaw("not-a-node-key", "?token=")).To(Equal(http.StatusUnauthorized))
		Expect(doRaw("node-1-key", "?token=")).To(Equal(http.StatusOK),
			"a registered node is still authorized, by its header")
		Expect(do("", extToken)).To(Equal(http.StatusUnauthorized))
	})

	It("does not open an extension that HAS a token to a bare ?token=", func() {
		Expect(doRaw("", "?token=")).To(Equal(http.StatusUnauthorized))
		Expect(doRaw("not-a-node-key", "?token=")).To(Equal(http.StatusUnauthorized))
	})

	It("fails closed when the extension is unknown or the store errors", func() {
		es.byID = map[string]*store.ExtensionRecord{}
		Expect(do("", extToken)).To(Equal(http.StatusUnauthorized))

		es.byID = map[string]*store.ExtensionRecord{extID: {ID: extID, DownloadToken: extToken}}
		es.err = errors.New("db down")
		Expect(do("", extToken)).To(Equal(http.StatusUnauthorized))
	})

	It("still admits an admin and a node when no extension store is wired", func() {
		mw = auth.ExtensionDownloadMiddleware(auth.NewAdminAuth(adminPass, auth.NewSessionStore(), ""), ns, nil)
		Expect(do(adminPass, "")).To(Equal(http.StatusOK))
		Expect(do("node-1-key", "")).To(Equal(http.StatusOK))
		Expect(do("", extToken)).To(Equal(http.StatusUnauthorized))
	})
})
