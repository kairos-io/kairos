package main

import (
	"github.com/mudler/c3os/installer/utils"
	"github.com/pterm/pterm"
)

func Reboot() {
	pterm.Info.Println("Rebooting node")
	utils.SH("reboot")
}
