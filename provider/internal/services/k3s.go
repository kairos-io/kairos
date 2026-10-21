package services

import (
	"fmt"

	"github.com/kairos-io/kairos/v4/sdk/machine/service"
)

// K3sServiceNames are the two k3s services: the server and the agent.
var K3sServiceNames = []string{"k3s", "k3s-agent"}

// K3sEnvFile is the file the k3s openrc script sources for its environment and
// for the arguments the provider writes at bootstrap. k3s ships the script and
// the path, Kairos only writes to it.
func K3sEnvFile(unit string) string {
	return fmt.Sprintf("/etc/rancher/k3s/%s.env", unit)
}

// K3sSysconfigFile is the EnvironmentFile of the systemd unit k3s packages,
// which is where the provider writes the node's environment on a systemd host.
func K3sSysconfigFile(unit string) string {
	return fmt.Sprintf("/etc/sysconfig/%s", unit)
}

// K3sSpec describes a k3s service without naming an init system.
//
// No unit body: k3s' own packaging installs the openrc script and the systemd
// unit, so all Kairos contributes is where to write the arguments. Both paths
// are stated here rather than defaulted in a backend, because they are k3s'
// packaging, not a convention of the init system.
func K3sSpec(name string) service.Spec {
	return service.Spec{
		Name: name,
		Init: map[service.Flavor]service.InitSpec{
			service.OpenRC:  {EnvFile: K3sEnvFile(name)},
			service.Systemd: {EnvFile: K3sSysconfigFile(name)},
		},
	}
}
