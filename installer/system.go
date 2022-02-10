package main

import (
	"os/exec"

	"github.com/pterm/pterm"
)

func Reboot() {
	pterm.Info.Println("Rebooting node")
	exec.Command("reboot").Start()
}
