package bus

import (
	"fmt"
	"os"

	"github.com/kairos-io/kairos-sdk/bus"
	"github.com/mudler/go-pluggable"
)

// Manager is the bus instance manager, which subscribes plugins to events emitted.
var Manager = NewBus()

func NewBus() *Bus {
	return &Bus{
		Manager: pluggable.NewManager(
			bus.AllEvents,
		),
	}
}

func Reload() {
	Manager = NewBus()
	Manager.Initialize()
}

type Bus struct {
	*pluggable.Manager
	registered bool
}

// LoadProviders autoloads the agent providers from the given paths. When no
// paths are provided it falls back to the default provider directories.
func (b *Bus) LoadProviders(paths ...string) {
	if len(paths) == 0 {
		wd, _ := os.Getwd()
		paths = []string{"/system/providers", "/usr/local/system/providers", wd}
	}
	b.Manager.Autoload("agent-provider", paths...).Register()
}

func (b *Bus) HasRegisteredPlugins() bool {
	return len(b.Plugins) > 0
}

func (b *Bus) Initialize(paths ...string) {
	if b.registered {
		return
	}

	b.LoadProviders(paths...)
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
			}

			if r.State != "" {
				fmt.Println(fmt.Sprintf("[provider event: %s]", e), r.State)
			}
		})
	}
	b.registered = true
}
