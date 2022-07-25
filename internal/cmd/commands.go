package cmd

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	providerConfig "github.com/c3os-io/c3os/internal/provider/config"
	edgeVPNClient "github.com/mudler/edgevpn/api/client"
	"github.com/mudler/edgevpn/api/client/service"
	"github.com/mudler/edgevpn/pkg/node"

	"github.com/urfave/cli"
	"gopkg.in/yaml.v2"
)

func CommonCommand(cmds ...cli.Command) []cli.Command {
	return append(commonCommands, cmds...)
}

var commonCommands = []cli.Command{
	{
		Name:      "get-kubeconfig",
		Usage:     "Return a deployment kubeconfig",
		UsageText: "Retrieve a c3os network kubeconfig (only for automated deployments)",
		Description: `
Retrieve a network kubeconfig and prints out to screen.

If a deployment was bootstrapped with a network token, you can use this command to retrieve the master node kubeconfig of a network id.

For example:

$ c3os get-kubeconfig --network-id c3os
`,
		Flags: networkAPI,
		Action: func(c *cli.Context) error {
			cc := service.NewClient(
				c.String("network-id"),
				edgeVPNClient.NewClient(edgeVPNClient.WithHost(c.String("api"))))
			str, _ := cc.Get("kubeconfig", "master")
			b, _ := base64.RawURLEncoding.DecodeString(str)
			masterIP, _ := cc.Get("master", "ip")
			fmt.Println(strings.ReplaceAll(string(b), "127.0.0.1", masterIP))
			return nil
		},
	},
	{
		Name:  "role",
		Usage: "Set or list node roles",
		Subcommands: []cli.Command{
			{
				Flags:     networkAPI,
				Name:      "set",
				Usage:     "Set a node role",
				UsageText: "c3os role set <UUID> master",
				Description: `
Sets a node role propagating the setting to the network.

A role must be set prior to the node joining a network. You can retrieve a node UUID by running "c3os uuid".

Example:

$ (node A) c3os uuid
$ (node B) c3os role set <UUID of node A> master
`,
				Action: func(c *cli.Context) error {
					cc := service.NewClient(
						c.String("network-id"),
						edgeVPNClient.NewClient(edgeVPNClient.WithHost(c.String("api"))))
					return cc.Set("role", c.Args()[0], c.Args()[1])
				},
			},
			{
				Flags:       networkAPI,
				Name:        "list",
				Description: "List node roles",
				Action: func(c *cli.Context) error {
					cc := service.NewClient(
						c.String("network-id"),
						edgeVPNClient.NewClient(edgeVPNClient.WithHost(c.String("api"))))
					advertizing, _ := cc.AdvertizingNodes()
					fmt.Println("Node\tRole")
					for _, a := range advertizing {
						role, _ := cc.Get("role", a)
						fmt.Printf("%s\t%s\n", a, role)
					}
					return nil
				},
			},
		},
	},
	{
		Name:      "create-config",
		Aliases:   []string{"c"},
		UsageText: "Create a config with a generated network token",

		Usage: "Creates a pristine config file",
		Description: `
Prints a vanilla YAML configuration on screen which can be used to bootstrap a c3os network.
`,
		ArgsUsage: "Optionally takes a token rotation interval (seconds)",

		Action: func(c *cli.Context) error {
			l := int(^uint(0) >> 1)
			args := c.Args()
			if len(args) > 0 {
				if i, err := strconv.Atoi(args[0]); err == nil {
					l = i
				}
			}
			cc := &providerConfig.Config{C3OS: &providerConfig.C3OS{NetworkToken: node.GenerateNewConnectionData(l).Base64()}}
			y, _ := yaml.Marshal(cc)
			fmt.Println(string(y))
			return nil
		},
	},
	{
		Name:      "generate-token",
		Aliases:   []string{"g"},
		UsageText: "Generate a network token",
		Usage:     "Creates a new token",
		Description: `
Generates a new token which can be used to bootstrap a c3os network.
`,
		ArgsUsage: "Optionally takes a token rotation interval (seconds)",

		Action: func(c *cli.Context) error {
			l := int(^uint(0) >> 1)
			args := c.Args()
			if len(args) > 0 {
				if i, err := strconv.Atoi(args[0]); err == nil {
					l = i
				}
			}
			fmt.Println(node.GenerateNewConnectionData(l).Base64())
			return nil
		},
	},
}
