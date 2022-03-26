package utils

import (
	"strings"

	"github.com/joho/godotenv"
	"github.com/pterm/pterm"
)

func Reboot() {
	pterm.Info.Println("Rebooting node")
	SH("reboot")
}

func PowerOFF() {
	pterm.Info.Println("Shutdown node")
	switch Flavor() {
	case "alpine":
		SH("poweroff")
	default:
		SH("shutdown")
	}
}

func Version() string {
	release, _ := godotenv.Read("/etc/os-release")
	v := release["VERSION"]
	return strings.ReplaceAll(v, "+k3s-c3OS", "-")
}

func Flavor() string {
	release, _ := godotenv.Read("/etc/os-release")
	v := release["NAME"]
	return strings.ReplaceAll(v, "c3os-", "")
}
