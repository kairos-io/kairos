package github_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/kairos-io/kairos-agent/v2/pkg/github"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/oauth2"
)

func TestReleases(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Releases Suite")
}

// rewriteTransport redirects any request to the given test server URL so no
// real network traffic is generated.
type rewriteTransport struct {
	target *url.URL
}

func (t rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = t.target.Scheme
	req.URL.Host = t.target.Host
	return http.DefaultTransport.RoundTrip(req)
}

var _ = Describe("Releases with a mocked API", func() {
	var server *httptest.Server
	var ctx context.Context

	BeforeEach(func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/repos/test/good/releases", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"name":"v1.0.0"},{"name":"v2.0.0"},{"name":"v1.5.0-rc1"},{"name":"untagged-release"}]`))
		})
		mux.HandleFunc("/repos/test/error/releases", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
		})
		server = httptest.NewServer(mux)

		serverURL, err := url.Parse(server.URL)
		Expect(err).ToNot(HaveOccurred())
		// Inject a client whose transport rewrites api.github.com requests to
		// the test server. oauth2.NewClient picks it up from the context, so a
		// non-empty token must be used to go through the oauth2 client path.
		hc := &http.Client{Transport: rewriteTransport{target: serverURL}}
		ctx = context.WithValue(context.Background(), oauth2.HTTPClient, hc)
	})

	AfterEach(func() {
		server.Close()
	})

	It("returns stable releases sorted higher first", func() {
		releases, err := github.FindReleases(ctx, "fake-token", "test/good", false)
		Expect(err).ToNot(HaveOccurred())
		Expect(releases).To(HaveLen(2))
		Expect(releases[0].Original()).To(Equal("v2.0.0"))
		Expect(releases[1].Original()).To(Equal("v1.0.0"))
	})

	It("includes prereleases when requested", func() {
		releases, err := github.FindReleases(ctx, "fake-token", "test/good", true)
		Expect(err).ToNot(HaveOccurred())
		Expect(releases).To(HaveLen(3))
		Expect(releases[0].Original()).To(Equal("v2.0.0"))
		Expect(releases[1].Original()).To(Equal("v1.5.0-rc1"))
		Expect(releases[2].Original()).To(Equal("v1.0.0"))
	})

	It("returns no error and no releases on 404", func() {
		releases, err := github.FindReleases(ctx, "fake-token", "test/missing", false)
		Expect(err).ToNot(HaveOccurred())
		Expect(releases).To(BeNil())
	})

	It("returns the error on a non-404 API error", func() {
		releases, err := github.FindReleases(ctx, "fake-token", "test/error", false)
		Expect(err).To(HaveOccurred())
		Expect(releases).To(BeNil())
	})

	It("fails on invalid slug without separator", func() {
		_, err := github.FindReleases(ctx, "fake-token", "invalidslug", false)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("Invalid slug format"))
	})

	It("fails on slug with empty owner or name", func() {
		_, err := github.FindReleases(ctx, "fake-token", "/name", false)
		Expect(err).To(HaveOccurred())
		_, err = github.FindReleases(ctx, "fake-token", "owner/", false)
		Expect(err).To(HaveOccurred())
		_, err = github.FindReleases(ctx, "fake-token", "a/b/c", false)
		Expect(err).To(HaveOccurred())
	})

	It("fails on invalid slug with an empty token (default client)", func() {
		// Empty token exercises the http.DefaultClient path of newHTTPClient,
		// the invalid slug aborts before any request is made.
		_, err := github.FindReleases(context.Background(), "", "invalidslug", false)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("Invalid slug format"))
	})
})

var _ = Describe("Releases", func() {
	It("can find the proper releases in order", func() {
		releases, err := github.FindReleases(context.Background(), "", "kairos-io/kairos", false)
		Expect(err).ToNot(HaveOccurred())
		Expect(len(releases)).To(BeNumerically(">", 0))
		// Expect the first one to be greater than the last one
		Expect(releases[0].GreaterThan(releases[len(releases)-1]))
	})
	It("can find the proper releases in order with prereleases", func() {
		releases, err := github.FindReleases(context.Background(), "", "kairos-io/kairos", true)
		Expect(err).ToNot(HaveOccurred())
		Expect(len(releases)).To(BeNumerically(">", 0))
		// Expect the first one to be greater than the last one
		Expect(releases[0].GreaterThan(releases[len(releases)-1]))
	})
})
