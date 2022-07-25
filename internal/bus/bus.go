package bus

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/c3os-io/c3os/pkg/bus"
	"github.com/mudler/go-pluggable"
)

// Manager is the bus instance manager, which subscribes plugins to events emitted.
var Manager = &Bus{
	Manager: pluggable.NewManager(
		[]pluggable.EventType{
			bus.EventBootstrap,
			bus.EventChallenge,
			bus.EventInstall,
		},
	),
}

type Bus struct {
	*pluggable.Manager
}

func (b *Bus) LoadProviders() {
	b.Manager.Autoload("agent-provider", "/system/providers").Register()
}

func (b *Bus) Initialize() {
	b.LoadProviders()
	for i := range b.Manager.Events {
		e := b.Manager.Events[i]
		b.Manager.Response(e, func(p *pluggable.Plugin, r *pluggable.EventResponse) {
			if os.Getenv("BUS_DEBUG") == "true" {
				fmt.Println(
					fmt.Sprintf("[provider event: %s]", e),
					"received from",
					p.Name,
					"at",
					p.Executable,
					r,
				)
			}
			if r.Errored() {
				err := fmt.Sprintf("Provider %s at %s had an error: %s", p.Name, p.Executable, r.Error)
				fmt.Println(err)
				os.Exit(1)
			} else {
				if r.State != "" {
					fmt.Println(fmt.Sprintf("[provider event: %s]", e), r.State)
				}
			}
		})
	}
}

func RunHookScript(s string) error {
	_, err := os.Stat(s)
	if err != nil {
		return nil
	}
	cmd := exec.Command(s)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
