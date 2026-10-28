package github_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"github.com/Masterminds/semver/v3"
	gogithub "github.com/google/go-github/v77/github"
	"github.com/kairos-io/kairos/v4/agent/pkg/github"
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
		Expect(err.Error()).To(ContainSubstring("invalid slug format"))
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
		Expect(err.Error()).To(ContainSubstring("invalid slug format"))
	})
})

var _ = Describe("Releases with names that are not semver", func() {
	findWith := func(body string) (semver.Collection, error) {
		mux := http.NewServeMux()
		mux.HandleFunc("/repos/test/odd/releases", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		})
		server := httptest.NewServer(mux)
		defer server.Close()

		serverURL, err := url.Parse(server.URL)
		Expect(err).ToNot(HaveOccurred())
		hc := &http.Client{Transport: rewriteTransport{target: serverURL}}
		ctx := context.WithValue(context.Background(), oauth2.HTTPClient, hc)

		return github.FindReleases(ctx, "fake-token", "test/odd", false)
	}

	It("skips a release with a null name", func() {
		releases, err := findWith(`[{"name":null},{"name":"v1.0.0"}]`)
		Expect(err).ToNot(HaveOccurred())
		Expect(releases).To(HaveLen(1))
		Expect(releases[0].Original()).To(Equal("v1.0.0"))
	})

	It("skips a release with no name", func() {
		releases, err := findWith(`[{"tag_name":"v1.0.0"},{"name":"v1.0.0"}]`)
		Expect(err).ToNot(HaveOccurred())
		Expect(releases).To(HaveLen(1))
	})

	It("skips a v-prefixed name that is not semver", func() {
		releases, err := findWith(`[{"name":"v2.0.0 - Hotfix"},{"name":"v1.0.0"}]`)
		Expect(err).ToNot(HaveOccurred())
		Expect(releases).To(HaveLen(1))
		Expect(releases[0].Original()).To(Equal("v1.0.0"))
	})
})

// budgetRefusal reports whether err is api.github.com declining the request
// because the shared request budget is spent, rather than anything about this
// code. The primary cap answers 403 with x-ratelimit-remaining 0, the
// secondary cap answers 403 with a secondary-rate-limits documentation link,
// and a 429 carries no shape of its own.
func budgetRefusal(err error) bool {
	var rateErr *gogithub.RateLimitError
	var abuseErr *gogithub.AbuseRateLimitError
	var respErr *gogithub.ErrorResponse

	if errors.As(err, &rateErr) || errors.As(err, &abuseErr) {
		return true
	}

	return errors.As(err, &respErr) && respErr.Response != nil &&
		respErr.Response.StatusCode == http.StatusTooManyRequests
}

// liveReleases asks api.github.com for the real kairos releases, and skips the
// calling spec when the API refuses for a reason that is about the request
// budget rather than about this code.
//
// The budget is shared and exhaustible. A workflow GITHUB_TOKEN is capped per
// repository per hour, across every run in that repository at once, and the
// anonymous cap is 60 per hour per runner IP. A busy pull request queue can
// spend either one, so leaving the refusal as a failure makes `go test ./...`
// for the whole monorepo go red for something no commit changed.
func liveReleases(token string, preReleases bool) semver.Collection {
	GinkgoHelper()

	releases, err := github.FindReleases(context.Background(), token, "kairos-io/kairos", preReleases)
	if budgetRefusal(err) {
		Skip("api.github.com request budget is spent: " + err.Error())
	}
	Expect(err).ToNot(HaveOccurred())

	return releases
}

var _ = Describe("Releases budget refusals", func() {
	// Drive the refusals through FindReleases rather than constructing the
	// error values, so this also covers how go-github maps a response onto
	// them. Anything else has to stay a failure, or a real break hides behind
	// a skip.
	refuse := func(status int, body string, headers map[string]string) error {
		mux := http.NewServeMux()
		mux.HandleFunc("/repos/test/refused/releases", func(w http.ResponseWriter, _ *http.Request) {
			for k, v := range headers {
				w.Header().Set(k, v)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		})
		server := httptest.NewServer(mux)
		defer server.Close()

		serverURL, err := url.Parse(server.URL)
		Expect(err).ToNot(HaveOccurred())
		hc := &http.Client{Transport: rewriteTransport{target: serverURL}}
		ctx := context.WithValue(context.Background(), oauth2.HTTPClient, hc)

		_, err = github.FindReleases(ctx, "fake-token", "test/refused", false)
		Expect(err).To(HaveOccurred())

		return err
	}

	It("recognises the primary rate limit", func() {
		err := refuse(http.StatusForbidden, `{"message":"API rate limit exceeded"}`,
			map[string]string{"X-RateLimit-Remaining": "0"})
		Expect(budgetRefusal(err)).To(BeTrue())
	})

	It("recognises the secondary rate limit", func() {
		err := refuse(http.StatusForbidden,
			`{"message":"You have exceeded a secondary rate limit","documentation_url":"https://docs.github.com/rest#secondary-rate-limits"}`, nil)
		Expect(budgetRefusal(err)).To(BeTrue())
	})

	It("recognises too many requests", func() {
		err := refuse(http.StatusTooManyRequests, `{"message":"too many requests"}`, nil)
		Expect(budgetRefusal(err)).To(BeTrue())
	})

	It("leaves a server error as a failure", func() {
		err := refuse(http.StatusInternalServerError, `{"message":"boom"}`, nil)
		Expect(budgetRefusal(err)).To(BeFalse())
	})
})

var _ = Describe("Releases", func() {
	// Live integration probe against api.github.com, so that release ordering
	// is checked against the shape the real API returns and not only against
	// the mock above. Pick up GITHUB_TOKEN if the caller set one: CI gets it
	// via secrets.GITHUB_TOKEN in _unit-tests.yaml, which raises the cap well
	// above the anonymous one.
	token := os.Getenv("GITHUB_TOKEN")

	It("can find the proper releases in order", func() {
		releases := liveReleases(token, false)
		Expect(len(releases)).To(BeNumerically(">", 0))
		// Expect the first one to be greater than the last one
		Expect(releases[0].GreaterThan(releases[len(releases)-1])).To(BeTrue())
	})
	It("can find the proper releases in order with prereleases", func() {
		releases := liveReleases(token, true)
		Expect(len(releases)).To(BeNumerically(">", 0))
		// Expect the first one to be greater than the last one
		Expect(releases[0].GreaterThan(releases[len(releases)-1])).To(BeTrue())
	})
})
