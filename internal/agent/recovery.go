package agent

import (
	"fmt"
	"time"

	"github.com/c3os-io/c3os/internal/bus"
	"github.com/c3os-io/c3os/internal/cmd"
	"github.com/c3os-io/c3os/pkg/utils"
	events "github.com/c3os-io/c3os/sdk/bus"

	machine "github.com/c3os-io/c3os/pkg/machine"
	qr "github.com/mudler/go-nodepair/qrcode"
	"github.com/mudler/go-pluggable"
	"github.com/pterm/pterm"
)

func Recovery() error {
	bus.Manager.Initialize()

	token := ""
	msg := ""
	busErr := ""

	bus.Manager.Response(events.EventRecovery, func(p *pluggable.Plugin, r *pluggable.EventResponse) {
		token = r.Data
		msg = r.State
		busErr = r.Error
	})

	cmd.PrintBranding(DefaultBanner)

	agentConfig, err := LoadConfig()
	if err != nil {
		return err
	}

	cmd.PrintText(agentConfig.Branding.Recovery, "Recovery")

	_, err = bus.Manager.Publish(events.EventRecovery, events.EventPayload{})
	if err != nil {
		return err
	}

	if busErr != "" {
		return fmt.Errorf(busErr)
	}

	time.Sleep(5 * time.Second)

	pterm.Info.Println(msg)

	if token != "" {
		qr.Print(token)
	}

	// Wait for user input and go back to shell
	utils.Prompt("") //nolint:errcheck
	_, err = bus.Manager.Publish(events.EventRecoveryStop, events.EventPayload{})
	if err != nil {
		return err
	}
	// give tty1 back
	svc, err := machine.Getty(1)
	if err == nil {
		svc.Start() //nolint:errcheck
	}

	return nil
}
