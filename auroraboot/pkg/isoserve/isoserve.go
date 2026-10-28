// Package isoserve serves a single pre-resolved file per opaque, expiring token
// over HTTP(S). It exists so AuroraBoot can hand a BMC a URL the BMC can fetch
// for a Redfish VirtualMedia.InsertMedia (URL-pull) deployment: a BMC cannot
// present AuroraBoot's admin/node auth, so the capability lives entirely in the
// unguessable token.
//
// The server is deliberately minimal and structurally immune to path traversal
// and directory listing: each token maps to exactly one absolute path that was
// resolved when the token was minted. The request path after the token is never
// joined onto a filesystem path — the token is the only capability, so there is
// nothing to traverse. It runs on its own http.Server and http.ServeMux and
// never touches http.DefaultServeMux.
package isoserve

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// tokenBytes is the number of crypto/rand bytes behind each token. 32 bytes of
// entropy is far beyond brute-forceable for a short-lived capability URL.
const tokenBytes = 32

// sweepInterval is how often the background sweeper reaps expired entries.
const sweepInterval = 30 * time.Second

// routePrefix is the fixed path prefix every served URL carries. The handler is
// registered on "<prefix>/" and matches "<prefix>/{token}/{basename}".
const routePrefix = "/redfish/iso"

// Config configures a Server.
type Config struct {
	// BaseURL is the advertised base URL the BMC will reach (scheme://host[:port]),
	// without a trailing slash. Register builds capability URLs from it. It may
	// differ from the bind address (e.g. the BMC management network differs from
	// the UI network).
	BaseURL string
	// BindAddr is the address the HTTP server listens on (e.g. ":8090" or
	// "10.0.0.5:8090"). It is intentionally not defaulted to 0.0.0.0 by this
	// package; callers must choose it explicitly.
	BindAddr string
	// CertFile and KeyFile, when both set, enable TLS (the opt-in HTTPS posture).
	CertFile string
	KeyFile  string
}

// entry is a single token→file binding.
type entry struct {
	absPath string
	expiry  time.Time
}

// Server serves tokenized single-file downloads.
type Server struct {
	baseURL  string
	bindAddr string
	certFile string
	keyFile  string

	mu      sync.RWMutex
	entries map[string]entry

	httpSrv *http.Server
	addr    net.Addr

	stop chan struct{}
	wg   sync.WaitGroup
}

// New builds a Server from cfg. It does not start listening; call Start.
func New(cfg Config) *Server {
	return &Server{
		baseURL:  strings.TrimRight(cfg.BaseURL, "/"),
		bindAddr: cfg.BindAddr,
		certFile: cfg.CertFile,
		keyFile:  cfg.KeyFile,
		entries:  make(map[string]entry),
		stop:     make(chan struct{}),
	}
}

// Register binds absPath to a fresh opaque token valid for ttl and returns the
// full advertised URL plus the token. absPath must be an absolute path to an
// existing regular file. The returned URL is trusted by construction (the token
// is unguessable and the path is fixed at mint time), so it does not need SSRF
// re-validation.
func (s *Server) Register(absPath string, ttl time.Duration) (url, token string, err error) {
	return s.RegisterAt(s.baseURL, absPath, ttl)
}

// RegisterAt is Register with the returned URL built on baseURL instead of the
// launch BaseURL, for when the BMC reaches this listener through a different
// advertised address (e.g. a reverse proxy). An empty baseURL falls back to the
// launch BaseURL. baseURL is not validated here; callers own that.
func (s *Server) RegisterAt(baseURL, absPath string, ttl time.Duration) (url, token string, err error) {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		baseURL = s.baseURL
	}
	if !filepath.IsAbs(absPath) {
		return "", "", fmt.Errorf("registering iso: path %q is not absolute", absPath)
	}
	clean := filepath.Clean(absPath)
	info, err := os.Stat(clean)
	if err != nil {
		return "", "", fmt.Errorf("registering iso: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", "", fmt.Errorf("registering iso: %q is not a regular file", clean)
	}
	if ttl <= 0 {
		return "", "", errors.New("registering iso: ttl must be positive")
	}

	tok, err := newToken()
	if err != nil {
		return "", "", fmt.Errorf("registering iso: generating token: %w", err)
	}

	s.mu.Lock()
	s.entries[tok] = entry{absPath: clean, expiry: time.Now().Add(ttl)}
	s.mu.Unlock()

	base := path.Base(clean)
	url = fmt.Sprintf("%s%s/%s/%s", baseURL, routePrefix, tok, base)
	return url, tok, nil
}

// Extend moves a live token's expiry to ttl from now. It reports false, and
// changes nothing, when the token is unknown, revoked or already expired.
func (s *Server) Extend(token string, ttl time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[token]
	if !ok || time.Now().After(e.expiry) {
		return false
	}
	e.expiry = time.Now().Add(ttl)
	s.entries[token] = e
	return true
}

// Revoke immediately invalidates a token. It is safe to call with an unknown or
// already-revoked token.
func (s *Server) Revoke(token string) {
	s.mu.Lock()
	delete(s.entries, token)
	s.mu.Unlock()
}

// Start begins listening and serving in the background and launches the sweeper.
// It returns once the listener is bound (or immediately for the goroutine model).
// Shutdown stops it. The supplied context is used to derive the listener context.
func (s *Server) Start(ctx context.Context) error {
	if s.bindAddr == "" {
		return errors.New("starting iso-serve: bind address is required")
	}
	// A half-configured pair is a misconfiguration, not a request for plain
	// HTTP. The capability token lives in the URL, so falling back to the
	// cleartext branch below would put it on the wire for the whole deploy,
	// silently and with an unchanged startup banner. Fail closed here rather
	// than in each command, so no caller can downgrade by omission.
	if s.certFile == "" && s.keyFile != "" {
		return errors.New("starting iso-serve: a TLS key was given without a certificate; set both or neither, because serving the capability token over plain HTTP would expose it")
	}
	if s.certFile != "" && s.keyFile == "" {
		return errors.New("starting iso-serve: a TLS certificate was given without a key; set both or neither, because serving the capability token over plain HTTP would expose it")
	}

	s.httpSrv = &http.Server{
		Addr:    s.bindAddr,
		Handler: s.Handler(),
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	// Load the pair before binding. ServeTLS only reads it inside the serve
	// goroutine, where a failure has no way back to the caller and would leave
	// a bound port that accepts connections and never answers them.
	if s.UsesTLS() {
		pair, err := tls.LoadX509KeyPair(s.certFile, s.keyFile)
		if err != nil {
			return fmt.Errorf("starting iso-serve: loading TLS certificate %s and key %s: %w", s.certFile, s.keyFile, err)
		}
		s.httpSrv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{pair}}
	}

	ln, err := net.Listen("tcp", s.bindAddr)
	if err != nil {
		return fmt.Errorf("starting iso-serve: listening on %s: %w", s.bindAddr, err)
	}
	s.addr = ln.Addr()

	s.wg.Add(2)
	go s.sweep()
	go func() {
		defer s.wg.Done()
		var serveErr error
		if s.UsesTLS() {
			serveErr = s.httpSrv.ServeTLS(ln, "", "")
		} else {
			serveErr = s.httpSrv.Serve(ln)
		}
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			// The listener died unexpectedly. There is no error channel back to
			// the caller after Start returns, so record nothing sensitive and let
			// Shutdown observe the closed server.
			_ = serveErr
		}
	}()

	return nil
}

// Shutdown gracefully stops the HTTP server and the sweeper.
func (s *Server) Shutdown(ctx context.Context) error {
	close(s.stop)
	var err error
	if s.httpSrv != nil {
		err = s.httpSrv.Shutdown(ctx)
	}
	s.wg.Wait()
	if err != nil {
		return fmt.Errorf("shutting down iso-serve: %w", err)
	}
	return nil
}

// Addr returns the address the server is listening on after Start. It is mainly
// useful when BindAddr used port 0 to get an OS-assigned port. It returns nil
// before Start.
func (s *Server) Addr() net.Addr {
	return s.addr
}

// UsesTLS reports whether the server serves over HTTPS, i.e. both a cert and a
// key were configured. Callers use this to keep an InsertMedia transfer protocol
// (and any advertised URL scheme) consistent with how the ISO is actually served.
func (s *Server) UsesTLS() bool {
	return s.certFile != "" && s.keyFile != ""
}

// Handler returns an http.Handler serving the tokenized route. It is exposed so
// the route behaviour can be exercised with httptest.NewServer without binding a
// real listener; the lifecycle (Start/Shutdown) is independent of it.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(routePrefix+"/", s.handle)
	return mux
}

// handle serves one token-bound file. The token is the only capability: the
// request path after the token is ignored for filesystem purposes, so a "../"
// in it cannot escape the single bound file.
func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tok := tokenFromPath(r.URL.Path)
	if tok == "" {
		http.NotFound(w, r)
		return
	}

	s.mu.RLock()
	e, ok := s.entries[tok]
	s.mu.RUnlock()
	if !ok || time.Now().After(e.expiry) {
		http.NotFound(w, r)
		return
	}

	f, err := os.Open(e.absPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}

	// http.ServeContent gives us Range support and correct conditional handling.
	http.ServeContent(w, r, path.Base(e.absPath), info.ModTime(), f)
}

// sweep periodically reaps expired entries so revoked/expired tokens don't
// accumulate.
func (s *Server) sweep() {
	defer s.wg.Done()
	t := time.NewTicker(sweepInterval)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			now := time.Now()
			s.mu.Lock()
			for tok, e := range s.entries {
				if now.After(e.expiry) {
					delete(s.entries, tok)
				}
			}
			s.mu.Unlock()
		}
	}
}

// tokenFromPath extracts the {token} segment from "<routePrefix>/{token}/...".
// It returns "" when the path does not match the expected shape.
func tokenFromPath(p string) string {
	rest := strings.TrimPrefix(p, routePrefix+"/")
	if rest == p {
		return ""
	}
	// The token is the first path segment; anything after it is ignored.
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		return rest[:i]
	}
	return rest
}

// newToken returns a base64url-encoded crypto/rand token.
func newToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
