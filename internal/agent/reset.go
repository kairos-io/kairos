package agent

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/kairos-io/kairos/internal/cmd"
	"github.com/kairos-io/kairos/pkg/machine"
	"github.com/kairos-io/kairos/pkg/utils"
	"github.com/pterm/pterm"
)

func Reset() error {

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

	time.Sleep(60 * time.Second)
	lock.Lock()
	args := []string{"reset"}
	args = append(args, "--reset-persistent")

	cmd := exec.Command("elemental", args...)
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

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

	time.Sleep(60 * time.Second)
	lock2.Lock()
	utils.Reboot()

	return nil
}
