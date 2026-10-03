package bus

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/mudler/go-pluggable"
)

const EventDiscoveryPassword pluggable.EventType = "discovery.password"

const prefix = "kcrypt-discovery"

// extensionPaths is a list of paths where the bus will look for plugins.
var extensionPaths = []string{
	"/sysroot/system/discovery",
	"/system/discovery",
	"/oem/kcrypt",
	"/oem/system/discovery",
}

// Manager is the bus instance manager, which subscribes plugins to events emitted.
var Manager = NewBus()

func NewBus() *Bus {
	return &Bus{
		Manager: pluggable.NewManager([]pluggable.EventType{EventDiscoveryPassword}),
	}
}

func Reload() {
	Manager = NewBus()
	Manager.Initialize()
}

type Bus struct {
	*pluggable.Manager
	registered bool
	// mu guards providerErrs. Discovery providers run concurrently, so the
	// response handler that appends to it runs on several goroutines at once.
	mu sync.Mutex
	// providerErrs holds the errors providers reported during the most recent
	// Publish. Publish returns them so the caller can decide what to do.
	providerErrs []error
}

func (b *Bus) LoadProviders() {
	wd, _ := os.Getwd()
	b.Autoload(prefix, append(extensionPaths, wd)...).Register()
}

// Publish asks every registered discovery provider for the password and
// returns the first failure it meets: the publishing error, or the errors the
// providers themselves reported.
//
// A provider that fails must not end the process: the other providers are the
// fallback that unlocks the partition, and the caller is the only place that
// knows which partition was being unlocked.
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

func (b *Bus) Initialize() {
	if b.registered {
		return
	}

	level := "info"
	if os.Getenv("BUS_DEBUG") == "true" {
		level = "debug"
	}

	log := logger.NewKairosLogger("kcrypt", level, false)
	defer log.Close()

	b.LoadProviders()
	for i := range b.Events {
		e := b.Events[i]
		b.Response(e, func(p *pluggable.Plugin, r *pluggable.EventResponse) {
			log.Logger.Debug().Str("from", p.Name).Str("at", p.Executable).Str("type", string(e)).Msg("Received event from provider")
			if r.Errored() {
				err := fmt.Errorf("provider %s at %s had an error: %s", p.Name, p.Executable, r.Error)
				log.Logger.Error().Err(err).Str("from", p.Name).Str("at", p.Executable).Str("type", string(e)).Msg("Error in provider")
				b.mu.Lock()
				b.providerErrs = append(b.providerErrs, err)
				b.mu.Unlock()
			}
			if r.State != "" {
				log.Logger.Debug().Str("state", r.State).Str("from", p.Name).Str("at", p.Executable).Str("type", string(e)).Msg("Received event from provider")
			}
		})
	}
	b.registered = true
}
