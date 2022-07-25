package utils

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/pterm/pterm"
)

func Reboot() {
	pterm.Info.Println("Rebooting node")
	SH("reboot") //nolint:errcheck
}

func PowerOFF() {
	pterm.Info.Println("Shutdown node")
	if IsOpenRCBased() {
		SH("poweroff") //nolint:errcheck
	} else {
		SH("shutdown") //nolint:errcheck
	}
}

func Version() string {
	release, _ := godotenv.Read("/etc/os-release")
	v := release["VERSION"]
	v = strings.ReplaceAll(v, "+k3s1-c3OS", "-")
	v = strings.ReplaceAll(v, "+k3s-c3OS", "-")
	return strings.ReplaceAll(v, "c3OS", "")
}

func OSRelease(key string) (string, error) {
	release, err := godotenv.Read("/etc/os-release")
	if err != nil {
		return "", err
	}
	v, exists := release[key]
	if !exists {
		return "", fmt.Errorf("key not found")
	}
	return v, nil
}

func Flavor() string {
	release, _ := godotenv.Read("/etc/os-release")
	v := release["NAME"]
	return strings.ReplaceAll(v, "c3os-", "")
}

func IsOpenRCBased() bool {
	f := Flavor()
	return f == "alpine" || f == "alpine-arm-rpi"
}

func GetInterfaceIP(in string) string {
	ifaces, err := net.Interfaces()
	if err != nil {
		fmt.Println("failed getting system interfaces")
		return ""
	}
	for _, i := range ifaces {
		if i.Name == in {
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

func K3sBin() string {
	for _, p := range []string{"/usr/bin/k3s", "/usr/local/bin/k3s"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	return ""
}
