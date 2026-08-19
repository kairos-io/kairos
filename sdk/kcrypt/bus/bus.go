package bus

import (
	"fmt"
	"os"

	"github.com/kairos-io/kairos-sdk/types/logger"
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
}

func (b *Bus) LoadProviders() {
	wd, _ := os.Getwd()
	b.Autoload(prefix, append(extensionPaths, wd)...).Register()
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
				log.Logger.Error().Err(fmt.Errorf("%s", r.Error)).Str("from", p.Name).Str("at", p.Executable).Str("type", string(e)).Msg("Error in provider")
				os.Exit(1)
			}
			if r.State != "" {
				log.Logger.Debug().Str("state", r.State).Str("from", p.Name).Str("at", p.Executable).Str("type", string(e)).Msg("Received event from provider")
			}
		})
	}
	b.registered = true
}
