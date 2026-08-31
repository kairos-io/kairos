package cli

import (
	"fmt"
	"os"
	"runtime"
	"strconv"

	"github.com/kairos-io/provider-kairos/v2/internal/provider"
	providerConfig "github.com/kairos-io/provider-kairos/v2/internal/provider/config"

	"github.com/kairos-io/kairos/v4/sdk/schema"
	"github.com/mudler/edgevpn/pkg/node"
	"github.com/urfave/cli/v2"
	"gopkg.in/yaml.v3"
)

// do not edit version here, it is set by LDFLAGS
// -X 'github.com/kairos-io/provider-kairos/v2/internal/cli.VERSION=$VERSION'
// see Earthlfile.
var VERSION = "0.0.0"
var Author = "Ettore Di Giacinto"

// apiFlagName is the name of the --api flag. It is looked up by name in
// setAPIFlagDefault, so it needs to be one value rather than a literal
// repeated at each place that declares or reads the flag.
//
// The name is shared, the meaning is not. In networkAPI below, and so in every
// command that includes it, --api is the address of an API to dial. The bridge
// command declares a flag of its own under the same name which is the address
// its own API server listens on, and that one is deliberately left out of
// setAPIFlagDefault: bridge runs on an operator's machine, where there is no
// local daemon whose address it could follow.
const apiFlagName = "api"

var networkAPI = []cli.Flag{
	&cli.StringFlag{
		Name: apiFlagName,
		Usage: "Edgevpn API endpoint. Accepts a TCP URL (e.g. http://127.0.0.1:8080) " +
			"or a unix socket path with the 'unix://' prefix (e.g. unix:///run/edgevpn.sock). " +
			"Defaults to whatever the local edgevpn daemon was configured to listen on, " +
			"so it normally needs no setting.",
		// Placeholder only. applyAPIDefault replaces this at startup with the
		// address the local daemon was actually given, so the two ends cannot
		// drift apart. It stands alone when there is no daemon configured yet.
		Value:   provider.DefaultEdgeVPNAPIAddress,
		EnvVars: []string{"EDGEVPN_API"},
	},
	&cli.StringFlag{
		Name:    "network-id",
		Value:   "kairos",
		Usage:   "Kubernetes Network Deployment ID",
		EnvVars: []string{"EDGEVPN_NETWORK_ID"},
	},
}

// setAPIFlagDefault points the shared --api flag at address. That flag is a
// single instance reused by every command which talks to the API, so setting it
// here covers all of them.
func setAPIFlagDefault(address string) {
	for _, f := range networkAPI {
		if sf, ok := f.(*cli.StringFlag); ok && sf.Name == apiFlagName {
			sf.Value = address
		}
	}
}

// applyAPIDefault makes the --api default follow the address the local edgevpn
// daemon was actually configured to listen on, read out of the env file the
// provider writes for the daemon's systemd unit.
//
// Without this, the daemon's address and the client's default are two separate
// decisions that work only while they happen to coincide. They stop coinciding
// as soon as anything passes an explicit address to the agent at bootstrap, and
// the failure is silent: the client queries an address nothing is listening on
// and prints an empty answer.
//
// This moves the default only. An explicit --api, or EDGEVPN_API in the
// environment, still wins, because urfave/cli prefers both over a flag's value.
func applyAPIDefault(envFile string) {
	setAPIFlagDefault(provider.ResolveAPIAddress(envFile))
}

const recoveryAddr = "127.0.0.1:2222"

var CreateConfigCMD = cli.Command{
	Name:      "create-config",
	Aliases:   []string{"c"},
	UsageText: "Create a config with a generated network token",

	Usage: "Creates a pristine config file",
	Description: `
		Prints a vanilla YAML configuration on screen which can be used to bootstrap a kairos network.
		`,
	ArgsUsage: "Optionally takes a token rotation interval (seconds)",

	Action: func(c *cli.Context) error {
		l := int(^uint(0) >> 1)
		if c.Args().Present() {
			if i, err := strconv.Atoi(c.Args().Get(0)); err == nil {
				l = i
			}
		}
		cc := &providerConfig.Config{P2P: &providerConfig.P2P{NetworkToken: node.GenerateNewConnectionData(l).Base64()}}
		y, _ := yaml.Marshal(cc)
		fmt.Printf("#cloud-config\n\n%s", string(y))
		return nil
	},
}

var GenerateTokenCMD = cli.Command{
	Name:      "generate-token",
	Aliases:   []string{"g"},
	UsageText: "Generate a network token",
	Usage:     "Creates a new token",
	Description: `
		Generates a new token which can be used to bootstrap a kairos network.
		`,
	ArgsUsage: "Optionally takes a token rotation interval (seconds)",

	Action: func(c *cli.Context) error {
		l := int(^uint(0) >> 1)
		if c.Args().Present() {
			if i, err := strconv.Atoi(c.Args().Get(0)); err == nil {
				l = i
			}
		}
		fmt.Println(node.GenerateNewConnectionData(l).Base64())
		return nil
	},
}

var ValidateSchemaCMD = cli.Command{
	Name: "validate",
	Action: func(c *cli.Context) error {
		config := c.Args().First()
		return schema.Validate(config)
	},
	Usage: "Validates a cloud config file",
	Description: `
The validate command expects a configuration file as its only argument. Local files and URLs are accepted.
		`,
}

var VersionCMD = cli.Command{
	Name: "version",
	Action: func(_ *cli.Context) error {
		printVersion()
		return nil
	},
	Description: "Prints version information of this binary",
}

func printVersion() {
	fmt.Printf("version: %s, compiled with: %s\n", VERSION, runtime.Version())
}

func Start() error {
	toolName := "kairos"

	// Do this before the app parses anything, so --help shows the address the
	// commands will really use rather than a default they may not use.
	applyAPIDefault(provider.EdgeVPNEnvFile)

	cli.VersionPrinter = func(_ *cli.Context) {
		printVersion()
	}

	app := &cli.App{
		Name:    toolName,
		Version: VERSION,
		Authors: []*cli.Author{
			{
				Name: Author,
			},
		},
		Usage: "kairos CLI to bootstrap, upgrade, connect and manage a kairos network",
		Description: `
The kairos CLI can be used to manage a kairos box and perform all day-two tasks, like:
- register a node (WARNING: this command will be deprecated in the next release, use the kairosctl binary instead)
- connect to a node in recovery mode
- to establish a VPN connection
- set, list roles
- interact with the network API

and much more.

For all the example cases, see: https://kairos.io/docs/
`,
		UsageText: ``,
		Copyright: Author,
		Commands: []*cli.Command{
			{
				Name:      "recovery-ssh-server",
				UsageText: "recovery-ssh-server",
				Usage:     "Starts SSH recovery service",
				Description: `
				Spawn up a simple standalone ssh server over p2p
		`,
				ArgsUsage: "Spawn up a simple standalone ssh server over p2p",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "token",
						EnvVars: []string{"TOKEN"},
					},
					&cli.StringFlag{
						Name:    "service",
						EnvVars: []string{"SERVICE"},
					},
					&cli.StringFlag{
						Name:    "password",
						EnvVars: []string{"PASSWORD"},
					},
					&cli.StringFlag{
						Name:    "listen",
						EnvVars: []string{"LISTEN"},
						Value:   recoveryAddr,
					},
				},
				Action: func(c *cli.Context) error {
					return StartRecoveryService(c)
				},
			},
			RegisterCMD(toolName),
			BridgeCMD(toolName),
			&GetKubeConfigCMD,
			&RoleCMD,
			&CreateConfigCMD,
			&GenerateTokenCMD,
			&ValidateSchemaCMD,
			&VersionCMD,
		},
	}

	return app.Run(os.Args)
}
