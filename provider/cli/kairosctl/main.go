package main

import (
	"fmt"
	"os"

	iCli "github.com/kairos-io/provider-kairos/v2/internal/cli"
	"github.com/urfave/cli/v2"
)

func checkErr(err error) {
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	os.Exit(0)
}

func main() {
	checkErr(Start())
}

func Start() error {
	toolName := "kairosctl"
	name := toolName
	app := &cli.App{
		Name:    name,
		Version: iCli.VERSION,
		Authors: []*cli.Author{
			{
				Name: iCli.Author,
			},
		},
		Copyright: iCli.Author,
		Commands: []*cli.Command{
			iCli.RegisterCMD(toolName),
			iCli.BridgeCMD(toolName),
			&iCli.GetKubeConfigCMD,
			&iCli.RoleCMD,
			&iCli.CreateConfigCMD,
			&iCli.GenerateTokenCMD,
			&iCli.ValidateSchemaCMD,
			&iCli.EditConfigCMD,
		},
	}

	return app.Run(os.Args)
}
