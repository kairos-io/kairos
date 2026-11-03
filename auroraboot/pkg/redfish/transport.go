package redfish

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// maxBMCResponseBytes caps the size of a single Redfish response body we will read
// from a BMC. A compromised or malfunctioning BMC is in our threat model; without
// a cap it could stream an unbounded body and OOM the process. Redfish documents
// are small JSON resources, so 8 MiB is generously above any legitimate response
// while still bounding a hostile one.
const maxBMCResponseBytes int64 = 8 << 20 // 8 MiB

// newDefensiveHTTPClient builds the *http.Client injected into gofish's
// ClientConfig.HTTPClient. It hardens the transport against a hostile BMC in two
// ways:
//
//  1. CheckRedirect rejects any cross-origin redirect, so the BMC cannot bounce
//     our authenticated client (carrying an X-Auth-Token) to a different host —
//     an SSRF / credential-exfiltration vector.
//  2. The RoundTripper wraps each response body in a size-capped reader so a
//     hostile BMC cannot make us read an unbounded body into memory.
//
// TLS verification is configured here (not by gofish): because we inject a custom
// RoundTripper rather than a bare *http.Transport, gofish's own InsecureSkipVerify
// plumbing does not apply to it, so we set NoModifyTransport and own the tls.Config
// ourselves. insecure mirrors ClientConfig.Insecure (the inverse of VerifySSL), so
// the existing TLS-verify-by-default behaviour is preserved exactly.
func newDefensiveHTTPClient(insecure bool) *http.Client {
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.TLSClientConfig = &tls.Config{
		// MinVersion pins a sane floor; InsecureSkipVerify is the inverse of
		// VerifySSL and stays off unless the caller explicitly opted out.
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: insecure, //nolint:gosec // gated by the VerifySSL flag; default is verify-on.
	}

	return &http.Client{
		Transport: &cappingTransport{base: base},
		// CheckRedirect rejects cross-origin redirects. Same-origin redirects are
		// allowed (the default 10-hop cap still applies via len(via)).
		CheckRedirect: rejectCrossOriginRedirect,
		// Deploys are long-lived (a Task can poll for minutes); rely on context for
		// cancellation rather than a coarse wall-clock Timeout here.
	}
}

// rejectCrossOriginRedirect is an http.Client.CheckRedirect that fails any
// redirect whose target host (scheme+host) differs from the previous request's.
// This stops a BMC from redirecting our credentialed client to an attacker-chosen
// origin.
func rejectCrossOriginRedirect(req *http.Request, via []*http.Request) error {
	if len(via) == 0 {
		return nil
	}
	if len(via) >= 10 {
		return fmt.Errorf("stopped after %d redirects", len(via))
	}
	prev := via[len(via)-1].URL
	if !strings.EqualFold(req.URL.Scheme, prev.Scheme) || !strings.EqualFold(req.URL.Host, prev.Host) {
		return fmt.Errorf("refusing cross-origin redirect from %s to %s://%s (possible hostile BMC)",
			prev.Host, req.URL.Scheme, req.URL.Host)
	}
	return nil
}

// cappingTransport wraps a base RoundTripper and replaces each response body with
// a size-capped reader, bounding how much a hostile BMC can make us read.
type cappingTransport struct {
	base http.RoundTripper
}

func (t *cappingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return resp, err
	}
	if resp.Body != nil {
		resp.Body = &cappedReadCloser{
			r:      io.LimitReader(resp.Body, maxBMCResponseBytes+1),
			closer: resp.Body,
			limit:  maxBMCResponseBytes,
		}
	}
	return resp, nil
}

// cappedReadCloser reads at most limit bytes and then returns an error instead of
// continuing, so an oversized body is rejected rather than buffered in full. It
// reads limit+1 bytes underneath (via the LimitReader) purely to detect overflow.
type cappedReadCloser struct {
	r      io.Reader
	closer io.Closer
	limit  int64
	read   int64
}

func (c *cappedReadCloser) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.read += int64(n)
	if c.read > c.limit {
		return n, fmt.Errorf("BMC response body exceeds the %d-byte cap (possible hostile BMC)", c.limit)
	}
	return n, err
}

func (c *cappedReadCloser) Close() error {
	if c.closer != nil {
		return c.closer.Close()
	}
	return nil
}
