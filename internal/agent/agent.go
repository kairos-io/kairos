package agent

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	events "github.com/c3os-io/c3os/sdk/bus"

	"github.com/c3os-io/c3os/internal/bus"
	machine "github.com/c3os-io/c3os/internal/machine"
	config "github.com/c3os-io/c3os/pkg/config"
	"github.com/nxadm/tail"
)

// setup needs edgevpn and k3s installed locally (both k3s and k3s-agent systemd services).
func Run(apiAddress string, dir []string, force bool) error {

	os.MkdirAll("/usr/local/.c3os", 0600) //nolint:errcheck

	// Reads config
	c, err := config.Scan(config.Directories(dir...))
	if err != nil {
		return err
	}
	bf := machine.BootFrom()
	if c.Install != nil && c.Install.Auto && (bf == machine.NetBoot || bf == machine.LiveCDBoot) {
		// Don't go ahead if we are asked to install from a booting live medium
		fmt.Println("Agent run aborted. Installation being performed from live medium")
		return nil
	}

	os.MkdirAll("/var/log/c3os", 0600) //nolint:errcheck
	fileName := filepath.Join("/var/log/c3os", "agent-provider.log")
	err = ioutil.WriteFile(fileName, []byte{}, os.ModePerm)
	if err != nil {
		return err
	}

	t, err := tail.TailFile(fileName, tail.Config{Follow: true})
	if err != nil {
		return err
	}

	go func() {
		for line := range t.Lines {
			fmt.Println(line.Text)
		}
	}()

	if !machine.SentinelExist("bundles") {
		opts := c.Bundles.Options()
		err := machine.RunBundles(opts...)
		if !c.IgnoreBundleErrors && err != nil {
			return err
		}

		// Re-load providers
		bus.Manager.LoadProviders()
		err = machine.CreateSentinel("bundles")
		if !c.IgnoreBundleErrors && err != nil {
			return err
		}
	}

	_, err = bus.Manager.Publish(events.EventBootstrap, events.BootstrapPayload{APIAddress: apiAddress, Config: c.String(), Logfile: fileName})
	return err
}
