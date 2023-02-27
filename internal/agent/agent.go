package agent

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos/pkg/utils"

	events "github.com/kairos-io/kairos/sdk/bus"

	hook "github.com/kairos-io/kairos/internal/agent/hooks"
	"github.com/kairos-io/kairos/internal/bus"
	config "github.com/kairos-io/kairos/pkg/config"
	machine "github.com/kairos-io/kairos/pkg/machine"
	"github.com/nxadm/tail"
)

// setup needs edgevpn and k3s installed locally (both k3s and k3s-agent systemd services).
func Run(opts ...Option) error {
	o := &Options{}
	if err := o.Apply(opts...); err != nil {
		return err
	}

	os.MkdirAll("/usr/local/.kairos", 0600) //nolint:errcheck

	// Reads config
	c, err := config.KScan(config.Directories(o.Dir...))
	if err != nil {
		return err
	}

	utils.SetEnv(c.Env)
	bf := machine.BootFrom()
	if c.Install.Auto && (bf == machine.NetBoot || bf == machine.LiveCDBoot) {
		// Don't go ahead if we are asked to install from a booting live medium
		fmt.Println("Agent run aborted. Installation being performed from live medium")
		return nil
	}

	os.MkdirAll("/var/log/kairos", 0600) //nolint:errcheck

	fileName := filepath.Join("/var/log/kairos", "agent-provider.log")

	// Create if not exist
	if _, err := os.Stat(fileName); err != nil {
		err = os.WriteFile(fileName, []byte{}, os.ModePerm)
		if err != nil {
			return err
		}
	}

	// Tail to the log
	t, err := tail.TailFile(fileName, tail.Config{Follow: true})
	if err != nil {
		return err
	}

	go func() {
		for line := range t.Lines {
			fmt.Println(line.Text)
		}
	}()

	if !machine.SentinelExist("firstboot") {

		if err := hook.KRun(*c, hook.FirstBoot...); err != nil {
			return err
		}

		// Re-load providers
		bus.Reload()
		err = machine.CreateSentinel("firstboot")
		if c.FailOnBundleErrors && err != nil {
			return err
		}

		// Re-read config files
		c, err = config.KScan(config.Directories(o.Dir...))
		if err != nil {
			return err
		}
	}

	_, err = bus.Manager.Publish(events.EventBootstrap, events.BootstrapPayload{APIAddress: o.APIAddress, Config: c.String(), Logfile: fileName})

	if o.Restart && err != nil {
		fmt.Println("Warning: Agent failed, restarting: ", err.Error())
		return Run(opts...)
	}
	return err
}
