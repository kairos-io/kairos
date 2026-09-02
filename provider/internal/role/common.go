package role

import (
	"os"

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

func getRoles(client *service.Client, nodes []string) ([]string, map[string]string) {
	unassignedNodes := []string{}
	currentRoles := map[string]string{}
	for _, a := range nodes {
		role, _ := client.Get("role", a)
		currentRoles[a] = role
		if role == "" {
			unassignedNodes = append(unassignedNodes, a)
		}
	}
	return unassignedNodes, currentRoles
}
