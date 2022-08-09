package clusterplugin

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/c3os-io/c3os/sdk/bus"
	"github.com/mudler/go-pluggable"
	yip "github.com/mudler/yip/pkg/schema"
	"gopkg.in/yaml.v2"
)

const clusterProviderCloudConfigFile = "/usr/local/cloud-config/cluster.c3os.yaml"

// ClusterProvider returns a yip configuration that configures a Kubernetes engine.  The yip config may use any elemental
// stages after initramfs.
type ClusterProvider func(cluster Cluster) yip.YipConfig

// ClusterPlugin creates a cluster plugin from a `ClusterProvider`.  It calls the cluster provider at the appropriate events
// and ensures it configuration is written where it will be executed.
type ClusterPlugin struct {
	Provider ClusterProvider
}

func (p ClusterPlugin) onBoot(event *pluggable.Event) pluggable.EventResponse {
	var payload bus.EventPayload
	var config Config
	var response pluggable.EventResponse

	// parse the boot payload
	if err := json.Unmarshal([]byte(event.Data), &payload); err != nil {
		response.Error = fmt.Sprintf("failed to parse boot event: %s", err.Error())
		return response
	}

	// parse config from boot payload
	if err := yaml.Unmarshal([]byte(payload.Config), &config); err != nil {
		response.Error = fmt.Sprintf("failed to parse config from boot event: %s", err.Error())
		return response
	}

	if config.Cluster == nil {
		return response
	}

	// request the cloud configuration of the provider
	cc := p.Provider(*config.Cluster)

	// open our cloud configuration file for writing
	f, err := filesystem.OpenFile(clusterProviderCloudConfigFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		response.Error = fmt.Sprintf("failed to parse boot event: %s", err.Error())
		return response
	}

	defer f.Close()

	// write the cloud configuration header
	_, err = f.WriteString("#cloud-config\n")
	if err != nil {
		response.Error = fmt.Sprintf("failed to parse boot event: %s", err.Error())
		return response
	}

	// encode the provider's configuration
	err = yaml.NewEncoder(f).Encode(cc)
	if err != nil {
		response.Error = fmt.Sprintf("failed to parse boot event: %s", err.Error())
		return response
	}

	return response
}

func (p ClusterPlugin) Run() error {
	return pluggable.NewPluginFactory(
		pluggable.FactoryPlugin{
			EventType:     bus.EventBoot,
			PluginHandler: p.onBoot,
		},
	).Run(pluggable.EventType(os.Args[1]), os.Stdin, os.Stdout)
}
