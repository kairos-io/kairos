package p2p

import (
	"fmt"
	"net"
	"strings"
)

// DefaultAllowlist computes the allowlist to use when the user gave none:
//   - static peers: exactly the (resolved) addresses of the listed peers
//   - discovery: the local subnet(s) of bindIP
func DefaultAllowlist(peers []string, bindIP string) (Allowlist, error) {
	var out Allowlist
	if len(peers) > 0 {
		for _, p := range peers {
			host, _, err := net.SplitHostPort(p)
			if err != nil {
				host = p
			}
			ips, err := net.LookupIP(host)
			if err != nil {
				return nil, fmt.Errorf("resolving peer %q: %w", p, err)
			}
			for _, ip := range ips {
				if v4 := ip.To4(); v4 != nil {
					if err := publicPeerCheck(v4); err != nil {
						return nil, err
					}
					out = append(out, &net.IPNet{IP: v4, Mask: net.CIDRMask(32, 32)})
				}
			}
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("no IPv4 address found for peers %v", peers)
		}
		return out, nil
	}
	n, err := subnetOf(bindIP)
	if err != nil {
		return nil, err
	}
	return Allowlist{n}, nil
}

// subnetOf returns the subnet of the local interface that owns ip.
func subnetOf(ip string) (*net.IPNet, error) {
	want := net.ParseIP(ip)
	if want == nil {
		return nil, fmt.Errorf("%q is not an IP address", ip)
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.Equal(want) {
			return &net.IPNet{IP: n.IP.Mask(n.Mask), Mask: n.Mask}, nil
		}
	}
	return nil, fmt.Errorf("no local interface owns %s", ip)
}

// AutoBind returns the IPv4 address of the only non-loopback, up interface.
// With several candidates it refuses to guess: the wrong one could be the
// management or an untrusted network.
func AutoBind() (string, error) {
	ifs, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	var cands []string
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && !n.IP.IsLinkLocalUnicast() {
				cands = append(cands, n.IP.String()+" ("+i.Name+")")
			}
		}
	}
	return chooseBind(cands)
}

// chooseBind picks the address when there is exactly one candidate
// ("ip (interface)"). With several it refuses to guess.
func chooseBind(cands []string) (string, error) {
	switch len(cands) {
	case 0:
		return "", fmt.Errorf("no usable IPv4 interface found, set p2p-source.bind")
	case 1:
		return strings.Fields(cands[0])[0], nil
	}
	return "", fmt.Errorf("several addresses could be used (%s): set p2p-source.bind to the one on the trusted network", strings.Join(cands, ", "))
}

// publicPeerCheck refuses a peer address that is not on a private network
// unless the allowlist was configured explicitly: the default allowlist is the
// peers named in the URI, which would otherwise allow any DNS name.
func publicPeerCheck(ip net.IP) error {
	if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return nil
	}
	return fmt.Errorf("peer %s is not on a private network: add it to p2p-source.allow explicitly if it is meant", ip)
}
