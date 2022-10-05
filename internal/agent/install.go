package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"syscall"
	"time"

	events "github.com/kairos-io/kairos/sdk/bus"

	config "github.com/kairos-io/kairos/pkg/config"

	hook "github.com/kairos-io/kairos/internal/agent/hooks"
	"github.com/kairos-io/kairos/internal/bus"

	"github.com/kairos-io/kairos/internal/cmd"
	"github.com/kairos-io/kairos/pkg/utils"

	machine "github.com/kairos-io/kairos/pkg/machine"
	qr "github.com/mudler/go-nodepair/qrcode"
	"github.com/mudler/go-pluggable"
	"github.com/pterm/pterm"
	"gopkg.in/yaml.v2"
)

func optsToArgs(options map[string]string) (res []string) {
	for k, v := range options {
		if k != "device" && k != "cc" && k != "reboot" && k != "poweroff" {
			res = append(res, fmt.Sprintf("--%s", k))
			if v != "" {
				res = append(res, v)
			}
		}
	}
	return
}

func ManualInstall(config string, options map[string]string) error {
	dat, err := ioutil.ReadFile(config)
	if err != nil {
		return err
	}
	options["cc"] = string(dat)

	return RunInstall(options)
}

func Install(dir ...string) error {
	utils.OnSignal(func() {
		svc, err := machine.Getty(1)
		if err == nil {
			svc.Start() //nolint:errcheck
		}
	}, syscall.SIGINT, syscall.SIGTERM)

	tk := ""
	r := map[string]string{}

	mergeOption := func(cloudConfig string) {
		c := &config.Config{}
		yaml.Unmarshal([]byte(cloudConfig), c) //nolint:errcheck
		for k, v := range c.Options {
			if k == "cc" {
				continue
			}
			r[k] = v
		}
	}
	bus.Manager.Response(events.EventChallenge, func(p *pluggable.Plugin, r *pluggable.EventResponse) {
		tk = r.Data
	})
	bus.Manager.Response(events.EventInstall, func(p *pluggable.Plugin, resp *pluggable.EventResponse) {
		err := json.Unmarshal([]byte(resp.Data), &r)
		if err != nil {
			fmt.Println(err)
		}
	})

	// Try to pull userdata once more. best-effort
	if _, err := os.Stat("/oem/userdata"); err != nil {
		if err := machine.ExecuteCloudConfig("/system/oem/00_datasource.yaml", "rootfs.before"); err != nil {
			fmt.Println("Warning: Failed pulling from datasources")
		}
	}

	// Reads config, and if present and offline is defined,
	// runs the installation
	cc, err := config.Scan(config.Directories(dir...), config.MergeBootLine)
	if err == nil && cc.Install != nil && cc.Install.Auto {
		r["cc"] = cc.String()
		r["device"] = cc.Install.Device
		mergeOption(cc.String())

		err = RunInstall(r)
		if err != nil {
			return err
		}

		svc, err := machine.Getty(1)
		if err == nil {
			svc.Start() //nolint:errcheck
		}

		return nil
	}

	_, err = bus.Manager.Publish(events.EventChallenge, events.EventPayload{Config: cc.String()})
	if err != nil {
		return err
	}

	cmd.PrintBranding(DefaultBanner)

	agentConfig, err := LoadConfig()
	if err != nil {
		return err
	}

	cmd.PrintText(agentConfig.Branding.Install, "Installation")

	if !agentConfig.Fast {
		time.Sleep(5 * time.Second)
	}

	if tk != "" {
		qr.Print(tk)
	}

	if _, err := bus.Manager.Publish(events.EventInstall, events.InstallPayload{Token: tk, Config: cc.String()}); err != nil {
		return err
	}

	if len(r) == 0 {
		return errors.New("no configuration, stopping installation")
	}

	// we receive a cloud config at this point
	cloudConfig, exists := r["cc"]

	// merge any options defined in it
	mergeOption(cloudConfig)

	// now merge cloud config from system and the one received from the agent-provider
	ccData := map[string]interface{}{}

	// make sure the config we write has at least the #node-config header, if any other was defined beforeahead
	header := "#node-config"
	if hasHeader, head := config.HasHeader(cc.String(), ""); hasHeader {
		header = head
	}

	// What we receive take precedence over the one in the system. best-effort
	yaml.Unmarshal([]byte(cc.String()), &ccData) //nolint:errcheck
	if exists {
		yaml.Unmarshal([]byte(cloudConfig), &ccData) //nolint:errcheck
		if hasHeader, head := config.HasHeader(cloudConfig, ""); hasHeader {
			header = head
		}
	}

	out, err := yaml.Marshal(ccData)
	if err != nil {
		return fmt.Errorf("failed marshalling cc: %w", err)
	}

	r["cc"] = config.AddHeader(header, string(out))

	pterm.Info.Println("Starting installation")

	if err := RunInstall(r); err != nil {
		return err
	}

	pterm.Info.Println("Installation completed, press enter to go back to the shell.")

	utils.Prompt("") //nolint:errcheck

	// give tty1 back
	svc, err := machine.Getty(1)
	if err == nil {
		svc.Start() //nolint: errcheck
	}

	return nil
}

func RunInstall(options map[string]string) error {
	utils.SH("elemental run-stage kairos-install.pre")             //nolint:errcheck
	events.RunHookScript("/usr/bin/kairos-agent.install.pre.hook") //nolint:errcheck

	f, _ := ioutil.TempFile("", "xxxx")
	defer os.RemoveAll(f.Name())

	device, ok := options["device"]
	if !ok {
		fmt.Println("device must be specified among options")
		os.Exit(1)
	}

	if device == "auto" {
		device = detectDevice()
	}

	cloudInit, ok := options["cc"]
	if !ok {
		fmt.Println("cloudInit must be specified among options")
		os.Exit(1)
	}

	c := &config.Config{}
	yaml.Unmarshal([]byte(cloudInit), c) //nolint:errcheck

	_, reboot := options["reboot"]
	_, poweroff := options["poweroff"]
	if c.Install == nil {
		c.Install = &config.Install{}
	}
	if poweroff {
		c.Install.Poweroff = true
	}
	if reboot {
		c.Install.Reboot = true
	}

	err := ioutil.WriteFile(f.Name(), []byte(cloudInit), os.ModePerm)
	if err != nil {
		fmt.Printf("could not write cloud init: %s\n", err.Error())
		os.Exit(1)
	}
	args := []string{"install"}
	args = append(args, optsToArgs(options)...)
	args = append(args, "-c", f.Name(), device)

	cmd := exec.Command("elemental", args...)
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if err := hook.Run(*c, hook.All...); err != nil {
		return err
	}

	return nil
}
