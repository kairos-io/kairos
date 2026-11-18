package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/builder"
	"github.com/kairos-io/AuroraBoot/pkg/server"
)

// recordingBuilder keeps the options of every build so tests can inspect the
// generated cloud-config.
type recordingBuilder struct {
	fakeBuilder
	mu   sync.Mutex
	opts []builder.BuildOptions
}

func (r *recordingBuilder) Build(ctx context.Context, opts builder.BuildOptions) (*builder.BuildStatus, error) {
	r.mu.Lock()
	r.opts = append(r.opts, opts)
	r.mu.Unlock()
	return r.fakeBuilder.Build(ctx, opts)
}

func (r *recordingBuilder) last() builder.BuildOptions {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.opts[len(r.opts)-1]
}

var _ = Describe("Registration token rotation", func() {
	var (
		srv *httptest.Server
		rb  *recordingBuilder
	)

	adminDo := func(method, path, body string) *http.Response {
		req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		Expect(err).NotTo(HaveOccurred())
		req.Header.Set("Authorization", "Bearer admin-pass")
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		Expect(err).NotTo(HaveOccurred())
		return resp
	}

	rotate := func() string {
		resp := adminDo(http.MethodPost, "/api/v1/settings/registration-token/rotate", "")
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var out map[string]string
		Expect(json.NewDecoder(resp.Body).Decode(&out)).To(Succeed())
		Expect(out["registrationToken"]).NotTo(BeEmpty())
		return out["registrationToken"]
	}

	buildArtifact := func() {
		resp := adminDo(http.MethodPost, "/api/v1/artifacts",
			`{"baseImage":"quay.io/kairos/ubuntu:24.04","outputs":{"iso":true}}`)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusCreated))
	}

	BeforeEach(func() {
		rb = &recordingBuilder{}
		srv = httptest.NewServer(server.New(server.Config{
			NodeStore:     &fakeNodeStore{},
			CommandStore:  &fakeCommandStore{},
			GroupStore:    &fakeGroupStore{},
			Builder:       rb,
			AdminPassword: "admin-pass",
			RegToken:      "reg-token",
			AuroraBootURL: "http://localhost:8080",
		}))
	})

	AfterEach(func() {
		srv.Close()
	})

	It("bakes the rotated token into artifacts built after the rotation", func() {
		newToken := rotate()
		Expect(newToken).NotTo(Equal("reg-token"))

		buildArtifact()

		opts := rb.last()
		Expect(opts.CloudConfig).To(ContainSubstring(newToken))
		Expect(opts.CloudConfig).NotTo(ContainSubstring("reg-token"))
		Expect(opts.LogRedactValues).To(ContainElement(newToken))
	})

	It("builds artifacts while the token rotates without racing", func() {
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(2)
			go func() {
				defer GinkgoRecover()
				defer wg.Done()
				rotate()
			}()
			go func() {
				defer GinkgoRecover()
				defer wg.Done()
				buildArtifact()
			}()
		}
		wg.Wait()

		final := rotate()
		buildArtifact()
		Expect(rb.last().CloudConfig).To(ContainSubstring(final))
	})
})
