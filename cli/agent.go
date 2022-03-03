package main

import (
	"context"
	"fmt"
	"os"

	config "github.com/c3os-io/c3os/cli/config"
	"github.com/c3os-io/c3os/cli/machine"
	role "github.com/c3os-io/c3os/cli/role"
	"github.com/c3os-io/c3os/cli/utils"
	"github.com/c3os-io/c3os/cli/vpn"
	edgeVPNClient "github.com/mudler/edgevpn/api/client"
	service "github.com/mudler/edgevpn/api/client/service"

	"github.com/denisbrodbeck/machineid"
	logging "github.com/ipfs/go-log"
)

func uuid() string {
	if os.Getenv("UUID") != "" {
		return os.Getenv("UUID")
	}
	id, _ := machineid.ID()
	hostname, _ := os.Hostname()
	return fmt.Sprintf("%s-%s", id, hostname)
}

// setup needs edgevpn and k3s installed locally
// (both k3s and k3s-agent systemd services)
func agent(apiAddress, dir string, force bool) error {
	utils.SH("sysctl -w net.core.rmem_max=2500000")

	os.MkdirAll("/usr/local/.c3os", 0600)

	// Reads config
	c, err := config.Scan(dir)
	if err != nil {
		return err
	}

	logLevel := c.C3OS.LogLevel
	if logLevel == "" {
		logLevel = "debug"
	}

	l := logging.Logger("c3os")

	lvl, err := logging.LevelFromString(logLevel)
	if err != nil {
		return err
	}
	logging.SetAllLoggers(lvl)

	if c.C3OS == nil || c.C3OS.NetworkToken == "" {
		l.Info("No network token provided, exiting.")
		return nil
	}

	if err := vpn.Setup(machine.EdgeVPNDefaultInstance, apiAddress, "/", true, c); err != nil {
		return err
	}

	networkID := "c3os"

	if c.C3OS.NetworkID != "" {
		networkID = c.C3OS.NetworkID
	}

	cc := service.NewClient(
		networkID,
		edgeVPNClient.NewClient(edgeVPNClient.WithHost(apiAddress)))

	nodeOpts := []service.Option{
		service.WithLogger(l),
		service.WithClient(cc),
		service.WithUUID(uuid()),
		service.WithStateDir("/usr/local/.c3os/state"),
		service.WithNetworkToken(c.C3OS.NetworkToken),
		service.WithPersistentRoles("auto"),
		service.WithRoles(
			service.RoleKey{
				Role:        "master",
				RoleHandler: role.Master(c),
			},
			service.RoleKey{
				Role:        "worker",
				RoleHandler: role.Worker(c),
			},
			service.RoleKey{
				Role:        "auto",
				RoleHandler: role.Auto(c),
			},
		),
	}

	// Optionally set up a specific node role if the user has defined so
	if c.C3OS.Role != "" {
		nodeOpts = append(nodeOpts, service.WithDefaultRoles(c.C3OS.Role))
	}

	k, err := service.NewNode(nodeOpts...)
	if err != nil {
		return err
	}
	return k.Start(context.Background())
}
