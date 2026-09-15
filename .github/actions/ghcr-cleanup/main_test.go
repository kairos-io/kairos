package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeGHCR is a stand-in for the subset of the GitHub packages REST API
// that the cleanup tool uses. It records every request and lets each
// test seed the version list plus whether the /orgs or /users endpoint
// is the "real" owner. Deleted versions drop out of subsequent listings.
type fakeGHCR struct {
	mu sync.Mutex

	// owner -> (org|user), i.e. which scope answers 2xx on GET.
	scopeOf map[string]string
	// (scope|owner|encodedPkg) -> versions still present.
	versions map[string][]pkgVersion
	// If deleteFailsFor contains a versionID (as int), DELETE returns 403.
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
		// (net/http decodes r.URL.Path but preserves r.URL.EscapedPath()).
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
		// Use EscapedPath so %2F inside a nested {pkg} stays a literal
		// segment (matching how the real API addresses these packages).
		parts := strings.Split(strings.TrimPrefix(r.URL.EscapedPath(), "/"), "/")
		if len(parts) < 6 || parts[2] != "packages" || parts[3] != "container" || parts[5] != "versions" {
			http.NotFound(w, r)
			return
		}
		scope := parts[0]
		owner := parts[1]
		pkg := parts[4]
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
			// Naive pagination: honor per_page + page. Total-count header
			// is not required by the client, so skip it.
			perPage := 100
			page := 1
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

func testClient(t *testing.T, srv *httptest.Server) *client {
	t.Helper()
	return &client{
		apiBase: srv.URL,
		token:   "test-token",
		http:    srv.Client(),
	}
}

func TestDeleteRefs_OrgSingleTag(t *testing.T) {
	fake := newFakeGHCR()
	fake.seedOrg("kairos-io", "kairos%2Fkairos-uki", []pkgVersion{
		{ID: 111, Metadata: pkgMeta{Container: containerMeta{Tags: []string{"sometag-abc123"}}}},
		{ID: 222, Metadata: pkgMeta{Container: containerMeta{Tags: []string{"other-tag"}}}},
	})
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	report, err := deleteRefs(t.Context(), testClient(t, srv),
		[]string{"ghcr.io/kairos-io/kairos/kairos-uki:sometag-abc123"})
	if err != nil {
		t.Fatalf("deleteRefs returned error: %v", err)
	}
	if report.Deleted != 1 || report.Failed != 0 {
		t.Fatalf("want 1 deleted / 0 failed, got %+v", report)
	}

	calls := fake.recordedCalls()
	wantGET := "GET /orgs/kairos-io/packages/container/kairos%2Fkairos-uki/versions?"
	wantDEL := "DELETE /orgs/kairos-io/packages/container/kairos%2Fkairos-uki/versions/111?"
	if !containsPrefix(calls, wantGET) {
		t.Errorf("missing GET call, got: %v", calls)
	}
	if !containsPrefix(calls, wantDEL) {
		t.Errorf("missing DELETE call, got: %v", calls)
	}
	if containsPrefix(calls, "DELETE /orgs/kairos-io/packages/container/kairos%2Fkairos-uki/versions/222?") {
		t.Errorf("unrelated version 222 should not have been deleted, got: %v", calls)
	}
}

func TestDeleteRefs_UserFallback(t *testing.T) {
	fake := newFakeGHCR()
	fake.seedUser("alice", "mypkg", []pkgVersion{
		{ID: 999, Metadata: pkgMeta{Container: containerMeta{Tags: []string{"pr-42"}}}},
	})
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	report, err := deleteRefs(t.Context(), testClient(t, srv),
		[]string{"ghcr.io/alice/mypkg:pr-42"})
	if err != nil {
		t.Fatalf("deleteRefs returned error: %v", err)
	}
	if report.Deleted != 1 {
		t.Fatalf("want 1 deleted, got %+v", report)
	}
	calls := fake.recordedCalls()
	if !containsPrefix(calls, "GET /orgs/alice/packages/container/mypkg/versions?") {
		t.Errorf("expected org endpoint tried first, got: %v", calls)
	}
	if !containsPrefix(calls, "GET /users/alice/packages/container/mypkg/versions?") {
		t.Errorf("expected user endpoint fallback, got: %v", calls)
	}
	if !containsPrefix(calls, "DELETE /users/alice/packages/container/mypkg/versions/999?") {
		t.Errorf("expected DELETE on user endpoint, got: %v", calls)
	}
}

func TestDeleteRefs_MissingTag_WarnsNoDelete(t *testing.T) {
	fake := newFakeGHCR()
	fake.seedOrg("kairos-io", "kairos%2Fkairos-uki", []pkgVersion{
		{ID: 111, Metadata: pkgMeta{Container: containerMeta{Tags: []string{"other-tag"}}}},
	})
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	report, err := deleteRefs(t.Context(), testClient(t, srv),
		[]string{"ghcr.io/kairos-io/kairos/kairos-uki:not-there"})
	if err != nil {
		t.Fatalf("deleteRefs returned error: %v", err)
	}
	if report.Deleted != 0 || report.Failed != 0 || report.NotFound != 1 {
		t.Fatalf("want 0/0/1 for deleted/failed/notfound, got %+v", report)
	}
	for _, c := range fake.recordedCalls() {
		if strings.HasPrefix(c, "DELETE ") {
			t.Errorf("no DELETE should have fired, got: %v", c)
		}
	}
}

func TestDeleteRefs_MissingPackage_Warns(t *testing.T) {
	fake := newFakeGHCR() // no seeded packages -- everything 404s.
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	report, err := deleteRefs(t.Context(), testClient(t, srv),
		[]string{"ghcr.io/kairos-io/kairos/does-not-exist:v1"})
	if err != nil {
		t.Fatalf("deleteRefs returned error: %v", err)
	}
	if report.Deleted != 0 || report.Failed != 0 || report.Unresolved != 1 {
		t.Fatalf("want 0/0/1 for deleted/failed/unresolved, got %+v", report)
	}
}

func TestDeleteRefs_DeleteFailure_ReportsFail(t *testing.T) {
	fake := newFakeGHCR()
	fake.seedOrg("kairos-io", "kairos%2Fkairos-uki", []pkgVersion{
		{ID: 111, Metadata: pkgMeta{Container: containerMeta{Tags: []string{"sometag-abc123"}}}},
	})
	fake.failDelete(111)
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	report, err := deleteRefs(t.Context(), testClient(t, srv),
		[]string{"ghcr.io/kairos-io/kairos/kairos-uki:sometag-abc123"})
	if err != nil {
		t.Fatalf("deleteRefs returned error: %v", err)
	}
	if report.Failed != 1 {
		t.Fatalf("want 1 failed, got %+v", report)
	}
}

func TestDeleteRefs_MultipleRefs(t *testing.T) {
	fake := newFakeGHCR()
	fake.seedOrg("kairos-io", "kairos%2Fkairos-uki", []pkgVersion{
		{ID: 111, Metadata: pkgMeta{Container: containerMeta{Tags: []string{"a"}}}},
		{ID: 222, Metadata: pkgMeta{Container: containerMeta{Tags: []string{"b"}}}},
		{ID: 333, Metadata: pkgMeta{Container: containerMeta{Tags: []string{"c"}}}},
	})
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	report, err := deleteRefs(t.Context(), testClient(t, srv), []string{
		"ghcr.io/kairos-io/kairos/kairos-uki:a",
		"ghcr.io/kairos-io/kairos/kairos-uki:c",
	})
	if err != nil {
		t.Fatalf("deleteRefs returned error: %v", err)
	}
	if report.Deleted != 2 {
		t.Fatalf("want 2 deleted, got %+v", report)
	}
	calls := fake.recordedCalls()
	if !containsPrefix(calls, "DELETE /orgs/kairos-io/packages/container/kairos%2Fkairos-uki/versions/111?") {
		t.Errorf("missing DELETE 111, got: %v", calls)
	}
	if !containsPrefix(calls, "DELETE /orgs/kairos-io/packages/container/kairos%2Fkairos-uki/versions/333?") {
		t.Errorf("missing DELETE 333, got: %v", calls)
	}
}

func TestDeleteRefs_TagShared_DeletesEveryMatchingVersion(t *testing.T) {
	// buildx imagetools create can leave two package versions carrying
	// the same tag; the API only lets us delete by version id, so a
	// tag-scoped cleanup has to walk every version.
	fake := newFakeGHCR()
	fake.seedOrg("kairos-io", "kairos%2Fkairos-uki", []pkgVersion{
		{ID: 111, Metadata: pkgMeta{Container: containerMeta{Tags: []string{"same"}}}},
		{ID: 222, Metadata: pkgMeta{Container: containerMeta{Tags: []string{"same", "extra"}}}},
	})
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	report, err := deleteRefs(t.Context(), testClient(t, srv),
		[]string{"ghcr.io/kairos-io/kairos/kairos-uki:same"})
	if err != nil {
		t.Fatalf("deleteRefs returned error: %v", err)
	}
	if report.Deleted != 2 {
		t.Fatalf("want 2 deleted, got %+v", report)
	}
}

func TestDeleteRefs_Pagination(t *testing.T) {
	fake := newFakeGHCR()
	// 150 versions; only the one on page 2 carries the target tag.
	versions := make([]pkgVersion, 0, 150)
	for i := 1; i <= 150; i++ {
		tags := []string{fmt.Sprintf("noise-%d", i)}
		if i == 120 {
			tags = []string{"target"}
		}
		versions = append(versions, pkgVersion{ID: i, Metadata: pkgMeta{Container: containerMeta{Tags: tags}}})
	}
	fake.seedOrg("kairos-io", "kairos%2Fkairos-uki", versions)
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	report, err := deleteRefs(t.Context(), testClient(t, srv),
		[]string{"ghcr.io/kairos-io/kairos/kairos-uki:target"})
	if err != nil {
		t.Fatalf("deleteRefs returned error: %v", err)
	}
	if report.Deleted != 1 {
		t.Fatalf("want 1 deleted, got %+v", report)
	}
	calls := fake.recordedCalls()
	if !containsSubstring(calls, "page=2") {
		t.Errorf("expected pagination to fetch page=2, got: %v", calls)
	}
}

func TestParseRefs_SkipsBlanksAndComments(t *testing.T) {
	got := parseRefs(strings.Join([]string{
		"",
		"# comment",
		"  ghcr.io/kairos-io/kairos/kairos-uki:a  ",
		"ghcr.io/kairos-io/kairos/kairos-uki:b # trailing",
		"",
	}, "\n"))
	want := []string{
		"ghcr.io/kairos-io/kairos/kairos-uki:a",
		"ghcr.io/kairos-io/kairos/kairos-uki:b",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("parseRefs mismatch: want %v, got %v", want, got)
	}
}

func TestParseImageRef(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		wantOwner  string
		wantPkg    string
		wantTag    string
		wantErr    bool
		wantErrMsg string
	}{
		{
			name:      "simple two-segment package",
			in:        "ghcr.io/alice/mypkg:pr-42",
			wantOwner: "alice", wantPkg: "mypkg", wantTag: "pr-42",
		},
		{
			name:      "nested package path is url-encoded",
			in:        "ghcr.io/kairos-io/kairos/kairos-uki:tag",
			wantOwner: "kairos-io", wantPkg: "kairos%2Fkairos-uki", wantTag: "tag",
		},
		{
			name: "non-ghcr.io is rejected", in: "quay.io/foo/bar:v1",
			wantErr: true, wantErrMsg: "not a ghcr.io",
		},
		{
			name: "missing tag is rejected", in: "ghcr.io/foo/bar",
			wantErr: true, wantErrMsg: "missing :tag",
		},
		{
			name: "empty tag is rejected", in: "ghcr.io/foo/bar:",
			wantErr: true, wantErrMsg: "missing :tag",
		},
		{
			name: "missing package is rejected", in: "ghcr.io/foo:tag",
			wantErr: true, wantErrMsg: "missing package",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, pkg, tag, err := parseImageRef(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want error, got owner=%q pkg=%q tag=%q", owner, pkg, tag)
				}
				if !strings.Contains(err.Error(), tt.wantErrMsg) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.wantErrMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if owner != tt.wantOwner || pkg != tt.wantPkg || tag != tt.wantTag {
				t.Errorf("want %q/%q/%q, got %q/%q/%q",
					tt.wantOwner, tt.wantPkg, tt.wantTag,
					owner, pkg, tag)
			}
		})
	}
}

func containsPrefix(haystack []string, prefix string) bool {
	for _, s := range haystack {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

func containsSubstring(haystack []string, needle string) bool {
	for _, s := range haystack {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}
