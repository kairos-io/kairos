package server

import (
	"fmt"
	"net"
	"strings"

	"github.com/labstack/echo/v4"
)

// ParseTrustedProxies parses the --trusted-proxy values: each is a CIDR such as
// 10.0.0.0/8 or a single IP, which is taken as a /32 (or /128 for IPv6).
func ParseTrustedProxies(values []string) ([]*net.IPNet, error) {
	nets := make([]*net.IPNet, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, n, err := net.ParseCIDR(v); err == nil {
			nets = append(nets, n)
			continue
		}
		ip := net.ParseIP(v)
		if ip == nil {
			return nil, fmt.Errorf("invalid trusted proxy %q: want a CIDR or an IP address", v)
		}
		bits := 128
		if ip4 := ip.To4(); ip4 != nil {
			ip, bits = ip4, 32
		}
		nets = append(nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
	}
	return nets, nil
}

// clientIPExtractor decides what c.RealIP() returns. With no trusted proxies it
// is the TCP peer address, so a client cannot pick its own IP through
// X-Forwarded-For or X-Real-IP. With trusted proxies, X-Forwarded-For is
// honoured only for hops inside those ranges; echo's default trust of loopback,
// link-local and private ranges is turned off so only the configured ones count.
func clientIPExtractor(trusted []*net.IPNet) echo.IPExtractor {
	if len(trusted) == 0 {
		return echo.ExtractIPDirect()
	}
	opts := []echo.TrustOption{
		echo.TrustLoopback(false),
		echo.TrustLinkLocal(false),
		echo.TrustPrivateNet(false),
	}
	for _, n := range trusted {
		opts = append(opts, echo.TrustIPRange(n))
	}
	return echo.ExtractIPFromXFFHeader(opts...)
}
