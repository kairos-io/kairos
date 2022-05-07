package main

import (
	"fmt"
	"strings"

	"github.com/c3os-io/c3os/cli/config"
	"github.com/c3os-io/c3os/cli/machine"
	"github.com/c3os-io/c3os/cli/machine/openrc"
	"github.com/c3os-io/c3os/cli/machine/systemd"
	role "github.com/c3os-io/c3os/cli/role"
	"github.com/c3os-io/c3os/cli/utils"
)

func oneTimeBootstrap(c *config.Config) error {
	if role.SentinelExist() {
		fmt.Println("Sentinel exists, nothing to do. exiting.")
		return nil
	}
	fmt.Println("One time bootstrap starting")

	var svc machine.Service
	k3sConfig := config.K3s{}
	svcName := "k3s"
	svcRole := "server"

	if c.K3s.Enabled {
		k3sConfig = c.K3s
	} else if c.K3sAgent.Enabled {
		k3sConfig = c.K3sAgent
		svcName = "k3s-agent"
		svcRole = "agent"
	}

	if utils.IsOpenRCBased() {
		svc, _ = openrc.NewService(
			openrc.WithName(svcName),
		)
	} else {
		svc, _ = systemd.NewService(
			systemd.WithName(svcName),
		)
	}

	envFile := fmt.Sprintf("/etc/sysconfig/%s", svcName)
	if svc == nil {
		return fmt.Errorf("could not detect OS")
	}

	// Setup systemd unit and starts it
	if err := utils.WriteEnv(envFile,
		k3sConfig.Env,
	); err != nil {
		return err
	}

	if err := svc.OverrideCmd(fmt.Sprintf("/usr/bin/k3s %s %s", svcRole, strings.Join(k3sConfig.Args, " "))); err != nil {
		return err
	}

	if err := svc.SetEnvFile(envFile); err != nil {
		return err
	}

	if err := svc.Start(); err != nil {
		return err
	}

	if err := svc.Enable(); err != nil {
		return err
	}

	return role.CreateSentinel()
}
