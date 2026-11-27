package p2p

import (
	"fmt"
	"net"
	"strings"
)

// Allowlist is a set of CIDRs. An empty Allowlist allows nothing: callers must
// opt in to the networks they trust.
type Allowlist []*net.IPNet

// ParseAllowlist parses a list of CIDRs (a bare IP is treated as /32 or /128).
func ParseAllowlist(cidrs []string) (Allowlist, error) {
	var out Allowlist
	for _, c := range cidrs {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if !strings.Contains(c, "/") {
			if ip := net.ParseIP(c); ip != nil && ip.To4() != nil {
				c += "/32"
			} else {
				c += "/128"
			}
		}
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %q: %w", c, err)
		}
		out = append(out, n)
	}
	return out, nil
}

// Allows reports whether ip is inside any of the allowed networks.
func (a Allowlist) Allows(ip net.IP) bool {
	for _, n := range a {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
