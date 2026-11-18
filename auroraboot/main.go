package main

import (
	"fmt"
	"os"

	cmd "github.com/kairos-io/AuroraBoot/internal/cmd"
)

var (
	version = "v0.0.0"
)

func main() {
	app := cmd.GetApp(version)

	err := app.Run(os.Args)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
