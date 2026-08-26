package debugbundle

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
)

// Server serves a single bundle file behind a random, unguessable path token.
type Server struct {
	server *http.Server
	token  string
	base   string
	port   int
}

// Serve starts an HTTP server on an ephemeral port that serves bundlePath at
// /<token>/<basename>. Every other path returns 404. Call Close to stop it.
func Serve(bundlePath string) (*Server, error) {
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		return nil, err
	}
	base := filepath.Base(bundlePath)
	urlPath := "/" + token + "/" + base

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != urlPath {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, bundlePath)
	})

	srv := &http.Server{Handler: mux}
	s := &Server{
		server: srv,
		token:  token,
		base:   base,
		port:   ln.Addr().(*net.TCPAddr).Port,
	}
	go func() { _ = srv.Serve(ln) }()
	return s, nil
}

func randomToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// URLs returns a curl-ready URL for each usable local IPv4 address.
func (s *Server) URLs() []string {
	var urls []string
	for _, ip := range localIPv4s() {
		urls = append(urls, fmt.Sprintf("http://%s:%d/%s/%s", ip, s.port, s.token, s.base))
	}
	return urls
}

// Port returns the listening port.
func (s *Server) Port() int { return s.port }

// Token returns the random path token.
func (s *Server) Token() string { return s.token }

// Base returns the bundle file's base name.
func (s *Server) Base() string { return s.base }

// Close stops the server.
func (s *Server) Close() error { return s.server.Close() }

func localIPv4s() []string {
	var ips []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ips
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		if v4 := ipnet.IP.To4(); v4 != nil {
			ips = append(ips, v4.String())
		}
	}
	return ips
}
