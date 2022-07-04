package cmd

import (
	"github.com/urfave/cli"
)

var networkAPI = []cli.Flag{
	&cli.StringFlag{
		Name:  "api",
		Usage: "API Address",
		Value: "http://localhost:8080",
	},
	&cli.StringFlag{
		Name:  "network-id",
		Value: "c3os",
		Usage: "Kubernetes Network Deployment ID",
	},
}
