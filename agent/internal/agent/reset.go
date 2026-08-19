package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	hook "github.com/kairos-io/kairos-agent/v2/internal/agent/hooks"
	"github.com/kairos-io/kairos-agent/v2/internal/bus"
	"github.com/kairos-io/kairos-agent/v2/internal/cmd"
	"github.com/kairos-io/kairos-agent/v2/pkg/action"
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/uki"
	internalutils "github.com/kairos-io/kairos-agent/v2/pkg/utils"
	sdk "github.com/kairos-io/kairos-sdk/bus"
	"github.com/kairos-io/kairos-sdk/collector"
	"github.com/kairos-io/kairos-sdk/machine"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	"github.com/kairos-io/kairos-sdk/utils"

	"github.com/mudler/go-pluggable"
)

func Reset(reboot, unattended, resetOem bool, dir ...string) error {
	// In both cases we want
	if internalutils.UkiBootMode() == internalutils.UkiHDD {
		return resetUki(reboot, unattended, resetOem, dir...)
	} else if internalutils.UkiBootMode() == internalutils.UkiRemovableMedia {
		return fmt.Errorf("reset is not supported on removable media, please run reset from the installed system recovery entry")
	} else {
		return reset(reboot, unattended, resetOem, dir...)
	}
}

func reset(reboot, unattended, resetOem bool, dir ...string) error {
	cfg, err := sharedReset(reboot, unattended, resetOem, dir...)
	if err != nil {
		return err
	}
	err = config.CheckConfigForUsers(cfg)

	if err != nil {
		return err
	}
	// Load the installation Config from the cloud-config data
	resetSpec, err := config.ReadResetSpecFromConfig(cfg)
	if err != nil {
		return err
	}

	err = resetSpec.Sanitize()
	if err != nil {
		return err
	}

	resetAction := action.NewResetAction(cfg, resetSpec)
	if err = resetAction.Run(); err != nil {
		cfg.Logger.Errorf("failed to reset: %s", err)
		return err
	}

	bus.Manager.Publish(sdk.EventAfterReset, sdk.EventPayload{}) //nolint:errcheck

	return hook.Run(*cfg, resetSpec, hook.FinishReset...)
}

func resetUki(reboot, unattended, resetOem bool, dir ...string) error {
	cfg, err := sharedReset(reboot, unattended, resetOem, dir...)
	if err != nil {
		return err
	}
	err = config.CheckConfigForUsers(cfg)
	if err != nil {
		return err
	}
	// Load the installation Config from the cloud-config data
	resetSpec, err := config.ReadUkiResetSpecFromConfig(cfg)
	if err != nil {
		return err
	}

	err = resetSpec.Sanitize()
	if err != nil {
		return err
	}

	resetAction := uki.NewResetAction(cfg, resetSpec)
	if err = resetAction.Run(); err != nil {
		cfg.Logger.Errorf("failed to reset uki: %s", err)
		return err
	}

	bus.Manager.Publish(sdk.EventAfterReset, sdk.EventPayload{}) //nolint:errcheck

	return hook.Run(*cfg, resetSpec, hook.FinishReset...)
}

// sharedReset is the common reset code for both uki and non-uki
// sets the config, runs the event handler, publish the envent and gets the config
func sharedReset(reboot, unattended, resetOem bool, dir ...string) (c *sdkConfig.Config, err error) {
	bus.Manager.Initialize()
	var optionsFromEvent map[string]string

	// This config is only for reset branding.
	agentConfig, err := LoadConfig()
	if err != nil {
		return c, err
	}

	if !unattended {
		cmd.PrintBranding(DefaultBanner)
		cmd.PrintText(agentConfig.Branding.Reset, "Reset")

		// We don't close the lock, as none of the following actions are expected to return
		lock := sync.Mutex{}
		go func() {
			// Wait for user input and go back to shell
			utils.Prompt("") //nolint:errcheck
			// give tty1 back
			svc, err := machine.Getty(1)
			if err == nil {
				svc.Start() //nolint:errcheck
			}

			lock.Lock()
			fmt.Println("Reset aborted")
			panic(utils.Shell().Run())
		}()

		if !agentConfig.Fast {
			time.Sleep(60 * time.Second)
		}

		lock.Lock()
	}

	ensureDataSourceReady()

	// This gets the options from an event that can be sent by anyone.
	// This should override the default config as it's much more dynamic
	bus.Manager.Response(sdk.EventBeforeReset, func(p *pluggable.Plugin, r *pluggable.EventResponse) {
		if r.Data == "" {
			return
		}
		err := json.Unmarshal([]byte(r.Data), &optionsFromEvent)
		if err != nil {
			fmt.Println(err)
		}
	})

	bus.Manager.Publish(sdk.EventBeforeReset, sdk.EventPayload{}) //nolint:errcheck

	// Prepare a config from the cli flags
	r := ExtraConfigReset{}
	r.Reset.ResetOem = resetOem

	if reboot {
		r.Reset.Reboot = true
	}

	// Override the config with the event options
	// Go over the possible options sent via event
	if len(optionsFromEvent) > 0 {
		if o := optionsFromEvent["reset-oem"]; o != "" {
			r.Reset.ResetOem = o == "true"
		}
	}

	d, err := json.Marshal(r)
	if err != nil {
		c.Logger.Errorf("failed to marshal reset cmdline flags/event options: %s", err)
		return c, err
	}
	cliConf := string(d)

	// cliconf goes last so it can override the rest of the config files
	c, err = config.Scan(collector.Directories(dir...), collector.Readers(strings.NewReader(cliConf)))
	if err != nil {
		return c, err
	}

	// Set strict validation from the event
	if len(optionsFromEvent) > 0 {
		if s := optionsFromEvent["strict"]; s != "" {
			c.Strict = s == "true"
		}
	}

	utils.SetEnv(c.Env)

	return c, nil
}

// ExtraConfigReset is the struct that holds the reset options that come from flags and events
type ExtraConfigReset struct {
	Reset struct {
		ResetOem bool `json:"reset-oem,omitempty"`
		Reboot   bool `json:"reboot,omitempty"`
	} `json:"reset"`
}
