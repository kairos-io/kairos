package agent

import (
	"fmt"

	events "github.com/kairos-io/kairos-sdk/bus"
	"github.com/kairos-io/kairos/internal/bus"
	"github.com/kairos-io/kairos/pkg/config"
	"github.com/kairos-io/kairos/pkg/config/collector"
	"github.com/mudler/go-pluggable"
)

func Notify(event string, dirs []string) error {
	bus.Manager.Initialize()

	c, err := config.Scan(collector.Directories(dirs...))
	if err != nil {
		return err
	}

	if !events.IsEventDefined(event) {
		return fmt.Errorf("event '%s' not defined", event)
	}

	configStr, err := c.String()
	if err != nil {
		return err
	}
	_, err = bus.Manager.Publish(pluggable.EventType(event), events.EventPayload{
		Config: configStr,
	})

	return err
}
