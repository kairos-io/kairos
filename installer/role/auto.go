package role

import (
	"math/rand"
	"time"

	utils "github.com/mudler/edgevpn/pkg/utils"

	service "github.com/mudler/edgevpn/api/client/service"
)

// TODO: HA-Auto

func Auto(c *service.RoleConfig) error {
	advertizing, _ := c.Client.AdvertizingNodes()
	actives, _ := c.Client.ActiveNodes()

	c.Logger.Info("Active nodes:", actives)

	if len(actives) < 2 && len(advertizing) < 2 {
		c.Logger.Info("Not enough nodes")
		return nil
	}

	// first get available nodes
	nodes := advertizing
	leader := c.UUID

	if len(advertizing) != 0 {
		leader = utils.Leader(advertizing)
	}

	// From now on, only the leader keeps processing
	if leader != c.UUID {
		c.Logger.Infof("<%s> not a leader, leader is '%s', sleeping", c.UUID, leader)
		return nil
	}

	// Assign roles to nodes
	currentRoles := map[string]string{}
	rand.Seed(time.Now().Unix())

	existsMaster := false
	for _, a := range nodes {
		role, _ := c.Client.Get("role", a)
		currentRoles[a] = role
		if role == "master" {
			existsMaster = true
		}
	}

	if !existsMaster && len(nodes) > 0 {
		var selected string

		// select one node without roles to become master
		if len(nodes) == 1 {
			selected = nodes[0]
		} else {
			selected = nodes[rand.Intn(len(nodes)-1)]
		}

		c.Client.Set("role", selected, "master")
		c.Logger.Info("master role for", selected)
		currentRoles[selected] = "master"
	}

	// cycle all empty roles and assign worker roles
	for uuid, role := range currentRoles {
		if role == "" {
			c.Client.Set("role", uuid, "worker")
			c.Logger.Info("worker role for", uuid)
		}
	}

	return nil
}
