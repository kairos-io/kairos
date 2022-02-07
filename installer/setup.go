package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	role "github.com/mudler/c3os/installer/role"
	edgeVPNClient "github.com/mudler/edgevpn/api/client"
	service "github.com/mudler/edgevpn/api/client/service"

	"github.com/denisbrodbeck/machineid"
	logging "github.com/ipfs/go-log"
	systemd "github.com/mudler/c3os/installer/systemd"
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
func setup(dir string) error {

	apiAddress := "127.0.0.1:8080"
	os.MkdirAll("/usr/local/.c3os", 0600)

	// Reads config
	c, err := ScanConfig(dir)
	if err != nil {
		return err
	}

	if c.C3OS.NetworkToken == "" {
		return errors.New("No network token")
	}

	l := logging.Logger("c3os")

	lvl, err := logging.LevelFromString("debug")
	if err != nil {
		return err
	}

	// Setup systemd unit and starts it
	systemd.EdgeVPN.Prepare(map[string]string{
		"EDGEVPNTOKEN":          c.C3OS.NetworkToken,
		"API":                   "true",
		"APILISTEN":             apiAddress,
		"EDGEVPNLOWPROFILEVPNN": "true",
		"DHCP":                  "true",
		"DHCPLEASEDIR":          "/usr/local/.c3os/lease",
	})

	if role.SentinelExist() {
		return nil
	}

	cc := service.NewClient(
		"c3os",
		edgeVPNClient.NewClient(edgeVPNClient.WithHost(fmt.Sprintf("http://%s", apiAddress))))
	logging.SetAllLoggers(lvl)

	k, err := service.NewNode(
		service.WithLogger(l),
		service.WithClient(cc),
		//	service.WithAPIAddress(apiAddress),
		service.WithUUID(uuid()),
		service.WithStateDir("/usr/local/.c3os/state"),
		service.WithNetworkToken(c.C3OS.NetworkToken),
		service.WithPersistentRoles("auto"),
		service.WithRoles(
			service.RoleKey{
				Role:        "master",
				RoleHandler: role.Master,
			},
			service.RoleKey{
				Role:        "worker",
				RoleHandler: role.Worker,
			},
			service.RoleKey{
				Role:        "auto",
				RoleHandler: role.Auto,
			},
		),
	)
	if err != nil {
		return err
	}
	return k.Start(context.Background())
}
