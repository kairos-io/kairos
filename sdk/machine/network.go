package machine

import (
	"net"
)

// isLoopback reports whether f belongs to a loopback interface.
//
// net.Flags is a bit set, so this has to mask rather than compare: a loopback
// interface that is up reports up|loopback|running, which is never equal to
// FlagLoopback on its own.
func isLoopback(f net.Flags) bool {
	return f&net.FlagLoopback != 0
}

func Interfaces() (in []string) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return
	}
	for _, i := range ifaces {
		if isLoopback(i.Flags) {
			continue
		}
		in = append(in, i.Name)
	}
	return
}

func LocalIPs() (ips []string) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return
	}
	for _, i := range ifaces {
		if isLoopback(i.Flags) {
			continue
		}
		addrs, err := i.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ip, _, err := net.ParseCIDR(a.String())
			if err != nil {
				continue
			}

			ips = append(ips, ip.String())
		}
	}
	return
}
