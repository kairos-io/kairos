package main

import (
	config "github.com/c3os-io/c3os/installer/config"
	systemd "github.com/c3os-io/c3os/installer/systemd"
	"github.com/c3os-io/c3os/installer/vpn"
)

func rotate(configDir, newToken, apiAddress, rootDir string, restart bool) error {
	if err := config.ReplaceToken(configDir, newToken); err != nil {
		return err
	}

	c, err := config.Scan(configDir)
	if err != nil {
		return err
	}

	err = vpn.Setup(systemd.EdgeVPNDefaultInstance, apiAddress, rootDir, false, c)
	if err != nil {
		return err
	}

	if restart {
		svc, err := systemd.EdgeVPN(systemd.EdgeVPNDefaultInstance, rootDir)
		if err != nil {
			return err
		}

		return svc.Restart()
	}
	return nil
}
