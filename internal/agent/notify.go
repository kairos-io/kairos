package agent

import (
	"github.com/c3os-io/c3os/internal/bus"
	"github.com/c3os-io/c3os/pkg/config"
	events "github.com/c3os-io/c3os/sdk/bus"
	"github.com/mudler/go-pluggable"
)

func Notify(event string, dirs []string) error {
	bus.Manager.Initialize()

	c, err := config.Scan(config.Directories(dirs...))
	if err != nil {
		return err
	}

	_, err = bus.Manager.Publish(pluggable.EventType(event), events.EventPayload{
		Config: c.String(),
	})

	return err
}
