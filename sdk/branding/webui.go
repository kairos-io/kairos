package branding

import (
	"net"
	"net/url"

	"github.com/kairos-io/kairos/v4/sdk/constants"
	"github.com/kairos-io/kairos/v4/sdk/machine"
)

// URLs returns the addresses a user can reach the web installer at, so a
// frontend can offer them without re-deriving them from the listen address.
//
// It is empty when the image turned the web UI off, and when the host has no
// address worth offering. Neither is an error: it only means there is no URL
// to show.
//
// A URL carries the token when the image set one, because the user these are
// printed for is at the console of the machine being installed, and a URL that
// answers 401 is not an address they can use. Physical access to that console
// already installs the machine, so the token is not being given away to anyone
// who could not have it.
func (w WebUI) URLs() []string {
	return w.urls(machine.LocalIPs)
}

// urls is URLs with the address lookup injected, so a test does not depend on
// the interfaces of the machine running it.
func (w WebUI) urls(localIPs func() []string) []string {
	if w.Disable {
		return nil
	}

	listen := w.ListenAddress
	if listen == "" {
		listen = constants.DefaultWebUIListenAddress
	}

	host, port, err := net.SplitHostPort(listen)
	if err != nil || port == "" {
		return nil
	}

	// An address the image pinned to one host is the whole answer. A wildcard
	// host, which is what the default ":8080" means, says the server answers
	// on every interface, so every usable local address is a way in.
	if host != "" && !isWildcardHost(host) {
		return []string{w.url(net.JoinHostPort(host, port))}
	}

	var out []string
	for _, ip := range localIPs() {
		if !isOfferableIP(ip) {
			continue
		}
		out = append(out, w.url(net.JoinHostPort(ip, port)))
	}
	return out
}

// url builds the URL for one host:port, with the token when there is one.
func (w WebUI) url(hostPort string) string {
	u := "http://" + hostPort
	if w.HasToken() {
		u += "/?" + TokenParam + "=" + url.QueryEscape(w.Token)
	}
	return u
}

// isWildcardHost reports whether host is the "any address" form, 0.0.0.0 or ::.
func isWildcardHost(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsUnspecified()
}

// isOfferableIP reports whether an address is one another machine can actually
// open. Loopback only works from the node itself, and a link-local address
// needs a zone the user would have to type, so neither is worth printing.
func isOfferableIP(s string) bool {
	ip := net.ParseIP(s)
	if ip == nil {
		return false
	}
	return ip.IsGlobalUnicast() && !ip.IsLinkLocalUnicast()
}
