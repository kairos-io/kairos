package main

import (
	machine "github.com/c3os-io/c3os/internal/machine"
	"github.com/c3os-io/c3os/internal/provider"
	providerConfig "github.com/c3os-io/c3os/internal/provider/config"
	config "github.com/c3os-io/c3os/pkg/config"
)

func rotate(configDir []string, newToken, apiAddress, rootDir string, restart bool) error {
	if err := config.ReplaceToken(configDir, newToken); err != nil {
		return err
	}

	c, err := config.Scan(config.Directories(configDir...))
	if err != nil {
		return err
	}

	providerCfg := &providerConfig.Config{}
	err = c.Unmarshal(providerCfg)
	if err != nil {
		return err
	}

	err = provider.SetupVPN(machine.EdgeVPNDefaultInstance, apiAddress, rootDir, false, providerCfg)
	if err != nil {
		return err
	}

	if restart {
		svc, err := machine.EdgeVPN(machine.EdgeVPNDefaultInstance, rootDir)
		if err != nil {
			return err
		}

		return svc.Restart()
	}
	return nil
}
