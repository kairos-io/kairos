package bus

import (
	"fmt"
	"os"
	"slices"

	"github.com/kairos-io/kairos/v4/sdk/bus"
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
	// loadedPaths are the directories LoadProviders actually read the
	// providers from, so that a later Initialize can tell whether it is asking
	// for something different.
	loadedPaths []string
}

// LoadProviders autoloads the agent providers from the given paths. When no
// paths are provided it falls back to the default provider directories.
func (b *Bus) LoadProviders(paths ...string) {
	if len(paths) == 0 {
		wd, _ := os.Getwd()
		paths = []string{"/system/providers", "/usr/local/system/providers", wd}
	}
	b.loadedPaths = paths
	b.Manager.Autoload("agent-provider", paths...).Register()
}

// LoadedPaths returns the directories the providers currently on the bus were
// loaded from.
func (b *Bus) LoadedPaths() []string {
	return b.loadedPaths
}

func (b *Bus) HasRegisteredPlugins() bool {
	return len(b.Plugins) > 0
}

// Initialize loads the providers found in paths, or in the default provider
// directories when paths is empty, and subscribes the response handlers.
//
// It takes effect once per bus. Registering subscribes every provider loaded
// so far to every event, so a second registration would make each provider
// already on the bus run twice for the same event. The directories of the
// first call are therefore the ones the process uses, and a later call asking
// for different ones is reported instead of being dropped in silence. A
// command that wants specific provider directories has to be the one that
// initializes the bus.
func (b *Bus) Initialize(paths ...string) {
	if b.registered {
		if len(paths) > 0 && !slices.Equal(paths, b.loadedPaths) {
			fmt.Printf("warning: the bus already loaded its providers from %v, so %v is ignored\n",
				b.loadedPaths, paths)
		}
		return
	}

	b.LoadProviders(paths...)
	for i := range b.Events {
		e := b.Events[i]
		b.Response(e, func(p *pluggable.Plugin, r *pluggable.EventResponse) {
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
