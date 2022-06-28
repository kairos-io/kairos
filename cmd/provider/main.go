package main

import (
	"os"

	"github.com/c3os-io/c3os/internal/provider"

	"github.com/c3os-io/c3os/pkg/bus"
	"github.com/mudler/go-pluggable"
)

func main() {
	factory := pluggable.NewPluginFactory()

	// Input: bus.EventInstallPayload
	// Expected output: map[string]string{}
	factory.Add(bus.EventInstall, provider.Install)

	factory.Add(bus.EventBootstrap, provider.Bootstrap)

	// Input: config
	// Expected output: string
	factory.Add(bus.EventChallenge, provider.Challenge)

	factory.Run(pluggable.EventType(os.Args[1]), os.Args[2], os.Stdout)
}
