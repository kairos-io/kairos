package role

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"

	service "github.com/mudler/edgevpn/api/client/service"
)

type Role func(*service.RoleConfig) error

func getIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		fmt.Println("failed getting system interfaces")
		return ""
	}
	for _, i := range ifaces {
		if i.Name == "edgevpn0" {
			addrs, _ := i.Addrs()
			// handle err
			for _, addr := range addrs {
				var ip net.IP
				switch v := addr.(type) {
				case *net.IPNet:
					ip = v.IP
				case *net.IPAddr:
					ip = v.IP
				}
				if ip != nil {
					return ip.String()

				}
			}
		}
	}
	return ""
}

func SentinelExist() bool {
	if _, err := os.Stat("/usr/local/.c3os/deployed"); err == nil {
		return true
	}
	return false
}

func CreateSentinel() error {
	return ioutil.WriteFile("/usr/local/.c3os/deployed", []byte{}, os.ModePerm)
}
