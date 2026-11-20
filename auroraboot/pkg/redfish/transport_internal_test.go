package redfish

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDefensiveClientRejectsCrossOriginRedirect verifies that the injected HTTP
// client refuses a redirect that crosses to a different host, so a hostile BMC
// cannot bounce our credentialed client elsewhere.
func TestDefensiveClientRejectsCrossOriginRedirect(t *testing.T) {
	// other is the "attacker" origin the BMC tries to redirect us to.
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "should never be reached")
	}))
	defer other.Close()

	// bmc redirects every request to the other origin.
	bmc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/evil", http.StatusFound)
	}))
	defer bmc.Close()

	client := newDefensiveHTTPClient(true) // insecure=true: plaintext httptest server
	resp, err := client.Get(bmc.URL + "/redfish/v1/")
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil {
		t.Fatalf("expected cross-origin redirect to be rejected, got no error")
	}
	if !strings.Contains(err.Error(), "cross-origin redirect") {
		t.Fatalf("expected a cross-origin redirect error, got: %v", err)
	}
}

// TestDefensiveClientAllowsSameOriginRedirect verifies the redirect guard does not
// break legitimate same-origin redirects (some BMCs redirect within themselves).
func TestDefensiveClientAllowsSameOriginRedirect(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/end", http.StatusFound)
			return
		}
		hits++
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	client := newDefensiveHTTPClient(true)
	resp, err := client.Get(srv.URL + "/start")
	if err != nil {
		t.Fatalf("same-origin redirect must be allowed, got: %v", err)
	}
	_ = resp.Body.Close()
	if hits != 1 {
		t.Fatalf("expected the same-origin redirect to be followed once, got %d hits", hits)
	}
}

// TestDefensiveClientCapsResponseBody verifies an oversized BMC response body is
// rejected rather than read unbounded into memory.
func TestDefensiveClientCapsResponseBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Stream well past the cap. We don't actually allocate it all at once;
		// io.Copy from a limited reader keeps the server side cheap.
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.Copy(w, io.LimitReader(neverEnding{}, maxBMCResponseBytes+1024))
	}))
	defer srv.Close()

	client := newDefensiveHTTPClient(true)
	resp, err := client.Get(srv.URL + "/big")
	if err != nil {
		t.Fatalf("request itself should succeed; the cap applies on read: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	_, err = io.ReadAll(resp.Body)
	if err == nil {
		t.Fatalf("expected reading an oversized body to error at the cap, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds the") {
		t.Fatalf("expected a body-cap error, got: %v", err)
	}
}

// TestDefensiveClientUnderCapReadsFully verifies a normal-sized body is read in
// full without tripping the cap.
func TestDefensiveClientUnderCapReadsFully(t *testing.T) {
	const payload = `{"ok":true}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, payload)
	}))
	defer srv.Close()

	client := newDefensiveHTTPClient(true)
	resp, err := client.Get(srv.URL + "/small")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading a small body must not error: %v", err)
	}
	if string(b) != payload {
		t.Fatalf("body mismatch: got %q want %q", string(b), payload)
	}
}

// neverEnding is an io.Reader that yields zero bytes forever; wrap it in an
// io.LimitReader to produce a body of a chosen size cheaply.
type neverEnding struct{}

func (neverEnding) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'a'
	}
	return len(p), nil
}
