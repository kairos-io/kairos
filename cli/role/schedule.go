package role

import (
	"math/rand"
	"time"

	service "github.com/mudler/edgevpn/api/client/service"
)

// scheduleRoles assigns roles to nodes. Meant to be called only by leaders
// TODO: HA-Auto
func scheduleRoles(nodes []string, c *service.RoleConfig) error {
	rand.Seed(time.Now().Unix())

	// Assign roles to nodes
	currentRoles := map[string]string{}

	existsMaster := false
	unassignedNodes := []string{}
	for _, a := range nodes {
		role, _ := c.Client.Get("role", a)
		currentRoles[a] = role
		if role == "master" {
			existsMaster = true
		} else if role == "" {
			unassignedNodes = append(unassignedNodes, a)
		}
	}

	c.Logger.Infof("I'm the leader. My UUID is: %s.\n Current assigned roles: %+v", c.UUID, currentRoles)
	c.Logger.Infof("Master already present: %t", existsMaster)
	c.Logger.Infof("Unassigned nodes: %+v", unassignedNodes)

	if !existsMaster && len(unassignedNodes) > 0 {
		var selected string

		// select one node without roles to become master
		if len(unassignedNodes) == 1 {
			selected = unassignedNodes[0]
		} else {
			selected = unassignedNodes[rand.Intn(len(unassignedNodes)-1)]
		}

		c.Client.Set("role", selected, "master")
		c.Logger.Info("-> Set master to", selected)
		currentRoles[selected] = "master"
	}

	// cycle all empty roles and assign worker roles
	for _, uuid := range unassignedNodes {
		c.Client.Set("role", uuid, "worker")
		c.Logger.Info("-> Set worker to", uuid)
	}

	c.Logger.Info("Done scheduling")

	return nil
}
