package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakeGHCR is a stand-in for the subset of the GitHub packages REST API
// that the cleanup tool uses. It records every request and lets each
// spec seed the version list plus whether /orgs or /users is the "real"
// owner. Deleted versions drop out of subsequent listings.
type fakeGHCR struct {
	mu sync.Mutex

	// owner -> "orgs" | "users" (which scope answers 2xx on GET).
	scopeOf map[string]string
	// (scope|owner|encodedPkg) -> versions still present.
	versions map[string][]pkgVersion
	// Version IDs whose DELETE returns 403.
	deleteFailsFor map[int]bool

	callsMu sync.Mutex
	calls   []string
}

func newFakeGHCR() *fakeGHCR {
	return &fakeGHCR{
		scopeOf:        map[string]string{},
		versions:       map[string][]pkgVersion{},
		deleteFailsFor: map[int]bool{},
	}
}

func (f *fakeGHCR) key(scope, owner, pkg string) string {
	return scope + "|" + owner + "|" + pkg
}

func (f *fakeGHCR) seedOrg(owner, encodedPkg string, vs []pkgVersion) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scopeOf[owner] = "orgs"
	f.versions[f.key("orgs", owner, encodedPkg)] = vs
}

func (f *fakeGHCR) seedUser(owner, encodedPkg string, vs []pkgVersion) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scopeOf[owner] = "users"
	f.versions[f.key("users", owner, encodedPkg)] = vs
}

func (f *fakeGHCR) failDelete(id int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteFailsFor[id] = true
}

func (f *fakeGHCR) recordedCalls() []string {
	f.callsMu.Lock()
	defer f.callsMu.Unlock()
	out := make([]string, len(f.calls))
	copy(out, f.calls)
	return out
}

func (f *fakeGHCR) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Record the escaped path so assertions can match %2F literally
		// (net/http decodes r.URL.Path but preserves EscapedPath()).
		f.callsMu.Lock()
		f.calls = append(f.calls, r.Method+" "+r.URL.EscapedPath()+"?"+r.URL.RawQuery)
		f.callsMu.Unlock()

		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			http.Error(w, "unauthorized: "+got, http.StatusUnauthorized)
			return
		}
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			http.Error(w, "missing accept header", http.StatusBadRequest)
			return
		}

		// Path shapes:
		//   /(orgs|users)/{owner}/packages/container/{pkg}/versions
		//   /(orgs|users)/{owner}/packages/container/{pkg}/versions/{id}
		parts := strings.Split(strings.TrimPrefix(r.URL.EscapedPath(), "/"), "/")
		if len(parts) < 6 || parts[2] != "packages" || parts[3] != "container" || parts[5] != "versions" {
			http.NotFound(w, r)
			return
		}
		scope, owner, pkg := parts[0], parts[1], parts[4]
		key := f.key(scope, owner, pkg)

		f.mu.Lock()
		wantScope, hasOwner := f.scopeOf[owner]
		vs, hasVersions := f.versions[key]
		f.mu.Unlock()

		if hasOwner && wantScope != scope {
			// Right owner, wrong endpoint -- 404 like GitHub does.
			http.NotFound(w, r)
			return
		}
		if !hasOwner || !hasVersions {
			http.NotFound(w, r)
			return
		}

		switch r.Method {
		case http.MethodGet:
			// Naive pagination: honor per_page + page. The total-count
			// header is not required by the client, so skip it.
			perPage, page := 100, 1
			if v := r.URL.Query().Get("per_page"); v != "" {
				fmt.Sscanf(v, "%d", &perPage)
			}
			if v := r.URL.Query().Get("page"); v != "" {
				fmt.Sscanf(v, "%d", &page)
			}
			start := (page - 1) * perPage
			end := start + perPage
			if start > len(vs) {
				start = len(vs)
			}
			if end > len(vs) {
				end = len(vs)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(vs[start:end])
		case http.MethodDelete:
			id := 0
			fmt.Sscanf(parts[6], "%d", &id)
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.deleteFailsFor[id] {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			kept := vs[:0]
			for _, v := range vs {
				if v.ID != id {
					kept = append(kept, v)
				}
			}
			f.versions[key] = kept
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

// hasPrefixIn / hasSubstringIn are match helpers for asserting on the
// recorded API call log without pulling in a full regex per assertion.
func hasPrefixIn(prefix string) func([]string) bool {
	return func(calls []string) bool {
		for _, c := range calls {
			if strings.HasPrefix(c, prefix) {
				return true
			}
		}
		return false
	}
}

func hasSubstringIn(needle string) func([]string) bool {
	return func(calls []string) bool {
		for _, c := range calls {
			if strings.Contains(c, needle) {
				return true
			}
		}
		return false
	}
}

var _ = Describe("parseRefs", func() {
	It("drops blanks and '#' comments and trims whitespace", func() {
		got := parseRefs(strings.Join([]string{
			"",
			"# comment",
			"  ghcr.io/kairos-io/kairos/kairos-uki:a  ",
			"ghcr.io/kairos-io/kairos/kairos-uki:b # trailing",
			"",
		}, "\n"))
		Expect(got).To(Equal([]string{
			"ghcr.io/kairos-io/kairos/kairos-uki:a",
			"ghcr.io/kairos-io/kairos/kairos-uki:b",
		}))
	})
})

var _ = Describe("parseImageRef", func() {
	It("splits a simple two-segment ref", func() {
		owner, pkg, tag, err := parseImageRef("ghcr.io/alice/mypkg:pr-42")
		Expect(err).ToNot(HaveOccurred())
		Expect(owner).To(Equal("alice"))
		Expect(pkg).To(Equal("mypkg"))
		Expect(tag).To(Equal("pr-42"))
	})

	It("URL-encodes a nested package path", func() {
		owner, pkg, tag, err := parseImageRef("ghcr.io/kairos-io/kairos/kairos-uki:tag")
		Expect(err).ToNot(HaveOccurred())
		Expect(owner).To(Equal("kairos-io"))
		Expect(pkg).To(Equal("kairos%2Fkairos-uki"))
		Expect(tag).To(Equal("tag"))
	})

	It("rejects non-ghcr.io refs", func() {
		_, _, _, err := parseImageRef("quay.io/foo/bar:v1")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not a ghcr.io"))
	})

	It("rejects a ref missing its tag", func() {
		_, _, _, err := parseImageRef("ghcr.io/foo/bar")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("missing :tag"))
	})

	It("rejects a ref with an empty tag", func() {
		_, _, _, err := parseImageRef("ghcr.io/foo/bar:")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("missing :tag"))
	})

	It("rejects a ref missing the package segment", func() {
		_, _, _, err := parseImageRef("ghcr.io/foo:tag")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("missing package"))
	})
})

var _ = Describe("deleteRefs", func() {
	var (
		fake *fakeGHCR
		srv  *httptest.Server
		c    *client
		ctx  context.Context
	)

	BeforeEach(func() {
		fake = newFakeGHCR()
		srv = httptest.NewServer(fake.handler())
		c = &client{apiBase: srv.URL, token: "test-token", http: srv.Client()}
		ctx = context.Background()
	})

	AfterEach(func() {
		srv.Close()
	})

	Context("with an org-owned package", func() {
		BeforeEach(func() {
			fake.seedOrg("kairos-io", "kairos%2Fkairos-uki", []pkgVersion{
				{ID: 111, Metadata: pkgMeta{Container: containerMeta{Tags: []string{"sometag-abc123"}}}},
				{ID: 222, Metadata: pkgMeta{Container: containerMeta{Tags: []string{"other-tag"}}}},
			})
		})

		It("deletes exactly the version carrying the target tag", func() {
			rpt, err := deleteRefs(ctx, c, []string{"ghcr.io/kairos-io/kairos/kairos-uki:sometag-abc123"})
			Expect(err).ToNot(HaveOccurred())
			Expect(rpt.Deleted).To(Equal(1))
			Expect(rpt.Failed).To(Equal(0))

			calls := fake.recordedCalls()
			Expect(calls).To(Satisfy(hasPrefixIn("GET /orgs/kairos-io/packages/container/kairos%2Fkairos-uki/versions?")))
			Expect(calls).To(Satisfy(hasPrefixIn("DELETE /orgs/kairos-io/packages/container/kairos%2Fkairos-uki/versions/111?")))
			Expect(calls).ToNot(Satisfy(hasPrefixIn("DELETE /orgs/kairos-io/packages/container/kairos%2Fkairos-uki/versions/222?")))
		})

		It("warns and does not delete when no version carries the tag", func() {
			rpt, err := deleteRefs(ctx, c, []string{"ghcr.io/kairos-io/kairos/kairos-uki:not-there"})
			Expect(err).ToNot(HaveOccurred())
			Expect(rpt.Deleted).To(Equal(0))
			Expect(rpt.Failed).To(Equal(0))
			Expect(rpt.NotFound).To(Equal(1))
			for _, c := range fake.recordedCalls() {
				Expect(c).ToNot(HavePrefix("DELETE "))
			}
		})
	})

	Context("with a user-owned package", func() {
		BeforeEach(func() {
			fake.seedUser("alice", "mypkg", []pkgVersion{
				{ID: 999, Metadata: pkgMeta{Container: containerMeta{Tags: []string{"pr-42"}}}},
			})
		})

		It("falls back to /users after /orgs 404s", func() {
			rpt, err := deleteRefs(ctx, c, []string{"ghcr.io/alice/mypkg:pr-42"})
			Expect(err).ToNot(HaveOccurred())
			Expect(rpt.Deleted).To(Equal(1))

			calls := fake.recordedCalls()
			Expect(calls).To(Satisfy(hasPrefixIn("GET /orgs/alice/packages/container/mypkg/versions?")))
			Expect(calls).To(Satisfy(hasPrefixIn("GET /users/alice/packages/container/mypkg/versions?")))
			Expect(calls).To(Satisfy(hasPrefixIn("DELETE /users/alice/packages/container/mypkg/versions/999?")))
		})
	})

	Context("when the package exists in neither /orgs nor /users", func() {
		It("counts the ref as unresolved without failing the report", func() {
			rpt, err := deleteRefs(ctx, c, []string{"ghcr.io/kairos-io/kairos/does-not-exist:v1"})
			Expect(err).ToNot(HaveOccurred())
			Expect(rpt.Deleted).To(Equal(0))
			Expect(rpt.Failed).To(Equal(0))
			Expect(rpt.Unresolved).To(Equal(1))
		})
	})

	Context("when a DELETE call fails", func() {
		BeforeEach(func() {
			fake.seedOrg("kairos-io", "kairos%2Fkairos-uki", []pkgVersion{
				{ID: 111, Metadata: pkgMeta{Container: containerMeta{Tags: []string{"sometag-abc123"}}}},
			})
			fake.failDelete(111)
		})

		It("counts the failure and returns without error", func() {
			rpt, err := deleteRefs(ctx, c, []string{"ghcr.io/kairos-io/kairos/kairos-uki:sometag-abc123"})
			Expect(err).ToNot(HaveOccurred())
			Expect(rpt.Failed).To(Equal(1))
		})
	})

	Context("with multiple refs against the same package", func() {
		BeforeEach(func() {
			fake.seedOrg("kairos-io", "kairos%2Fkairos-uki", []pkgVersion{
				{ID: 111, Metadata: pkgMeta{Container: containerMeta{Tags: []string{"a"}}}},
				{ID: 222, Metadata: pkgMeta{Container: containerMeta{Tags: []string{"b"}}}},
				{ID: 333, Metadata: pkgMeta{Container: containerMeta{Tags: []string{"c"}}}},
			})
		})

		It("deletes each matching version", func() {
			rpt, err := deleteRefs(ctx, c, []string{
				"ghcr.io/kairos-io/kairos/kairos-uki:a",
				"ghcr.io/kairos-io/kairos/kairos-uki:c",
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(rpt.Deleted).To(Equal(2))

			calls := fake.recordedCalls()
			Expect(calls).To(Satisfy(hasPrefixIn("DELETE /orgs/kairos-io/packages/container/kairos%2Fkairos-uki/versions/111?")))
			Expect(calls).To(Satisfy(hasPrefixIn("DELETE /orgs/kairos-io/packages/container/kairos%2Fkairos-uki/versions/333?")))
		})
	})

	Context("when a tag is shared across two versions", func() {
		// buildx imagetools create can leave two package versions
		// carrying the same tag; the API only lets us delete by
		// version id, so a tag-scoped cleanup has to walk every version.
		BeforeEach(func() {
			fake.seedOrg("kairos-io", "kairos%2Fkairos-uki", []pkgVersion{
				{ID: 111, Metadata: pkgMeta{Container: containerMeta{Tags: []string{"same"}}}},
				{ID: 222, Metadata: pkgMeta{Container: containerMeta{Tags: []string{"same", "extra"}}}},
			})
		})

		It("deletes every version whose tag list contains the target", func() {
			rpt, err := deleteRefs(ctx, c, []string{"ghcr.io/kairos-io/kairos/kairos-uki:same"})
			Expect(err).ToNot(HaveOccurred())
			Expect(rpt.Deleted).To(Equal(2))
		})
	})

	Context("with more versions than fit on one page", func() {
		BeforeEach(func() {
			versions := make([]pkgVersion, 0, 150)
			for i := 1; i <= 150; i++ {
				tags := []string{fmt.Sprintf("noise-%d", i)}
				if i == 120 {
					tags = []string{"target"}
				}
				versions = append(versions, pkgVersion{ID: i, Metadata: pkgMeta{Container: containerMeta{Tags: tags}}})
			}
			fake.seedOrg("kairos-io", "kairos%2Fkairos-uki", versions)
		})

		It("paginates until the target tag is found", func() {
			rpt, err := deleteRefs(ctx, c, []string{"ghcr.io/kairos-io/kairos/kairos-uki:target"})
			Expect(err).ToNot(HaveOccurred())
			Expect(rpt.Deleted).To(Equal(1))
			Expect(fake.recordedCalls()).To(Satisfy(hasSubstringIn("page=2")))
		})
	})
})
