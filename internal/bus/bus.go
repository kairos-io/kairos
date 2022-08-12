package bus

import (
	"fmt"
	"os"

	"github.com/c3os-io/c3os/sdk/bus"

	"github.com/mudler/go-pluggable"
)

// Manager is the bus instance manager, which subscribes plugins to events emitted.
var Manager = &Bus{
	Manager: pluggable.NewManager(
		bus.AllEvents,
	),
}

type Bus struct {
	*pluggable.Manager
}

func (b *Bus) LoadProviders() {
	wd, _ := os.Getwd()
	b.Manager.Autoload("agent-provider", "/system/providers", wd).Register()
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
