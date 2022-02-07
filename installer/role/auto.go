package role

import (
	"hash/fnv"
	"math/rand"
	"time"

	service "github.com/mudler/edgevpn/api/client/service"
)

func hash(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

// TODO: HA-Auto

func Auto(c *service.RoleConfig) error {
	actives, _ := c.Client.ActiveNodes()

	c.Logger.Info("Active nodes:", actives)

	if len(actives) < 2 {
		c.Logger.Info("Not enough nodes")
		return nil
	}

	// first get available nodes
	nodes := []string{}
	leaderboard := map[string]uint32{}

	leader := actives[0]

	// Compute who is leader at the moment
	for _, a := range actives {
		leaderboard[a] = hash(a)
		if leaderboard[leader] < leaderboard[a] {
			leader = a
		}
		// This prevent to assign roles to ourselves
		//if a != c.UUID {
		nodes = append(nodes, a)
		//}
	}

	// From now on, only the leader keeps processing
	c.Logger.Info("Leaderboard: ", leaderboard)
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
