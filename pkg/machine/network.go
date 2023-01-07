package machine

import (
	"net"
)

func LocalIPs() (ips []string) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return
	}
	for _, i := range ifaces {
		if i.Flags == net.FlagLoopback {
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
