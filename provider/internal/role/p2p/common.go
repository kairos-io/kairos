package role

import (
	"fmt"
	"net"

	providerConfig "github.com/kairos-io/kairos/v4/provider/internal/provider/config"
)

const (
	RoleWorker            = "worker"
	RoleMaster            = "master"
	RoleMasterHA          = "master/ha"
	RoleMasterClusterInit = "master/clusterinit"
	RoleAuto              = "auto"
)

func guessInterface(pconfig *providerConfig.Config) string {
	if pconfig.KubeVIP.Interface != "" {
		return pconfig.KubeVIP.Interface
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		fmt.Println("failed getting system interfaces")
		return ""
	}
	for _, i := range ifaces {
		if i.Name != "lo" {
			return i.Name
		}
	}
	return ""
}

// appendUserArgs folds the `args:` a Kubernetes block carries into the ones
// the role generated for it. `replace_args: true` means the block takes the
// command line over, so the generated ones are dropped.
//
// Every path that builds a k3s or k0s command line goes through here, so the
// two distributions cannot drift apart on what `args:` means.
func appendUserArgs(generated, user []string, replace bool) []string {
	if replace {
		return user
	}

	return append(generated, user...)
}
