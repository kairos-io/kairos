package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"

	"github.com/pterm/pterm"
)

func optsToArgs(options map[string]string) (res []string) {
	for k, v := range options {
		if k != "device" && k != "cc" && k != "reboot" {
			res = append(res, fmt.Sprintf("--%s", k))
			res = append(res, fmt.Sprintf("%s", v))
		}
	}
	return
}

func runInstall(options map[string]string) {
	fmt.Println("Running install", options)
	f, _ := ioutil.TempFile("", "xxxx")

	device, ok := options["device"]
	if !ok {
		fmt.Println("device must be specified among options")
		os.Exit(1)
	}

	cloudInit, ok := options["cc"]
	if !ok {
		fmt.Println("cloudInit must be specified among options")
		os.Exit(1)
	}

	_, reboot := options["reboot"]

	ioutil.WriteFile(f.Name(), []byte(cloudInit), os.ModePerm)
	args := []string{}
	args = append(args, optsToArgs(options)...)
	args = append(args, "-c", f.Name(), fmt.Sprintf("%s", device))

	cmd := exec.Command("cos-installer", args...)
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if reboot {
		pterm.Info.Println("Rebooting node")
		exec.Command("reboot").Start()
	}
}
