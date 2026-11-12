package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	hook "github.com/kairos-io/kairos/v4/agent/internal/agent/hooks"
	"github.com/kairos-io/kairos/v4/agent/internal/bus"
	"github.com/kairos-io/kairos/v4/agent/internal/cmd"
	"github.com/kairos-io/kairos/v4/agent/pkg/action"
	"github.com/kairos-io/kairos/v4/agent/pkg/config"
	"github.com/kairos-io/kairos/v4/agent/pkg/uki"
	internalutils "github.com/kairos-io/kairos/v4/agent/pkg/utils"
	"github.com/kairos-io/kairos/v4/sdk/branding"
	sdk "github.com/kairos-io/kairos/v4/sdk/bus"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	"github.com/kairos-io/kairos/v4/sdk/machine"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	"github.com/kairos-io/kairos/v4/sdk/utils"

	"github.com/mudler/go-pluggable"
	"golang.org/x/term"
)

func Reset(reboot, unattended, resetOem, strictValidations bool, dir ...string) error {
	// In both cases we want
	if internalutils.UkiBootMode() == internalutils.UkiHDD {
		return resetUki(reboot, unattended, resetOem, strictValidations, dir...)
	} else if internalutils.UkiBootMode() == internalutils.UkiRemovableMedia {
		return fmt.Errorf("reset is not supported on removable media, please run reset from the installed system recovery entry")
	} else {
		return reset(reboot, unattended, resetOem, strictValidations, dir...)
	}
}

func reset(reboot, unattended, resetOem, strictValidations bool, dir ...string) error {
	cfg, err := sharedReset(reboot, unattended, resetOem, strictValidations, dir...)
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

func resetUki(reboot, unattended, resetOem, strictValidations bool, dir ...string) error {
	cfg, err := sharedReset(reboot, unattended, resetOem, strictValidations, dir...)
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

// operatorAbortedReset blocks on the prompt and reports whether the operator
// asked to abort the reset. On a terminal any return from the prompt is an
// abort, including EOF from Ctrl-D. Without a terminal a read error (stdin at
// EOF, /dev/null, a closed pipe) lets the reset go on.
func operatorAbortedReset(prompt func(string) (string, error), stdinIsTerminal bool) bool {
	_, err := prompt("")
	return err == nil || stdinIsTerminal
}

// abortedResetExitCode runs the shell handed to the operator after an aborted
// reset and returns the exit code for the agent once that shell is gone.
func abortedResetExitCode(shell func() error) int {
	if err := shell(); err != nil {
		fmt.Printf("shell exited with error: %s\n", err)
		return 1
	}
	return 0
}

// sharedReset is the common reset code for both uki and non-uki
// sets the config, runs the event handler, publish the envent and gets the config
func sharedReset(reboot, unattended, resetOem, strictValidations bool, dir ...string) (c *sdkConfig.Config, err error) {
	bus.Manager.Initialize()
	var optionsFromEvent map[string]string

	// This config is only for reset branding.
	agentConfig, err := branding.LoadConfig()
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
			if !operatorAbortedReset(utils.Prompt, term.IsTerminal(int(os.Stdin.Fd()))) {
				return
			}
			// give tty1 back
			svc, err := machine.Getty(1)
			if err == nil {
				svc.Start() //nolint:errcheck
			}

			lock.Lock()
			fmt.Println("Reset aborted")
			os.Exit(abortedResetExitCode(utils.Shell().Run))
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

	c, err = scanResetConfig(cliConf, strictValidations, dir...)
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

// scanResetConfig reads the cloud config a reset will run with. cliConf goes
// last so the command line options override the config files, and
// strictValidations decides whether a config that does not match the schema
// stops the reset or only prints a warning, as it does for install and
// upgrade.
func scanResetConfig(cliConf string, strictValidations bool, dir ...string) (*sdkConfig.Config, error) {
	return config.Scan(
		collector.Directories(dir...),
		collector.Readers(strings.NewReader(cliConf)),
		collector.StrictValidation(strictValidations),
	)
}

// ExtraConfigReset is the struct that holds the reset options that come from flags and events
type ExtraConfigReset struct {
	Reset struct {
		ResetOem bool `json:"reset-oem,omitempty"`
		Reboot   bool `json:"reboot,omitempty"`
	} `json:"reset"`
}
