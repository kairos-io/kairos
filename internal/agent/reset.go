package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	sdk "github.com/kairos-io/kairos-sdk/bus"
	hook "github.com/kairos-io/kairos/internal/agent/hooks"
	"github.com/kairos-io/kairos/internal/bus"
	"github.com/kairos-io/kairos/internal/cmd"
	"github.com/kairos-io/kairos/pkg/config"
	"github.com/kairos-io/kairos/pkg/machine"
	"github.com/kairos-io/kairos/pkg/utils"

	"github.com/mudler/go-pluggable"
	"github.com/pterm/pterm"
)

func Reset(dir ...string) error {
	bus.Manager.Initialize()

	options := map[string]string{}

	bus.Manager.Response(sdk.EventBeforeReset, func(p *pluggable.Plugin, r *pluggable.EventResponse) {
		err := json.Unmarshal([]byte(r.Data), &options)
		if err != nil {
			fmt.Println(err)
		}
	})

	cmd.PrintBranding(DefaultBanner)

	agentConfig, err := LoadConfig()
	if err != nil {
		return err
	}

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
	args := []string{"reset"}

	bus.Manager.Publish(sdk.EventBeforeReset, sdk.EventPayload{}) //nolint:errcheck

	optsArgs := optsToArgs(options)
	if len(optsArgs) > 0 {
		args = append(args, optsArgs...)
	} else {
		args = append(args, "--reset-persistent")
	}

	c, err := config.Scan(config.Directories(dir...))
	if err != nil {
		return err
	}

	utils.SetEnv(c.Env)

	cmd := exec.Command("elemental", args...)
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if err := hook.Run(*c, hook.AfterReset...); err != nil {
		return err
	}

	bus.Manager.Publish(sdk.EventAfterReset, sdk.EventPayload{}) //nolint:errcheck

	pterm.Info.Println("Rebooting in 60 seconds, press Enter to abort...")

	// We don't close the lock, as none of the following actions are expected to return
	lock2 := sync.Mutex{}
	go func() {
		// Wait for user input and go back to shell
		utils.Prompt("") //nolint:errcheck
		// give tty1 back
		svc, err := machine.Getty(1)
		if err == nil {
			svc.Start() //nolint:errcheck
		}

		lock2.Lock()
		fmt.Println("Reboot aborted")
		panic(utils.Shell().Run())
	}()

	if !agentConfig.Fast {
		time.Sleep(60 * time.Second)
	}
	lock2.Lock()
	utils.Reboot()

	return nil
}
