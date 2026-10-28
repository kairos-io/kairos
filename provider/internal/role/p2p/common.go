package role

import (
	"fmt"
	"net"

	providerConfig "github.com/kairos-io/kairos/v4/provider/internal/provider/config"
)

const (
	RoleWorker            = "worker"
	RoleMaster            = "master"
	RoleMasterHA          = "master/ha"
	RoleMasterClusterInit = "master/clusterinit"
	RoleAuto              = "auto"
)

type systemInterface struct {
	Name string
	IPs  []net.IP
}

// listInterfaces is a variable so tests can replace the host's interfaces.
var listInterfaces = func() ([]systemInterface, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	out := make([]systemInterface, 0, len(ifaces))
	for _, i := range ifaces {
		si := systemInterface{Name: i.Name}
		addrs, _ := i.Addrs()
		for _, addr := range addrs {
			switch v := addr.(type) {
			case *net.IPNet:
				si.IPs = append(si.IPs, v.IP)
			case *net.IPAddr:
				si.IPs = append(si.IPs, v.IP)
			}
		}
		out = append(out, si)
	}
	return out, nil
}

// usableIP returns the first address another node can reach us on, skipping
// loopback, unspecified and link-local addresses.
func usableIP(ips []net.IP) string {
	for _, ip := range ips {
		if ip == nil || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() {
			continue
		}
		return ip.String()
	}
	return ""
}

// interfaceIP returns a usable address of the named interface, or "" if it
// has none.
func interfaceIP(name string) string {
	ifaces, err := listInterfaces()
	if err != nil {
		fmt.Println("failed getting system interfaces")
		return ""
	}
	for _, i := range ifaces {
		if i.Name == name {
			return usableIP(i.IPs)
		}
	}
	return ""
}

// guessInterface returns the kube-vip interface if set, otherwise the first
// non-loopback interface with a usable address. If none has one, it falls back
// to the first non-loopback interface.
func guessInterface(pconfig *providerConfig.Config) string {
	if pconfig.KubeVIP.Interface != "" {
		return pconfig.KubeVIP.Interface
	}
	ifaces, err := listInterfaces()
	if err != nil {
		fmt.Println("failed getting system interfaces")
		return ""
	}
	fallback := ""
	for _, i := range ifaces {
		if i.Name == "lo" {
			continue
		}
		if usableIP(i.IPs) != "" {
			return i.Name
		}
		if fallback == "" {
			fallback = i.Name
		}
	}
	return fallback
}
