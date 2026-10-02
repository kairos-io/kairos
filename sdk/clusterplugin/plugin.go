package clusterplugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/kairos-io/kairos/v4/sdk/bus"
	"github.com/mudler/go-pluggable"
	yip "github.com/mudler/yip/pkg/schema"
	"github.com/twpayne/go-vfs/v4"
	"gopkg.in/yaml.v3"
)

const clusterProviderCloudConfigFile = "/usr/local/cloud-config/cluster.kairos.yaml"

const EventClusterReset pluggable.EventType = "cluster.reset"

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

	configFilePath := clusterProviderCloudConfigFile

	if len(config.Cluster.ClusterConfigPath) != 0 {
		configFilePath = config.Cluster.ClusterConfigPath
	}

	if err := writeClusterConfig(configFilePath, cc); err != nil {
		response.Error = err.Error()
		return response
	}

	return response
}

// writeClusterConfig writes the provider's cloud-config to configFilePath.
//
// The file holds the cluster token in cleartext and is written as root on
// every boot, at a path on the persistent partition, so the open has to
// survive whatever is sitting there already (kairos-io/kairos#4867):
//
//   - the parent directory is created first. A provider that points
//     cluster_config_path at a directory the image does not ship, or a node
//     whose /usr/local/cloud-config has not been created yet, used to fail the
//     whole boot event on ENOENT.
//   - O_NOFOLLOW refuses a symlink at the last path element. Without it a
//     symlink planted there redirects the write, and O_TRUNC empties whatever
//     it points at. Only the last element is protected, so a symlinked
//     cloud-config directory keeps working.
//   - O_NONBLOCK returns instead of parking in open(2) when the path is a FIFO
//     with no reader, and the fstat that follows rejects every file type that
//     is not a regular file. The flag does not change writes to a regular file.
//   - the mode is set after the open. O_CREATE applies perm only when it
//     creates the file, so a file pre-created by another user kept its own mode
//     and then received the cluster token.
//
// Planting any of these needs write access to a persistent path, so this is
// robustness rather than a privilege boundary. What it buys is that the failure
// is reported, instead of being a truncated stranger's file, a boot that hangs
// forever, or a token anyone can read.
func writeClusterConfig(configFilePath string, cc yip.YipConfig) error {
	dir := filepath.Dir(configFilePath)
	if err := vfs.MkdirAll(filesystem, dir, 0700); err != nil {
		return fmt.Errorf("failed to create the cluster config directory %s: %w", dir, err)
	}

	f, err := filesystem.OpenFile(configFilePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		// Name what is in the way. Both of these arrive as a bare errno that
		// says nothing about the path holding something other than a file.
		switch {
		case errors.Is(err, syscall.ELOOP):
			return fmt.Errorf("refusing to write the cluster config to %s: it is a symlink, not a regular file", configFilePath)
		case errors.Is(err, syscall.ENXIO):
			return fmt.Errorf("refusing to write the cluster config to %s: it is a named pipe, not a regular file", configFilePath)
		}

		return fmt.Errorf("failed to open the cluster config file %s: %w", configFilePath, err)
	}

	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat the cluster config file %s: %w", configFilePath, err)
	}

	if !stat.Mode().IsRegular() {
		return fmt.Errorf("refusing to write the cluster config to %s: it is %s, not a regular file", configFilePath, fileTypeName(stat.Mode()))
	}

	if stat.Mode().Perm() != 0600 {
		if err := f.Chmod(0600); err != nil {
			return fmt.Errorf("failed to restrict the cluster config file %s to 0600: %w", configFilePath, err)
		}
	}

	// write the cloud configuration header
	if _, err := f.WriteString("#cloud-config\n"); err != nil {
		return fmt.Errorf("failed to write the cluster config file %s: %w", configFilePath, err)
	}

	// encode the provider's configuration
	if err := yaml.NewEncoder(f).Encode(cc); err != nil {
		return fmt.Errorf("failed to encode the cluster config into %s: %w", configFilePath, err)
	}

	return nil
}

// fileTypeName names the file type in an error somebody has to act on, since
// "not a regular file" on its own does not say what to go and look for.
func fileTypeName(mode os.FileMode) string {
	switch {
	case mode&os.ModeNamedPipe != 0:
		return "a named pipe"
	case mode&os.ModeSocket != 0:
		return "a socket"
	case mode&os.ModeCharDevice != 0:
		return "a character device"
	case mode&os.ModeDevice != 0:
		return "a block device"
	case mode&os.ModeSymlink != 0:
		return "a symlink"
	case mode.IsDir():
		return "a directory"
	default:
		return "an irregular file"
	}
}

func (p ClusterPlugin) Run(extraPlugins ...pluggable.FactoryPlugin) error {
	plugins := []pluggable.FactoryPlugin{
		{
			EventType:     bus.EventBoot,
			PluginHandler: p.onBoot,
		},
	}
	plugins = append(plugins, extraPlugins...)

	f := pluggable.NewPluginFactory(plugins...)

	return f.Run(pluggable.EventType(os.Args[1]), os.Stdin, os.Stdout)
}
