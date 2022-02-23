package role

import (
	utils "github.com/mudler/edgevpn/pkg/utils"

	service "github.com/mudler/edgevpn/api/client/service"
)


func Auto() Role {
	return func(c *service.RoleConfig) error {
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

		return scheduleRoles(nodes, c)
	}
}
