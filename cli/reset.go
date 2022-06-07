package main

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/c3os-io/c3os/cli/machine"
	"github.com/c3os-io/c3os/cli/utils"
	"github.com/pterm/pterm"
	"github.com/urfave/cli"
)

func reset(c *cli.Context) error {

	utils.PrintBanner(banner)

	pterm.DefaultBox.WithTitle("Reset").WithTitleBottomRight().WithRightPadding(0).WithBottomPadding(0).Println(
		`Welcome to c3os!
The node will automatically reset its state in a few.`)

	pterm.Info.Println("Press any key to abort this process. To restart run 'c3os reset'.")

	pterm.Info.Println("Starting in 60 seconds...")
	pterm.Print("\n\n") // Add two new lines as spacer.

	// We don't close the lock, as none of the following actions are expected to return
	lock := sync.Mutex{}
	go func() {
		// Wait for user input and go back to shell
		utils.Prompt("")
		// give tty1 back
		svc, err := machine.Getty(1)
		if err == nil {
			svc.Start()
		}

		fmt.Println("Reset aborted")
		lock.Lock()
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
	return nil
}
