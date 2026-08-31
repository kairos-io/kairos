package cli

import (
	"encoding/base64"
	"fmt"
	"strings"

	edgeVPNClient "github.com/mudler/edgevpn/api/client"
	"github.com/mudler/edgevpn/api/client/service"
	"github.com/urfave/cli/v2"
)

var GetKubeConfigCMD = cli.Command{
	Name:      "get-kubeconfig",
	Usage:     "Return a deployment kubeconfig",
	UsageText: "Retrieve a kairos network kubeconfig (only for automated deployments)",
	Description: `
		Retrieve a network kubeconfig and prints out to screen.
		
		If a deployment was bootstrapped with a network token, you can use this command to retrieve the master node kubeconfig of a network id.
		
		For example:
		
		$ kairos get-kubeconfig --network-id kairos
		`,
	Flags: networkAPI,
	Action: func(c *cli.Context) error {
		address := c.String("api")
		cc := service.NewClient(
			c.String("network-id"),
			edgeVPNClient.NewClient(edgeVPNClient.WithHost(address)))

		str, err := cc.Get("kubeconfig", "master")
		if err != nil {
			return apiError("kubeconfig", address, err)
		}

		b, err := base64.RawURLEncoding.DecodeString(str)
		if err != nil {
			return fmt.Errorf("the kubeconfig stored in the network is not valid base64: %w", err)
		}

		masterIP, err := cc.Get("master", "ip")
		if err != nil {
			return apiError("master IP", address, err)
		}

		fmt.Println(strings.ReplaceAll(string(b), "127.0.0.1", masterIP))
		return nil
	},
}
