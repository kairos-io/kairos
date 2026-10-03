package bus

import (
	"errors"
	"fmt"
	"os"
	"sync"

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
	// mu guards providerErrs. Providers run concurrently, so the response
	// handler that appends to it runs on several goroutines at once.
	mu sync.Mutex
	// providerErrs holds the errors providers reported during the most recent
	// Publish. Publish returns them so the caller can decide what to do.
	providerErrs []error
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

// Publish sends the event to every registered provider and returns the first
// failure it meets: the publishing error, or the errors the providers
// themselves reported.
//
// A provider that fails must not end the process: that would skip every other
// provider and every deferred cleanup the caller has in flight, in the middle
// of an install, an upgrade or a reset.
//
// The underlying Publish runs every provider in its own goroutine and only
// returns once all of them have answered, so the errors are complete by the
// time it does, but they are collected under b.mu rather than in order.
func (b *Bus) Publish(event pluggable.EventType, obj interface{}) (*pluggable.Manager, error) {
	b.mu.Lock()
	b.providerErrs = nil
	b.mu.Unlock()

	m, err := b.Manager.Publish(event, obj)
	if err != nil {
		return m, err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	return m, errors.Join(b.providerErrs...)
}

func (b *Bus) Initialize(paths ...string) {
	if b.registered {
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
				err := fmt.Errorf("provider %s at %s had an error: %s", p.Name, p.Executable, r.Error)
				fmt.Println(err)
				b.mu.Lock()
				b.providerErrs = append(b.providerErrs, err)
				b.mu.Unlock()
			}

			if r.State != "" {
				fmt.Println(fmt.Sprintf("[provider event: %s]", e), r.State)
			}
		})
	}
	b.registered = true
}
