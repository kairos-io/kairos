package agent

import (
	"fmt"

	"github.com/kairos-io/kairos/internal/bus"
	"github.com/kairos-io/kairos/pkg/config"
	events "github.com/kairos-io/kairos/sdk/bus"
	"github.com/mudler/go-pluggable"
)

func Notify(event string, dirs []string) error {
	bus.Manager.Initialize()

	c, err := config.Scan(config.Directories(dirs...))
	if err != nil {
		return err
	}

	if !events.IsEventDefined(event) {
		return fmt.Errorf("event '%s' not defined", event)
	}

	_, err = bus.Manager.Publish(pluggable.EventType(event), events.EventPayload{
		Config: c.String(),
	})

	return err
}
