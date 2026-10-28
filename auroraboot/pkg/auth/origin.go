package auth

import (
	"log"
	"net/http"
	"net/url"
	"strings"
)

// OriginAllowed reports whether a request's Origin header names this server.
// Browsers send Origin on WebSocket upgrades and on cross-origin and
// state-changing requests; agents and the SDK do not, so a request without one
// is allowed. With one, its host and port must equal the request's Host, or
// the host of publicURL for a reverse proxy that rewrites Host.
func OriginAllowed(r *http.Request, publicURL string) bool {
	if originAllowed(r, publicURL) {
		return true
	}
	log.Printf("refused a request from Origin %q: it matches neither the request Host %q nor the public URL host %q (behind a reverse proxy that rewrites Host, set --url / AURORABOOT_URL to the address the browser uses)",
		r.Header.Get("Origin"), r.Host, publicURLHost(publicURL))
	return false
}

func originAllowed(r *http.Request, publicURL string) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}
	if publicURL == "" {
		return false
	}
	pub, err := url.Parse(publicURL)
	if err != nil || pub.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, pub.Host)
}

// publicURLHost returns the host of publicURL, or "" when it is unset or
// unparsable.
func publicURLHost(publicURL string) string {
	u, err := url.Parse(publicURL)
	if err != nil {
		return ""
	}
	return u.Host
}
