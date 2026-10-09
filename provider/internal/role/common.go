package role

import (
	"os"

	providerConfig "github.com/kairos-io/kairos/v4/provider/internal/provider/config"
	service "github.com/mudler/edgevpn/api/client/service"
)

type Role func(*service.RoleConfig) error

func SentinelExist() bool {
	if _, err := os.Stat("/usr/local/.kairos/deployed"); err == nil {
		return true
	}
	return false
}

func CreateSentinel() error {
	return os.WriteFile("/usr/local/.kairos/deployed", []byte{}, os.ModePerm)
}

// unassignedRole reports whether a `role/<UUID>` ledger value means the node
// still needs a role from the leader.
//
// The empty string is the obvious case: nothing was ever written. `none` is
// the one that is not obvious. It was the default the Kairos schema advertised
// for `p2p.role`, and a binary from before the fix for #4670 wrote it straight
// into the ledger from the configuration before NewK8sNode refused it, so
// every cluster that hit that bug has entries which are non-empty and yet were
// never an assignment. Reading them as assigned is what leaves those nodes
// wedged after an upgrade: the leader never puts them in unassignedNodes, so
// nothing ever writes a real role over them.
func unassignedRole(role string) bool {
	return role == "" || role == providerConfig.RoleNone
}

func getRoles(client *service.Client, nodes []string) ([]string, map[string]string) {
	unassignedNodes := []string{}
	currentRoles := map[string]string{}
	for _, a := range nodes {
		role, _ := client.Get("role", a)
		currentRoles[a] = role
		if unassignedRole(role) {
			unassignedNodes = append(unassignedNodes, a)
		}
	}
	return unassignedNodes, currentRoles
}
