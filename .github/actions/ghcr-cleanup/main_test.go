package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

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
	// After this many successful DELETEs, the next DELETE returns 429
	// so the rate-limit-stop path can be exercised. -1 (default)
	// disables the behavior.
	rateLimitAfterDeletes int
	deletesSoFar          int

	callsMu sync.Mutex
	calls   []string
}

func newFakeGHCR() *fakeGHCR {
	return &fakeGHCR{
		scopeOf:               map[string]string{},
		versions:              map[string][]pkgVersion{},
		deleteFailsFor:        map[int]bool{},
		rateLimitAfterDeletes: -1,
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
			if f.rateLimitAfterDeletes >= 0 && f.deletesSoFar >= f.rateLimitAfterDeletes {
				w.Header().Set("Retry-After", "60")
				http.Error(w, "too many requests", http.StatusTooManyRequests)
				return
			}
			f.deletesSoFar++
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

	// Regression test for the pagination/delete race the review flagged:
	// with the tag on two versions straddling a page boundary, deleting
	// while paginating would shift the server-side window and the
	// version originally on page 2 would never be seen. The collect-
	// then-delete implementation walks every page first and only issues
	// DELETEs once the full match set is known, so both hits land.
	Context("with matching versions on either side of a page boundary", func() {
		BeforeEach(func() {
			versions := make([]pkgVersion, 0, 150)
			for i := 1; i <= 150; i++ {
				tags := []string{fmt.Sprintf("noise-%d", i)}
				if i == 50 || i == 120 {
					tags = []string{"target"}
				}
				versions = append(versions, pkgVersion{ID: i, Metadata: pkgMeta{Container: containerMeta{Tags: tags}}})
			}
			fake.seedOrg("kairos-io", "kairos%2Fkairos-uki", versions)
		})

		It("deletes every match across pages without being tripped by the shrinking list", func() {
			rpt, err := deleteRefs(ctx, c, []string{"ghcr.io/kairos-io/kairos/kairos-uki:target"})
			Expect(err).ToNot(HaveOccurred())
			Expect(rpt.Deleted).To(Equal(2))
			Expect(fake.recordedCalls()).To(Satisfy(hasSubstringIn("page=2")))
		})
	})
})

var _ = Describe("parseRetention", func() {
	DescribeTable("valid inputs",
		func(in string, want time.Duration) {
			got, err := parseRetention(in)
			Expect(err).ToNot(HaveOccurred())
			Expect(got).To(Equal(want))
		},
		Entry("hours", "6h", 6*time.Hour),
		Entry("days", "14d", 14*24*time.Hour),
		Entry("weeks", "2w", 2*7*24*time.Hour),
		Entry("weeks == 336h", "2w", 336*time.Hour),
	)
	DescribeTable("invalid inputs",
		func(in string) {
			_, err := parseRetention(in)
			Expect(err).To(HaveOccurred())
		},
		Entry("empty", ""),
		Entry("bad chars", "hours"),
		Entry("negative", "-1h"),
		Entry("bad day count", "xd"),
	)
})

var _ = Describe("parsePackageSpecs", func() {
	It("splits pairs and URL-encodes nested package paths", func() {
		got, err := parsePackageSpecs(strings.Join([]string{
			"",
			"# comment",
			"ghcr.io/kairos-io/kairos/hadron        2w",
			"ghcr.io/kairos-io/kairos/kairos-uki    6h",
			"ghcr.io/alice/mypkg                    14d",
		}, "\n"))
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(HaveLen(3))
		Expect(got[0].Owner).To(Equal("kairos-io"))
		Expect(got[0].EncPkg).To(Equal("kairos%2Fhadron"))
		Expect(got[0].Retention).To(Equal(2 * 7 * 24 * time.Hour))
		Expect(got[1].EncPkg).To(Equal("kairos%2Fkairos-uki"))
		Expect(got[1].Retention).To(Equal(6 * time.Hour))
		Expect(got[2].Owner).To(Equal("alice"))
		Expect(got[2].EncPkg).To(Equal("mypkg"))
	})

	DescribeTable("rejects malformed lines",
		func(line, wantMsg string) {
			_, err := parsePackageSpecs(line)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(wantMsg))
		},
		Entry("with a :tag", "ghcr.io/kairos-io/kairos/hadron:tag 2w", "must not include a :tag"),
		Entry("non-ghcr.io", "quay.io/foo/bar 2w", "not a ghcr.io"),
		Entry("missing retention", "ghcr.io/kairos-io/kairos/hadron", "want \"<ref>"),
		Entry("bad retention", "ghcr.io/kairos-io/kairos/hadron 2q", "retention"),
	)
})

var _ = Describe("tagMatchesProtected", func() {
	DescribeTable("glob semantics",
		func(tag string, protected []string, want bool) {
			Expect(tagMatchesProtected(tag, protected)).To(Equal(want))
		},
		Entry("exact match", "master", []string{"master", "latest"}, true),
		Entry("no match", "pr-42", []string{"master", "latest"}, false),
		Entry("v-tag pattern", "v4.3.0", []string{"v[0-9]*"}, true),
		Entry("release candidate pattern", "v4.3.0-rc1", []string{"v[0-9]*"}, true),
		Entry("pattern doesn't match master-scratch", "master-scratch", []string{"master"}, false),
		Entry("empty protected list", "anything", nil, false),
	)
})

var _ = Describe("parseProtectedGlobs", func() {
	It("returns each valid pattern", func() {
		got, err := parseProtectedGlobs("master\n# comment\nlatest\nv[0-9]*\n")
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(Equal([]string{"master", "latest", "v[0-9]*"}))
	})
	It("aborts on an invalid glob so a typo cannot silently lose protection", func() {
		_, err := parseProtectedGlobs("master\nv[abc\n")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not a valid glob"))
		Expect(err.Error()).To(ContainSubstring("v[abc"))
	})
})

var _ = Describe("pruneAged", func() {
	var (
		fake *fakeGHCR
		srv  *httptest.Server
		c    *client
		ctx  context.Context
		now  time.Time
	)

	BeforeEach(func() {
		fake = newFakeGHCR()
		srv = httptest.NewServer(fake.handler())
		c = &client{apiBase: srv.URL, token: "test-token", http: srv.Client()}
		ctx = context.Background()
		now = time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	})

	AfterEach(func() {
		srv.Close()
	})

	Context("mixed ages and protected tags on one package", func() {
		BeforeEach(func() {
			fake.seedOrg("kairos-io", "kairos%2Fhadron", []pkgVersion{
				// Aged out, unprotected -> delete.
				{ID: 1, Name: "sha256:1", CreatedAt: now.Add(-30 * 24 * time.Hour),
					Metadata: pkgMeta{Container: containerMeta{Tags: []string{"hadron-v0.5.1-core-amd64-generic-v0.0.0-oldsha"}}}},
				// Aged out but tagged master -> keep.
				{ID: 2, Name: "sha256:2", CreatedAt: now.Add(-30 * 24 * time.Hour),
					Metadata: pkgMeta{Container: containerMeta{Tags: []string{"master"}}}},
				// Aged out but tagged v-release -> keep.
				{ID: 3, Name: "sha256:3", CreatedAt: now.Add(-90 * 24 * time.Hour),
					Metadata: pkgMeta{Container: containerMeta{Tags: []string{"v4.3.0"}}}},
				// Aged out but tagged latest -> keep.
				{ID: 4, Name: "sha256:4", CreatedAt: now.Add(-30 * 24 * time.Hour),
					Metadata: pkgMeta{Container: containerMeta{Tags: []string{"latest"}}}},
				// Fresh, unprotected -> keep.
				{ID: 5, Name: "sha256:5", CreatedAt: now.Add(-24 * time.Hour),
					Metadata: pkgMeta{Container: containerMeta{Tags: []string{"hadron-v0.5.1-core-amd64-generic-v0.0.0-freshsha"}}}},
				// Untagged aged, but referenced by the kept :master
				// manifest list below -> keep.
				{ID: 6, Name: "sha256:6", CreatedAt: now.Add(-90 * 24 * time.Hour),
					Metadata: pkgMeta{Container: containerMeta{Tags: nil}}},
			})
		})

		It("deletes exactly the aged unprotected tagged versions and keeps referenced children", func() {
			specs := []packageSpec{{
				Ref:       "ghcr.io/kairos-io/kairos/hadron",
				Owner:     "kairos-io",
				EncPkg:    "kairos%2Fhadron",
				Retention: 14 * 24 * time.Hour,
			}}
			protected := []string{"master", "latest", "master-scratch", "v[0-9]*"}
			mf := newFakeManifestFetcher()
			// :master (id=2) is a manifest list referencing id=6's digest,
			// so id=6 (untagged aged) must stay put.
			mf.setChildren("kairos-io", "kairos%2Fhadron", "sha256:2", []string{"sha256:6"})
			rpt, err := pruneAged(ctx, c, mf, specs, protected, now, false)
			Expect(err).ToNot(HaveOccurred())
			Expect(rpt.Deleted).To(Equal(1))
			Expect(rpt.Failed).To(Equal(0))
			Expect(fake.recordedCalls()).To(Satisfy(hasPrefixIn(
				"DELETE /orgs/kairos-io/packages/container/kairos%2Fhadron/versions/1?")))
			for _, id := range []int{2, 3, 4, 5, 6} {
				Expect(fake.recordedCalls()).ToNot(Satisfy(hasPrefixIn(
					fmt.Sprintf("DELETE /orgs/kairos-io/packages/container/kairos%%2Fhadron/versions/%d?", id))))
			}
		})

		It("issues no DELETEs in dry-run mode but still counts them", func() {
			specs := []packageSpec{{
				Ref:       "ghcr.io/kairos-io/kairos/hadron",
				Owner:     "kairos-io",
				EncPkg:    "kairos%2Fhadron",
				Retention: 14 * 24 * time.Hour,
			}}
			mf := newFakeManifestFetcher()
			mf.setChildren("kairos-io", "kairos%2Fhadron", "sha256:2", []string{"sha256:6"})
			rpt, err := pruneAged(ctx, c, mf, specs, []string{"master", "latest", "v[0-9]*"}, now, true)
			Expect(err).ToNot(HaveOccurred())
			Expect(rpt.Deleted).To(Equal(1)) // counted as "would delete"
			for _, c := range fake.recordedCalls() {
				Expect(c).ToNot(HavePrefix("DELETE "))
			}
		})
	})

	Context("different retention per package", func() {
		BeforeEach(func() {
			// hadron: 14d retention. kairos-uki: 6h retention.
			// Same 8-hour age -> hadron keeps, uki deletes.
			ageBoth := now.Add(-8 * time.Hour)
			fake.seedOrg("kairos-io", "kairos%2Fhadron", []pkgVersion{
				{ID: 10, Name: "sha256:h", CreatedAt: ageBoth,
					Metadata: pkgMeta{Container: containerMeta{Tags: []string{"hadron-v0.5.1-core-amd64-generic-v0.0.0-sha"}}}},
			})
			fake.seedOrg("kairos-io", "kairos%2Fkairos-uki", []pkgVersion{
				{ID: 20, Name: "sha256:u", CreatedAt: ageBoth,
					Metadata: pkgMeta{Container: containerMeta{Tags: []string{"upgrade-abcd"}}}},
			})
		})

		It("applies the correct retention to each package", func() {
			specs := []packageSpec{
				{Ref: "ghcr.io/kairos-io/kairos/hadron", Owner: "kairos-io", EncPkg: "kairos%2Fhadron", Retention: 14 * 24 * time.Hour},
				{Ref: "ghcr.io/kairos-io/kairos/kairos-uki", Owner: "kairos-io", EncPkg: "kairos%2Fkairos-uki", Retention: 6 * time.Hour},
			}
			rpt, err := pruneAged(ctx, c, nil, specs, nil, now, false)
			Expect(err).ToNot(HaveOccurred())
			Expect(rpt.Deleted).To(Equal(1))
			Expect(fake.recordedCalls()).ToNot(Satisfy(hasPrefixIn(
				"DELETE /orgs/kairos-io/packages/container/kairos%2Fhadron/versions/10?")))
			Expect(fake.recordedCalls()).To(Satisfy(hasPrefixIn(
				"DELETE /orgs/kairos-io/packages/container/kairos%2Fkairos-uki/versions/20?")))
		})
	})

	Context("when a version has multiple tags and any is protected", func() {
		BeforeEach(func() {
			// Post-promote: buildx imagetools create dedupes to one
			// version that carries both :master and :master-scratch
			// (same digest). The protected list covers :master, so the
			// whole version must stay -- deleting it would take :master
			// with it, exactly the concern master.yaml documented.
			fake.seedOrg("kairos-io", "kairos%2Fkairos-init", []pkgVersion{
				{ID: 100, Name: "sha256:promoted", CreatedAt: now.Add(-30 * 24 * time.Hour),
					Metadata: pkgMeta{Container: containerMeta{Tags: []string{"master", "master-scratch"}}}},
			})
		})

		It("keeps the version because any protected tag protects the whole", func() {
			specs := []packageSpec{{Ref: "ghcr.io/kairos-io/kairos/kairos-init", Owner: "kairos-io", EncPkg: "kairos%2Fkairos-init", Retention: 14 * 24 * time.Hour}}
			rpt, err := pruneAged(ctx, c, nil, specs, []string{"master"}, now, false)
			Expect(err).ToNot(HaveOccurred())
			Expect(rpt.Deleted).To(Equal(0))
			for _, c := range fake.recordedCalls() {
				Expect(c).ToNot(HavePrefix("DELETE "))
			}
		})
	})

	Context("with multi-arch children (manifest-list referencing pass)", func() {
		var mf *fakeManifestFetcher
		BeforeEach(func() {
			mf = newFakeManifestFetcher()
			// Post-promote package layout:
			//   - id=1 tagged ":master" (kept by protected pattern),
			//     manifest list referencing arch children A and B.
			//   - id=2 untagged, digest A (referenced by 1's list).
			//   - id=3 untagged, digest B (referenced by 1's list).
			//   - id=4 untagged, digest C (NOT referenced -- true
			//     orphan from an older master push whose tagged
			//     parent was already deleted).
			//   - id=5 tagged ":pr-old" (aged, unprotected), manifest
			//     list referencing D and E. Since this parent is
			//     being deleted this run, its children (id=6 D, id=7
			//     E) are NOT referenced and are aged, so they get
			//     deleted in the same pass.
			fake.seedOrg("kairos-io", "kairos%2Fkairos-init", []pkgVersion{
				{ID: 1, Name: "sha256:master-list", CreatedAt: now.Add(-30 * 24 * time.Hour),
					Metadata: pkgMeta{Container: containerMeta{Tags: []string{"master"}}}},
				{ID: 2, Name: "sha256:A", CreatedAt: now.Add(-30 * 24 * time.Hour),
					Metadata: pkgMeta{Container: containerMeta{Tags: nil}}},
				{ID: 3, Name: "sha256:B", CreatedAt: now.Add(-30 * 24 * time.Hour),
					Metadata: pkgMeta{Container: containerMeta{Tags: nil}}},
				{ID: 4, Name: "sha256:C", CreatedAt: now.Add(-30 * 24 * time.Hour),
					Metadata: pkgMeta{Container: containerMeta{Tags: nil}}},
				{ID: 5, Name: "sha256:pr-old-list", CreatedAt: now.Add(-30 * 24 * time.Hour),
					Metadata: pkgMeta{Container: containerMeta{Tags: []string{"pr-42"}}}},
				{ID: 6, Name: "sha256:D", CreatedAt: now.Add(-30 * 24 * time.Hour),
					Metadata: pkgMeta{Container: containerMeta{Tags: nil}}},
				{ID: 7, Name: "sha256:E", CreatedAt: now.Add(-30 * 24 * time.Hour),
					Metadata: pkgMeta{Container: containerMeta{Tags: nil}}},
			})
			mf.setChildren("kairos-io", "kairos%2Fkairos-init", "sha256:master-list",
				[]string{"sha256:A", "sha256:B"})
			mf.setChildren("kairos-io", "kairos%2Fkairos-init", "sha256:pr-old-list",
				[]string{"sha256:D", "sha256:E"})
		})

		It("keeps children of every tagged parent (both kept and aged-to-delete), prunes true orphans, defers aged-parent children to the next run", func() {
			specs := []packageSpec{{Ref: "ghcr.io/kairos-io/kairos/kairos-init", Owner: "kairos-io", EncPkg: "kairos%2Fkairos-init", Retention: 14 * 24 * time.Hour}}
			rpt, err := pruneAged(ctx, c, mf, specs, []string{"master"}, now, false)
			Expect(err).ToNot(HaveOccurred())
			// Delete this run: id=5 (aged tagged parent) + id=4 (true
			// orphan, no tagged parent references its digest).
			// Keep this run: id=1 (protected by :master), id=2/id=3
			// (referenced by kept parent id=1), and id=6/id=7 (still
			// referenced by aged parent id=5's manifest we fetched
			// this run -- they age out on the next prune when id=5
			// is gone and no one references them).
			Expect(rpt.Deleted).To(Equal(2))
			calls := fake.recordedCalls()
			for _, id := range []int{4, 5} {
				Expect(calls).To(Satisfy(hasPrefixIn(
					fmt.Sprintf("DELETE /orgs/kairos-io/packages/container/kairos%%2Fkairos-init/versions/%d?", id))))
			}
			for _, id := range []int{1, 2, 3, 6, 7} {
				Expect(calls).ToNot(Satisfy(hasPrefixIn(
					fmt.Sprintf("DELETE /orgs/kairos-io/packages/container/kairos%%2Fkairos-init/versions/%d?", id))))
			}
		})

		It("keeps an unreferenced untagged version when it is younger than the retention", func() {
			// Nudge the true-orphan (id=4) to be young enough that it
			// slips under the retention cutoff. Everything else
			// stays the same.
			fake.mu.Lock()
			for i := range fake.versions[fake.key("orgs", "kairos-io", "kairos%2Fkairos-init")] {
				if fake.versions[fake.key("orgs", "kairos-io", "kairos%2Fkairos-init")][i].ID == 4 {
					fake.versions[fake.key("orgs", "kairos-io", "kairos%2Fkairos-init")][i].CreatedAt = now.Add(-2 * time.Hour)
				}
			}
			fake.mu.Unlock()
			specs := []packageSpec{{Ref: "ghcr.io/kairos-io/kairos/kairos-init", Owner: "kairos-io", EncPkg: "kairos%2Fkairos-init", Retention: 14 * 24 * time.Hour}}
			rpt, err := pruneAged(ctx, c, mf, specs, []string{"master"}, now, false)
			Expect(err).ToNot(HaveOccurred())
			// id=4 no longer deleted (still aged<retention); id=5
			// stays (its children stay via the aged-parent fetch).
			Expect(rpt.Deleted).To(Equal(1))
			Expect(fake.recordedCalls()).ToNot(Satisfy(hasPrefixIn(
				"DELETE /orgs/kairos-io/packages/container/kairos%2Fkairos-init/versions/4?")))
		})

		It("dedupes manifest fetches by digest across tags that share it", func() {
			// A second tag pointing at the same manifest as :master.
			// The referencing pass should only fetch one manifest per
			// unique digest, so :master and :latest here -- same
			// digest -- collapse to one fetch.
			fake.mu.Lock()
			fake.versions[fake.key("orgs", "kairos-io", "kairos%2Fkairos-init")] = append(
				fake.versions[fake.key("orgs", "kairos-io", "kairos%2Fkairos-init")],
				pkgVersion{ID: 8, Name: "sha256:master-list", CreatedAt: now.Add(-30 * 24 * time.Hour),
					Metadata: pkgMeta{Container: containerMeta{Tags: []string{"latest"}}}},
			)
			fake.mu.Unlock()
			specs := []packageSpec{{Ref: "ghcr.io/kairos-io/kairos/kairos-init", Owner: "kairos-io", EncPkg: "kairos%2Fkairos-init", Retention: 14 * 24 * time.Hour}}
			_, err := pruneAged(ctx, c, mf, specs, []string{"master", "latest"}, now, false)
			Expect(err).ToNot(HaveOccurred())
			// Two unique digests total (master-list and pr-old-list);
			// master-list shows up on both id=1 and id=8 but should
			// fetch once.
			mfetched := mf.fetchedDigests("kairos-io", "kairos%2Fkairos-init")
			Expect(mfetched).To(ConsistOf("sha256:master-list", "sha256:pr-old-list"))
		})

		It("fails closed: skips untagged pruning for the whole package if any manifest fetch fails", func() {
			// Break the fetch for the kept parent's manifest. Without
			// its children (A, B), we cannot tell them from true orphans
			// like C, so a naive prune would delete them and break
			// `docker pull` of :master. Fail-closed keeps all untagged
			// versions of this package this run instead. The aged
			// tagged parent still gets deleted -- the tagged pass does
			// not depend on the referenced set.
			mf.failFor("kairos-io", "kairos%2Fkairos-init", "sha256:master-list")
			specs := []packageSpec{{Ref: "ghcr.io/kairos-io/kairos/kairos-init", Owner: "kairos-io", EncPkg: "kairos%2Fkairos-init", Retention: 14 * 24 * time.Hour}}
			rpt, err := pruneAged(ctx, c, mf, specs, []string{"master"}, now, false)
			Expect(err).ToNot(HaveOccurred())
			// Delete: only id=5 (aged tagged, unprotected). No
			// untagged prunes because the referenced set is
			// incomplete.
			Expect(rpt.Deleted).To(Equal(1))
			calls := fake.recordedCalls()
			Expect(calls).To(Satisfy(hasPrefixIn(
				"DELETE /orgs/kairos-io/packages/container/kairos%2Fkairos-init/versions/5?")))
			for _, id := range []int{2, 3, 4, 6, 7} {
				Expect(calls).ToNot(Satisfy(hasPrefixIn(
					fmt.Sprintf("DELETE /orgs/kairos-io/packages/container/kairos%%2Fkairos-init/versions/%d?", id))))
			}
		})
	})

	Context("when the API rate-limits us mid-delete", func() {
		BeforeEach(func() {
			// Three aged unprotected versions; the fake will return
			// 429 on the second DELETE.
			fake.seedOrg("kairos-io", "kairos%2Fhadron", []pkgVersion{
				{ID: 1, Name: "sha256:1", CreatedAt: now.Add(-30 * 24 * time.Hour),
					Metadata: pkgMeta{Container: containerMeta{Tags: []string{"a"}}}},
				{ID: 2, Name: "sha256:2", CreatedAt: now.Add(-30 * 24 * time.Hour),
					Metadata: pkgMeta{Container: containerMeta{Tags: []string{"b"}}}},
				{ID: 3, Name: "sha256:3", CreatedAt: now.Add(-30 * 24 * time.Hour),
					Metadata: pkgMeta{Container: containerMeta{Tags: []string{"c"}}}},
			})
			fake.rateLimitAfterDeletes = 1
		})

		It("stops cleanly (no Failed count) and leaves the rest to the next run", func() {
			specs := []packageSpec{{Ref: "ghcr.io/kairos-io/kairos/hadron", Owner: "kairos-io", EncPkg: "kairos%2Fhadron", Retention: 14 * 24 * time.Hour}}
			rpt, err := pruneAged(ctx, c, nil, specs, nil, now, false)
			Expect(err).ToNot(HaveOccurred())
			// One delete succeeded, one hit the rate limit and stopped
			// the run. Failed stays 0 -- a rate limit is not a real
			// failure, just backpressure.
			Expect(rpt.Deleted).To(Equal(1))
			Expect(rpt.Failed).To(Equal(0))
		})
	})
})

// fakeManifestFetcher implements manifestFetcher against a canned
// per-digest children map, and records which (owner, pkg, digest)
// tuples were fetched so the dedupe / failure-mode specs can assert.
// Setting a digest to `errChildren` sentinel makes the fetch fail so
// the fail-closed behavior can be exercised.
var errChildren = []string{"__fail__"}

type fakeManifestFetcher struct {
	mu       sync.Mutex
	children map[string][]string // key = owner|pkg|digest
	fetched  map[string][]string // key = owner|pkg, value = digests fetched
}

func newFakeManifestFetcher() *fakeManifestFetcher {
	return &fakeManifestFetcher{
		children: map[string][]string{},
		fetched:  map[string][]string{},
	}
}

func (f *fakeManifestFetcher) setChildren(owner, pkg, digest string, children []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.children[owner+"|"+pkg+"|"+digest] = children
}

func (f *fakeManifestFetcher) failFor(owner, pkg, digest string) {
	f.setChildren(owner, pkg, digest, errChildren)
}

func (f *fakeManifestFetcher) referencedDigests(ctx context.Context, owner, pkg, digest string) ([]string, error) {
	f.mu.Lock()
	f.fetched[owner+"|"+pkg] = append(f.fetched[owner+"|"+pkg], digest)
	c := f.children[owner+"|"+pkg+"|"+digest]
	f.mu.Unlock()
	if len(c) == 1 && c[0] == errChildren[0] {
		return nil, fmt.Errorf("simulated manifest fetch failure for %s", digest)
	}
	return c, nil
}

func (f *fakeManifestFetcher) fetchedDigests(owner, pkg string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.fetched[owner+"|"+pkg]))
	copy(out, f.fetched[owner+"|"+pkg])
	return out
}
