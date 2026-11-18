package cli

import (
	"fmt"
	"slices"
	"strings"

	providerConfig "github.com/kairos-io/kairos/v4/provider/internal/provider/config"
	p2p "github.com/kairos-io/kairos/v4/provider/internal/role/p2p"
	edgeVPNClient "github.com/mudler/edgevpn/api/client"
	"github.com/mudler/edgevpn/api/client/service"
	"github.com/urfave/cli/v2"
)

// settableRoles are the values "role set" accepts: every role the provider
// knows how to run, plus "none", which clears an assignment.
var settableRoles = []string{
	p2p.RoleWorker,
	p2p.RoleMaster,
	p2p.RoleMasterHA,
	p2p.RoleMasterClusterInit,
	p2p.RoleAuto,
	providerConfig.RoleNone,
}

func validateRoleSetArgs(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: kairos provider role set <UUID> <role>: expected 2 arguments, got %d", len(args))
	}
	if !slices.Contains(settableRoles, args[1]) {
		return fmt.Errorf("unknown role %q, valid roles are: %s", args[1], strings.Join(settableRoles, ", "))
	}
	return nil
}

var RoleCMD = cli.Command{
	Name:  "role",
	Usage: "Set or list node roles",
	Subcommands: []*cli.Command{
		{
			Flags:     networkAPI,
			Name:      "set",
			Usage:     "Set a node role",
			UsageText: "kairos provider role set <UUID> <role>",
			Description: `
		Sets a node role propagating the setting to the network.

		A role must be set prior to the node joining a network. You can retrieve a node UUID by running "kairos agent uuid".

		Example:

		$ (node A) kairos agent uuid
		$ (node B) kairos provider role set <UUID of node A> master

		Valid roles: worker, master, master/ha, master/clusterinit, auto, none.
		`,
			Action: func(c *cli.Context) error {
				if err := validateRoleSetArgs(c.Args().Slice()); err != nil {
					return err
				}
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
