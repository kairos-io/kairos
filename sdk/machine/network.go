package machine

import (
	"net"
)

func Interfaces() (in []string) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return
	}
	for _, i := range ifaces {
		if isLoopbackInterface(i.Flags) {
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
		if isLoopbackInterface(i.Flags) {
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

			if !isAdvertisableIP(ip) {
				continue
			}
			ips = append(ips, ip.String())
		}
	}
	return
}

func isLoopbackInterface(flags net.Flags) bool {
	return flags&net.FlagLoopback != 0
}

func isAdvertisableIP(ip net.IP) bool {
	return ip != nil && ip.IsGlobalUnicast() && !ip.IsLinkLocalUnicast()
}
