package agent

import (
	"fmt"
	"os"

	hook "github.com/kairos-io/kairos-agent/v2/internal/agent/hooks"
	"github.com/kairos-io/kairos-agent/v2/internal/bus"
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/implementations/spec"
	events "github.com/kairos-io/kairos-sdk/bus"
	"github.com/kairos-io/kairos-sdk/collector"
	"github.com/kairos-io/kairos-sdk/machine"
	"github.com/kairos-io/kairos-sdk/utils"
)

// Run starts the agent provider emitting the bootstrap event.
func Run(opts ...Option) error {
	o := &Options{}
	if err := o.Apply(opts...); err != nil {
		return err
	}

	os.MkdirAll("/usr/local/.kairos", 0600) //nolint:errcheck

	// Reads config
	c, err := config.Scan(collector.Directories(o.Dir...))
	if err != nil {
		return err
	}

	utils.SetEnv(c.Env)
	bf := machine.BootFrom()
	if c.Install != nil && c.Install.Auto && (bf == machine.NetBoot || bf == machine.LiveCDBoot) {
		// Don't go ahead if we are asked to install from a booting live medium
		fmt.Println("Agent run aborted. Installation being performed from live medium")
		return nil
	}

	fileName := "/var/log/kairos/agent-provider.log"

	if !machine.SentinelExist("firstboot") {
		spec := v1.EmptySpec{}
		if err := hook.Run(*c, &spec, hook.FirstBoot...); err != nil {
			return err
		}

		// Re-load providers
		bus.Reload()
		err = machine.CreateSentinel("firstboot")
		if c.FailOnBundleErrors && err != nil {
			return err
		}

		// Re-read config files
		c, err = config.Scan(collector.Directories(o.Dir...))
		if err != nil {
			return err
		}
	}
	configStr, err := c.Collector.String()
	if err != nil {
		panic(err)
	}
	_, err = bus.Manager.Publish(events.EventBootstrap, events.BootstrapPayload{APIAddress: o.APIAddress, Config: configStr, Logfile: fileName})

	// If phonehome config is present in cloud-config, install+start the service.
	enablePhoneHomeIfConfigured(c)

	if o.Restart && err != nil {
		fmt.Println("Warning: Agent failed, restarting: ", err.Error())
		return Run(opts...)
	}
	return err
}
