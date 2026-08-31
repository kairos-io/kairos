package cli

import (
	"fmt"

	edgeVPNClient "github.com/mudler/edgevpn/api/client"
	"github.com/mudler/edgevpn/api/client/service"
	"github.com/urfave/cli/v2"
)

var RoleCMD = cli.Command{
	Name:  "role",
	Usage: "Set or list node roles",
	Subcommands: []*cli.Command{
		{
			Flags:     networkAPI,
			Name:      "set",
			Usage:     "Set a node role",
			UsageText: "kairos role set <UUID> master",
			Description: `
		Sets a node role propagating the setting to the network.

		A role must be set prior to the node joining a network. You can retrieve a node UUID by running "kairos uuid".

		Example:

		$ (node A) kairos uuid
		$ (node B) kairos role set <UUID of node A> master
		`,
			Action: func(c *cli.Context) error {
				cc := service.NewClient(
					c.String("network-id"),
					edgeVPNClient.NewClient(edgeVPNClient.WithHost(c.String(apiFlagName))))
				return cc.Set("role", c.Args().Get(0), c.Args().Get(1))
			},
		},
		{
			Flags:       networkAPI,
			Name:        "list",
			Description: "List node roles",
			Action: func(c *cli.Context) error {
				address := c.String(apiFlagName)
				cc := service.NewClient(
					c.String("network-id"),
					edgeVPNClient.NewClient(edgeVPNClient.WithHost(address)))

				advertizing, err := cc.AdvertizingNodes()
				if err != nil {
					return apiError("node list", address, err)
				}

				fmt.Printf("%-47s  %-30s  %-15s\n", "Node", "Role", "IP")
				fmt.Printf("%s  %s  %s\n",
					"-----------------------------------------------",
					"------------------------------",
					"---------------")
				for _, a := range advertizing {
					// Deliberately tolerant, unlike the call above. A node that
					// is advertizing itself before a role or an IP has been
					// assigned to it is an ordinary state during bootstrap, and
					// an empty cell is the honest way to show it. The listing
					// itself already succeeded, so the API is reachable.
					role, _ := cc.Get("role", a)
					ip, _ := cc.Get("ip", a)
					fmt.Printf("%-47s  %-30s  %-15s\n", a, role, ip)
				}
				return nil
			},
		},
	},
}
