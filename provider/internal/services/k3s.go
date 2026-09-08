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

// K3sSpec describes a k3s service without naming an init system.
//
// No unit body: k3s' own packaging installs the openrc script and the systemd
// unit, so all Kairos contributes is where to write the arguments. On systemd
// that is the /etc/sysconfig default of the unit k3s ships.
func K3sSpec(name string) service.Spec {
	return service.Spec{
		Name: name,
		Init: map[service.Flavor]service.InitSpec{
			service.OpenRC: {EnvFile: K3sEnvFile(name)},
		},
	}
}
