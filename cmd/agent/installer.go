package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"syscall"
	"time"

	events "github.com/c3os-io/c3os/pkg/bus"
	config "github.com/c3os-io/c3os/pkg/config"

	"github.com/c3os-io/c3os/internal/bus"
	"github.com/c3os-io/c3os/internal/c3os"
	"github.com/c3os-io/c3os/internal/cmd"
	"github.com/c3os-io/c3os/internal/utils"

	machine "github.com/c3os-io/c3os/internal/machine"
	qr "github.com/mudler/go-nodepair/qrcode"
	"github.com/mudler/go-pluggable"
	"github.com/pterm/pterm"
	"gopkg.in/yaml.v2"
)

func optsToArgs(options map[string]string) (res []string) {
	for k, v := range options {
		if k != "device" && k != "cc" && k != "reboot" && k != "poweroff" {
			res = append(res, fmt.Sprintf("--%s", k))
			res = append(res, fmt.Sprintf("%s", v))
		}
	}
	return
}

func install(dir ...string) error {
	utils.OnSignal(func() {
		svc, err := machine.Getty(1)
		if err == nil {
			svc.Start()
		}
	}, syscall.SIGINT, syscall.SIGTERM)

	tk := ""
	r := map[string]string{}
	bus.Manager.Response(events.EventChallenge, func(p *pluggable.Plugin, r *pluggable.EventResponse) {
		tk = r.Data
	})
	bus.Manager.Response(events.EventInstall, func(p *pluggable.Plugin, resp *pluggable.EventResponse) {
		err := json.Unmarshal([]byte(resp.Data), &r)
		if err != nil {
			fmt.Println(err)
		}
	})

	// Reads config, and if present and offline is defined,
	// runs the installation
	cc, err := config.Scan(dir...)
	if err == nil && cc.C3OS != nil && cc.C3OS.Offline {
		runInstall(map[string]string{
			"device": cc.C3OS.Device,
			"cc":     cc.String(),
		})

		svc, err := machine.Getty(1)
		if err == nil {
			svc.Start()
		}

		return nil
	}

	_, err = bus.Manager.Publish(events.EventChallenge, events.EventPayload{Config: cc.String()})
	if err != nil {
		return err
	}

	cmd.PrintBranding(banner)

	cmd.PrintTextFromFile(c3os.BrandingFile("install_text"), "Installation")

	time.Sleep(5 * time.Second)

	if tk != "" {
		qr.Print(tk)
	}

	if _, err := bus.Manager.Publish(events.EventInstall, events.InstallPayload{Token: tk, Config: cc.String()}); err != nil {
		return err
	}

	if len(r) == 0 {
		return errors.New("no configuration, stopping installation")
	}

	pterm.Info.Println("Starting installation")
	utils.SH("elemental run-stage c3os-install.pre")
	bus.RunHookScript("/usr/bin/c3os-agent.install.pre.hook")

	runInstall(r)

	pterm.Info.Println("Installation completed, press enter to go back to the shell.")

	utils.Prompt("")

	// give tty1 back
	svc, err := machine.Getty(1)
	if err == nil {
		svc.Start()
	}

	return nil
}

func runInstall(options map[string]string) error {
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

	c := &config.Config{}
	yaml.Unmarshal([]byte(cloudInit), c)

	_, reboot := options["reboot"]
	_, poweroff := options["poweroff"]

	ioutil.WriteFile(f.Name(), []byte(cloudInit), os.ModePerm)
	args := []string{"install"}
	args = append(args, optsToArgs(options)...)
	args = append(args, "-c", f.Name(), fmt.Sprintf("%s", device))

	cmd := exec.Command("elemental", args...)
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	utils.SH("elemental run-stage c3os-install.after")
	bus.RunHookScript("/usr/bin/c3os-agent.install.after.hook")

	if reboot || c.C3OS != nil && c.C3OS.Reboot {
		utils.Reboot()
	}

	if poweroff || c.C3OS != nil && c.C3OS.Poweroff {
		utils.PowerOFF()
	}
	return nil
}
