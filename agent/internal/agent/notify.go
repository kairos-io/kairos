package agent

import (
	"fmt"

	"github.com/kairos-io/kairos-agent/v2/internal/bus"
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	events "github.com/kairos-io/kairos-sdk/bus"
	"github.com/kairos-io/kairos-sdk/collector"
	"github.com/mudler/go-pluggable"
	"gopkg.in/yaml.v3"
)

func Notify(event string, dirs []string) error {
	c, err := config.Scan(collector.Directories(dirs...))
	if err != nil {
		return err
	}

	// Check if the config defines a custom ordered list of provider paths under
	// `providers.paths`. If it does, use it to override the default provider
	// directories so the dirs order can be controlled. Otherwise the bus falls
	// back to the existing default paths.
	var providerPaths []string
	if paths, qerr := c.Collector.Query("providers.paths"); qerr == nil && paths != "" {
		if uerr := yaml.Unmarshal([]byte(paths), &providerPaths); uerr != nil {
			c.Logger.Warnf("failed to parse providers.paths, using default provider paths: %s", uerr.Error())
		}
	}

	bus.Manager.Initialize(providerPaths...)

	if !events.IsEventDefined(event) {
		return fmt.Errorf("event '%s' not defined", event)
	}

	configStr, err := c.Collector.String()
	if err != nil {
		return err
	}
	_, err = bus.Manager.Publish(pluggable.EventType(event), events.EventPayload{
		Config: configStr,
	})

	return err
}
